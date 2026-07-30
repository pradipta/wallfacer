package cmd

import (
	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/agent/claudecode"
	"github.com/pradipta/wallfacer/internal/agent/cursor"
	"github.com/pradipta/wallfacer/internal/agent/kirocli"
	"github.com/pradipta/wallfacer/internal/store"
)

func init() {
	agent.Register(claudecode.New())
	agent.Register(cursor.New())
	agent.Register(kirocli.New())
}

// defaultAgentType is what `new` launches, and what the browser falls back to,
// when no agent is named. A configurable preferred agent would replace this.
const defaultAgentType = "claude-code"

// openStore opens the wallfacer DB in the standard data directory.
// Callers must Close() it.
func openStore() (*store.Store, error) {
	dir, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	return store.Open(dir)
}
