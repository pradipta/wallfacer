// Package tui implements the interactive session browser. It never launches
// agents itself: picking a session returns an Action to the caller, which
// runs the agent with the real terminal and reopens the browser afterward.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/pradipta/wallfacer/internal/banner"
	"github.com/pradipta/wallfacer/internal/store"
)

type ActionType int

const (
	ActionQuit ActionType = iota
	ActionResume
	ActionNew
)

// Action is what the user chose to do; the caller executes it. The overlay
// fields mirror the flags on `wallfacer new` so the browser can start a
// session with the same metadata the CLI can.
type Action struct {
	Type    ActionType
	Session store.Session // for ActionResume

	// for ActionNew
	Dir     string
	Title   string
	Project string
	Tags    []string
}

// Run shows the browser and blocks until the user quits or picks an action.
func Run(s *store.Store) (Action, error) {
	m, err := newModel(s)
	if err != nil {
		return Action{}, err
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return Action{}, err
	}
	return final.(model).action, nil
}

// item adapts a session to bubbles' list. Rendering lives entirely in
// sessionDelegate; the only thing the list itself needs is a string to fuzzy
// match against. The title comes first so the delegate can map match indices
// back onto the title it draws.
type item struct{ s store.Session }

func (i item) FilterValue() string {
	return strings.Join(append([]string{i.s.DisplayTitle(), i.s.Project, i.s.Dir}, i.s.Tags...), " ")
}

type inputKind int

const (
	inputNone inputKind = iota
	inputRename
	inputTags
	inputProject
	// The inputNew* kinds form the new-session chain, asked in this order.
	inputNewDir
	inputNewTitle
	inputNewProject
	inputNewTags
)

var inputPrompts = map[inputKind]string{
	inputRename:     "rename: ",
	inputTags:       "tags (comma-separated, replaces all): ",
	inputProject:    "project (empty to clear): ",
	inputNewDir:     "new session in dir: ",
	inputNewTitle:   "title (optional, enter to skip): ",
	inputNewProject: "project (optional, enter to skip): ",
	inputNewTags:    "tags (comma-separated, optional): ",
}

type model struct {
	store  *store.Store
	list   list.Model
	input  textinput.Model
	kind   inputKind
	action Action
	// draft accumulates the new-session answers across the inputNew* chain.
	draft Action
	// confirmingDelete is set while the d→y/n prompt is up.
	confirmingDelete bool
	// compl holds the directory completions tab is cycling through, complIdx
	// the one currently shown (-1 before the first cycle), and complValue the
	// input value tab last wrote. Any other key clears them, so the cycle only
	// continues while tab is the only thing being pressed.
	compl      []string
	complIdx   int
	complValue string
	status     string
	// splash is true while the launch banner is showing; w/h track the
	// terminal size so the banner can be centered.
	splash bool
	w, h   int

	// all is the unfiltered session set. It backs the header counts and the
	// project/tag cycles, and is deliberately not refetched when a filter
	// changes so the cycles stay stable while you page through them.
	all      []store.Session
	projects []string
	tags     []string
	// projIdx and tagIdx index into projects/tags; -1 means no filter.
	projIdx, tagIdx int
	// showDetail is the user's toggle; the pane also hides itself on narrow
	// terminals regardless.
	showDetail bool
}

// splashDoneMsg ends the launch splash after splashDuration elapses.
type splashDoneMsg struct{}

const splashDuration = 1200 * time.Millisecond

// Layout constants.
const (
	// chromeHeight is the header, the footer, and the blank line between the
	// panes and the footer.
	chromeHeight = 3
	// detailMinTotalWidth is the terminal width below which the detail pane
	// is dropped and the list takes everything.
	detailMinTotalWidth = 100
	detailMinWidth      = 34
	detailMaxWidth      = 56
)

// splashShown ensures the banner appears once per process, not every time the
// browser reopens after an agent session exits.
var splashShown bool

func newModel(s *store.Store) (model, error) {
	l := list.New(nil, sessionDelegate{}, 0, 0)
	// All chrome is drawn by View so the header can carry counts and filter
	// chips and the footer can share its line with prompts.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.AdditionalShortHelpKeys = extraKeys
	l.AdditionalFullHelpKeys = extraKeys

	ti := textinput.New()
	ti.CharLimit = 512

	m := model{
		store:      s,
		list:       l,
		input:      ti,
		projIdx:    -1,
		tagIdx:     -1,
		showDetail: true,
	}
	if err := m.reload(); err != nil {
		return model{}, err
	}
	if !splashShown {
		m.splash = true
		splashShown = true
	}
	return m, nil
}

// reload refreshes the unfiltered snapshot, the project/tag cycles, and the
// visible items. Call it after anything that can change overlay data.
func (m *model) reload() error {
	all, err := m.store.List(store.Filter{})
	if err != nil {
		return err
	}
	m.all = all

	projSet, tagSet := map[string]bool{}, map[string]bool{}
	for _, x := range all {
		if x.Project != "" {
			projSet[x.Project] = true
		}
		for _, t := range x.Tags {
			tagSet[t] = true
		}
	}
	// Preserve the active filters across the reload where they still exist.
	prevProj, prevTag := m.activeProject(), m.activeTag()
	m.projects, m.tags = sortedKeys(projSet), sortedKeys(tagSet)
	m.projIdx = indexOf(m.projects, prevProj)
	m.tagIdx = indexOf(m.tags, prevTag)

	return m.applyFilter()
}

// applyFilter re-queries the store with the active project/tag filters. Going
// back through store.List rather than filtering in memory reuses the query
// path the CLI already exercises.
func (m *model) applyFilter() error {
	sessions, err := m.store.List(store.Filter{
		Project: m.activeProject(),
		Tag:     m.activeTag(),
	})
	if err != nil {
		return err
	}
	items := make([]list.Item, len(sessions))
	for i, x := range sessions {
		items[i] = item{x}
	}
	m.list.SetItems(items)
	if m.list.Index() >= len(items) {
		m.list.ResetSelected()
	}
	return nil
}

func (m model) activeProject() string { return at(m.projects, m.projIdx) }
func (m model) activeTag() string     { return at(m.tags, m.tagIdx) }

// cycle advances an index through [0, n) and then to -1 ("no filter"), so
// repeatedly pressing the key always returns you to the unfiltered view.
func cycle(idx, n int) int {
	if n == 0 {
		return -1
	}
	if idx+1 >= n {
		return -1
	}
	return idx + 1
}

func at(xs []string, i int) string {
	if i < 0 || i >= len(xs) {
		return ""
	}
	return xs[i]
}

func indexOf(xs []string, want string) int {
	if want == "" {
		return -1
	}
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func extraKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tags")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "project")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "filter project")),
		key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "filter tag")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "clear filters")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "detail")),
	}
}

func (m model) Init() tea.Cmd {
	if m.splash {
		return tea.Tick(splashDuration, func(time.Time) tea.Msg { return splashDoneMsg{} })
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.resize()
		return m, nil
	case splashDoneMsg:
		m.splash = false
		return m, nil
	case tea.KeyMsg:
		// Any key dismisses the splash early.
		if m.splash {
			m.splash = false
			return m, nil
		}
		if m.kind != inputNone {
			return m.updateInput(msg)
		}
		if m.confirmingDelete {
			return m.updateConfirm(msg)
		}
		return m.updateBrowse(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// detailWidth returns the width of the detail pane, or 0 when it is hidden
// because the user toggled it off or the terminal is too narrow to split.
func (m model) detailWidth() int {
	if !m.showDetail || m.w < detailMinTotalWidth {
		return 0
	}
	w := m.w * 2 / 5
	return min(max(w, detailMinWidth), detailMaxWidth)
}

// resize recomputes the list's size from the terminal size and the current
// detail pane visibility. Both callers — the resize message and the tab
// toggle — must go through here.
func (m *model) resize() {
	if m.w == 0 || m.h == 0 {
		return
	}
	listW := m.w - m.detailWidth()
	m.list.SetSize(max(listW, 1), max(m.h-chromeHeight, 1))
}

func (m model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the fuzzy filter is being typed, every key belongs to it.
	if m.list.FilterState() == list.Filtering {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	m.status = ""
	sel, hasSel := m.selected()
	switch msg.String() {
	case "ctrl+c", "q":
		m.action = Action{Type: ActionQuit}
		return m, tea.Quit
	case "enter":
		if hasSel {
			m.action = Action{Type: ActionResume, Session: sel}
			return m, tea.Quit
		}
	case "n":
		wd, _ := os.Getwd()
		m.draft = Action{Type: ActionNew}
		return m.openInput(inputNewDir, wd), nil
	case "r":
		if hasSel {
			return m.openInput(inputRename, sel.Title), nil
		}
	case "t":
		if hasSel {
			return m.openInput(inputTags, strings.Join(sel.Tags, ",")), nil
		}
	case "p":
		if hasSel {
			return m.openInput(inputProject, sel.Project), nil
		}
	case "d":
		if hasSel {
			m.confirmingDelete = true
		}
		return m, nil
	case "P":
		m.projIdx = cycle(m.projIdx, len(m.projects))
		return m.refilter(), nil
	case "T":
		m.tagIdx = cycle(m.tagIdx, len(m.tags))
		return m.refilter(), nil
	case "x":
		if m.projIdx == -1 && m.tagIdx == -1 {
			break
		}
		m.projIdx, m.tagIdx = -1, -1
		return m.refilter(), nil
	case "tab":
		m.showDetail = !m.showDetail
		m.resize()
		return m, nil
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// refilter reapplies the project/tag filters, surfacing any error in the
// status line rather than tearing the browser down.
func (m model) refilter() model {
	if err := m.applyFilter(); err != nil {
		m.status = "error: " + err.Error()
	}
	return m
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Esc abandons the whole new-session chain, not just this step.
		if m.kind >= inputNewDir {
			m.draft = Action{}
			m.status = "new session cancelled"
		}
		m.kind = inputNone
		m.input.Blur()
		return m, nil
	case "enter":
		return m.submitInput()
	case "tab":
		// Tab is a completion key only where a path is being asked for; the
		// other prompts have nothing to complete against.
		if m.kind == inputNewDir {
			return m.completeDir(), nil
		}
		return m, nil
	}
	m.clearCompletions()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// completeDir advances the directory completion for the current input: first
// tab fills in as much as every match agrees on, and further tabs cycle the
// matches one at a time.
func (m model) completeDir() model {
	value := m.input.Value()
	if len(m.compl) > 0 && value == m.complValue {
		m.complIdx = (m.complIdx + 1) % len(m.compl)
		return m.setCompletion(m.compl[m.complIdx])
	}

	m.clearCompletions()
	cands := dirCandidates(value)
	switch len(cands) {
	case 0:
		return m
	case 1:
		// Unambiguous: take it, with nothing left to cycle through.
		return m.setCompletion(cands[0])
	}
	m.compl = cands
	if common := commonPrefix(cands); common != value {
		// There is still shared text to fill in; leave the cycle at the start
		// so the next tab offers the first match.
		m.complIdx = -1
		return m.setCompletion(common)
	}
	m.complIdx = 0
	return m.setCompletion(cands[0])
}

func (m model) setCompletion(value string) model {
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.complValue = value
	return m
}

func (m *model) clearCompletions() {
	m.compl, m.complIdx, m.complValue = nil, 0, ""
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.confirmingDelete = false
	if msg.String() != "y" {
		m.status = "delete aborted"
		return m, nil
	}
	sel, ok := m.selected()
	if !ok {
		return m, nil
	}
	if _, err := m.store.Trash(sel); err != nil {
		m.status = "error: " + err.Error()
		return m, nil
	}
	m.list.RemoveItem(m.list.Index())
	// Drop it from the unfiltered snapshot too so the header count is honest.
	for i, x := range m.all {
		if x.ID == sel.ID {
			m.all = append(m.all[:i:i], m.all[i+1:]...)
			break
		}
	}
	m.status = fmt.Sprintf("trashed %q (restore from %s)", sel.DisplayTitle(), m.store.TrashDir())
	return m, nil
}

func (m model) openInput(kind inputKind, initial string) model {
	m.kind = kind
	m.clearCompletions()
	m.input.Prompt = promptStyle.Render(inputPrompts[kind])
	m.input.SetValue(initial)
	m.input.CursorEnd()
	m.input.Focus()
	return m
}

func (m model) submitInput() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	kind := m.kind
	m.kind = inputNone
	m.input.Blur()

	if kind >= inputNewDir {
		return m.submitNew(kind, value)
	}

	sel, ok := m.selected()
	if !ok {
		return m, nil
	}
	var err error
	switch kind {
	case inputRename:
		err = m.store.SetTitle(sel.ID, value)
	case inputProject:
		err = m.store.SetProject(sel.ID, value)
	case inputTags:
		err = m.replaceTags(sel, splitTags(value))
	}
	if err != nil {
		m.status = "error: " + err.Error()
		return m, nil
	}
	// A new project or tag has to enter the P/T cycles, and the session may
	// no longer match the active filter, so rebuild everything.
	if kind == inputProject || kind == inputTags {
		if err := m.reload(); err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		m.selectByID(sel.ID)
		m.status = "saved"
		return m, nil
	}
	fresh, err := m.store.Get(sel.ID)
	if err != nil {
		m.status = "error: " + err.Error()
		return m, nil
	}
	cmd := m.list.SetItem(m.list.Index(), item{*fresh})
	m.status = "saved"
	return m, cmd
}

// submitNew advances the new-session chain one step, and quits with the
// assembled action once the last answer is in. The project and tag steps are
// prefilled from the active filters: if you are looking at one project, a
// session started from that view almost certainly belongs to it.
func (m model) submitNew(kind inputKind, value string) (tea.Model, tea.Cmd) {
	switch kind {
	case inputNewDir:
		if value == "" {
			m.draft = Action{}
			return m, nil
		}
		// Clean drops the trailing separator tab completion leaves behind.
		m.draft.Dir = filepath.Clean(expandHome(value))
		return m.openInput(inputNewTitle, ""), nil
	case inputNewTitle:
		m.draft.Title = value
		return m.openInput(inputNewProject, m.activeProject()), nil
	case inputNewProject:
		m.draft.Project = value
		return m.openInput(inputNewTags, m.activeTag()), nil
	case inputNewTags:
		m.draft.Tags = splitTags(value)
		m.action = m.draft
		return m, tea.Quit
	}
	return m, nil
}

// selectByID moves the cursor back to a session after the list was rebuilt.
// If the session no longer matches the active filter the cursor stays put.
func (m *model) selectByID(id string) {
	for i, li := range m.list.Items() {
		if it, ok := li.(item); ok && it.s.ID == id {
			m.list.Select(i)
			return
		}
	}
}

func (m model) replaceTags(sel store.Session, want []string) error {
	have := map[string]bool{}
	for _, t := range sel.Tags {
		have[t] = true
	}
	wanted := map[string]bool{}
	for _, t := range want {
		wanted[t] = true
		if !have[t] {
			if err := m.store.AddTag(sel.ID, t); err != nil {
				return err
			}
		}
	}
	for _, t := range sel.Tags {
		if !wanted[t] {
			if err := m.store.RemoveTag(sel.ID, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m model) selected() (store.Session, bool) {
	it, ok := m.list.SelectedItem().(item)
	if !ok {
		return store.Session{}, false
	}
	return it.s, true
}

func (m model) View() string {
	if m.splash {
		return m.splashView()
	}
	body := m.list.View()
	if dw := m.detailWidth(); dw > 0 {
		detail := ""
		if sel, ok := m.selected(); ok {
			detail = renderDetail(sel, dw, max(m.h-chromeHeight, 1))
		}
		body = horizontal(body, detail)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), body, "", m.footerView())
}

// headerView is the brand mark on the left, active filter chips in the middle,
// and the session/project counts on the right, separated by a rule.
func (m model) headerView() string {
	left := headerMarkStyle.Render("wallfacer")
	if chips := m.chipsView(); chips != "" {
		left += "  " + chips
	}

	projects := 0
	for _, p := range m.projects {
		if p != "" {
			projects++
		}
	}
	right := headerCountStyle.Render(fmt.Sprintf("%s · %s",
		plural(len(m.all), "session"), plural(projects, "project")))

	// A rule fills whatever is left between them; if nothing is, drop the
	// counts rather than wrapping.
	gap := m.w - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
	if gap < 1 {
		return ansi.Truncate(left, max(m.w, 1), "…")
	}
	return left + " " + headerRuleStyle.Render(strings.Repeat("─", gap)) + " " + right
}

// chipsView renders the active project/tag filters. They are shown only when
// set, so the header stays quiet in the common unfiltered case.
func (m model) chipsView() string {
	var chips []string
	if p := m.activeProject(); p != "" {
		chips = append(chips, chipProjectStyle.Render("◆ "+p))
	}
	if t := m.activeTag(); t != "" {
		chips = append(chips, chipTagStyle.Render("#"+t))
	}
	if len(chips) == 0 {
		return ""
	}
	return strings.Join(chips, " ") + statusStyle.Render(" (x to clear)")
}

// footerView shares one line between the prompts and the help. Prompts win:
// when you are typing or confirming, that is the only thing worth showing.
func (m model) footerView() string {
	switch {
	case m.kind != inputNone:
		return ansi.Truncate(m.input.View()+m.completionHint(), max(m.w, 1), "…")
	case m.confirmingDelete:
		sel, _ := m.selected()
		return promptStyle.Render(fmt.Sprintf("move %q to trash? [y/N]", sel.DisplayTitle()))
	case m.status != "":
		return statusStyle.Render(ansi.Truncate(m.status, max(m.w, 1), "…"))
	}
	return footerStyle.Render(m.list.Help.View(m.list))
}

// completionHint advertises tab completion on the directory prompt, and while
// several directories match, says where in the cycle you are.
func (m model) completionHint() string {
	if m.kind != inputNewDir {
		return ""
	}
	if n := len(m.compl); n > 1 {
		if m.complIdx < 0 {
			return statusStyle.Render(fmt.Sprintf("  (%d matches, tab to cycle)", n))
		}
		return statusStyle.Render(fmt.Sprintf("  (%d/%d, tab to cycle)", m.complIdx+1, n))
	}
	return statusStyle.Render("  (tab to complete)")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// splashView centers the launch banner in the terminal.
func (m model) splashView() string {
	art := strings.Trim(banner.Art, "\n")
	body := lipgloss.JoinVertical(lipgloss.Center,
		badgeStyle.Render(art),
		"",
		statusStyle.Render("loading sessions…"),
	)
	if m.w == 0 || m.h == 0 {
		return body
	}
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, body)
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
