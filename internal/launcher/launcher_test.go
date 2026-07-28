package launcher_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pradipta-s/wallfacer/internal/agent"
	"github.com/pradipta-s/wallfacer/internal/launcher"
	"github.com/pradipta-s/wallfacer/internal/store"
)

// fakeAdapter simulates an agent CLI: "launching" writes a session file into
// sessionsDir, either named after the pre-assigned id or a fixed name when
// the adapter doesn't support pre-assigned ids. The file content is the cwd.
type fakeAdapter struct {
	sessionsDir string
	supportsID  bool
}

func (f *fakeAdapter) Type() string            { return "fake" }
func (f *fakeAdapter) SupportsSessionID() bool { return f.supportsID }

func (f *fakeAdapter) ListSessionFiles() ([]agent.SessionFile, error) {
	entries, err := os.ReadDir(f.sessionsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []agent.SessionFile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, _ := e.Info()
		out = append(out, agent.SessionFile{
			ID:    strings.TrimSuffix(e.Name(), ".jsonl"),
			Path:  filepath.Join(f.sessionsDir, e.Name()),
			Size:  info.Size(),
			Mtime: info.ModTime(),
		})
	}
	return out, nil
}

func (f *fakeAdapter) ParseMetadata(path string) (*agent.Metadata, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &agent.Metadata{
		Dir:         strings.TrimSpace(string(b)),
		FirstPrompt: "fake prompt",
		CreatedAt:   info.ModTime(),
	}, nil
}

func (f *fakeAdapter) NewSessionCmd(dir, id string) *exec.Cmd {
	name := id
	if name == "" {
		name = "unassigned-xyz"
	}
	cmd := exec.Command("sh", "-c", "pwd > "+filepath.Join(f.sessionsDir, name+".jsonl"))
	cmd.Dir = dir
	return cmd
}

func (f *fakeAdapter) ResumeCmd(dir, id string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", "touch "+filepath.Join(f.sessionsDir, id+".jsonl"))
	cmd.Dir = dir
	return cmd
}

func setup(t *testing.T, supportsID bool) (*store.Store, *fakeAdapter, string) {
	t.Helper()
	fake := &fakeAdapter{sessionsDir: t.TempDir(), supportsID: supportsID}
	agent.Register(fake)
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	workDir, err := filepath.EvalSymlinks(t.TempDir()) // macOS /var → /private/var
	if err != nil {
		t.Fatal(err)
	}
	return s, fake, workDir
}

func TestNewWithPreassignedID(t *testing.T) {
	s, fake, workDir := setup(t, true)

	res, err := launcher.New(s, fake, workDir, launcher.Overlay{
		Title: "My Task", Project: "wallfacer", Tags: []string{"test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID == "" || res.ExitErr != nil {
		t.Fatalf("result: %+v", res)
	}
	sess, err := s.Get(res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Dir != workDir {
		t.Errorf("Dir = %q, want %q", sess.Dir, workDir)
	}
	if sess.Title != "My Task" || sess.Project != "wallfacer" ||
		len(sess.Tags) != 1 || sess.Tags[0] != "test" {
		t.Errorf("overlay not applied: %+v", sess)
	}
}

func TestNewFallbackDetection(t *testing.T) {
	s, fake, workDir := setup(t, false)

	res, err := launcher.New(s, fake, workDir, launcher.Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionID != "unassigned-xyz" {
		t.Fatalf("fallback should find the new session by dir+time, got %q", res.SessionID)
	}
}

func TestResume(t *testing.T) {
	s, fake, workDir := setup(t, true)
	res, err := launcher.New(s, fake, workDir, launcher.Overlay{})
	if err != nil || res.SessionID == "" {
		t.Fatal(err, res)
	}

	sess, _ := s.Get(res.SessionID)
	before := sess.LastActiveAt
	time.Sleep(1100 * time.Millisecond) // mtime + last_active_at have 1s resolution

	rres, err := launcher.Resume(s, *sess)
	if err != nil || rres.ExitErr != nil {
		t.Fatal(err, rres)
	}
	after, _ := s.Get(res.SessionID)
	if !after.LastActiveAt.After(before) {
		t.Errorf("resume should bump last_active_at (%v → %v)", before, after.LastActiveAt)
	}
}

func TestResumeMissingDir(t *testing.T) {
	s, fake, workDir := setup(t, true)
	res, err := launcher.New(s, fake, workDir, launcher.Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	sess, _ := s.Get(res.SessionID)
	sess.Dir = filepath.Join(workDir, "gone")
	if _, err := launcher.Resume(s, *sess); err == nil {
		t.Error("resuming into a missing directory should error")
	}
}
