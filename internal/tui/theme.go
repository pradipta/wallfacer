// Package tui — theme.go holds the colour palette and every shared lipgloss
// style. Colours are adaptive so the browser stays legible on light terminals.
package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/pradipta/wallfacer/internal/store"
)

// Palette. Light values are chosen for contrast on white backgrounds; the dark
// values keep the original 256-colour look the TUI shipped with.
var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#A21CAF", Dark: "212"}
	colProject = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "117"}
	colTag     = lipgloss.AdaptiveColor{Light: "#047857", Dark: "115"}
	colTitle   = lipgloss.AdaptiveColor{Light: "#111827", Dark: "252"}
	colDim     = lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "241"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "238"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "214"}
)

var (
	// Kept from the original TUI so call sites read the same.
	badgeStyle  = lipgloss.NewStyle().Foreground(colAccent)
	promptStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	statusStyle = lipgloss.NewStyle().Foreground(colDim)

	// Chrome.
	headerMarkStyle  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	headerCountStyle = lipgloss.NewStyle().Foreground(colDim)
	headerRuleStyle  = lipgloss.NewStyle().Foreground(colBorder)
	footerStyle      = lipgloss.NewStyle().Foreground(colDim)
	// noticeStyle is the "update available" footer hint: warm enough to catch
	// the eye on the first frame, not loud enough to look like an error.
	noticeStyle   = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	helpKeyStyle  = lipgloss.NewStyle().Foreground(colTitle)
	helpDescStyle = lipgloss.NewStyle().Foreground(colDim)

	// Filter chips in the header.
	chipProjectStyle = lipgloss.NewStyle().Foreground(colProject).Bold(true)
	chipTagStyle     = lipgloss.NewStyle().Foreground(colTag).Bold(true)
	chipAgentStyle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	// Agent chooser (new-session flow). The unselected chips stay dim so the
	// current choice reads at a glance on the shared footer line.
	choiceStyle         = lipgloss.NewStyle().Foreground(colDim)
	choiceSelectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Underline(true)

	// List rows. Layout-only wrappers: they set indentation, never colour, so
	// the coloured spans composed inside a row survive untouched. Selected
	// rows swap the two-space indent for an accent bar plus one space, which
	// keeps the text column from shifting — the same trick bubbles'
	// DefaultDelegate uses.
	rowPadStyle = lipgloss.NewStyle().Padding(0, 0, 0, 2)
	rowBarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colAccent).
			Padding(0, 0, 0, 1)

	// Row content colours.
	titleNormalStyle   = lipgloss.NewStyle().Foreground(colTitle)
	titleSelectedStyle = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	titleDimmedStyle   = lipgloss.NewStyle().Foreground(colBorder)
	metaStyle          = lipgloss.NewStyle().Foreground(colDim)
	rowProjectStyle    = lipgloss.NewStyle().Foreground(colProject)
	rowTagStyle        = lipgloss.NewStyle().Foreground(colTag)
	matchStyle         = lipgloss.NewStyle().Underline(true)

	// Detail pane.
	detailPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(colBorder).
			Padding(0, 1)
	detailTitleStyle = lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	detailLabelStyle = lipgloss.NewStyle().Foreground(colDim)
	detailValueStyle = lipgloss.NewStyle().Foreground(colTitle)
	detailRuleStyle  = lipgloss.NewStyle().Foreground(colBorder)
	warnStyle        = lipgloss.NewStyle().Foreground(colWarn)
)

// glyph returns the status indicator for a session status.
func glyph(status string) (string, lipgloss.Style) {
	switch status {
	case store.StatusMissing:
		return "⚠", warnStyle
	case store.StatusTrashed:
		return "␡", statusStyle
	default:
		return "●", lipgloss.NewStyle().Foreground(colTag)
	}
}
