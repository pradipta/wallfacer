package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

// completionTree lays out a few directories (and one regular file, which must
// never be offered) to complete against.
func completionTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"alpha", "alfredo", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "alpine.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func completionModel(t *testing.T, value string) model {
	t.Helper()
	m := model{input: textinput.New(), kind: inputNewDir, projIdx: -1, tagIdx: -1}
	m.input.SetValue(value)
	return m
}

// tab drives one press of the completion key.
func tab(t *testing.T, m model) model {
	t.Helper()
	return m.completeDir()
}

func TestCompleteDirUniqueMatch(t *testing.T) {
	root := completionTree(t)
	m := tab(t, completionModel(t, filepath.Join(root, "be")))

	if want := filepath.Join(root, "beta") + "/"; m.input.Value() != want {
		t.Errorf("value = %q, want %q", m.input.Value(), want)
	}
	if len(m.compl) != 0 {
		t.Errorf("unique match should leave nothing to cycle, got %q", m.compl)
	}
}

func TestCompleteDirFillsCommonPrefixThenCycles(t *testing.T) {
	root := completionTree(t)
	m := tab(t, completionModel(t, filepath.Join(root, "a")))

	if want := filepath.Join(root, "al"); m.input.Value() != want {
		t.Fatalf("first tab = %q, want the common prefix %q", m.input.Value(), want)
	}
	if len(m.compl) != 2 {
		t.Fatalf("candidates = %q, want alfredo and alpha", m.compl)
	}

	m = tab(t, m)
	if want := filepath.Join(root, "alfredo") + "/"; m.input.Value() != want {
		t.Errorf("second tab = %q, want %q", m.input.Value(), want)
	}
	m = tab(t, m)
	if want := filepath.Join(root, "alpha") + "/"; m.input.Value() != want {
		t.Errorf("third tab = %q, want %q", m.input.Value(), want)
	}
	// The cycle wraps rather than dead-ending on the last candidate.
	m = tab(t, m)
	if want := filepath.Join(root, "alfredo") + "/"; m.input.Value() != want {
		t.Errorf("fourth tab = %q, want the cycle to wrap to %q", m.input.Value(), want)
	}
}

func TestCompleteDirSkipsFilesAndHidden(t *testing.T) {
	root := completionTree(t)
	got := dirCandidates(root + "/")
	want := []string{root + "/alfredo/", root + "/alpha/", root + "/beta/"}
	if len(got) != len(want) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidates = %q, want %q", got, want)
		}
	}
	// A dot is an explicit request for the hidden entries.
	if hidden := dirCandidates(root + "/."); len(hidden) != 1 || hidden[0] != root+"/.hidden/" {
		t.Errorf("dot prefix = %q, want just .hidden", hidden)
	}
}

func TestCompleteDirNoMatchLeavesInputAlone(t *testing.T) {
	root := completionTree(t)
	value := filepath.Join(root, "zzz")
	m := tab(t, completionModel(t, value))
	if m.input.Value() != value {
		t.Errorf("value = %q, want it untouched", m.input.Value())
	}
}

func TestCompleteDirExpandsHomeButKeepsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := tab(t, completionModel(t, "~/pro"))
	if got := m.input.Value(); got != "~/projects/" {
		t.Errorf("value = %q, want ~/projects/", got)
	}
}
