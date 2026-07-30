// Package agent defines the adapter interface that each supported coding
// agent (Claude Code, OpenCode, Codex, ...) implements, plus a registry.
package agent

import (
	"os/exec"
	"sort"
	"time"
)

// SessionFile is the cheap, stat-only view of a session on disk, used to
// decide whether a file needs (re-)parsing during sync.
type SessionFile struct {
	ID    string // agent-native session identifier
	Path  string
	Size  int64
	Mtime time.Time
}

// Metadata is what an adapter can extract from a session's own storage.
// User overlay data (title, tags, project) never lives here.
type Metadata struct {
	Dir         string // working directory the session ran in
	FirstPrompt string // first human prompt, truncated
	Summary     string // agent-generated summary, if any
	GitBranch   string
	CreatedAt   time.Time
	// Sidechain marks agent-internal transcripts (e.g. Claude Code
	// subagents) that shouldn't be listed as user sessions.
	Sidechain bool
}

type Adapter interface {
	// Type is the stable identifier stored in the DB, e.g. "claude-code".
	Type() string
	// ListSessionFiles enumerates session files with stat info only.
	ListSessionFiles() ([]SessionFile, error)
	// ParseMetadata reads a single session file and extracts Metadata.
	ParseMetadata(path string) (*Metadata, error)
	// NewSessionCmd returns a command that starts a fresh interactive
	// session in dir. If the adapter supports pre-assigned IDs, id is used;
	// SupportsSessionID reports whether it does.
	NewSessionCmd(dir, id string) *exec.Cmd
	SupportsSessionID() bool
	// ResumeCmd returns a command that resumes session id in dir.
	ResumeCmd(dir, id string) *exec.Cmd
}

// CompanionFiler is an optional interface for adapters whose sessions occupy
// more than one path on disk — sidecar metadata, prompt history, scratch
// directories. wallfacer tracks exactly one file per session, so without this
// the rest of the set would survive `rm` and keep showing up in the agent's
// own session listings.
type CompanionFiler interface {
	// CompanionFiles returns sibling paths — files or directories — that
	// belong to the same session as the tracked file at path. Paths that do
	// not exist may be returned; callers treat the whole set as best-effort.
	CompanionFiles(path string) []string
}

var registry = map[string]Adapter{}

func Register(a Adapter) { registry[a.Type()] = a }

func Get(agentType string) (Adapter, bool) {
	a, ok := registry[agentType]
	return a, ok
}

// All returns registered adapters in stable (type-sorted) order.
func All() []Adapter {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]Adapter, 0, len(types))
	for _, t := range types {
		out = append(out, registry[t])
	}
	return out
}
