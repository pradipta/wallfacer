package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/pradipta/wallfacer/internal/store"
)

// filterModel builds a browser over two sessions from different agents, one of
// which carries a project and a tag.
func filterModel(t *testing.T) model {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	for _, x := range []store.Session{
		{
			ID: "aaa-111", AgentType: "claude-code", Dir: "/w/api",
			AutoTitle: "refactor auth", LastActiveAt: time.Now().Add(-time.Hour),
		},
		{
			ID: "bbb-222", AgentType: "kiro-cli", Dir: "/w/wallfacer",
			AutoTitle: "index kiro sessions", LastActiveAt: time.Now(),
		},
	} {
		if err := s.Upsert(x); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetProject("aaa-111", "api"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTag("aaa-111", "go"); err != nil {
		t.Fatal(err)
	}

	m, err := newModel(s)
	if err != nil {
		t.Fatal(err)
	}
	// The launch splash swallows the first keystroke; tests drive the browser.
	m.splash = false
	return m
}

func TestAgentFilterCycles(t *testing.T) {
	m := filterModel(t)
	if len(m.agents) != 2 || m.agents[0] != "claude-code" || m.agents[1] != "kiro-cli" {
		t.Fatalf("agents = %q, want the types present in the index, sorted", m.agents)
	}
	if len(m.list.Items()) != 2 {
		t.Fatalf("unfiltered list has %d items, want 2", len(m.list.Items()))
	}

	m = press(t, m, "A")
	if got := m.activeAgent(); got != "claude-code" {
		t.Fatalf("first A = %q", got)
	}
	if items := m.list.Items(); len(items) != 1 || items[0].(item).s.ID != "aaa-111" {
		t.Errorf("claude-code filter: %v", items)
	}
	m.w = 80
	if header := m.headerView(); !strings.Contains(header, "claude-code") {
		t.Errorf("header should chip the active agent, got %q", header)
	}

	m = press(t, m, "A")
	if got := m.activeAgent(); got != "kiro-cli" {
		t.Fatalf("second A = %q", got)
	}
	if items := m.list.Items(); len(items) != 1 || items[0].(item).s.ID != "bbb-222" {
		t.Errorf("kiro-cli filter: %v", items)
	}

	// A third press wraps back to unfiltered, like P and T do.
	m = press(t, m, "A")
	if got := m.activeAgent(); got != "" {
		t.Fatalf("third A should clear the filter, got %q", got)
	}
	if len(m.list.Items()) != 2 {
		t.Errorf("list should be unfiltered again, got %d items", len(m.list.Items()))
	}
}

func TestClearFiltersIncludesAgent(t *testing.T) {
	m := filterModel(t)
	m = press(t, m, "A")
	m = press(t, m, "P")
	m = press(t, m, "T")
	if m.activeAgent() == "" || m.activeProject() == "" || m.activeTag() == "" {
		t.Fatalf("expected three active filters, got agent=%q project=%q tag=%q",
			m.activeAgent(), m.activeProject(), m.activeTag())
	}

	m = press(t, m, "x")
	if m.activeAgent() != "" || m.activeProject() != "" || m.activeTag() != "" {
		t.Errorf("x should clear all three: agent=%q project=%q tag=%q",
			m.activeAgent(), m.activeProject(), m.activeTag())
	}
	if len(m.list.Items()) != 2 {
		t.Errorf("list should be unfiltered after x, got %d items", len(m.list.Items()))
	}
}

func TestAgentFilterSurvivesReload(t *testing.T) {
	m := filterModel(t)
	m = press(t, m, "A")
	want := m.activeAgent()
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if got := m.activeAgent(); got != want {
		t.Errorf("agent filter after reload = %q, want %q", got, want)
	}
}
func TestFilterValueIncludesAgentType(t *testing.T) {
	m := filterModel(t)

	var got string
	for _, li := range m.list.Items() {
		it := li.(item)
		if it.s.ID != "bbb-222" {
			continue
		}
		got = it.FilterValue()
		if !strings.HasPrefix(got, it.s.DisplayTitle()) {
			t.Fatalf("filter value should start with the title %q, got %q", it.s.DisplayTitle(), got)
		}
		fields := strings.Fields(got)
		if len(fields) == 0 || fields[len(fields)-1] != it.s.AgentType {
			t.Fatalf("filter value should append agent type %q at the end, got %q", it.s.AgentType, got)
		}
		break
	}

	if got == "" {
		t.Fatal("did not find the kiro-cli fixture session")
	}
}
