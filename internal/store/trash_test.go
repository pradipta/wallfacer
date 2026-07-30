package store_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pradipta/wallfacer/internal/agent"
	"github.com/pradipta/wallfacer/internal/agent/claudecode"
	"github.com/pradipta/wallfacer/internal/store"
)

// multiFileAdapter stands in for an agent like kiro-cli, whose sessions span a
// transcript plus a sidecar, a history file and a scratch directory. It reports
// no session files of its own so it stays inert during other tests' Sync calls.
type multiFileAdapter struct{}

func (multiFileAdapter) Type() string                                   { return "multi-file" }
func (multiFileAdapter) ListSessionFiles() ([]agent.SessionFile, error) { return nil, nil }
func (multiFileAdapter) ParseMetadata(string) (*agent.Metadata, error) {
	return &agent.Metadata{}, nil
}
func (multiFileAdapter) SupportsSessionID() bool                { return false }
func (multiFileAdapter) NewSessionCmd(string, string) *exec.Cmd { return exec.Command("true") }
func (multiFileAdapter) ResumeCmd(string, string) *exec.Cmd     { return exec.Command("true") }
func (multiFileAdapter) CompanionFiles(path string) []string {
	stem := strings.TrimSuffix(path, ".jsonl")
	return []string{stem + ".json", stem + ".history", stem}
}

// sessionFileSet writes a transcript with its companions and returns the
// transcript's path.
func sessionFileSet(t *testing.T, dir, id string) string {
	t.Helper()
	stem := filepath.Join(dir, id)
	transcript := stem + ".jsonl"
	for path, content := range map[string]string{
		transcript:          "transcript",
		stem + ".json":      `{"cwd":"/home/x/proj"}`,
		stem + ".history":   "prompt history",
		stem + ".unrelated": "not a companion",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(stem, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stem, "tasks", "t1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return transcript
}

func TestTrashAndPurgeMoveCompanionFiles(t *testing.T) {
	agent.Register(multiFileAdapter{})
	s := openTestStore(t)

	srcDir := t.TempDir()
	transcript := sessionFileSet(t, srcDir, "mmm-111")

	sess := sampleSession("mmm-111")
	sess.AgentType = "multi-file"
	sess.FilePath = transcript
	if err := s.Upsert(sess); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("mmm-111")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Trash(*got); err != nil {
		t.Fatal(err)
	}

	// Every companion left the agent's directory...
	for _, name := range []string{"mmm-111.jsonl", "mmm-111.json", "mmm-111.history", "mmm-111"} {
		if _, err := os.Lstat(filepath.Join(srcDir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should have been moved out of the sessions dir", name)
		}
	}
	// ...but an unrelated sibling the adapter never claimed stayed put.
	if _, err := os.Lstat(filepath.Join(srcDir, "mmm-111.unrelated")); err != nil {
		t.Errorf("unrelated sibling should be untouched: %v", err)
	}
	// ...and landed in the trash, scratch directory contents included.
	for _, name := range []string{"mmm-111.jsonl", "mmm-111.json", "mmm-111.history"} {
		if _, err := os.Stat(filepath.Join(s.TrashDir(), name)); err != nil {
			t.Errorf("%s missing from trash: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.TrashDir(), "mmm-111", "tasks", "t1.json")); err != nil {
		t.Errorf("scratch directory not trashed: %v", err)
	}

	trashed, err := s.ResolveAny("mmm-111")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Purge(*trashed); err != nil {
		t.Fatal(err)
	}
	// Companions are re-derived from the trashed path, so purge clears them too.
	entries, err := os.ReadDir(s.TrashDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("trash should be empty after purge, still holds %v", names)
	}
	if _, err := s.Get("mmm-111"); err != store.ErrNotFound {
		t.Errorf("purge should remove the row, got %v", err)
	}
}

func TestTrashLeavesSiblingsForPlainAdapters(t *testing.T) {
	// claude-code does not implement agent.CompanionFiler; one file in, one
	// file out, exactly as before.
	agent.Register(&claudecode.Adapter{ProjectsDir: t.TempDir()})
	s := openTestStore(t)

	srcDir := t.TempDir()
	transcript := sessionFileSet(t, srcDir, "ccc-111")

	sess := sampleSession("ccc-111")
	sess.FilePath = transcript
	if err := s.Upsert(sess); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("ccc-111")
	if _, err := s.Trash(*got); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(transcript); !os.IsNotExist(err) {
		t.Error("tracked file should still be trashed")
	}
	for _, name := range []string{"ccc-111.json", "ccc-111.history", "ccc-111"} {
		if _, err := os.Lstat(filepath.Join(srcDir, name)); err != nil {
			t.Errorf("%s should be untouched without CompanionFiler: %v", name, err)
		}
	}
	if entries, _ := os.ReadDir(s.TrashDir()); len(entries) != 1 {
		t.Errorf("trash should hold only the tracked file, got %d entries", len(entries))
	}
}
