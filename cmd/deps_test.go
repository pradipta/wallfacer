package cmd

import (
	"testing"

	"github.com/pradipta/wallfacer/internal/agent"
)

// The registry is what sync, the TUI picker and --agent all read, so the set
// of registered adapters is worth asserting directly.
func TestRegisteredAdapters(t *testing.T) {
	want := []string{"claude-code", "cursor-agent", "kiro-cli"}
	all := agent.All()
	if len(all) != len(want) {
		t.Fatalf("registered %d adapters, want %d", len(all), len(want))
	}
	for i, tp := range want {
		if got := all[i].Type(); got != tp {
			t.Errorf("adapter %d = %q, want %q", i, got, tp)
		}
		if _, ok := agent.Get(tp); !ok {
			t.Errorf("agent.Get(%q) found nothing", tp)
		}
	}
	if _, ok := agent.Get(defaultAgentType); !ok {
		t.Errorf("the default agent %q is not registered", defaultAgentType)
	}
}
