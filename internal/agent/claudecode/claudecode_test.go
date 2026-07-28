package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, root, project, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const normalSession = `{"type":"mode","mode":"normal","sessionId":"aaa"}
{"type":"file-history-snapshot","messageId":"m1","snapshot":{"trackedFileBackups":{},"timestamp":"2026-07-28T06:26:30.752Z"}}
{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"Caveat: local command output below"},"timestamp":"2026-07-28T06:26:30.296Z","cwd":"/Users/alice/projects/my-app","gitBranch":"main","sessionId":"aaa"}
{"parentUuid":"u1","isSidechain":false,"type":"user","message":{"role":"user","content":"Build me a session manager CLI"},"timestamp":"2026-07-28T06:26:31.000Z","cwd":"/Users/alice/projects/my-app","gitBranch":"main","sessionId":"aaa"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Sure."}]},"timestamp":"2026-07-28T06:26:35.000Z","cwd":"/Users/alice/projects/my-app","sessionId":"aaa"}
`

const summarySession = `{"type":"summary","summary":"Refactor auth middleware","leafUuid":"x"}
{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"text","text":"please refactor the auth middleware"}]},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/home/alice/work/api","gitBranch":"feat/auth","sessionId":"bbb"}
`

const sidechainSession = `{"parentUuid":null,"isSidechain":true,"type":"user","message":{"role":"user","content":"You are a search subagent, find X"},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/home/alice/work/api","sessionId":"ccc"}
`

func TestParseMetadata(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "-Users-alice-projects-my-app", "aaa.jsonl", normalSession)

	a := &Adapter{ProjectsDir: root, Binary: "claude"}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Dir != "/Users/alice/projects/my-app" {
		t.Errorf("Dir = %q", md.Dir)
	}
	if md.FirstPrompt != "Build me a session manager CLI" {
		t.Errorf("FirstPrompt = %q (caveat line should be skipped)", md.FirstPrompt)
	}
	if md.GitBranch != "main" {
		t.Errorf("GitBranch = %q", md.GitBranch)
	}
	if md.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if md.Sidechain {
		t.Error("Sidechain should be false")
	}
}

func TestParseMetadataSummaryAndBlocks(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "-home-alice-work-api", "bbb.jsonl", summarySession)

	a := &Adapter{ProjectsDir: root}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if md.Summary != "Refactor auth middleware" {
		t.Errorf("Summary = %q", md.Summary)
	}
	if md.FirstPrompt != "please refactor the auth middleware" {
		t.Errorf("FirstPrompt = %q (array content blocks)", md.FirstPrompt)
	}
}

func TestParseMetadataSidechain(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "-home-alice-work-api", "ccc.jsonl", sidechainSession)

	a := &Adapter{ProjectsDir: root}
	md, err := a.ParseMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if !md.Sidechain {
		t.Error("expected Sidechain=true for subagent transcript")
	}
}

func TestListSessionFiles(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "-p-one", "aaa.jsonl", normalSession)
	writeFixture(t, root, "-p-two", "bbb.jsonl", summarySession)
	writeFixture(t, root, "-p-two", "notes.txt", "ignore me")

	a := &Adapter{ProjectsDir: root}
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
	if !ids["aaa"] || !ids["bbb"] {
		t.Errorf("unexpected ids: %v", ids)
	}
}

func TestListSessionFilesMissingRoot(t *testing.T) {
	a := &Adapter{ProjectsDir: filepath.Join(t.TempDir(), "nope")}
	files, err := a.ListSessionFiles()
	if err != nil || files != nil {
		t.Errorf("missing root should be (nil, nil), got (%v, %v)", files, err)
	}
}
