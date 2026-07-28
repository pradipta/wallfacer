package cmd

import (
	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/agent/claudecode"
	"github.com/pradipta/wallfacer/internal/store"
)

func init() {
	agent.Register(claudecode.New())
}

// openStore opens the wallfacer DB in the standard data directory.
// Callers must Close() it.
func openStore() (*store.Store, error) {
	dir, err := store.DataDir()
	if err != nil {
		return nil, err
	}
	return store.Open(dir)
}
