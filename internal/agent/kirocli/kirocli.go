// Package kirocli implements the wallfacer agent adapter for Kiro CLI, whose
// sessions live in a flat directory at ~/.kiro/sessions/cli.
//
// Each session is spread over several files sharing one UUID stem:
//
//	<uuid>.jsonl    the transcript (one JSON record per line)
//	<uuid>.json     a sidecar with cwd, timestamps, title, parent session
//	<uuid>.history  the prompt history of the interactive shell
//	<uuid>.lock     present while a session is live
//	<uuid>/         per-session scratch state (task lists)
//
// The transcript is the file wallfacer tracks: its mtime is the only honest
// "last active" signal. Metadata comes from the sidecar, which records the
// working directory explicitly — the directory is never derived from a path.
package kirocli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pradipta/wallfacer/internal/agent"
)

const (
	// maxScanLines bounds how far into a transcript we look for the first
	// human prompt; it is normally the very first record.
	maxScanLines = 100
	// maxLineBytes must fit the largest transcript lines (tool results
	// carrying whole files) without erroring the scan.
	maxLineBytes = 16 * 1024 * 1024
	// maxPromptLen caps the stored first prompt.
	maxPromptLen = 500

	transcriptExt = ".jsonl"
	sidecarExt    = ".json"
)

type Adapter struct {
	// SessionsDir defaults to ~/.kiro/sessions/cli (honouring $KIRO_HOME);
	// overridable for tests.
	SessionsDir string
	// Binary defaults to "kiro-cli".
	Binary string
}

func New() *Adapter {
	return &Adapter{
		SessionsDir: filepath.Join(kiroHome(), "sessions", "cli"),
		Binary:      "kiro-cli",
	}
}

// kiroHome resolves Kiro's home directory, which $KIRO_HOME overrides.
func kiroHome() string {
	if env := os.Getenv("KIRO_HOME"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kiro")
}

func (a *Adapter) Type() string { return "kiro-cli" }

// ListSessionFiles enumerates transcripts. Sessions that never got past the
// prompt have only a .history file and are correctly invisible here.
func (a *Adapter) ListSessionFiles() ([]agent.SessionFile, error) {
	entries, err := os.ReadDir(a.SessionsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []agent.SessionFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), transcriptExt) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, agent.SessionFile{
			ID:    strings.TrimSuffix(e.Name(), transcriptExt),
			Path:  filepath.Join(a.SessionsDir, e.Name()),
			Size:  info.Size(),
			Mtime: info.ModTime(),
		})
	}
	return files, nil
}

// sidecar is the subset of <uuid>.json wallfacer needs. The file also carries
// a large session_state blob, which is deliberately not modelled.
type sidecar struct {
	Cwd       string `json:"cwd"`
	CreatedAt string `json:"created_at"`
	Title     string `json:"title"`
	// ParentSessionID is set on subagent transcripts. session_created_reason
	// is *not* usable for this: Kiro CLI writes "subagent" on ordinary
	// top-level sessions too.
	ParentSessionID string `json:"parent_session_id"`
}

// transcriptLine is a loose view of one record. Prompt records hold the human
// turns; AssistantMessage and ToolResults records are ignored.
type transcriptLine struct {
	Kind string `json:"kind"`
	Data struct {
		Content []contentBlock `json:"content"`
		Meta    struct {
			Timestamp int64 `json:"timestamp"`
		} `json:"meta"`
	} `json:"data"`
}

// contentBlock's data is a bare string for text blocks and an object for
// tool-use blocks, so it stays raw until the kind is known.
type contentBlock struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// ParseMetadata reads the sidecar for the session's own record of where and
// when it ran, then the head of the transcript for the first human prompt.
// A missing or malformed sidecar is not an error: an indexed session with a
// prompt but no directory is still more useful than none.
func (a *Adapter) ParseMetadata(path string) (*agent.Metadata, error) {
	md := &agent.Metadata{}
	if sc, err := readSidecar(sidecarPath(path)); err == nil {
		md.Dir = sc.Cwd
		// Kiro's title is its own summary of the session, so it takes the
		// same precedence Claude Code's summary does.
		md.Summary = strings.TrimSpace(sc.Title)
		md.Sidechain = sc.ParentSessionID != ""
		if ts, err := time.Parse(time.RFC3339Nano, sc.CreatedAt); err == nil {
			md.CreatedAt = ts
		}
	}

	prompt, promptedAt, err := firstPrompt(path)
	if err != nil {
		return nil, err
	}
	md.FirstPrompt = prompt
	// Fall back to the transcript's own clock when the sidecar is gone.
	if md.CreatedAt.IsZero() && promptedAt > 0 {
		md.CreatedAt = time.Unix(promptedAt, 0)
	}
	return md, nil
}

// sidecarPath maps <uuid>.jsonl to its <uuid>.json sibling.
func sidecarPath(transcript string) string {
	return strings.TrimSuffix(transcript, transcriptExt) + sidecarExt
}

func readSidecar(path string) (*sidecar, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sc sidecar
	if err := json.Unmarshal(b, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

// firstPrompt returns the first human prompt in the transcript, truncated,
// along with its unix timestamp.
func firstPrompt(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for n := 0; sc.Scan() && n < maxScanLines; n++ {
		var ln transcriptLine
		if err := json.Unmarshal(sc.Bytes(), &ln); err != nil {
			continue
		}
		if ln.Kind != "Prompt" {
			continue
		}
		if p := extractText(ln.Data.Content); p != "" {
			return truncate(p, maxPromptLen), ln.Data.Meta.Timestamp, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", 0, fmt.Errorf("scanning %s: %w", path, err)
	}
	return "", 0, nil
}

// extractText returns the first non-empty text block of a prompt, skipping
// image and tool blocks.
func extractText(blocks []contentBlock) string {
	for _, b := range blocks {
		if b.Kind != "text" {
			continue
		}
		var s string
		if err := json.Unmarshal(b.Data, &s); err != nil {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SupportsSessionID reports false: `kiro-cli chat` has no flag for
// pre-assigning a session ID, so the launcher detects the newborn session
// after the fact.
func (a *Adapter) SupportsSessionID() bool { return false }

func (a *Adapter) NewSessionCmd(dir, _ string) *exec.Cmd {
	cmd := exec.Command(a.Binary, "chat")
	cmd.Dir = dir
	return cmd
}

func (a *Adapter) ResumeCmd(dir, id string) *exec.Cmd {
	cmd := exec.Command(a.Binary, "chat", "--resume-id", id)
	cmd.Dir = dir
	return cmd
}

// CompanionFiles returns the rest of the session's file set: the metadata
// sidecar, the prompt history, the liveness lock, and the scratch directory.
// Deleting a Kiro session means taking all of them, or `kiro-cli chat
// --resume-picker` keeps offering a session whose transcript is gone.
func (a *Adapter) CompanionFiles(path string) []string {
	stem := strings.TrimSuffix(path, transcriptExt)
	return []string{
		stem + sidecarExt,
		stem + ".history",
		stem + ".lock",
		stem, // <uuid>/ scratch directory
	}
}
