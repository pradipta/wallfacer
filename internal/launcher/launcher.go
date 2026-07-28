// Package launcher starts and resumes interactive agent sessions, handing
// the terminal to the agent process and reconciling the index afterward.
package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/pradipta-s/wallfacer/internal/agent"
	"github.com/pradipta-s/wallfacer/internal/store"
)

// Overlay is metadata applied to a newly created session after it exits.
type Overlay struct {
	Title   string
	Project string
	Tags    []string
}

// Result reports what happened after an interactive run.
type Result struct {
	SessionID string // empty if no session file was produced (e.g. instant quit)
	ExitErr   error  // the agent's exit error, if any (session may still exist)
}

// New launches a fresh interactive session in dir and indexes it afterward.
func New(s *store.Store, a agent.Adapter, dir string, ov Overlay) (Result, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("not a directory: %s", dir)
	}

	id := ""
	if a.SupportsSessionID() {
		id = uuid.NewString()
	}
	started := time.Now()
	runErr := runInteractive(a.NewSessionCmd(dir, id))

	// Index whatever the run produced, even if the agent exited non-zero.
	if _, err := s.Sync(); err != nil {
		return Result{ExitErr: runErr}, err
	}
	sessionID, err := findNewSession(s, a, id, dir, started)
	if err != nil {
		return Result{ExitErr: runErr}, err
	}
	if sessionID != "" {
		applyOverlay(s, sessionID, ov)
	}
	return Result{SessionID: sessionID, ExitErr: runErr}, nil
}

// Resume reopens an existing session in its original directory.
func Resume(s *store.Store, sess store.Session) (Result, error) {
	a, ok := agent.Get(sess.AgentType)
	if !ok {
		return Result{}, fmt.Errorf("no adapter registered for agent type %q", sess.AgentType)
	}
	dir := sess.Dir
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Result{}, fmt.Errorf("session directory no longer exists: %s", dir)
	}
	runErr := runInteractive(a.ResumeCmd(dir, sess.ID))
	if _, err := s.Sync(); err != nil {
		return Result{SessionID: sess.ID, ExitErr: runErr}, err
	}
	return Result{SessionID: sess.ID, ExitErr: runErr}, nil
}

// runInteractive hands the terminal to the agent process. Interrupts are
// ignored by wallfacer while the child runs — Ctrl+C must reach the agent,
// not kill the wrapper around it.
func runInteractive(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	signal.Ignore(os.Interrupt, syscall.SIGTERM)
	defer signal.Reset(os.Interrupt, syscall.SIGTERM)
	if errors.Is(cmd.Err, exec.ErrNotFound) {
		return fmt.Errorf("agent binary not found: %s", cmd.Path)
	}
	return cmd.Run()
}

// findNewSession locates the session the run created: by the pre-assigned ID
// when honored, otherwise the most recent session in dir created since the
// launch (covers agents/versions without pre-assigned ID support).
func findNewSession(s *store.Store, a agent.Adapter, id, dir string, started time.Time) (string, error) {
	if id != "" {
		if _, err := s.Get(id); err == nil {
			return id, nil
		}
	}
	sessions, err := s.List(store.Filter{AgentType: a.Type(), Dir: dir})
	if err != nil {
		return "", err
	}
	for _, x := range sessions { // newest first
		if x.CreatedAt.After(started.Add(-5 * time.Second)) {
			return x.ID, nil
		}
	}
	return "", nil
}

func applyOverlay(s *store.Store, id string, ov Overlay) {
	if ov.Title != "" {
		s.SetTitle(id, ov.Title)
	}
	if ov.Project != "" {
		s.SetProject(id, ov.Project)
	}
	for _, t := range ov.Tags {
		if t != "" {
			s.AddTag(id, t)
		}
	}
}
