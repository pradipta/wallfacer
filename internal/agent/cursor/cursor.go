// Package cursor implements the wallfacer agent adapter for Cursor's CLI
// agent (cursor-agent), whose chats are spread over two trees under
// ~/.cursor:
//
//	chats/<workspace-hash>/<chat-uuid>/
//	    meta.json            the tracked file: cwd, title, created/updated ms
//	    store.db             SQLite message store — only ever stat'ed
//	    prompt_history.json  workspace-wide prompt history (see below)
//	    pasted_text.json     pasted buffers
//	projects/<slug>/agent-transcripts/<chat-uuid>/<chat-uuid>.jsonl
//	    the conversation as JSONL, one record per turn
//
// meta.json is what wallfacer tracks. It is the chat's own record of where
// and when it ran, and its mtime follows the session's updatedAtMs, so it
// doubles as the "last active" signal. store.db never has to be opened: its
// mere existence separates real chats from the placeholders cursor-agent
// leaves behind for chats that were created and abandoned.
package cursor

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pradipta/wallfacer/internal/agent"
)

const (
	// metaName is the per-chat metadata file, and the file wallfacer tracks.
	metaName = "meta.json"
	// storeName is the SQLite message store. Its presence is the only thing
	// this adapter uses it for — a chat without one holds no conversation.
	storeName = "store.db"
	// transcriptsDir holds one directory per chat inside a project.
	transcriptsDir = "agent-transcripts"
	// transcriptExt completes <chat-uuid>/<chat-uuid>.jsonl.
	transcriptExt = ".jsonl"

	// maxScanLines bounds how far into a transcript we look for the opening
	// human turn; it is normally the very first record.
	maxScanLines = 50
	// maxLineBytes must fit the largest transcript lines (tool results
	// carrying whole files) without erroring the scan.
	maxLineBytes = 16 * 1024 * 1024
	// maxPromptLen caps the stored first prompt.
	maxPromptLen = 500
)

type Adapter struct {
	// ChatsDir defaults to ~/.cursor/chats; overridable for tests.
	ChatsDir string
	// ProjectsDir defaults to ~/.cursor/projects, where the JSONL
	// transcripts live; overridable for tests.
	ProjectsDir string
	// Binary defaults to "cursor-agent".
	Binary string

	// mu guards workspaceDirs, the cache behind the cwd fallback.
	mu            sync.Mutex
	workspaceDirs map[string]string // workspace-hash dir → working directory
}

func New() *Adapter {
	home, _ := os.UserHomeDir()
	return &Adapter{
		ChatsDir:    filepath.Join(home, ".cursor", "chats"),
		ProjectsDir: filepath.Join(home, ".cursor", "projects"),
		Binary:      "cursor-agent",
	}
}

func (a *Adapter) Type() string { return "cursor-agent" }

// ListSessionFiles enumerates chats two levels down, one per
// <workspace-hash>/<chat-uuid> directory holding both a meta.json and a
// store.db. Chats missing the store are placeholders cursor-agent wrote when
// a chat was created and abandoned: they have no transcript, often no working
// directory, and cannot be resumed, so they are skipped outright rather than
// indexed and hidden. Stat only — no file is opened here.
func (a *Adapter) ListSessionFiles() ([]agent.SessionFile, error) {
	workspaces, err := os.ReadDir(a.ChatsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []agent.SessionFile
	for _, ws := range workspaces {
		if !ws.IsDir() {
			continue
		}
		wsDir := filepath.Join(a.ChatsDir, ws.Name())
		chats, err := os.ReadDir(wsDir)
		if err != nil {
			continue
		}
		for _, c := range chats {
			if !c.IsDir() {
				continue
			}
			chatDir := filepath.Join(wsDir, c.Name())
			if _, err := os.Stat(filepath.Join(chatDir, storeName)); err != nil {
				continue
			}
			meta := filepath.Join(chatDir, metaName)
			info, err := os.Stat(meta)
			if err != nil {
				continue
			}
			files = append(files, agent.SessionFile{
				ID:    c.Name(),
				Path:  meta,
				Size:  info.Size(),
				Mtime: info.ModTime(),
			})
		}
	}
	return files, nil
}

// chatMeta is the subset of meta.json wallfacer needs. `title` is written by
// the agent and reads like a summary ("Login Switch"), so it lands in
// Metadata.Summary rather than being mistaken for a user-supplied name.
// `cwd` was added in a later schema revision and is absent from older chats.
type chatMeta struct {
	CreatedAtMs int64  `json:"createdAtMs"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	// HasConversation is not consulted: ListSessionFiles already filters
	// on store.db, which tracks it exactly and costs one stat.
	HasConversation bool `json:"hasConversation"`
}

// ParseMetadata reads the chat's meta.json for where and when it ran, then
// the head of its transcript for the opening prompt. A missing or malformed
// file is not an error — an unreadable chat still indexes, it just gets a
// bland title.
func (a *Adapter) ParseMetadata(path string) (*agent.Metadata, error) {
	md := &agent.Metadata{}
	if m, err := readMeta(path); err == nil {
		md.Dir = m.Cwd
		md.Summary = strings.TrimSpace(m.Title)
		if m.CreatedAtMs > 0 {
			md.CreatedAt = time.UnixMilli(m.CreatedAtMs)
		}
	}
	md.FirstPrompt = a.firstPrompt(chatID(path))
	// Chats written before cwd was added to the schema have to borrow it
	// from a sibling; see workspaceCwd.
	if md.Dir == "" {
		md.Dir = a.workspaceCwd(filepath.Dir(filepath.Dir(path)))
	}
	return md, nil
}

// workspaceCwd recovers a working directory for chats whose meta.json predates
// the cwd field. Every chat under one <workspace-hash> directory belongs to
// the same workspace, so any sibling that does record a cwd answers for all of
// them — including abandoned placeholder chats, which often carry one.
// Successful lookups are cached because the first sync of a long-lived install
// asks this once per old chat.
func (a *Adapter) workspaceCwd(wsDir string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cwd, ok := a.workspaceDirs[wsDir]; ok {
		return cwd
	}
	cwd := scanWorkspaceCwd(wsDir)
	if cwd == "" {
		// Nothing to remember: a workspace where no chat ever recorded a
		// directory stays unresolved, and rescanning it is cheap.
		return ""
	}
	if a.workspaceDirs == nil {
		a.workspaceDirs = map[string]string{}
	}
	a.workspaceDirs[wsDir] = cwd
	return cwd
}

// scanWorkspaceCwd returns the first working directory recorded by any chat in
// a workspace directory.
func scanWorkspaceCwd(wsDir string) string {
	chats, err := os.ReadDir(wsDir)
	if err != nil {
		return ""
	}
	for _, c := range chats {
		if !c.IsDir() {
			continue
		}
		m, err := readMeta(filepath.Join(wsDir, c.Name(), metaName))
		if err == nil && m.Cwd != "" {
			return m.Cwd
		}
	}
	return ""
}

// chatID recovers the chat's UUID from the path of its meta.json.
func chatID(metaPath string) string {
	return filepath.Base(filepath.Dir(metaPath))
}

func readMeta(path string) (*chatMeta, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m chatMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// transcriptRecord is one line of a chat transcript. Assistant turns share
// the shape but carry tool calls this adapter ignores.
type transcriptRecord struct {
	Role    string `json:"role"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// firstPrompt returns the opening human turn of a chat, read from the head of
// its JSONL transcript in the projects tree.
//
// The chat's own prompt_history.json looks like the obvious source and is
// not: it is the *workspace's* history, so it carries prompts typed in
// sibling chats as well as interactive commands like /clear, in an order that
// says nothing about which chat asked what. The transcript is append-only and
// per chat, so its first user record is the real opening prompt.
func (a *Adapter) firstPrompt(id string) string {
	dir := a.transcriptDir(id)
	if dir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, id+transcriptExt))
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	for n := 0; sc.Scan() && n < maxScanLines; n++ {
		var rec transcriptRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if rec.Role != "user" {
			continue
		}
		var text strings.Builder
		for _, c := range rec.Message.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
		}
		if p := cleanPrompt(text.String()); p != "" {
			return truncate(p, maxPromptLen)
		}
	}
	return ""
}

// transcriptDir locates a chat's transcript directory,
// projects/<slug>/agent-transcripts/<chat-uuid>. The slug encodes the working
// directory, but lossily — "Users-alice-projects-alice-github-io" could be
// /Users/alice/projects/alice.github.io — so it is only ever used to find
// files, never decoded into a path. Returns "" when the chat has no
// transcript, which happens for chats older than the transcripts feature.
func (a *Adapter) transcriptDir(id string) string {
	slugs, err := os.ReadDir(a.ProjectsDir)
	if err != nil {
		return ""
	}
	for _, s := range slugs {
		if !s.IsDir() {
			continue
		}
		dir := filepath.Join(a.ProjectsDir, s.Name(), transcriptsDir, id)
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
	}
	return ""
}

// cleanPrompt unwraps the envelope cursor-agent writes around a human turn:
// newer transcripts prefix a <timestamp> element, and the text itself sits
// inside <user_query> tags.
func cleanPrompt(s string) string {
	if i := strings.Index(s, "<timestamp>"); i >= 0 {
		if j := strings.Index(s, "</timestamp>"); j > i {
			s = s[:i] + s[j+len("</timestamp>"):]
		}
	}
	if i := strings.Index(s, "<user_query>"); i >= 0 {
		s = s[i+len("<user_query>"):]
		if j := strings.Index(s, "</user_query>"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// SupportsSessionID reports false: cursor-agent mints chat IDs itself
// (`cursor-agent create-chat` prints one) and has no flag for adopting an ID
// chosen by the caller, so the launcher identifies the newborn chat after the
// fact from its directory and creation time.
func (a *Adapter) SupportsSessionID() bool { return false }

func (a *Adapter) NewSessionCmd(dir, _ string) *exec.Cmd {
	cmd := exec.Command(a.Binary)
	cmd.Dir = dir
	return cmd
}

func (a *Adapter) ResumeCmd(dir, id string) *exec.Cmd {
	cmd := exec.Command(a.Binary, "--resume", id)
	cmd.Dir = dir
	return cmd
}

// CompanionFiles returns the rest of a chat's footprint: its own directory —
// which holds store.db, prompt_history.json and pasted_text.json, so moving it
// sweeps all three at once — and its transcript directory over in the projects
// tree. Both have to go, or `cursor-agent ls` keeps offering a chat whose
// metadata wallfacer has taken away.
//
// The chat directory is only returned for a file still living under ChatsDir.
// Trash and Purge call this with the session's *current* path, which for an
// already-trashed session is wallfacer's trash directory — returning that as a
// companion would delete every other trashed session with it. The cost of the
// guard is that purging a session that was trashed first leaves the moved
// directories behind in the trash; purging directly removes everything.
//
// Plan files (~/.cursor/plans/<title>-<id-prefix>.plan.md) are deliberately
// left alone: they are user-facing documents, and they don't keep a deleted
// chat in the picker.
func (a *Adapter) CompanionFiles(path string) []string {
	var companions []string
	if chatDir := filepath.Dir(path); a.withinChats(chatDir) {
		companions = append(companions, chatDir)
	}
	if dir := a.transcriptDir(chatID(path)); dir != "" {
		companions = append(companions, dir)
	}
	return companions
}

// withinChats reports whether dir is a chat directory inside ChatsDir.
func (a *Adapter) withinChats(dir string) bool {
	root := filepath.Clean(a.ChatsDir) + string(os.PathSeparator)
	return strings.HasPrefix(filepath.Clean(dir)+string(os.PathSeparator), root)
}
