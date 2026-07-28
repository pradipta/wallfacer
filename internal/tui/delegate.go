// Package tui — delegate.go renders one session as a three-line list row.
//
// Each line is budgeted against the pane width independently. That is the
// whole point: the previous implementation appended the project and tag badges
// to the end of the title and let bubbles' DefaultDelegate truncate the result,
// so on any session whose auto-title filled the pane — which is most of them —
// the badges were clipped off the right edge and never seen.
package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pradipta/wallfacer/internal/format"
	"github.com/pradipta/wallfacer/internal/store"
)

const (
	rowHeight  = 3
	rowSpacing = 1
	// rowIndent is the cell cost of the left gutter, which is either two
	// spaces or an accent bar plus one space.
	rowIndent = 2
	// minDirWidth is the narrowest a directory may be squeezed before the
	// trailing metadata is dropped to make room.
	minDirWidth = 10
)

// rowState selects the colour treatment for a row.
type rowState int

const (
	rowNormal rowState = iota
	rowSelected
	rowDimmed
)

type sessionDelegate struct{}

func (sessionDelegate) Height() int                         { return rowHeight }
func (sessionDelegate) Spacing() int                        { return rowSpacing }
func (sessionDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d sessionDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	it, ok := listItem.(item)
	if !ok {
		return
	}
	width := m.Width()
	if width <= 0 {
		return
	}

	// Mirror DefaultDelegate's three states: everything is dimmed while an
	// empty filter is being typed, and selection is not highlighted mid-filter.
	var state rowState
	switch {
	case m.FilterState() == list.Filtering && m.FilterValue() == "":
		state = rowDimmed
	case index == m.Index() && m.FilterState() != list.Filtering:
		state = rowSelected
	}

	var matches []int
	if m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied {
		matches = m.MatchesForItem(index)
	}

	fmt.Fprint(w, strings.Join(renderRow(it.s, width, state, matches), "\n"))
}

// renderRow lays out one session as exactly rowHeight lines, none of which
// exceeds width display cells. It is separated from Render so it can be tested
// without constructing a list.Model.
func renderRow(s store.Session, width int, state rowState, matches []int) []string {
	budget := width - rowIndent
	if budget < 1 {
		budget = 1
	}
	lines := []string{
		titleLine(s, budget, state, matches),
		metaLine(s, budget, state),
		tagLine(s, budget, state),
	}
	wrap := rowPadStyle
	if state == rowSelected {
		wrap = rowBarStyle
	}
	for i, l := range lines {
		lines[i] = wrap.Render(l)
	}
	return lines
}

// titleLine is the status glyph plus the session title, with fuzzy-filter
// matches underlined.
func titleLine(s store.Session, budget int, state rowState, matches []int) string {
	mark, markStyle := glyph(s.Status)
	if state == rowDimmed {
		markStyle = titleDimmedStyle
	}
	// The glyph and its trailing space cost two cells.
	titleBudget := budget - 2
	if titleBudget < 1 {
		return markStyle.Render(mark)
	}
	title := ansi.Truncate(s.DisplayTitle(), titleBudget, "…")

	base := titleNormalStyle
	switch state {
	case rowSelected:
		base = titleSelectedStyle
	case rowDimmed:
		base = titleDimmedStyle
	}

	// item.FilterValue puts the title first, so match indices below the
	// (possibly truncated) title length address the title itself. Anything
	// past that belongs to the project, dir or tags and is not shown here.
	if n := len([]rune(title)); len(matches) > 0 && n > 0 {
		var in []int
		for _, i := range matches {
			if i >= 0 && i < n {
				in = append(in, i)
			}
		}
		if len(in) > 0 {
			unmatched := base.Inline(true)
			return markStyle.Render(mark) + " " +
				lipgloss.StyleRunes(title, in, unmatched.Inherit(matchStyle), unmatched)
		}
	}
	return markStyle.Render(mark) + " " + base.Render(title)
}

// metaLine is `dir · when · agent` on the left with the project badge pushed to
// the right edge. The directory absorbs the slack and is truncated from the
// left, so the leaf stays readable: ~/…/wallfacer beats ~/projects/wall….
func metaLine(s store.Session, budget int, state rowState) string {
	dim, proj := metaStyle, rowProjectStyle
	if state == rowDimmed {
		dim, proj = titleDimmedStyle, titleDimmedStyle
	}

	badge, badgeCost := "", 0
	if s.Project != "" {
		badge = "◆ " + s.Project
		if ansi.StringWidth(badge) > budget/2 {
			badge = ansi.Truncate(badge, budget/2, "…")
		}
		badgeCost = ansi.StringWidth(badge) + 2 // + gap
	}

	left := budget - badgeCost
	if left < 1 {
		return proj.Render(ansi.Truncate(badge, budget, "…"))
	}

	// Shed trailing metadata before starving the directory.
	dir := format.CollapseHome(s.Dir)
	tail := " · " + format.RelTime(s.LastActiveAt) + " · " + s.AgentType
	if left-ansi.StringWidth(tail) < minDirWidth {
		tail = " · " + format.RelTime(s.LastActiveAt)
	}
	if left-ansi.StringWidth(tail) < minDirWidth {
		tail = ""
	}
	dirBudget := left - ansi.StringWidth(tail)
	if dirBudget < 1 {
		dirBudget = 1
	}
	if ansi.StringWidth(dir) > dirBudget {
		dir = "…" + ansi.TruncateLeft(dir, ansi.StringWidth(dir)-dirBudget+1, "")
	}

	text := dim.Render(dir + tail)
	if badge == "" {
		return text
	}
	gap := budget - ansi.StringWidth(dir+tail) - ansi.StringWidth(badge)
	if gap < 1 {
		gap = 1
	}
	return text + strings.Repeat(" ", gap) + proj.Render(badge)
}

// tagLine renders `#a #b`, dropping tags that do not fit and reporting the
// remainder as +N. Sessions without tags get a blank line so row heights — and
// therefore the list's pagination arithmetic — stay uniform.
func tagLine(s store.Session, budget int, state rowState) string {
	if len(s.Tags) == 0 {
		return ""
	}
	style := rowTagStyle
	if state == rowDimmed {
		style = titleDimmedStyle
	}

	var shown []string
	used := 0
	for i, t := range s.Tags {
		chip := "#" + t
		cost := ansi.StringWidth(chip)
		if i > 0 {
			cost++ // separating space
		}
		// Reserve room for the overflow marker unless this is the last tag.
		reserve := 0
		if i < len(s.Tags)-1 {
			reserve = len(fmt.Sprintf(" +%d", len(s.Tags)-i-1))
		}
		if used+cost+reserve > budget {
			break
		}
		used += cost
		shown = append(shown, chip)
	}
	if len(shown) == 0 {
		return style.Render(ansi.Truncate("#"+s.Tags[0], budget, "…"))
	}
	out := strings.Join(shown, " ")
	if n := len(s.Tags) - len(shown); n > 0 {
		out += fmt.Sprintf(" +%d", n)
	}
	return style.Render(out)
}
