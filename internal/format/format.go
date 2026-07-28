// Package format holds tiny display helpers shared by the CLI and TUI.
package format

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// RelTime renders a compact human-relative timestamp.
func RelTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// CollapseHome shortens the user's home directory prefix to ~.
func CollapseHome(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}

// Clip truncates s to at most n characters with an ellipsis.
func Clip(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}
