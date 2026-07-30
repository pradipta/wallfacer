package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// withAgents substitutes the launchable agent list for one test.
func withAgents(t *testing.T, types ...string) {
	t.Helper()
	prev := availableAgents
	availableAgents = func() []string { return types }
	t.Cleanup(func() { availableAgents = prev })
}

// keyMsg builds the KeyMsg for a single keystroke or named key.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// press feeds one key into the model and returns the updated model.
func press(t *testing.T, m model, k string) model {
	t.Helper()
	next, _ := m.Update(keyMsg(k))
	return next.(model)
}

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

// chooserModel is a model whose picker is already open over the given agents.
func chooserModel(agents ...string) model {
	m := newChainModel()
	m.draft = Action{Type: ActionNew}
	m.agentChoices = agents
	m.agentChoice = max(indexOf(agents, defaultAgentType), 0)
	m.choosingAgent = true
	return m
}

func TestAgentChooserDefaultsToClaudeCode(t *testing.T) {
	m := chooserModel("claude-code", "kiro-cli")
	if got := m.agentChoices[m.agentChoice]; got != "claude-code" {
		t.Fatalf("preselected %q, want claude-code", got)
	}
	m = press(t, m, "enter")
	if m.choosingAgent {
		t.Error("enter should close the picker")
	}
	if m.kind != inputNewDir {
		t.Errorf("kind = %v, want inputNewDir right after the agent step", m.kind)
	}
	if m.draft.Agent != "claude-code" {
		t.Errorf("draft agent = %q", m.draft.Agent)
	}
}

func TestAgentChooserMovesAndWraps(t *testing.T) {
	m := chooserModel("claude-code", "kiro-cli")
	m = press(t, m, "right")
	if got := m.agentChoices[m.agentChoice]; got != "kiro-cli" {
		t.Errorf("after right: %q", got)
	}
	m = press(t, m, "right")
	if got := m.agentChoices[m.agentChoice]; got != "claude-code" {
		t.Errorf("right should wrap around, got %q", got)
	}
	m = press(t, m, "left")
	if got := m.agentChoices[m.agentChoice]; got != "kiro-cli" {
		t.Errorf("left should wrap backwards, got %q", got)
	}
	// The chooser owns the footer line while it is up.
	m.w = 80
	if view := m.footerView(); !strings.Contains(view, "kiro-cli") || !strings.Contains(view, "agent") {
		t.Errorf("footer should show the picker, got %q", view)
	}
}

func TestAgentChooserDigitSelects(t *testing.T) {
	m := chooserModel("claude-code", "kiro-cli")
	m = press(t, m, "2")
	if m.choosingAgent {
		t.Error("a digit should confirm immediately")
	}
	if m.draft.Agent != "kiro-cli" {
		t.Errorf("draft agent = %q, want kiro-cli", m.draft.Agent)
	}
	if m.kind != inputNewDir {
		t.Errorf("kind = %v, want inputNewDir", m.kind)
	}
	// Out-of-range digits are ignored rather than picking nothing.
	m2 := press(t, chooserModel("claude-code", "kiro-cli"), "9")
	if !m2.choosingAgent {
		t.Error("digit beyond the choices should leave the picker open")
	}
}

func TestAgentChooserEscAbandonsFlow(t *testing.T) {
	m := press(t, chooserModel("claude-code", "kiro-cli"), "esc")
	if m.choosingAgent {
		t.Error("esc should close the picker")
	}
	if m.kind != inputNone {
		t.Errorf("esc should not open the dir prompt, kind = %v", m.kind)
	}
	if m.draft.Type != ActionQuit || m.action.Type != ActionQuit {
		t.Error("esc should leave no pending new-session action")
	}
}

func TestStartNewSessionSkipsPickerForSingleAgent(t *testing.T) {
	withAgents(t, "only-agent")

	m := newChainModel()
	m.draft = Action{Type: ActionNew}
	m = m.startNewSession()

	if m.choosingAgent {
		t.Error("a single registered adapter needs no picker")
	}
	if m.kind != inputNewDir {
		t.Errorf("kind = %v, want inputNewDir", m.kind)
	}
	if m.draft.Agent != "only-agent" {
		t.Errorf("draft agent = %q, want the only registered adapter", m.draft.Agent)
	}
}

func TestStartNewSessionOpensPickerForSeveralAgents(t *testing.T) {
	withAgents(t, "claude-code", "kiro-cli")

	m := newChainModel()
	m.draft = Action{Type: ActionNew}
	m = m.startNewSession()

	if !m.choosingAgent {
		t.Fatal("two adapters should open the picker")
	}
	if got := m.agentChoices[m.agentChoice]; got != "claude-code" {
		t.Errorf("preselected %q, want the default agent", got)
	}
}

// With three adapters registered the picker still preselects the default, and
// its digit shortcuts and wrap-around track the longer list.
func TestAgentChooserWithThreeAgents(t *testing.T) {
	withAgents(t, "claude-code", "cursor-agent", "kiro-cli")

	m := newChainModel()
	m.draft = Action{Type: ActionNew}
	m = m.startNewSession()

	if len(m.agentChoices) != 3 {
		t.Fatalf("agentChoices = %q, want all three adapters", m.agentChoices)
	}
	if got := m.agentChoices[m.agentChoice]; got != "claude-code" {
		t.Errorf("preselected %q, want the default agent", got)
	}

	if got := press(t, m, "2").draft.Agent; got != "cursor-agent" {
		t.Errorf("digit 2 chose %q, want cursor-agent", got)
	}
	if got := press(t, m, "3").draft.Agent; got != "kiro-cli" {
		t.Errorf("digit 3 chose %q, want kiro-cli", got)
	}

	back := press(t, m, "left")
	if got := back.agentChoices[back.agentChoice]; got != "kiro-cli" {
		t.Errorf("left should wrap to the last of three, got %q", got)
	}
	back.w = 100
	view := back.footerView()
	for _, want := range []string{"claude-code", "cursor-agent", "kiro-cli"} {
		if !strings.Contains(view, want) {
			t.Errorf("footer %q is missing %s", view, want)
		}
	}
}

func TestNewChainCarriesAgentThrough(t *testing.T) {
	m := chooserModel("claude-code", "kiro-cli")
	m = press(t, m, "2")
	m = step(t, m, inputNewDir, "/tmp/foo")
	m = step(t, m, inputNewTitle, "index kiro sessions")
	m = step(t, m, inputNewProject, "wallfacer")
	m = step(t, m, inputNewTags, "go")

	if got := m.action; got.Agent != "kiro-cli" || got.Dir != "/tmp/foo" ||
		got.Title != "index kiro sessions" || got.Project != "wallfacer" {
		t.Errorf("action = %+v", got)
	}
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
