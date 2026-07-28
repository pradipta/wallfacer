// Package tui — detail.go renders the right-hand pane for the highlighted
// session. It shows the same field set as `wallfacer show` so the two views
// never disagree.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pradipta/wallfacer/internal/format"
	"github.com/pradipta/wallfacer/internal/store"
)

// labelWidth is the fixed left column of the label/value block.
const labelWidth = 8

// renderDetail draws the session detail pane at exactly w cells wide and at
// most h lines tall. It is a pure function of the session so it can be tested
// directly.
func renderDetail(s store.Session, w, h int) string {
	// The pane's border and padding cost three cells of the given width.
	inner := w - 3
	if inner < 8 || h < 3 {
		return ""
	}

	var b []string
	add := func(lines ...string) { b = append(b, lines...) }

	add(wrap(detailTitleStyle.Render(s.DisplayTitle()), inner)...)

	if badges := detailBadges(s, inner); badges != "" {
		add("", badges)
	}
	add(rule(inner))

	for _, kv := range [][2]string{
		{"dir", format.CollapseHome(s.Dir)},
		{"branch", s.GitBranch},
		{"agent", s.AgentType},
		{"status", s.Status},
		{"created", s.CreatedAt.Format("2006-01-02 15:04")},
		{"active", s.LastActiveAt.Format("2006-01-02 15:04")},
		{"size", format.Size(s.FileSize)},
		{"id", store.ShortID(s.ID)},
	} {
		if kv[1] == "" {
			continue
		}
		add(field(kv[0], kv[1], inner)...)
	}

	if p := strings.TrimSpace(s.FirstPrompt); p != "" {
		add(rule(inner), detailLabelStyle.Render("first prompt"))
		add(wrap(p, inner)...)
	}

	// Clip to the available height before styling, so the border the pane
	// style draws is exactly as tall as the content we keep.
	if len(b) > h {
		b = b[:h]
	}
	return detailPaneStyle.Height(h).Render(strings.Join(b, "\n"))
}

// field renders one `label  value` pair, wrapping long values under a hanging
// indent so they line up with the value column.
func field(label, value string, inner int) []string {
	valWidth := inner - labelWidth
	if valWidth < 4 {
		// Too narrow for two columns; stack them instead.
		out := []string{detailLabelStyle.Render(ansi.Truncate(label, inner, ""))}
		return append(out, wrap(value, inner)...)
	}
	pad := strings.Repeat(" ", labelWidth-min(len(label), labelWidth-1))
	head := detailLabelStyle.Render(ansi.Truncate(label, labelWidth-1, "")) + pad

	var out []string
	for i, line := range strings.Split(ansi.Wrap(value, valWidth, ""), "\n") {
		if i == 0 {
			out = append(out, head+detailValueStyle.Render(line))
			continue
		}
		out = append(out, strings.Repeat(" ", labelWidth)+detailValueStyle.Render(line))
	}
	return out
}

// detailBadges renders the project and tags on one line, clipped to width.
func detailBadges(s store.Session, inner int) string {
	var parts []string
	if s.Project != "" {
		parts = append(parts, rowProjectStyle.Render("◆ "+s.Project))
	}
	if len(s.Tags) > 0 {
		parts = append(parts, rowTagStyle.Render("#"+strings.Join(s.Tags, " #")))
	}
	if len(parts) == 0 {
		return ""
	}
	return ansi.Truncate(strings.Join(parts, "  "), inner, "…")
}

// wrap soft-wraps text to width and returns it as lines.
func wrap(text string, width int) []string {
	if width < 1 {
		return nil
	}
	return strings.Split(ansi.Wrap(text, width, ""), "\n")
}

func rule(width int) string {
	return detailRuleStyle.Render(strings.Repeat("─", width))
}

// horizontal is a thin alias documenting intent at the one call site that
// glues the list and the detail pane together.
func horizontal(left, right string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
