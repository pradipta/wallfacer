package kirocli

import (
	"os"
	"path/filepath"
	"testing"
)

// write drops one fixture file into the flat sessions directory.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const normalSidecar = `{
  "session_id": "aaa",
  "cwd": "/Users/alice/projects/my-app",
  "created_at": "2026-07-30T08:17:53.812342Z",
  "updated_at": "2026-07-30T08:31:05.932208Z",
  "title": "Create GitHub issues from the roadmap",
  "session_created_reason": "subagent",
  "session_state": {"version": "v1", "agent_name": null}
}`

const normalTranscript = `{"version":"v1","kind":"Prompt","data":{"message_id":"m1","content":[{"kind":"text","data":"In the readme, there are a few roadmap items. Create GitHub issues for them."}],"meta":{"timestamp":1785399519}}}
{"version":"v1","kind":"AssistantMessage","data":{"message_id":"m2","content":[{"kind":"text","data":""},{"kind":"toolUse","data":{"toolUseId":"t1","name":"read","input":{}}}]}}
{"version":"v1","kind":"ToolResults","data":{"message_id":"m3","content":[{"kind":"toolResult","data":{"toolUseId":"t1","status":"success"}}]}}
`

const subagentSidecar = `{
  "session_id": "bbb",
  "cwd": "/Users/alice/projects/my-app",
  "created_at": "2026-07-10T02:47:02.974626Z",
  "title": "You are working on the backend project.",
  "parent_session_id": "aaa",
  "session_created_reason": "subagent"
}`

const subagentTranscript = `{"version":"v1","kind":"Prompt","data":{"message_id":"m1","content":[{"kind":"text","data":"You are working on the backend project. Move these files."}],"meta":{"timestamp":1783651622}}}
`

// orphanTranscript has no sidecar, and leads with a tool-use-only prompt block
// to prove non-text blocks are skipped.
const orphanTranscript = `{"version":"v1","kind":"Prompt","data":{"message_id":"m1","content":[{"kind":"image","data":{"bytes":"…"}},{"kind":"text","data":"   "}],"meta":{"timestamp":1783651000}}}
{"version":"v1","kind":"Prompt","data":{"message_id":"m2","content":[{"kind":"text","data":"what does this repo do?"}],"meta":{"timestamp":1783651100}}}
`

func newTestAdapter(dir string) *Adapter {
	return &Adapter{SessionsDir: dir, Binary: "kiro-cli"}
}

func TestParseMetadata(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "aaa.json", normalSidecar)
	path := write(t, dir, "aaa.jsonl", normalTranscript)

	md, err := newTestAdapter(dir).ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/my-app" {
		t.Errorf("Dir = %q", md.Dir)
	}
	if md.Summary != "Create GitHub issues from the roadmap" {
		t.Errorf("Summary = %q (sidecar title)", md.Summary)
	}
	if md.FirstPrompt != "In the readme, there are a few roadmap items. Create GitHub issues for them." {
		t.Errorf("FirstPrompt = %q", md.FirstPrompt)
	}
	if md.CreatedAt.IsZero() || md.CreatedAt.Year() != 2026 {
		t.Errorf("CreatedAt = %v", md.CreatedAt)
	}
	if md.GitBranch != "" {
		t.Errorf("GitBranch = %q, Kiro records none", md.GitBranch)
	}
	// session_created_reason is "subagent" here on purpose: it must not be
	// mistaken for a sidechain marker.
	if md.Sidechain {
		t.Error("Sidechain should be false without a parent_session_id")
	}
}

func TestParseMetadataSubagent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bbb.json", subagentSidecar)
	path := write(t, dir, "bbb.jsonl", subagentTranscript)

	md, err := newTestAdapter(dir).ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if !md.Sidechain {
		t.Error("expected Sidechain=true when parent_session_id is set")
	}
}

func TestParseMetadataWithoutSidecar(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "ccc.jsonl", orphanTranscript)

	md, err := newTestAdapter(dir).ParseMetadata(path)
	if err != nil {
		t.Fatalf("a missing sidecar must degrade gracefully, got %v", err)
	}
	if md.FirstPrompt != "what does this repo do?" {
		t.Errorf("FirstPrompt = %q (blank and non-text blocks should be skipped)", md.FirstPrompt)
	}
	if md.Dir != "" || md.Summary != "" {
		t.Errorf("expected no sidecar-derived fields, got dir=%q summary=%q", md.Dir, md.Summary)
	}
	// The transcript's own timestamp stands in for the sidecar's created_at.
	if md.CreatedAt.Unix() != 1783651100 {
		t.Errorf("CreatedAt = %v, want the prompt timestamp", md.CreatedAt)
	}
}

func TestParseMetadataMalformedSidecar(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ddd.json", "{not json")
	path := write(t, dir, "ddd.jsonl", normalTranscript)

	md, err := newTestAdapter(dir).ParseMetadata(path)
	if err != nil {
		t.Fatalf("a malformed sidecar must degrade gracefully, got %v", err)
	}
	if md.FirstPrompt == "" {
		t.Error("transcript should still be parsed")
	}
}

func TestListSessionFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "aaa.json", normalSidecar)
	write(t, dir, "aaa.jsonl", normalTranscript)
	write(t, dir, "bbb.json", subagentSidecar)
	write(t, dir, "bbb.jsonl", subagentTranscript)
	// Companions and empty sessions must not be listed as sessions.
	write(t, dir, "aaa.history", "some prompt history")
	write(t, dir, "aaa.lock", "pid")
	write(t, dir, "eee.history", "session that never produced a transcript")
	if err := os.MkdirAll(filepath.Join(dir, "aaa", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := newTestAdapter(dir).ListSessionFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %+v", len(files), files)
	}
	ids := map[string]bool{}
	for _, f := range files {
		ids[f.ID] = true
		if f.Size == 0 || f.Mtime.IsZero() {
			t.Errorf("file %s missing stat info", f.ID)
		}
	}
	if !ids["aaa"] || !ids["bbb"] {
		t.Errorf("unexpected ids: %v", ids)
	}
}

func TestListSessionFilesMissingRoot(t *testing.T) {
	a := newTestAdapter(filepath.Join(t.TempDir(), "nope"))
	files, err := a.ListSessionFiles()
	if err != nil || files != nil {
		t.Errorf("missing root should be (nil, nil), got (%v, %v)", files, err)
	}
}

func TestCommands(t *testing.T) {
	a := newTestAdapter(t.TempDir())
	if a.SupportsSessionID() {
		t.Error("kiro-cli cannot pre-assign session IDs")
	}

	newCmd := a.NewSessionCmd("/tmp/work", "ignored-id")
	if got := newCmd.Args[1:]; len(got) != 1 || got[0] != "chat" {
		t.Errorf("new session args = %q", got)
	}
	if newCmd.Dir != "/tmp/work" {
		t.Errorf("new session dir = %q", newCmd.Dir)
	}

	resume := a.ResumeCmd("/tmp/work", "aaa")
	want := []string{"chat", "--resume-id", "aaa"}
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

func TestKiroHomeHonoursEnv(t *testing.T) {
	t.Setenv("KIRO_HOME", "/custom/kiro")
	if got := New().SessionsDir; got != filepath.Join("/custom/kiro", "sessions", "cli") {
		t.Errorf("SessionsDir = %q", got)
	}
}
