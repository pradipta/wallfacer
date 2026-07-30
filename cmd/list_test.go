package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pradipta/wallfacer/internal/store"
)

func TestWriteSessionTableShowsAgent(t *testing.T) {
	sessions := []store.Session{
		{
			ID:           "aaaaaaaa-1111",
			AgentType:    "claude-code",
			Title:        "refactor auth",
			Project:      "api",
			Dir:          "/home/alice/work/api",
			LastActiveAt: time.Now().Add(-2 * time.Hour),
			Status:       store.StatusActive,
			Tags:         []string{"go"},
		},
		{
			ID:           "bbbbbbbb-2222",
			AgentType:    "kiro-cli",
			Title:        "index kiro sessions",
			Dir:          "/home/alice/work/wallfacer",
			LastActiveAt: time.Now().Add(-30 * time.Minute),
			Status:       store.StatusActive,
		},
	}

	var buf bytes.Buffer
	if err := writeSessionTable(&buf, sessions); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines:\n%s", len(lines), buf.String())
	}

	// The agent sits between the title and the project, in both header and rows.
	header := strings.Fields(lines[0])
	want := []string{"ID", "TITLE", "AGENT", "PROJECT", "TAGS", "DIR", "LAST", "ACTIVE"}
	for i := range want {
		if i >= len(header) || header[i] != want[i] {
			t.Fatalf("header = %q, want %q", header, want)
		}
	}
	if !strings.Contains(lines[1], "claude-code") {
		t.Errorf("row 1 missing agent type: %q", lines[1])
	}
	if !strings.Contains(lines[2], "kiro-cli") {
		t.Errorf("row 2 missing agent type: %q", lines[2])
	}
	// Columns must still line up: tabwriter pads to a common width.
	if a, b := strings.Index(lines[1], "claude-code"), strings.Index(lines[2], "kiro-cli"); a != b {
		t.Errorf("agent column misaligned: %d vs %d\n%s", a, b, buf.String())
	}
}

func TestWriteSessionTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSessionTable(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no sessions found") {
		t.Errorf("got %q", buf.String())
	}
}
