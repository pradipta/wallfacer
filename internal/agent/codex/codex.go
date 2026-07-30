// Package codex implements the wallfacer agent adapter for Codex, whose
// sessions live at ~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<ts>-<uuid>.jsonl.
//
// Sessions are one JSONL file each, nested under date directories, so listing
// walks the tree recursively. The session's real working directory is read
// from the "cwd" field of the session_meta record inside the file — never
// derived from the path. The UUID in the filename is the session id that
// `codex resume` expects.
//
// Codex writes a single flat transcript per session: there are no sub-agent or
// sidechain transcripts to hide, so Metadata.Sidechain is always false.
package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pradipta/wallfacer/internal/agent"
)

const (
	// maxScanLines bounds how far into a rollout we look for metadata; cwd
	// (session_meta) and the first prompt appear within the first few records.
	maxScanLines = 100
	// maxLineBytes must fit the largest rollout lines (tool output carrying
	// whole files) without erroring the scan.
	maxLineBytes = 16 * 1024 * 1024
	// maxPromptLen caps the stored first prompt.
	maxPromptLen = 500
)

// idPattern matches a rollout filename's trailing session UUID. The launch
// timestamp in the name also contains hyphens, so the UUID can't be recovered
// by splitting on "-"; anchoring on the UUID shape is unambiguous.
var idPattern = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\.jsonl$`)

type Adapter struct {
	// SessionsDir defaults to ~/.codex/sessions (honouring $CODEX_HOME);
	// overridable for tests.
	SessionsDir string
	// Binary defaults to "codex".
	Binary string
}

func New() *Adapter {
	return &Adapter{
		SessionsDir: filepath.Join(codexHome(), "sessions"),
		Binary:      "codex",
	}
}

// codexHome resolves Codex's home directory, which $CODEX_HOME overrides.
func codexHome() string {
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func (a *Adapter) Type() string { return "codex" }

// ListSessionFiles walks the date-nested sessions tree and returns one entry
// per rollout file. It is stat-only: no file is opened. Files whose names don't
// carry a session UUID (and anything unreadable) are skipped rather than
// failing the walk.
func (a *Adapter) ListSessionFiles() ([]agent.SessionFile, error) {
	if _, err := os.Stat(a.SessionsDir); os.IsNotExist(err) {
		return nil, nil
	}
	var files []agent.SessionFile
	err := filepath.WalkDir(a.SessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable dirs/entries
		}
		if d.IsDir() {
			return nil
		}
		m := idPattern.FindStringSubmatch(d.Name())
		if m == nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, agent.SessionFile{
			ID:    m[1],
			Path:  path,
			Size:  info.Size(),
			Mtime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// rolloutLine is the envelope every record shares. The payload shape depends on
// Type, so it stays raw until the type is known.
type rolloutLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// sessionMetaPayload is the subset of the first-line session header wallfacer
// needs. The header also carries a large context/state blob, not modelled.
type sessionMetaPayload struct {
	Cwd       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	Git       struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

// eventMsgPayload carries the UI event stream; the "user_message" event holds
// the plain human prompt (as opposed to the injected <environment_context>
// that leads the model transcript).
type eventMsgPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// responseItemPayload is a model-transcript record; its user "message" items
// are the fallback source for the first prompt.
type responseItemPayload struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// ParseMetadata reads the head of a rollout for the session's own record of
// where and when it ran and its first prompt. Malformed lines are skipped, not
// errored: a session with only a Dir and CreatedAt still indexes fine.
func (a *Adapter) ParseMetadata(path string) (*agent.Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	md := &agent.Metadata{}
	var fallbackPrompt string

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for n := 0; sc.Scan() && n < maxScanLines; n++ {
		var ln rolloutLine
		if err := json.Unmarshal(sc.Bytes(), &ln); err != nil {
			continue
		}
		switch ln.Type {
		case "session_meta":
			var p sessionMetaPayload
			if err := json.Unmarshal(ln.Payload, &p); err == nil {
				if md.Dir == "" {
					md.Dir = p.Cwd
				}
				if md.GitBranch == "" {
					md.GitBranch = p.Git.Branch
				}
				if md.CreatedAt.IsZero() {
					if ts, err := time.Parse(time.RFC3339Nano, p.Timestamp); err == nil {
						md.CreatedAt = ts
					}
				}
			}
		case "event_msg":
			if md.FirstPrompt != "" {
				continue
			}
			var p eventMsgPayload
			if err := json.Unmarshal(ln.Payload, &p); err == nil && p.Type == "user_message" {
				if s := cleanPrompt(p.Message); s != "" {
					md.FirstPrompt = truncate(s, maxPromptLen)
				}
			}
		case "response_item":
			if fallbackPrompt != "" {
				continue
			}
			var p responseItemPayload
			if err := json.Unmarshal(ln.Payload, &p); err == nil && p.Type == "message" && p.Role == "user" {
				for _, c := range p.Content {
					if c.Type != "input_text" {
						continue
					}
					if s := cleanPrompt(c.Text); s != "" {
						fallbackPrompt = truncate(s, maxPromptLen)
						break
					}
				}
			}
		}
		// Fall back to the record's own clock when session_meta lacked one.
		if md.CreatedAt.IsZero() && ln.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, ln.Timestamp); err == nil {
				md.CreatedAt = ts
			}
		}
		if md.Dir != "" && md.FirstPrompt != "" && !md.CreatedAt.IsZero() {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", path, err)
	}
	if md.FirstPrompt == "" {
		md.FirstPrompt = fallbackPrompt
	}
	return md, nil
}

// cleanPrompt rejects the harness-injected wrappers Codex leads a transcript
// with (environment context, user-instructions envelope, aborted-turn markers,
// permission preambles) so titles reflect what the human actually typed.
func cleanPrompt(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{
		"<environment_context>",
		"<user_instructions>",
		"<turn_aborted>",
		"<permissions instructions>",
	} {
		if strings.HasPrefix(s, prefix) {
			return ""
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SupportsSessionID reports false: `codex` has no flag to pre-assign a session
// ID, so the launcher detects the newborn session after the fact.
func (a *Adapter) SupportsSessionID() bool { return false }

func (a *Adapter) NewSessionCmd(dir, _ string) *exec.Cmd {
	cmd := exec.Command(a.Binary)
	cmd.Dir = dir
	return cmd
}

func (a *Adapter) ResumeCmd(dir, id string) *exec.Cmd {
	cmd := exec.Command(a.Binary, "resume", id)
	cmd.Dir = dir
	return cmd
}
