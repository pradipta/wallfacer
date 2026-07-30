package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pradipta/wallfacer/internal/agent"
)

var (
	_ agent.Adapter = (*Adapter)(nil)
)

// writeFixture writes content to sessions/<datePath>/<name> under root,
// mirroring Codex's ~/.codex/sessions/<YYYY>/<MM>/<DD>/ layout.
func writeFixture(t *testing.T, root, datePath, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, datePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// normalSession has the clean human prompt in an event_msg user_message, after
// the injected environment_context that leads the model transcript.
const normalSession = `{"timestamp":"2026-07-30T12:30:18.533Z","type":"session_meta","payload":{"session_id":"019fb2ff-b280-72f0-8401-7eab2c6e1f1d","id":"019fb2ff-b280-72f0-8401-7eab2c6e1f1d","timestamp":"2026-07-30T12:28:49.808Z","cwd":"/Users/alice/projects/my-app","originator":"codex-tui","cli_version":"0.146.0","git":{"commit_hash":"abc","branch":"main","repository_url":"git@github.com:alice/my-app.git"}}}
{"timestamp":"2026-07-30T12:30:18.545Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions instructions>\nFilesystem sandboxing ..."}]}}
{"timestamp":"2026-07-30T12:30:18.548Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/Users/alice/projects/my-app</cwd>\n</environment_context>"}]}}
{"timestamp":"2026-07-30T12:30:18.575Z","type":"event_msg","payload":{"type":"user_message","message":"Build me a session manager CLI","images":[]}}
{"timestamp":"2026-07-30T12:30:20.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Sure."}}
`

// fallbackSession has no event_msg user_message; the first prompt must fall
// back to the response_item user message, skipping the environment_context.
const fallbackSession = `{"timestamp":"2026-07-01T03:43:02.945Z","type":"session_meta","payload":{"session_id":"019f1bc5-e152-7c93-b56d-a7b4e0203555","timestamp":"2026-07-01T03:43:01.179Z","cwd":"/home/bob/work/api","git":{"branch":"feat/auth"}}}
{"timestamp":"2026-07-01T03:43:02.948Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/home/bob/work/api</cwd>\n</environment_context>"}]}}
{"timestamp":"2026-07-01T03:43:02.949Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please refactor the auth middleware"}]}}
`

func TestParseMetadata(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "2026/07/30", "rollout-2026-07-30T17-58-49-019fb2ff-b280-72f0-8401-7eab2c6e1f1d.jsonl", normalSession)

	a := &Adapter{SessionsDir: root, Binary: "codex"}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/my-app" {
		t.Errorf("Dir = %q", md.Dir)
	}
	if md.FirstPrompt != "Build me a session manager CLI" {
		t.Errorf("FirstPrompt = %q (want the user_message, not the env context)", md.FirstPrompt)
	}
	if md.GitBranch != "main" {
		t.Errorf("GitBranch = %q", md.GitBranch)
	}
	if md.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if md.CreatedAt.Year() != 2026 {
		t.Errorf("CreatedAt year = %d, want session_meta payload time (2026)", md.CreatedAt.Year())
	}
	if md.Sidechain {
		t.Error("Sidechain should be false (Codex has no sidechains)")
	}
}

func TestParseMetadataFallbackPrompt(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "2026/07/01", "rollout-2026-07-01T09-13-01-019f1bc5-e152-7c93-b56d-a7b4e0203555.jsonl", fallbackSession)

	a := &Adapter{SessionsDir: root}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/home/bob/work/api" {
		t.Errorf("Dir = %q", md.Dir)
	}
	if md.FirstPrompt != "please refactor the auth middleware" {
		t.Errorf("FirstPrompt = %q (env context should be skipped, response_item used)", md.FirstPrompt)
	}
	if md.GitBranch != "feat/auth" {
		t.Errorf("GitBranch = %q", md.GitBranch)
	}
}

func TestParseMetadataMalformed(t *testing.T) {
	root := t.TempDir()
	// A truncated/garbage file must degrade, not error.
	path := writeFixture(t, root, "2026/07/30", "rollout-2026-07-30T00-00-00-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl", "not json at all\n{broken")

	a := &Adapter{SessionsDir: root}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatalf("malformed file should degrade, got error: %v", err)
	}
	if md.Dir != "" || md.FirstPrompt != "" {
		t.Errorf("expected empty metadata from garbage, got %+v", md)
	}
}

func TestListSessionFiles(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "2026/07/30", "rollout-2026-07-30T17-58-49-019fb2ff-b280-72f0-8401-7eab2c6e1f1d.jsonl", normalSession)
	writeFixture(t, root, "2026/07/01", "rollout-2026-07-01T09-13-01-019f1bc5-e152-7c93-b56d-a7b4e0203555.jsonl", fallbackSession)
	// A non-rollout file in the tree must be ignored.
	writeFixture(t, root, "2026/07/01", "notes.jsonl", "ignore me")

	a := &Adapter{SessionsDir: root}
	files, err := a.ListSessionFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	ids := map[string]bool{}
	for _, f := range files {
		ids[f.ID] = true
		if f.Size == 0 || f.Mtime.IsZero() {
			t.Errorf("file %s missing stat info", f.ID)
		}
	}
	if !ids["019fb2ff-b280-72f0-8401-7eab2c6e1f1d"] || !ids["019f1bc5-e152-7c93-b56d-a7b4e0203555"] {
		t.Errorf("unexpected ids: %v", ids)
	}
}

func TestListSessionFilesMissingRoot(t *testing.T) {
	a := &Adapter{SessionsDir: filepath.Join(t.TempDir(), "nope")}
	files, err := a.ListSessionFiles()
	if err != nil || files != nil {
		t.Errorf("missing root should be (nil, nil), got (%v, %v)", files, err)
	}
}

func TestCommands(t *testing.T) {
	a := &Adapter{SessionsDir: "/tmp", Binary: "codex"}

	if a.SupportsSessionID() {
		t.Error("SupportsSessionID should be false")
	}

	newCmd := a.NewSessionCmd("/work/api", "ignored-id")
	if got := newCmd.Args; len(got) != 1 || got[0] != "codex" {
		t.Errorf("NewSessionCmd args = %v, want [codex]", got)
	}
	if newCmd.Dir != "/work/api" {
		t.Errorf("NewSessionCmd Dir = %q", newCmd.Dir)
	}

	resumeCmd := a.ResumeCmd("/work/api", "019fb2ff")
	wantArgs := []string{"codex", "resume", "019fb2ff"}
	if len(resumeCmd.Args) != len(wantArgs) {
		t.Fatalf("ResumeCmd args = %v, want %v", resumeCmd.Args, wantArgs)
	}
	for i, w := range wantArgs {
		if resumeCmd.Args[i] != w {
			t.Errorf("ResumeCmd args[%d] = %q, want %q", i, resumeCmd.Args[i], w)
		}
	}
	if resumeCmd.Dir != "/work/api" {
		t.Errorf("ResumeCmd Dir = %q", resumeCmd.Dir)
	}
}
