package cursor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pradipta/wallfacer/internal/agent"
)

var (
	_ agent.Adapter        = (*Adapter)(nil)
	_ agent.CompanionFiler = (*Adapter)(nil)
)

// Fixture metadata, copied in shape from real ~/.cursor/chats records: one
// line, and with `title`/`cwd` present only on some of them.
const (
	metaFull = `{"schemaVersion":1,"createdAtMs":1783144343476,"hasConversation":true,"title":"Login Switch","updatedAtMs":1783144552812,"cwd":"/Users/alice/projects/web"}`
	// metaNoCwd is the older schema: a real chat that never recorded a
	// working directory.
	metaNoCwd = `{"schemaVersion":1,"createdAtMs":1782570516884,"hasConversation":true,"title":"Rebase Helper","updatedAtMs":1782570653052}`
	// metaPlaceholder is a chat that was created and abandoned. Real ones
	// have no store.db, which is how they are recognised.
	metaPlaceholder = `{"schemaVersion":1,"createdAtMs":1784653554154,"hasConversation":false,"updatedAtMs":1784653554181,"cwd":"/Users/alice/projects/web"}`
)

func testAdapter(t *testing.T) *Adapter {
	t.Helper()
	root := t.TempDir()
	return &Adapter{
		ChatsDir:    filepath.Join(root, "chats"),
		ProjectsDir: filepath.Join(root, "projects"),
		Binary:      "cursor-agent",
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chat creates chats/<hash>/<id>/ from a name→content map and returns the
// path of its meta.json, whether or not one was written.
func chat(t *testing.T, a *Adapter, hash, id string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(a.ChatsDir, hash, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	return filepath.Join(dir, metaName)
}

// transcript creates projects/<slug>/agent-transcripts/<id>/<id>.jsonl.
func transcript(t *testing.T, a *Adapter, slug, id, content string) string {
	t.Helper()
	path := filepath.Join(a.ProjectsDir, slug, transcriptsDir, id, id+".jsonl")
	writeFile(t, path, content)
	return path
}

func TestListSessionFiles(t *testing.T) {
	a := testAdapter(t)
	chat(t, a, "ws1", "aaa", map[string]string{
		metaName:              metaFull,
		storeName:             "SQLite format 3\x00...",
		"prompt_history.json": `["something"]`,
	})
	chat(t, a, "ws1", "bbb", map[string]string{metaName: metaNoCwd, storeName: "db"})
	// A placeholder chat: metadata but no message store.
	chat(t, a, "ws1", "placeholder", map[string]string{metaName: metaPlaceholder})
	// A store with no metadata: nothing to index it by.
	chat(t, a, "ws2", "orphan", map[string]string{storeName: "db"})
	// Stray files at both levels must not be mistaken for chats.
	writeFile(t, filepath.Join(a.ChatsDir, "loose.json"), "{}")
	writeFile(t, filepath.Join(a.ChatsDir, "ws1", "stray.json"), "{}")

	files, err := a.ListSessionFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(files), files)
	}
	ids := map[string]bool{}
	for _, f := range files {
		ids[f.ID] = true
		if filepath.Base(f.Path) != metaName {
			t.Errorf("chat %s: tracked path = %q, want a meta.json", f.ID, f.Path)
		}
		if f.Size == 0 || f.Mtime.IsZero() {
			t.Errorf("chat %s: missing stat info", f.ID)
		}
	}
	if !ids["aaa"] || !ids["bbb"] {
		t.Errorf("unexpected ids: %v", ids)
	}
	if ids["placeholder"] {
		t.Error("a chat without store.db holds no conversation and must be skipped")
	}
	if ids["orphan"] {
		t.Error("a chat without meta.json has nothing to index and must be skipped")
	}
}

func TestListSessionFilesMissingRoot(t *testing.T) {
	a := testAdapter(t) // ChatsDir is never created
	files, err := a.ListSessionFiles()
	if err != nil || files != nil {
		t.Errorf("missing root should be (nil, nil), got (%v, %v)", files, err)
	}
}

func TestParseMetadata(t *testing.T) {
	a := testAdapter(t)
	path := chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/web" {
		t.Errorf("Dir = %q", md.Dir)
	}
	// The agent writes `title` itself, so it counts as a summary and wins
	// over the first prompt when wallfacer picks an automatic title.
	if md.Summary != "Login Switch" {
		t.Errorf("Summary = %q, want the meta.json title", md.Summary)
	}
	if want := time.UnixMilli(1783144343476); !md.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", md.CreatedAt, want)
	}
	if md.GitBranch != "" {
		t.Errorf("GitBranch = %q, Cursor records none", md.GitBranch)
	}
	if md.Sidechain {
		t.Error("Cursor keeps no local subagent transcripts; Sidechain must stay false")
	}
}

func TestParseMetadataMalformed(t *testing.T) {
	a := testAdapter(t)
	path := chat(t, a, "ws1", "aaa", map[string]string{metaName: `{"createdAtMs":17831`, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatalf("malformed metadata must degrade gracefully, got %v", err)
	}
	if md.Dir != "" || md.Summary != "" || !md.CreatedAt.IsZero() {
		t.Errorf("expected empty metadata, got %+v", md)
	}
}

// The current transcript format wraps the human turn in a <timestamp> element
// followed by <user_query> tags.
const newFormatTranscript = `{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>Saturday, Jul 4, 2026, 11:22 AM (UTC+5:30)</timestamp>\n<user_query>\nHow cleanly can we disable Login with Microsoft and Facebook for now?\n</user_query>"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"I'll scan the codebase."},{"type":"tool_use","name":"Grep","input":{"pattern":"Microsoft"}}]}}
`

// Older transcripts carry no timestamp element.
const oldFormatTranscript = `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nWhen a booking completes, the calendar stops showing it.\n</user_query>"}]}}
`

// Some transcripts open with the assistant, so the scan must keep going.
const assistantFirstTranscript = `{"role":"assistant","message":{"content":[{"type":"text","text":"Continuing from the plan."}]}}
{"role":"user","message":{"content":[{"type":"text","text":"<user_query>\nrename the serial column\n</user_query>"}]}}
`

func TestParseMetadataFirstPrompt(t *testing.T) {
	cases := []struct {
		name       string
		transcript string
		want       string
	}{
		{"timestamp wrapper", newFormatTranscript,
			"How cleanly can we disable Login with Microsoft and Facebook for now?"},
		{"no timestamp wrapper", oldFormatTranscript,
			"When a booking completes, the calendar stops showing it."},
		{"assistant first", assistantFirstTranscript, "rename the serial column"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := testAdapter(t)
			path := chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})
			transcript(t, a, "Users-alice-projects-web", "aaa", tc.transcript)

			md, err := a.ParseMetadata(path)
			if err != nil {
				t.Fatal(err)
			}
			if md.FirstPrompt != tc.want {
				t.Errorf("FirstPrompt = %q, want %q", md.FirstPrompt, tc.want)
			}
		})
	}
}

func TestParseMetadataWithoutTranscript(t *testing.T) {
	a := testAdapter(t)
	// A transcript belonging to another chat must not be picked up.
	transcript(t, a, "Users-alice-projects-web", "bbb", oldFormatTranscript)
	path := chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatalf("a missing transcript must degrade gracefully, got %v", err)
	}
	if md.FirstPrompt != "" {
		t.Errorf("FirstPrompt = %q, want empty", md.FirstPrompt)
	}
	// The rest of the metadata still comes through.
	if md.Summary != "Login Switch" || md.Dir == "" {
		t.Errorf("metadata lost: %+v", md)
	}
}

// prompt_history.json sits next to meta.json and looks like the obvious
// source for the first prompt, but it is workspace-wide: this fixture mirrors
// a real one, whose newest entry belongs to a different chat.
func TestPromptHistoryIsIgnored(t *testing.T) {
	a := testAdapter(t)
	path := chat(t, a, "ws1", "aaa", map[string]string{
		metaName:  metaFull,
		storeName: "db",
		"prompt_history.json": `["a prompt from a sibling chat","/clear",` +
			`"When a booking completes, the calendar stops showing it."]`,
	})
	transcript(t, a, "Users-alice-projects-web", "aaa", newFormatTranscript)

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.FirstPrompt != "How cleanly can we disable Login with Microsoft and Facebook for now?" {
		t.Errorf("FirstPrompt = %q, want the transcript's opening turn", md.FirstPrompt)
	}
}

// A chat whose meta.json predates the cwd field borrows the directory from a
// sibling chat in the same workspace-hash directory.
func TestParseMetadataBorrowsCwdFromSibling(t *testing.T) {
	a := testAdapter(t)
	// The sibling here is an abandoned placeholder, which is still valid
	// evidence of where the workspace lives.
	chat(t, a, "ws1", "newer", map[string]string{metaName: metaPlaceholder})
	path := chat(t, a, "ws1", "older", map[string]string{metaName: metaNoCwd, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/web" {
		t.Errorf("Dir = %q, want the sibling's cwd", md.Dir)
	}
	if md.Summary != "Rebase Helper" {
		t.Errorf("Summary = %q, want the chat's own title", md.Summary)
	}
	// The lookup is cached per workspace directory.
	if got := a.workspaceDirs[filepath.Join(a.ChatsDir, "ws1")]; got != "/Users/alice/projects/web" {
		t.Errorf("cached cwd = %q", got)
	}
}

// A workspace where no chat ever recorded a cwd stays unresolved rather than
// guessing from the workspace hash or the project slug.
func TestParseMetadataNoSiblingKnowsCwd(t *testing.T) {
	a := testAdapter(t)
	chat(t, a, "ws1", "sibling", map[string]string{metaName: metaNoCwd, storeName: "db"})
	path := chat(t, a, "ws1", "older", map[string]string{metaName: metaNoCwd, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "" {
		t.Errorf("Dir = %q, want empty", md.Dir)
	}
	if len(a.workspaceDirs) != 0 {
		t.Errorf("a failed lookup should not be cached: %v", a.workspaceDirs)
	}
}

func TestParseMetadataOwnCwdWins(t *testing.T) {
	a := testAdapter(t)
	// A stale sibling in the same workspace, recording a different path.
	chat(t, a, "ws1", "aaa", map[string]string{
		metaName: `{"schemaVersion":1,"createdAtMs":1782570516884,"hasConversation":true,` +
			`"title":"Elsewhere","updatedAtMs":1782570653052,"cwd":"/Users/alice/projects/other"}`,
	})
	path := chat(t, a, "ws1", "bbb", map[string]string{metaName: metaFull, storeName: "db"})

	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/web" {
		t.Errorf("Dir = %q, want the chat's own cwd", md.Dir)
	}
}

func TestCommands(t *testing.T) {
	a := testAdapter(t)
	if a.SupportsSessionID() {
		t.Error("cursor-agent cannot adopt a caller-chosen chat ID")
	}

	newCmd := a.NewSessionCmd("/tmp/work", "ignored-id")
	if got := newCmd.Args[1:]; len(got) != 0 {
		t.Errorf("new session args = %q, want none", got)
	}
	if newCmd.Dir != "/tmp/work" {
		t.Errorf("new session dir = %q", newCmd.Dir)
	}

	resume := a.ResumeCmd("/tmp/work", "aaa")
	want := []string{"--resume", "aaa"}
	got := resume.Args[1:]
	if len(got) != len(want) {
		t.Fatalf("resume args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resume args = %q, want %q", got, want)
		}
	}
	if resume.Dir != "/tmp/work" {
		t.Errorf("resume dir = %q", resume.Dir)
	}
}

func TestCompanionFiles(t *testing.T) {
	a := testAdapter(t)
	path := chat(t, a, "ws1", "aaa", map[string]string{
		metaName:              metaFull,
		storeName:             "db",
		"prompt_history.json": `["x"]`,
		"pasted_text.json":    `{}`,
	})
	tr := transcript(t, a, "Users-alice-projects-web", "aaa", newFormatTranscript)
	// Another chat's files must not be swept up.
	other := chat(t, a, "ws1", "bbb", map[string]string{metaName: metaNoCwd, storeName: "db"})

	companions := a.CompanionFiles(path)
	want := map[string]bool{
		filepath.Dir(path): false, // the chat directory
		filepath.Dir(tr):   false, // the transcript directory
	}
	for _, c := range companions {
		if c == path {
			t.Error("the tracked meta.json must not be returned as its own companion")
		}
		if _, ok := want[c]; !ok {
			t.Errorf("unexpected companion %q", c)
			continue
		}
		want[c] = true
	}
	for p, seen := range want {
		if !seen {
			t.Errorf("missing companion %q, got %q", p, companions)
		}
	}

	// Deleting the tracked file plus its companions must leave nothing of
	// this chat in either tree, and leave the sibling chat alone.
	for _, p := range append(companions, path) {
		if err := os.RemoveAll(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Dir(path), filepath.Dir(tr)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%q survived deletion", p)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("sibling chat was disturbed: %v", err)
	}
}

// Trash and Purge pass a session's *current* path, which for an
// already-trashed session is wallfacer's trash directory. Returning that as a
// companion would take every other trashed session down with it.
func TestCompanionFilesOutsideChatsDir(t *testing.T) {
	a := testAdapter(t)
	trash := filepath.Join(t.TempDir(), "trash")
	writeFile(t, filepath.Join(trash, metaName), metaFull)
	writeFile(t, filepath.Join(trash, "another-session.jsonl"), "{}")

	for _, c := range a.CompanionFiles(filepath.Join(trash, metaName)) {
		if c == trash {
			t.Fatal("the trash directory must never be returned as a companion")
		}
	}
}

func TestParseMetadataMissingFile(t *testing.T) {
	a := testAdapter(t)
	md, err := a.ParseMetadata(filepath.Join(a.ChatsDir, "ws1", "gone", metaName))
	if err != nil {
		t.Fatalf("missing metadata must degrade gracefully, got %v", err)
	}
	if md == nil {
		t.Fatal("expected non-nil metadata")
	}
}

func TestDefaultPaths(t *testing.T) {
	a := New()
	if a.Type() != "cursor-agent" {
		t.Errorf("Type = %q", a.Type())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if want := filepath.Join(home, ".cursor", "chats"); a.ChatsDir != want {
		t.Errorf("ChatsDir = %q, want %q", a.ChatsDir, want)
	}
	if want := filepath.Join(home, ".cursor", "projects"); a.ProjectsDir != want {
		t.Errorf("ProjectsDir = %q, want %q", a.ProjectsDir, want)
	}
}
