package cursor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/store"
)

// indexed syncs a fixture chat into a throwaway store and returns both.
func indexed(t *testing.T, a *Adapter) (*store.Store, store.Session) {
	t.Helper()
	agent.Register(a)
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.Sync(); err != nil {
		t.Fatal(err)
	}
	sessions, err := s.List(store.Filter{AgentType: a.Type()})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("indexed %d sessions, want 1: %+v", len(sessions), sessions)
	}
	return s, sessions[0]
}

// A real delete has to empty both trees, or `cursor-agent ls` keeps offering a
// chat wallfacer has taken the metadata away from.
func TestTrashSweepsBothTrees(t *testing.T) {
	a := testAdapter(t)
	meta := chat(t, a, "ws1", "aaa", map[string]string{
		metaName:              metaFull,
		storeName:             "db",
		"prompt_history.json": `["x"]`,
	})
	tr := transcript(t, a, "Users-alice-projects-web", "aaa", newFormatTranscript)

	s, sess := indexed(t, a)
	if sess.FilePath != meta {
		t.Fatalf("tracked path = %q, want %q", sess.FilePath, meta)
	}
	if sess.Dir != "/Users/alice/projects/web" || sess.DisplayTitle() != "Login Switch" {
		t.Fatalf("indexed session = %+v", sess)
	}

	if _, err := s.Trash(sess); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Dir(meta), filepath.Dir(tr)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%q survived the trash move", p)
		}
	}
	// The chat directory keeps its (unique) name in the trash; the tracked
	// meta.json lands beside it under its own generic name.
	for _, p := range []string{
		filepath.Join(s.TrashDir(), metaName),
		filepath.Join(s.TrashDir(), "aaa"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %q in the trash: %v", p, err)
		}
	}
}

// Purging a session that was trashed first must not take the trash directory —
// and everything else in it — with it.
func TestPurgeAfterTrashSparesTheTrash(t *testing.T) {
	a := testAdapter(t)
	chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})
	s, sess := indexed(t, a)

	if _, err := s.Trash(sess); err != nil {
		t.Fatal(err)
	}
	// A second session's remains, sharing the trash directory.
	bystander := filepath.Join(s.TrashDir(), "another-session")
	writeFile(t, bystander, "someone else's transcript")

	trashed, err := s.ResolveAny(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(*trashed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.TrashDir()); err != nil {
		t.Fatalf("the trash directory must survive a purge: %v", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Errorf("another session's trashed files were destroyed: %v", err)
	}
}

// Purging directly, without trashing first, removes the whole footprint.
func TestPurgeRemovesBothTrees(t *testing.T) {
	a := testAdapter(t)
	meta := chat(t, a, "ws1", "aaa", map[string]string{metaName: metaFull, storeName: "db"})
	tr := transcript(t, a, "Users-alice-projects-web", "aaa", newFormatTranscript)
	s, sess := indexed(t, a)

	if err := s.Purge(sess); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Dir(meta), filepath.Dir(tr)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%q survived the purge", p)
		}
	}
	if _, err := s.Get(sess.ID); err == nil {
		t.Error("the session row should be gone")
	}
}
