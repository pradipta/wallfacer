package format

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestClipIsWidthAware(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
	}{
		{"ascii fits", "hello", 10},
		{"ascii clipped", "hello world", 8},
		{"multibyte fits", "café ☕", 10},
		{"multibyte clipped", "日本語のセッション", 6},
		{"emoji clipped", "🚀🚀🚀🚀🚀", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Clip(tt.in, tt.n)
			if w := ansi.StringWidth(got); w > tt.n {
				t.Errorf("Clip(%q, %d) = %q, width %d exceeds %d", tt.in, tt.n, got, w, tt.n)
			}
			// Truncation must never split a rune.
			for i, r := range got {
				if r == '�' {
					t.Errorf("Clip(%q, %d) = %q: invalid rune at %d", tt.in, tt.n, got, i)
				}
			}
		})
	}
}

func TestClipNonPositive(t *testing.T) {
	if got := Clip("anything", 0); got != "" {
		t.Errorf("Clip(_, 0) = %q, want empty", got)
	}
	if got := Clip("anything", -3); got != "" {
		t.Errorf("Clip(_, -3) = %q, want empty", got)
	}
}

func TestSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{5 * 1024 * 1024, "5.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
	}
	for _, tt := range tests {
		if got := Size(tt.in); got != tt.want {
			t.Errorf("Size(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
