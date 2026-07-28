package store

import (
	"fmt"
	"strings"

	"github.com/pradipta-s/wallfacer/internal/agent"
)

// SyncResult summarizes what a sync pass did.
type SyncResult struct {
	Scanned int // session files seen on disk
	Parsed  int // files (re-)parsed because new or changed
	Missing int // previously-known sessions whose files vanished
	Skipped int // sidechain/subagent transcripts ignored
}

// Sync reconciles the DB with on-disk sessions for every registered adapter.
// Files whose (size, mtime) match the cached values are not re-parsed.
func (s *Store) Sync() (SyncResult, error) {
	var res SyncResult
	for _, a := range agent.All() {
		r, err := s.syncAdapter(a)
		res.Scanned += r.Scanned
		res.Parsed += r.Parsed
		res.Missing += r.Missing
		res.Skipped += r.Skipped
		if err != nil {
			return res, fmt.Errorf("sync %s: %w", a.Type(), err)
		}
	}
	return res, nil
}

func (s *Store) syncAdapter(a agent.Adapter) (SyncResult, error) {
	var res SyncResult
	files, err := a.ListSessionFiles()
	if err != nil {
		return res, err
	}
	known, err := s.KnownFiles(a.Type())
	if err != nil {
		return res, err
	}

	seen := map[string]bool{}
	for _, f := range files {
		res.Scanned++
		seen[f.ID] = true
		if k, ok := known[f.ID]; ok && k.Size == f.Size && k.Mtime.Equal(f.Mtime) &&
			k.Status != StatusMissing {
			continue
		}
		md, err := a.ParseMetadata(f.Path)
		if err != nil {
			// Unreadable file: leave any existing row as-is, keep going.
			continue
		}
		res.Parsed++
		status := StatusActive
		if md.Sidechain {
			// Indexed (hidden) so the next sync's stat cache skips it.
			res.Skipped++
			status = StatusSidechain
		}
		if err := s.Upsert(Session{
			ID:           f.ID,
			AgentType:    a.Type(),
			Dir:          md.Dir,
			AutoTitle:    autoTitle(md),
			FirstPrompt:  md.FirstPrompt,
			GitBranch:    md.GitBranch,
			CreatedAt:    md.CreatedAt,
			LastActiveAt: f.Mtime,
			FilePath:     f.Path,
			FileSize:     f.Size,
			FileMtime:    f.Mtime,
			Status:       status,
		}); err != nil {
			return res, err
		}
	}

	// Anything known-active that is no longer on disk went missing.
	var gone []string
	for id, k := range known {
		if !seen[id] && k.Status == StatusActive {
			gone = append(gone, id)
		}
	}
	res.Missing = len(gone)
	return res, s.MarkMissing(a.Type(), gone)
}

const autoTitleLen = 80

// autoTitle prefers the agent-generated summary over the first prompt.
func autoTitle(md *agent.Metadata) string {
	t := md.Summary
	if t == "" {
		t = md.FirstPrompt
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	if len(t) > autoTitleLen {
		t = t[:autoTitleLen] + "…"
	}
	return t
}
