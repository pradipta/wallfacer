package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The notice arrives asynchronously: Init schedules a command that waits for the
// update check, and the answer lands as a noticeMsg whenever it resolves.
func TestNoticeArrivesAsynchronously(t *testing.T) {
	m := filterModel(t)
	m.awaitNotice = func() string { return "update available: v0.1.0 → v0.2.0" }

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init scheduled no command for the update check")
	}
	msg := cmd() // tea runs this in its own goroutine, so blocking here is free
	got, ok := msg.(noticeMsg)
	if !ok {
		t.Fatalf("Init's command produced %T, want noticeMsg", msg)
	}
	next, _ := m.Update(got)
	if n := next.(model).notice; n != "update available: v0.1.0 → v0.2.0" {
		t.Errorf("notice = %q, want the message", n)
	}
}

// A late answer must not stomp on a prompt or on the result of an action.
func TestLateNoticeYieldsToPromptsAndStatus(t *testing.T) {
	base := filterModel(t)

	busy := map[string]func(model) model{
		"typing":     func(m model) model { m.kind = inputRename; return m },
		"confirming": func(m model) model { m.confirmingDelete = true; return m },
		"picking":    func(m model) model { m.choosingAgent = true; return m },
		"status set": func(m model) model { m.status = "renamed"; return m },
	}
	for name, setup := range busy {
		m := setup(base)
		next, _ := m.Update(noticeMsg("update available: v0.1.0 → v0.2.0"))
		if n := next.(model).notice; n != "" {
			t.Errorf("%s: notice took the footer anyway (%q)", name, n)
		}
	}
}

// The update notice owns the footer on the first frame, and steps aside as soon
// as the user does anything.
func TestUpdateNoticeShowsOnFooterThenClears(t *testing.T) {
	m := filterModel(t)
	m.w, m.h = 120, 30
	m.notice = "update available: v0.1.0 → v0.2.0"

	footer := m.footerView()
	if !strings.Contains(footer, "v0.2.0") {
		t.Fatalf("footer does not show the notice: %q", footer)
	}
	if !strings.Contains(footer, "releases") {
		t.Errorf("footer does not show where to get it: %q", footer)
	}
	// It has to survive composition into the full frame, not just footerView.
	if !strings.Contains(m.View(), "v0.2.0") {
		t.Error("notice missing from the rendered frame")
	}

	after := press(t, m, "j")
	if after.notice != "" {
		t.Error("notice survived a keypress")
	}
	if f := after.footerView(); strings.Contains(f, "v0.2.0") {
		t.Errorf("footer still shows the notice after a keypress: %q", f)
	}
}

// A real status message (the result of an action) outranks the notice.
func TestStatusOutranksUpdateNotice(t *testing.T) {
	m := filterModel(t)
	m.w, m.h = 120, 30
	m.notice = "update available: v0.1.0 → v0.2.0"
	m.status = "renamed"
	if f := m.footerView(); !strings.Contains(f, "renamed") || strings.Contains(f, "v0.2.0") {
		t.Errorf("footer = %q, want the status only", f)
	}
}

// With no notice the footer is the ordinary help line.
func TestFooterWithoutNoticeShowsHelp(t *testing.T) {
	m := filterModel(t)
	m.w, m.h = 120, 30
	if f := m.footerView(); strings.Contains(f, "update available") {
		t.Errorf("footer = %q, want no notice", f)
	}
}

// The footer must not overflow a narrow terminal.
func TestUpdateNoticeFitsNarrowTerminal(t *testing.T) {
	m := filterModel(t)
	m.w, m.h = 40, 20
	m.notice = "update available: v0.1.0 → v0.2.0"
	for _, line := range strings.Split(m.footerView(), "\n") {
		if w := ansi.StringWidth(line); w > m.w {
			t.Errorf("footer line width %d exceeds terminal width %d: %q", w, m.w, line)
		}
	}
}
