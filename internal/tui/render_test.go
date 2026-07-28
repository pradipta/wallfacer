package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/pradipta/wallfacer/internal/store"
)

// testWidths spans a cramped pane, the narrowest terminal the split layout
// allows, and a wide one.
var testWidths = []int{40, 60, 80, 120}

func richSession() store.Session {
	now := time.Now()
	return store.Session{
		ID:           "9f8e7d6c-1234-4321-abcd-0123456789ab",
		AgentType:    "claude-code",
		Dir:          "/Users/someone/projects/deeply/nested/path/wallfacer",
		AutoTitle:    "Refactor the incremental sync loop so it stops re-parsing every transcript on each run",
		Project:      "wallfacer-core",
		FirstPrompt:  "the sync loop re-parses every file on each run even when nothing changed;\nmake it use the cached size and mtime",
		GitBranch:    "feat/incremental-sync",
		CreatedAt:    now.Add(-72 * time.Hour),
		LastActiveAt: now.Add(-2 * time.Hour),
		FileSize:     412 * 1024,
		Status:       store.StatusActive,
		Tags:         []string{"perf", "wip", "needs-review", "sync", "backlog"},
	}
}

// TestRenderRowNeverExceedsWidth is the regression guard for the bug this
// delegate exists to fix. Every emitted line must fit the pane; nothing may be
// appended to a line that is already full.
func TestRenderRowNeverExceedsWidth(t *testing.T) {
	states := map[string]rowState{"normal": rowNormal, "selected": rowSelected, "dimmed": rowDimmed}
	for _, w := range testWidths {
		for name, state := range states {
			lines := renderRow(richSession(), w, state, nil)
			if len(lines) != rowHeight {
				t.Fatalf("width %d %s: got %d lines, want %d", w, name, len(lines), rowHeight)
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got > w {
					t.Errorf("width %d %s: line %d is %d cells: %q", w, name, i, got, l)
				}
			}
		}
	}
}

// TestRenderRowShowsProjectAndTags is the positive half: at usable widths the
// metadata that `wallfacer list` prints must actually be on screen.
func TestRenderRowShowsProjectAndTags(t *testing.T) {
	s := richSession()
	for _, w := range []int{60, 80, 120} {
		plain := ansi.Strip(strings.Join(renderRow(s, w, rowNormal, nil), "\n"))
		if !strings.Contains(plain, "◆") {
			t.Errorf("width %d: project badge missing from %q", w, plain)
		}
		if !strings.Contains(plain, "#perf") {
			t.Errorf("width %d: tags missing from %q", w, plain)
		}
		if !strings.Contains(plain, "wallfacer") {
			t.Errorf("width %d: directory leaf missing from %q", w, plain)
		}
	}
}

// The tag line drops what does not fit and says how many it dropped.
func TestRenderRowTagOverflow(t *testing.T) {
	plain := ansi.Strip(tagLine(richSession(), 24, rowNormal))
	if !strings.Contains(plain, "+") {
		t.Errorf("expected an overflow marker in %q", plain)
	}
	if got := ansi.StringWidth(plain); got > 24 {
		t.Errorf("tag line is %d cells, want <= 24: %q", got, plain)
	}
}

// A bare session must not render stray separators or badge glyphs.
func TestRenderRowBareSession(t *testing.T) {
	s := store.Session{
		ID:        "abc",
		AgentType: "claude-code",
		Dir:       "/tmp",
		Status:    store.StatusActive,
	}
	lines := renderRow(s, 60, rowNormal, nil)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(plain, "◆") {
		t.Errorf("project badge rendered for a session with no project: %q", plain)
	}
	if strings.Contains(plain, "#") {
		t.Errorf("tag chip rendered for a session with no tags: %q", plain)
	}
	if !strings.Contains(plain, "(untitled)") {
		t.Errorf("expected the untitled placeholder in %q", plain)
	}
	if got := strings.TrimSpace(ansi.Strip(lines[2])); got != "" {
		t.Errorf("tag line should be blank, got %q", got)
	}
}

// Degenerate widths must not panic or produce negative-length work.
func TestRenderRowTinyWidths(t *testing.T) {
	for w := -1; w <= 6; w++ {
		lines := renderRow(richSession(), w, rowSelected, nil)
		for i, l := range lines {
			if got := ansi.StringWidth(l); w > 0 && got > max(w, rowIndent+1) {
				t.Errorf("width %d: line %d is %d cells: %q", w, i, got, l)
			}
		}
	}
}

func TestRenderDetailBounds(t *testing.T) {
	const w, h = 40, 20
	out := renderDetail(richSession(), w, h)
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Errorf("detail pane is %d lines, want <= %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Errorf("detail line %d is %d cells, want <= %d: %q", i, got, w, l)
		}
	}
}

func TestRenderDetailContent(t *testing.T) {
	plain := ansi.Strip(renderDetail(richSession(), 48, 30))
	for _, want := range []string{
		"Refactor the incremental sync loop", // title
		"◆ wallfacer-core",                   // project badge
		"#perf",                              // tags
		"feat/incremental-sync",              // branch
		"claude-code",                        // agent
		"412.0 KB",                           // size
		"9f8e7d6c",                           // short id
		"first prompt",                       // prompt section
		"re-parses",                          // prompt body
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("detail pane missing %q\n---\n%s", want, plain)
		}
	}
}

// Empty fields are skipped rather than rendered as dangling labels.
func TestRenderDetailSkipsEmptyFields(t *testing.T) {
	s := richSession()
	s.GitBranch = ""
	s.FirstPrompt = ""
	plain := ansi.Strip(renderDetail(s, 48, 30))
	if strings.Contains(plain, "branch") {
		t.Errorf("empty branch should be omitted:\n%s", plain)
	}
	if strings.Contains(plain, "first prompt") {
		t.Errorf("empty prompt section should be omitted:\n%s", plain)
	}
}

// Too small to be useful means render nothing, not render garbage.
func TestRenderDetailTooSmall(t *testing.T) {
	if got := renderDetail(richSession(), 6, 20); got != "" {
		t.Errorf("want empty for a 6-cell pane, got %q", got)
	}
	if got := renderDetail(richSession(), 40, 2); got != "" {
		t.Errorf("want empty for a 2-line pane, got %q", got)
	}
}
