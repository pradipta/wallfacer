// Package claudecode implements the wallfacer agent adapter for Claude Code,
// whose sessions live at ~/.claude/projects/<encoded-dir>/<uuid>.jsonl.
//
// The encoded directory name is lossy (dashes vs path separators are
// ambiguous), so the session's real working directory is always read from the
// "cwd" field inside the JSONL, never decoded from the folder name.
package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pradipta-s/wallfacer/internal/agent"
)

const (
	// maxScanLines bounds how far into a file we look for metadata; the
	// fields we need (summary, cwd, first prompt) appear near the top.
	maxScanLines = 250
	// maxLineBytes must fit the largest JSONL lines (file snapshots,
	// pasted content) without erroring the scan.
	maxLineBytes = 16 * 1024 * 1024
	// maxPromptLen caps the stored first prompt.
	maxPromptLen = 500
)

type Adapter struct {
	// ProjectsDir defaults to ~/.claude/projects; overridable for tests.
	ProjectsDir string
	// Binary defaults to "claude".
	Binary string
}

func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		ProjectsDir: filepath.Join(home, ".claude", "projects"),
		Binary:      "claude",
	}
}

func (a *Adapter) Type() string { return "claude-code" }

func (a *Adapter) ListSessionFiles() ([]agent.SessionFile, error) {
	projectDirs, err := os.ReadDir(a.ProjectsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []agent.SessionFile
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(a.ProjectsDir, pd.Name()))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, agent.SessionFile{
				ID:    strings.TrimSuffix(e.Name(), ".jsonl"),
				Path:  filepath.Join(a.ProjectsDir, pd.Name(), e.Name()),
				Size:  info.Size(),
				Mtime: info.ModTime(),
			})
		}
	}
	return files, nil
}

// jsonlLine is a loose superset of the fields we care about across the
// various record types Claude Code writes.
type jsonlLine struct {
	Type        string          `json:"type"`
	Summary     string          `json:"summary"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

type messageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func (a *Adapter) ParseMetadata(path string) (*agent.Metadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	md := &agent.Metadata{}
	sawUserLine := false

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for n := 0; sc.Scan() && n < maxScanLines; n++ {
		var ln jsonlLine
		if err := json.Unmarshal(sc.Bytes(), &ln); err != nil {
			continue
		}
		if ln.Summary != "" && md.Summary == "" {
			md.Summary = ln.Summary
		}
		if ln.Cwd != "" && md.Dir == "" {
			md.Dir = ln.Cwd
		}
		if ln.GitBranch != "" && md.GitBranch == "" {
			md.GitBranch = ln.GitBranch
		}
		if md.CreatedAt.IsZero() && ln.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, ln.Timestamp); err == nil {
				md.CreatedAt = ts
			}
		}
		if ln.Type == "user" && md.FirstPrompt == "" {
			if !sawUserLine {
				sawUserLine = true
				// The whole file is a subagent transcript when its
				// first user message is a sidechain entry.
				md.Sidechain = ln.IsSidechain
			}
			if p := extractPrompt(ln.Message); p != "" {
				md.FirstPrompt = truncate(p, maxPromptLen)
			}
		}
		if md.Dir != "" && md.FirstPrompt != "" && !md.CreatedAt.IsZero() {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", path, err)
	}
	return md, nil
}

// extractPrompt pulls displayable text out of a user message, whose content
// is either a plain string or an array of content blocks.
func extractPrompt(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg messageBody
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Role != "user" {
		return ""
	}
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return cleanPrompt(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && cleanPrompt(b.Text) != "" {
				return cleanPrompt(b.Text)
			}
		}
	}
	return ""
}

// cleanPrompt rejects harness-injected pseudo-prompts (slash-command
// envelopes, caveats, system reminders) so titles reflect what the human
// actually typed.
func cleanPrompt(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"<command-name>", "<local-command", "Caveat:", "<system-reminder>", "<command-message>"} {
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

func (a *Adapter) SupportsSessionID() bool { return true }

func (a *Adapter) NewSessionCmd(dir, id string) *exec.Cmd {
	args := []string{}
	if id != "" {
		args = append(args, "--session-id", id)
	}
	cmd := exec.Command(a.Binary, args...)
	cmd.Dir = dir
	return cmd
}

func (a *Adapter) ResumeCmd(dir, id string) *exec.Cmd {
	cmd := exec.Command(a.Binary, "--resume", id)
	cmd.Dir = dir
	return cmd
}
