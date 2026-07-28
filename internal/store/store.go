// Package store persists wallfacer's session index and user overlay data
// (titles, tags, projects) in SQLite. Agent-native session files are never
// modified; this DB can always be rebuilt from disk except for overlay data.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	StatusActive    = "active"
	StatusMissing   = "missing"   // session file disappeared from disk
	StatusTrashed   = "trashed"   // moved to wallfacer's trash dir by `rm`
	StatusSidechain = "sidechain" // agent-internal transcript, indexed only to cache its stat
)

// Session is a row in the sessions table joined with its tags.
type Session struct {
	ID           string
	AgentType    string
	Dir          string
	Title        string // user-set; empty → fall back to AutoTitle
	AutoTitle    string // summary or truncated first prompt
	Project      string
	FirstPrompt  string
	GitBranch    string
	CreatedAt    time.Time
	LastActiveAt time.Time
	FilePath     string
	FileSize     int64
	FileMtime    time.Time
	Status       string
	Tags         []string
}

// DisplayTitle returns the user title if set, else the auto title, else a
// placeholder.
func (s Session) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	if s.AutoTitle != "" {
		return s.AutoTitle
	}
	return "(untitled)"
}

// Filter narrows List results; zero value means no filtering.
type Filter struct {
	AgentType string
	Project   string
	Tag       string
	Dir       string
	Query     string // LIKE match over title, auto_title, first_prompt, dir, project
	// IncludeHidden includes missing/trashed sessions (default: active only).
	IncludeHidden bool
}

type Store struct {
	db *sql.DB
	// dataDir is where the DB (and trash/) live.
	dataDir string
}

// DataDir returns wallfacer's data directory, honoring $XDG_DATA_HOME and
// falling back to ~/.local/share/wallfacer on both macOS and Linux.
func DataDir() (string, error) {
	if env := os.Getenv("WALLFACER_DATA_DIR"); env != "" {
		return env, nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "wallfacer"), nil
}

// Open opens (creating if needed) the wallfacer DB in dataDir.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "wallfacer.db"))
	if err != nil {
		return nil, err
	}
	// modernc sqlite works best with a single writer connection.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error     { return s.db.Close() }
func (s *Store) TrashDir() string { return filepath.Join(s.dataDir, "trash") }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  id             TEXT PRIMARY KEY,
  agent_type     TEXT NOT NULL,
  dir            TEXT NOT NULL DEFAULT '',
  title          TEXT NOT NULL DEFAULT '',
  auto_title     TEXT NOT NULL DEFAULT '',
  project        TEXT NOT NULL DEFAULT '',
  first_prompt   TEXT NOT NULL DEFAULT '',
  git_branch     TEXT NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL DEFAULT 0,
  last_active_at INTEGER NOT NULL DEFAULT 0,
  file_path      TEXT NOT NULL DEFAULT '',
  file_size      INTEGER NOT NULL DEFAULT 0,
  file_mtime     INTEGER NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'active'
);
CREATE TABLE IF NOT EXISTS tags (
  session_id TEXT NOT NULL,
  tag        TEXT NOT NULL,
  PRIMARY KEY (session_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_sessions_last_active ON sessions(last_active_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project);
`)
	return err
}

// Upsert inserts or refreshes a session's disk-derived fields, preserving
// overlay data (title, project, tags) on conflict. An empty Status means
// active.
func (s *Store) Upsert(sess Session) error {
	if sess.Status == "" {
		sess.Status = StatusActive
	}
	_, err := s.db.Exec(`
INSERT INTO sessions (id, agent_type, dir, auto_title, first_prompt, git_branch,
                      created_at, last_active_at, file_path, file_size, file_mtime, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  dir            = excluded.dir,
  auto_title     = excluded.auto_title,
  first_prompt   = excluded.first_prompt,
  git_branch     = excluded.git_branch,
  created_at     = excluded.created_at,
  last_active_at = excluded.last_active_at,
  file_path      = excluded.file_path,
  file_size      = excluded.file_size,
  file_mtime     = excluded.file_mtime,
  status         = excluded.status`,
		sess.ID, sess.AgentType, sess.Dir, sess.AutoTitle, sess.FirstPrompt,
		sess.GitBranch, sess.CreatedAt.Unix(), sess.LastActiveAt.Unix(),
		sess.FilePath, sess.FileSize, sess.FileMtime.UnixNano(), sess.Status)
	return err
}

// FileStat is the cached stat info used to skip unchanged files during sync.
type FileStat struct {
	Size   int64
	Mtime  time.Time
	Status string
}

// KnownFiles returns id → cached stat for one agent type.
func (s *Store) KnownFiles(agentType string) (map[string]FileStat, error) {
	rows, err := s.db.Query(
		`SELECT id, file_size, file_mtime, status FROM sessions WHERE agent_type = ?`, agentType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FileStat{}
	for rows.Next() {
		var id, status string
		var size, mtimeNS int64
		if err := rows.Scan(&id, &size, &mtimeNS, &status); err != nil {
			return nil, err
		}
		out[id] = FileStat{Size: size, Mtime: time.Unix(0, mtimeNS), Status: status}
	}
	return out, rows.Err()
}

// MarkMissing flags sessions whose files vanished outside wallfacer's control.
func (s *Store) MarkMissing(agentType string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, agentType)
	ph := make([]string, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.Exec(fmt.Sprintf(
		`UPDATE sessions SET status = '%s' WHERE agent_type = ? AND status = '%s' AND id IN (%s)`,
		StatusMissing, StatusActive, strings.Join(ph, ",")), args...)
	return err
}

var ErrNotFound = errors.New("session not found")

// ShortID returns the first 8 characters of an ID for display.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Get returns one session by exact ID.
func (s *Store) Get(id string) (*Session, error) {
	list, err := s.list(`WHERE s.id = ?`, []any{id})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// Resolve finds a single active session by ID prefix or (case-insensitive)
// exact title match. Ambiguity is an error listing the candidates.
func (s *Store) Resolve(ref string) (*Session, error) {
	return s.resolve(ref, `s.status = 'active'`)
}

// ResolveAny is Resolve over all user-visible sessions, including missing
// and trashed ones (needed by rm --purge and show).
func (s *Store) ResolveAny(ref string) (*Session, error) {
	return s.resolve(ref, `s.status != 'sidechain'`)
}

func (s *Store) resolve(ref, statusCond string) (*Session, error) {
	list, err := s.list(
		`WHERE `+statusCond+` AND (s.id LIKE ? OR lower(s.title) = lower(?))`,
		[]any{ref + "%", ref})
	if err != nil {
		return nil, err
	}
	switch len(list) {
	case 0:
		return nil, fmt.Errorf("%w: %q", ErrNotFound, ref)
	case 1:
		return &list[0], nil
	default:
		ids := make([]string, len(list))
		for i, x := range list {
			ids[i] = fmt.Sprintf("%s (%s)", ShortID(x.ID), x.DisplayTitle())
		}
		return nil, fmt.Errorf("%q is ambiguous: %s", ref, strings.Join(ids, ", "))
	}
}

// List returns sessions matching the filter, most recently active first.
func (s *Store) List(f Filter) ([]Session, error) {
	var conds []string
	var args []any
	if !f.IncludeHidden {
		conds = append(conds, `s.status = 'active'`)
	} else {
		// Even "hidden" listings never show agent-internal transcripts.
		conds = append(conds, `s.status != 'sidechain'`)
	}
	if f.AgentType != "" {
		conds = append(conds, `s.agent_type = ?`)
		args = append(args, f.AgentType)
	}
	if f.Project != "" {
		conds = append(conds, `s.project = ?`)
		args = append(args, f.Project)
	}
	if f.Dir != "" {
		conds = append(conds, `s.dir = ?`)
		args = append(args, f.Dir)
	}
	if f.Tag != "" {
		conds = append(conds, `s.id IN (SELECT session_id FROM tags WHERE tag = ?)`)
		args = append(args, f.Tag)
	}
	if f.Query != "" {
		q := "%" + f.Query + "%"
		conds = append(conds, `(s.title LIKE ? OR s.auto_title LIKE ? OR s.first_prompt LIKE ?
		   OR s.dir LIKE ? OR s.project LIKE ?
		   OR s.id IN (SELECT session_id FROM tags WHERE tag LIKE ?))`)
		args = append(args, q, q, q, q, q, q)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	return s.list(where, args)
}

func (s *Store) list(where string, args []any) ([]Session, error) {
	rows, err := s.db.Query(`
SELECT s.id, s.agent_type, s.dir, s.title, s.auto_title, s.project, s.first_prompt,
       s.git_branch, s.created_at, s.last_active_at, s.file_path, s.file_size,
       s.file_mtime, s.status,
       COALESCE((SELECT group_concat(tag, ',') FROM
                  (SELECT tag FROM tags WHERE session_id = s.id ORDER BY tag)), '')
FROM sessions s `+where+` ORDER BY s.last_active_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		var created, lastActive, mtimeNS int64
		var tagCSV string
		if err := rows.Scan(&x.ID, &x.AgentType, &x.Dir, &x.Title, &x.AutoTitle,
			&x.Project, &x.FirstPrompt, &x.GitBranch, &created, &lastActive,
			&x.FilePath, &x.FileSize, &mtimeNS, &x.Status, &tagCSV); err != nil {
			return nil, err
		}
		x.CreatedAt = time.Unix(created, 0)
		x.LastActiveAt = time.Unix(lastActive, 0)
		x.FileMtime = time.Unix(0, mtimeNS)
		if tagCSV != "" {
			x.Tags = strings.Split(tagCSV, ",")
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) SetTitle(id, title string) error     { return s.setField(id, "title", title) }
func (s *Store) SetProject(id, project string) error { return s.setField(id, "project", project) }

func (s *Store) SetStatus(id, status string) error { return s.setField(id, "status", status) }

func (s *Store) setField(id, field, value string) error {
	res, err := s.db.Exec(`UPDATE sessions SET `+field+` = ? WHERE id = ?`, value, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddTag(id, tag string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO tags (session_id, tag) VALUES (?, ?)`, id, tag)
	return err
}

func (s *Store) RemoveTag(id, tag string) error {
	_, err := s.db.Exec(`DELETE FROM tags WHERE session_id = ? AND tag = ?`, id, tag)
	return err
}

// Delete removes the DB row and its tags (used with --purge).
func (s *Store) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM tags WHERE session_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}
