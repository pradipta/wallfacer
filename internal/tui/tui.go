// Package tui implements the interactive session browser. It never launches
// agents itself: picking a session returns an Action to the caller, which
// runs the agent with the real terminal and reopens the browser afterward.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pradipta/wallfacer/internal/format"
	"github.com/pradipta/wallfacer/internal/store"
)

type ActionType int

const (
	ActionQuit ActionType = iota
	ActionResume
	ActionNew
)

// Action is what the user chose to do; the caller executes it.
type Action struct {
	Type    ActionType
	Session store.Session // for ActionResume
	Dir     string        // for ActionNew
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

type item struct{ s store.Session }

func (i item) Title() string {
	t := i.s.DisplayTitle()
	var badges []string
	if i.s.Project != "" {
		badges = append(badges, "◆ "+i.s.Project)
	}
	if len(i.s.Tags) > 0 {
		badges = append(badges, "#"+strings.Join(i.s.Tags, " #"))
	}
	if len(badges) > 0 {
		t += "  " + badgeStyle.Render(strings.Join(badges, "  "))
	}
	return t
}

func (i item) Description() string {
	return fmt.Sprintf("%s · %s · %s",
		format.CollapseHome(i.s.Dir), format.RelTime(i.s.LastActiveAt), i.s.AgentType)
}

func (i item) FilterValue() string {
	return strings.Join(append([]string{i.s.DisplayTitle(), i.s.Project, i.s.Dir}, i.s.Tags...), " ")
}

type inputKind int

const (
	inputNone inputKind = iota
	inputRename
	inputTags
	inputProject
	inputNewDir
)

var inputPrompts = map[inputKind]string{
	inputRename:  "rename: ",
	inputTags:    "tags (comma-separated, replaces all): ",
	inputProject: "project (empty to clear): ",
	inputNewDir:  "new session in dir: ",
}

type model struct {
	store  *store.Store
	list   list.Model
	input  textinput.Model
	kind   inputKind
	action Action
	// confirmingDelete is set while the d→y/n prompt is up.
	confirmingDelete bool
	status           string
}

var (
	badgeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

func newModel(s *store.Store) (model, error) {
	sessions, err := s.List(store.Filter{})
	if err != nil {
		return model{}, err
	}
	items := make([]list.Item, len(sessions))
	for i, x := range sessions {
		items[i] = item{x}
	}
	d := list.NewDefaultDelegate()
	l := list.New(items, d, 0, 0)
	l.Title = "wallfacer — sessions"
	l.SetShowStatusBar(false)
	l.AdditionalShortHelpKeys = extraKeys
	l.AdditionalFullHelpKeys = extraKeys

	ti := textinput.New()
	ti.CharLimit = 512
	return model{store: s, list: l, input: ti}, nil
}

func extraKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "tags")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "project")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-2)
		return m, nil
	case tea.KeyMsg:
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
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.kind = inputNone
		m.input.Blur()
		return m, nil
	case "enter":
		return m.submitInput()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	m.status = fmt.Sprintf("trashed %q (restore from %s)", sel.DisplayTitle(), m.store.TrashDir())
	return m, nil
}

func (m model) openInput(kind inputKind, initial string) model {
	m.kind = kind
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

	if kind == inputNewDir {
		if value == "" {
			return m, nil
		}
		m.action = Action{Type: ActionNew, Dir: expandHome(value)}
		return m, tea.Quit
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
	fresh, err := m.store.Get(sel.ID)
	if err != nil {
		m.status = "error: " + err.Error()
		return m, nil
	}
	cmd := m.list.SetItem(m.list.Index(), item{*fresh})
	m.status = "saved"
	return m, cmd
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
	var bottom string
	switch {
	case m.kind != inputNone:
		bottom = m.input.View()
	case m.confirmingDelete:
		sel, _ := m.selected()
		bottom = promptStyle.Render(fmt.Sprintf("move %q to trash? [y/N]", sel.DisplayTitle()))
	case m.status != "":
		bottom = statusStyle.Render(m.status)
	}
	return m.list.View() + "\n" + bottom
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
