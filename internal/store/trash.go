package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pradipta/wallfacer/internal/agent"
)

// Trash moves the session's file into wallfacer's trash directory and marks
// the row trashed. The original file name is preserved; a numeric suffix is
// added on collision.
//
// Agents that spread a session over several paths (see agent.CompanionFiler)
// get the whole set moved. Only the tracked file is load-bearing: companions
// that are missing, or directories that cannot be renamed across filesystems,
// are skipped rather than failing the delete.
func (s *Store) Trash(sess Session) (string, error) {
	trashDir := s.TrashDir()
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return "", err
	}
	companions := companionPaths(sess)

	dest := freeName(trashDir, sess.FilePath)
	if err := movePath(sess.FilePath, dest); err != nil {
		return "", fmt.Errorf("moving session file to trash: %w", err)
	}
	for _, p := range companions {
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		_ = movePath(p, freeName(trashDir, p))
	}

	if err := s.SetStatus(sess.ID, StatusTrashed); err != nil {
		return dest, err
	}
	// Remember where it went so a future restore/purge can find it.
	return dest, s.setField(sess.ID, "file_path", dest)
}

// Purge permanently deletes the session's files (wherever they are) and its
// row.
func (s *Store) Purge(sess Session) error {
	for _, p := range companionPaths(sess) {
		_ = os.RemoveAll(p)
	}
	if sess.FilePath != "" {
		if err := os.RemoveAll(sess.FilePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return s.Delete(sess.ID)
}

// companionPaths asks the session's adapter for the other paths belonging to
// it. They are derived from the session's *current* FilePath, which is also
// correct once a session sits in the trash, because Trash keeps basenames. The
// exception is a name collision that forced a numeric suffix: the companions
// then simply are not found, and are skipped.
func companionPaths(sess Session) []string {
	a, ok := agent.Get(sess.AgentType)
	if !ok {
		return nil
	}
	cf, ok := a.(agent.CompanionFiler)
	if !ok {
		return nil
	}
	var out []string
	for _, p := range cf.CompanionFiles(sess.FilePath) {
		// Never treat the tracked file itself as a companion: it is moved
		// separately, and moving it twice would lose it.
		if p != "" && p != sess.FilePath {
			out = append(out, p)
		}
	}
	return out
}

// freeName returns a path inside dir named after src, suffixed if taken.
func freeName(dir, src string) string {
	base := filepath.Base(src)
	dest := filepath.Join(dir, base)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			return dest
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s.%d", base, i))
	}
}

// movePath renames, falling back to copy+remove across filesystems. The
// fallback only covers regular files; a directory that cannot be renamed is
// reported as an error for the caller to decide about.
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("cannot move %s across filesystems", src)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
