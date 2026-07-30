package cursor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/launcher"
	"github.com/pradipta/wallfacer/internal/store"
)

// Sync must not re-parse a chat whose meta.json has not moved: that stat cache
// is what keeps sync fast once an install has hundreds of chats.
func TestSyncIsIncremental(t *testing.T) {
	a := testAdapter(t)
	chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})
	transcript(t, a, "Users-alice-projects-web", "aaa", newFormatTranscript)
	agent.Register(a)

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if first.Scanned != 1 || first.Parsed != 1 {
		t.Fatalf("first sync = %+v, want one scanned and parsed", first)
	}
	second, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if second.Scanned != 1 || second.Parsed != 0 {
		t.Errorf("second sync = %+v, want one scanned and none parsed", second)
	}
}

// cursor-agent cannot adopt a caller-chosen chat ID, so the launcher has to
// recognise the chat it just created from its directory and creation time.
// This stands a script in for the agent, writing a chat the way the real one
// does, and checks the overlay lands on it.
func TestLauncherDetectsNewbornChat(t *testing.T) {
	a := testAdapter(t)
	work := t.TempDir()
	id := "5e0f2dbd-4d7a-4b0e-9f47-2b1d9c2f1a33"
	chatDir := filepath.Join(a.ChatsDir, "ws1", id)

	script := filepath.Join(t.TempDir(), "fake-cursor-agent")
	body := fmt.Sprintf(`#!/bin/sh
mkdir -p %[1]q
now=$(date +%%s)000
printf '{"schemaVersion":1,"createdAtMs":%%s,"hasConversation":true,"title":"Fresh Chat","updatedAtMs":%%s,"cwd":"%[2]s"}' "$now" "$now" > %[1]q/meta.json
printf 'SQLite format 3' > %[1]q/store.db
`, chatDir, work)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	a.Binary = script
	agent.Register(a)

	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := launcher.New(s, a, work, launcher.Overlay{Title: "cursor smoke", Tags: []string{"smoke"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitErr != nil {
		t.Fatalf("stand-in agent failed: %v", res.ExitErr)
	}
	if res.SessionID != id {
		t.Fatalf("detected session %q, want %q", res.SessionID, id)
	}
	sess, err := s.Get(res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Title != "cursor smoke" {
		t.Errorf("Title = %q, the overlay did not land", sess.Title)
	}
	if sess.AutoTitle != "Fresh Chat" {
		t.Errorf("AutoTitle = %q, want the agent's own title", sess.AutoTitle)
	}
	if sess.Dir != work {
		t.Errorf("Dir = %q, want %q", sess.Dir, work)
	}
}
