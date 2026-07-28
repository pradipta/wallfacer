package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dirCandidates lists the directories that could complete value, as full
// replacement values (the typed parent plus the entry name plus a trailing
// separator, so the next tab descends into it). "~" is expanded for the lookup
// but kept in the returned values, so the prompt shows what you typed.
//
// Hidden directories are offered only once the prefix itself starts with a dot,
// which keeps a bare tab in a home directory readable.
func dirCandidates(value string) []string {
	parent, prefix := splitDirPrefix(value)
	search := expandHome(parent)
	if search == "" {
		search = "."
	}
	entries, err := os.ReadDir(search)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !isDirEntry(search, e) {
			continue
		}
		out = append(out, parent+name+"/")
	}
	sort.Strings(out)
	return out
}

// splitDirPrefix cuts value at the last separator into the parent it names
// (separator included, so joining is plain concatenation) and the partial entry
// name being typed.
func splitDirPrefix(value string) (parent, prefix string) {
	if i := strings.LastIndex(value, "/"); i >= 0 {
		return value[:i+1], value[i+1:]
	}
	return "", value
}

// isDirEntry reports whether e is a directory, following symlinks so linked
// checkouts complete like real directories.
func isDirEntry(dir string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// commonPrefix is the longest string every candidate starts with, which is how
// far an unambiguous completion can go.
func commonPrefix(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	out := xs[0]
	for _, x := range xs[1:] {
		for !strings.HasPrefix(x, out) {
			out = out[:len(out)-1]
			if out == "" {
				return ""
			}
		}
	}
	return out
}
