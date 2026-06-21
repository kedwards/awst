// Package tui holds the interactive terminal surfaces for awst. It is kept
// separate so the command layer stays TUI-agnostic and easily testable.
package tui

import (
	"errors"
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrAborted is returned by SelectProfile when the user quits the picker
// without choosing (esc / ctrl-c / q). Callers should treat it as a clean,
// no-op exit rather than a failure.
var ErrAborted = errors.New("selection aborted")

var (
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	titleStyle    = lipgloss.NewStyle().Bold(true)
)

// ProfileItem is one selectable row: an AWS profile and the sso_session it
// resolves to.
type ProfileItem struct {
	Profile string
	Session string
}

func (i ProfileItem) FilterValue() string { return i.Profile }

// itemDelegate renders each profile on a single line with a ">" cursor —
// the compact style used by tools like `assume`, rather than the tall
// two-line default delegate.
type itemDelegate struct{}

func (itemDelegate) Height() int                             { return 1 }
func (itemDelegate) Spacing() int                            { return 0 }
func (itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (itemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(ProfileItem)
	if !ok {
		return
	}
	session := ""
	if it.Session != "" {
		session = "  " + dimStyle.Render("sso_session: "+it.Session)
	}
	if index == m.Index() {
		fmt.Fprint(w, "> "+selectedStyle.Render(it.Profile)+session)
		return
	}
	fmt.Fprint(w, "  "+it.Profile+session)
}

type model struct {
	list    list.Model
	choice  string
	aborted bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Size the inline picker to the item count, capped at 10 per page, so
		// it takes only as much room as it needs. Items are 1 line tall, so
		// after sizing to the full height we trim the overflow to land PerPage
		// exactly on the target (no chrome math, no wasted blank space).
		target := min(len(m.list.Items()), 10)
		m.list.SetSize(msg.Width, msg.Height)
		if over := m.list.Paginator.PerPage - target; over > 0 {
			m.list.SetSize(msg.Width, msg.Height-over)
		}
		return m, nil
	case tea.KeyMsg:
		// While the filter input is active, let the list own every key
		// (typing, esc-to-clear) instead of treating esc/q as abort.
		if m.list.FilterState() != list.Filtering {
			switch msg.String() {
			case "ctrl+c", "esc", "q":
				m.aborted = true
				return m, tea.Quit
			case "enter":
				if it, ok := m.list.SelectedItem().(ProfileItem); ok {
					m.choice = it.Profile
				}
				return m, tea.Quit
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() string { return m.list.View() }

// SelectProfile shows an arrow-key list of profiles and returns the chosen
// profile name. It returns ErrAborted if the user quits without selecting.
func SelectProfile(items []ProfileItem) (string, error) {
	rows := make([]list.Item, len(items))
	for i, it := range items {
		rows[i] = it
	}
	l := list.New(rows, itemDelegate{}, 0, 0)
	l.Title = "Select a profile to log in"
	l.SetShowStatusBar(false)
	l.Styles.Title = titleStyle

	res, err := tea.NewProgram(model{list: l}).Run()
	if err != nil {
		return "", err
	}
	fm := res.(model)
	if fm.aborted || fm.choice == "" {
		return "", ErrAborted
	}
	return fm.choice, nil
}
