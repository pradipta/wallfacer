package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

// step feeds one answer into the new-session chain and returns the model as it
// waits for the next one.
func step(t *testing.T, m model, kind inputKind, value string) model {
	t.Helper()
	next, _ := m.submitNew(kind, value)
	return next.(model)
}

func newChainModel() model {
	return model{input: textinput.New(), projIdx: -1, tagIdx: -1}
}

func TestNewChainCollectsOverlay(t *testing.T) {
	m := newChainModel()
	m.draft = Action{Type: ActionNew}

	m = step(t, m, inputNewDir, "/tmp/foo")
	if m.kind != inputNewTitle {
		t.Fatalf("after dir, kind = %v, want inputNewTitle", m.kind)
	}
	m = step(t, m, inputNewTitle, "refactor auth")
	if m.kind != inputNewProject {
		t.Fatalf("after title, kind = %v, want inputNewProject", m.kind)
	}
	m = step(t, m, inputNewProject, "wallfacer")
	if m.kind != inputNewTags {
		t.Fatalf("after project, kind = %v, want inputNewTags", m.kind)
	}
	m = step(t, m, inputNewTags, "go, tui")

	got := m.action
	if got.Type != ActionNew {
		t.Errorf("type = %v, want ActionNew", got.Type)
	}
	if got.Dir != "/tmp/foo" || got.Title != "refactor auth" || got.Project != "wallfacer" {
		t.Errorf("got %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" || got.Tags[1] != "tui" {
		t.Errorf("tags = %q, want [go tui]", got.Tags)
	}
}

func TestNewChainSkippedFieldsStayEmpty(t *testing.T) {
	m := newChainModel()
	m.draft = Action{Type: ActionNew}

	m = step(t, m, inputNewDir, "/tmp/foo")
	m = step(t, m, inputNewTitle, "")
	m = step(t, m, inputNewProject, "")
	m = step(t, m, inputNewTags, "")

	if got := m.action; got.Title != "" || got.Project != "" || len(got.Tags) != 0 {
		t.Errorf("expected bare action, got %+v", got)
	}
}

func TestNewChainPrefillsFromActiveFilters(t *testing.T) {
	m := newChainModel()
	m.projects, m.projIdx = []string{"wallfacer"}, 0
	m.tags, m.tagIdx = []string{"go"}, 0
	m.draft = Action{Type: ActionNew}

	m = step(t, m, inputNewDir, "/tmp/foo")
	m = step(t, m, inputNewTitle, "")
	if got := m.input.Value(); got != "wallfacer" {
		t.Errorf("project prefill = %q, want the active project filter", got)
	}
	m = step(t, m, inputNewProject, m.input.Value())
	if got := m.input.Value(); got != "go" {
		t.Errorf("tag prefill = %q, want the active tag filter", got)
	}
}

func TestNewChainEmptyDirAborts(t *testing.T) {
	m := newChainModel()
	m.draft = Action{Type: ActionNew}

	m = step(t, m, inputNewDir, "")
	if m.kind != inputNone {
		t.Errorf("kind = %v, want inputNone", m.kind)
	}
	if m.action.Type != ActionQuit || m.draft.Type != ActionQuit {
		t.Errorf("empty dir should leave no pending new-session action")
	}
}
