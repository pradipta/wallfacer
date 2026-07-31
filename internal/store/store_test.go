package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/agent/claudecode"
	"github.com/pradipta/wallfacer/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleSession(id string) store.Session {
	return store.Session{
		ID:           id,
		AgentType:    "claude-code",
		Dir:          "/home/x/proj",
		AutoTitle:    "auto title " + id,
		FirstPrompt:  "do the thing",
		GitBranch:    "main",
		CreatedAt:    time.Unix(1700000000, 0),
		LastActiveAt: time.Unix(1700000100, 0),
		FilePath:     "/tmp/" + id + ".jsonl",
		FileSize:     123,
		FileMtime:    time.Unix(1700000100, 0),
	}
}

func TestUpsertPreservesOverlay(t *testing.T) {
	s := openTestStore(t)
	if err := s.Upsert(sampleSession("aaa-111")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitle("aaa-111", "My Session"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProject("aaa-111", "wallfacer"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag("aaa-111", "cli"); err != nil {
		t.Fatal(err)
	}

	// Re-sync with fresh disk data must not clobber overlay fields.
	updated := sampleSession("aaa-111")
	updated.AutoTitle = "newer auto title"
	if err := s.Upsert(updated); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get("aaa-111")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "My Session" || got.Project != "wallfacer" {
		t.Errorf("overlay clobbered: title=%q project=%q", got.Title, got.Project)
	}
	if got.AutoTitle != "newer auto title" {
		t.Errorf("AutoTitle not refreshed: %q", got.AutoTitle)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "cli" {
		t.Errorf("tags = %v", got.Tags)
	}
	if got.DisplayTitle() != "My Session" {
		t.Errorf("DisplayTitle = %q", got.DisplayTitle())
	}
}

func TestListFiltersAndSearch(t *testing.T) {
	s := openTestStore(t)
	a := sampleSession("aaa-111")
	b := sampleSession("bbb-222")
	b.Dir = "/home/x/other"
	b.FirstPrompt = "refactor the auth middleware"
	b.AutoTitle = "refactor the auth middleware"
	for _, x := range []store.Session{a, b} {
		if err := s.Upsert(x); err != nil {
			t.Fatal(err)
		}
	}
	s.SetProject("aaa-111", "wallfacer")
	s.AddTag("bbb-222", "auth")

	if got, _ := s.List(store.Filter{Project: "wallfacer"}); len(got) != 1 || got[0].ID != "aaa-111" {
		t.Errorf("project filter: %v", got)
	}
	if got, _ := s.List(store.Filter{Tag: "auth"}); len(got) != 1 || got[0].ID != "bbb-222" {
		t.Errorf("tag filter: %v", got)
	}
	if got, _ := s.List(store.Filter{Query: "middleware"}); len(got) != 1 || got[0].ID != "bbb-222" {
		t.Errorf("query search: %v", got)
	}
	if got, _ := s.List(store.Filter{Dir: "/home/x/other"}); len(got) != 1 {
		t.Errorf("dir filter: %v", got)
	}
	if got, _ := s.List(store.Filter{}); len(got) != 2 {
		t.Errorf("unfiltered: %v", got)
	}
}

func TestSearchAgentType(t *testing.T) {
	s := openTestStore(t)

	a := sampleSession("aaa-111")
	a.AgentType = "claude-code"
	a.AutoTitle = "do the thing"
	a.FirstPrompt = "do the thing"
	a.Dir = "/tmp/test"

	b := sampleSession("bbb-222")
	b.AgentType = "kiro-cli"
	b.AutoTitle = "some other task"
	b.FirstPrompt = "some other task"
	b.Dir = "/tmp/other"

	for _, x := range []store.Session{a, b} {
		if err := s.Upsert(x); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.List(store.Filter{Query: "claude-code"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "aaa-111" {
		t.Errorf("claude-code search: got %d items, want aaa-111: %v", len(got), got)
	}

	got, err = s.List(store.Filter{Query: "kiro-cli"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "bbb-222" {
		t.Errorf("kiro-cli search: got %d items, want bbb-222: %v", len(got), got)
	}

	got, err = s.List(store.Filter{Query: "nonexistent-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("nonexistent search: got %d items, want 0", len(got))
	}
}

func TestListAgentTypeMatchesSubstringsCaseInsensitively(t *testing.T) {
	s := openTestStore(t)
	for _, x := range []store.Session{
		func() store.Session { x := sampleSession("aaa-111"); x.AgentType = "claude-code"; return x }(),
		func() store.Session { x := sampleSession("bbb-222"); x.AgentType = "cursor-agent"; return x }(),
		func() store.Session { x := sampleSession("ccc-333"); x.AgentType = "kiro-cli"; return x }(),
	} {
		if err := s.Upsert(x); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "prefix", query: "kiro", want: []string{"ccc-333"}},
		{name: "fragment", query: "a", want: []string{"aaa-111", "bbb-222"}},
		{name: "case insensitive", query: "CLAUDE", want: []string{"aaa-111"}},
		{name: "exact", query: "cursor-agent", want: []string{"bbb-222"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.List(store.Filter{AgentType: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d sessions, want %d: %v", len(got), len(tc.want), got)
			}
			gotIDs := make(map[string]bool, len(got))
			for _, session := range got {
				gotIDs[session.ID] = true
			}
			for _, id := range tc.want {
				if !gotIDs[id] {
					t.Errorf("result IDs = %v, want %q", gotIDs, id)
				}
			}
		})
	}
}

func TestResolve(t *testing.T) {
	s := openTestStore(t)
	s.Upsert(sampleSession("abc-123"))
	s.Upsert(sampleSession("abd-456"))
	s.SetTitle("abd-456", "Auth Work")

	if got, err := s.Resolve("abc"); err != nil || got.ID != "abc-123" {
		t.Errorf("prefix resolve: %v %v", got, err)
	}
	if got, err := s.Resolve("auth work"); err != nil || got.ID != "abd-456" {
		t.Errorf("title resolve: %v %v", got, err)
	}
	if _, err := s.Resolve("ab"); err == nil {
		t.Error("ambiguous prefix should error")
	}
	if _, err := s.Resolve("zzz"); err == nil {
		t.Error("unknown ref should error")
	}
}

func TestTrashAndPurge(t *testing.T) {
	dataDir := t.TempDir()
	s, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// A real file on disk standing in for the agent's session JSONL.
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "ttt-111.jsonl")
	os.WriteFile(srcFile, []byte("session content"), 0o644)

	sess := sampleSession("ttt-111")
	sess.FilePath = srcFile
	if err := s.Upsert(sess); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get("ttt-111")
	dest, err := s.Trash(*got)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("original file should be gone after trash")
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "session content" {
		t.Errorf("trashed file content: %s, %v", b, err)
	}

	// Trashed sessions are hidden by default, visible with IncludeHidden,
	// and cannot be resolved for resume.
	if list, _ := s.List(store.Filter{}); len(list) != 0 {
		t.Errorf("trashed session in default list: %v", list)
	}
	if list, _ := s.List(store.Filter{IncludeHidden: true}); len(list) != 1 ||
		list[0].Status != store.StatusTrashed {
		t.Errorf("hidden list: %v", list)
	}
	if _, err := s.Resolve("ttt"); err == nil {
		t.Error("Resolve should not find trashed sessions")
	}
	trashed, err := s.ResolveAny("ttt")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Purge(*trashed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("purge should remove the trashed file")
	}
	if _, err := s.Get("ttt-111"); err != store.ErrNotFound {
		t.Errorf("purge should remove the row, got %v", err)
	}
}

const fixtureSession = `{"type":"summary","summary":"Build session manager"}
{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"Build me a session manager CLI"},"timestamp":"2026-07-28T06:26:31.000Z","cwd":"/Users/alice/projects/my-app","gitBranch":"main","sessionId":"aaa"}
`

const fixtureSidechain = `{"parentUuid":null,"isSidechain":true,"type":"user","message":{"role":"user","content":"subagent task"},"timestamp":"2026-07-01T10:00:00.000Z","cwd":"/Users/alice/projects/my-app"}
`

func TestSyncEndToEnd(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-Users-alice-projects-my-app")
	os.MkdirAll(dir, 0o755)
	mainFile := filepath.Join(dir, "e2e-aaa.jsonl")
	os.WriteFile(mainFile, []byte(fixtureSession), 0o644)
	os.WriteFile(filepath.Join(dir, "e2e-side.jsonl"), []byte(fixtureSidechain), 0o644)

	agent.Register(&claudecode.Adapter{ProjectsDir: projects})
	s := openTestStore(t)

	res, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.Scanned != 2 || res.Parsed != 2 || res.Skipped != 1 {
		t.Errorf("first sync: %+v", res)
	}
	list, _ := s.List(store.Filter{})
	if len(list) != 1 {
		t.Fatalf("want 1 indexed session, got %d", len(list))
	}
	got := list[0]
	if got.ID != "e2e-aaa" || got.Dir != "/Users/alice/projects/my-app" ||
		got.AutoTitle != "Build session manager" || got.AgentType != "claude-code" {
		t.Errorf("indexed session: %+v", got)
	}

	// Second sync: nothing changed, nothing re-parsed.
	res, err = s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if res.Parsed != 0 {
		t.Errorf("incremental sync should skip unchanged files: %+v", res)
	}

	// File disappears → session marked missing and hidden from default list.
	os.Remove(mainFile)
	res, _ = s.Sync()
	if res.Missing != 1 {
		t.Errorf("want 1 missing, got %+v", res)
	}
	if list, _ := s.List(store.Filter{}); len(list) != 0 {
		t.Errorf("missing session should be hidden: %v", list)
	}
	if list, _ := s.List(store.Filter{IncludeHidden: true}); len(list) != 1 ||
		list[0].Status != store.StatusMissing {
		t.Errorf("hidden list: %v", list)
	}
}
