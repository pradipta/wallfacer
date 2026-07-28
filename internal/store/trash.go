package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Trash moves the session's file into wallfacer's trash directory and marks
// the row trashed. The original file name is preserved; a numeric suffix is
// added on collision.
func (s *Store) Trash(sess Session) (string, error) {
	trashDir := s.TrashDir()
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(trashDir, filepath.Base(sess.FilePath))
	for i := 1; ; i++ {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			break
		}
		dest = filepath.Join(trashDir, fmt.Sprintf("%s.%d", filepath.Base(sess.FilePath), i))
	}
	if err := moveFile(sess.FilePath, dest); err != nil {
		return "", fmt.Errorf("moving session file to trash: %w", err)
	}
	if err := s.SetStatus(sess.ID, StatusTrashed); err != nil {
		return dest, err
	}
	// Remember where it went so a future restore/purge can find it.
	return dest, s.setField(sess.ID, "file_path", dest)
}

// Purge permanently deletes the session's file (wherever it is) and its row.
func (s *Store) Purge(sess Session) error {
	if sess.FilePath != "" {
		if err := os.Remove(sess.FilePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return s.Delete(sess.ID)
}

// moveFile renames, falling back to copy+remove across filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
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
