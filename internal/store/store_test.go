package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pradipta-s/wallfacer/internal/agent"
	"github.com/pradipta-s/wallfacer/internal/agent/claudecode"
	"github.com/pradipta-s/wallfacer/internal/store"
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
