// Package tui holds the interactive terminal surfaces for awst. It is kept
// separate so the command layer stays TUI-agnostic and easily testable.
package tui

import (
	"errors"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// ErrAborted is returned by SelectProfile when the user quits the picker
// without choosing (esc / ctrl-c / q). Callers should treat it as a clean,
// no-op exit rather than a failure.
var ErrAborted = errors.New("selection aborted")

// ProfileItem is one selectable row: an AWS profile and the sso_session it
// resolves to.
type ProfileItem struct {
	Profile string
	Session string
}

func (i ProfileItem) Title() string       { return i.Profile }
func (i ProfileItem) Description() string  { return "sso_session: " + i.Session }
func (i ProfileItem) FilterValue() string  { return i.Profile }

type model struct {
	list    list.Model
	choice  string
	aborted bool
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
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
	l := list.New(rows, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select a profile to log in"
	l.SetShowStatusBar(false)

	res, err := tea.NewProgram(model{list: l}, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	fm := res.(model)
	if fm.aborted || fm.choice == "" {
		return "", ErrAborted
	}
	return fm.choice, nil
}
