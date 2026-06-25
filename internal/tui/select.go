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
func (i ProfileItem) choiceValue() string { return i.Profile }

// selectable is any list row that knows the value to return when chosen,
// letting the shared model handle Enter without caring about the item type.
type selectable interface {
	list.Item
	choiceValue() string
}

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
				if it, ok := m.list.SelectedItem().(selectable); ok {
					m.choice = it.choiceValue()
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

// RegionItem is one selectable AWS region row.
type RegionItem struct{ Name string }

func (i RegionItem) FilterValue() string { return i.Name }
func (i RegionItem) choiceValue() string { return i.Name }

// regionDelegate renders a region on a single line with a ">" cursor.
type regionDelegate struct{}

func (regionDelegate) Height() int                             { return 1 }
func (regionDelegate) Spacing() int                            { return 0 }
func (regionDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (regionDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(RegionItem)
	if !ok {
		return
	}
	if index == m.Index() {
		fmt.Fprint(w, "> "+selectedStyle.Render(it.Name))
		return
	}
	fmt.Fprint(w, "  "+it.Name)
}

// SelectRegion shows an arrow-key list of regions and returns the chosen
// region. It returns ErrAborted if the user quits without selecting.
func SelectRegion(regions []string) (string, error) {
	rows := make([]list.Item, len(regions))
	for i, r := range regions {
		rows[i] = RegionItem{Name: r}
	}
	l := list.New(rows, regionDelegate{}, 0, 0)
	l.Title = "Select a region"
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

// InstanceItem is one selectable SSM-managed instance row.
type InstanceItem struct {
	ID    string
	Name  string
	State string
	Ping  string
}

func (i InstanceItem) FilterValue() string { return i.Name }
func (i InstanceItem) choiceValue() string { return i.ID }

// instanceDelegate renders an instance on a single line with a ">" cursor:
// the Name, then dimmed id · state · ping. Column widths are precomputed
// across all rows (nameW/idW/stateW) so the fields line up vertically.
type instanceDelegate struct{ nameW, idW, stateW int }

func (instanceDelegate) Height() int                             { return 1 }
func (instanceDelegate) Spacing() int                            { return 0 }
func (instanceDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d instanceDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(InstanceItem)
	if !ok {
		return
	}
	name := it.Name
	if name == "" {
		name = "-"
	}
	name = fmt.Sprintf("%-*s", d.nameW, name) // pad to column width before styling
	meta := "  " + dimStyle.Render(fmt.Sprintf("%-*s · %-*s · %s", d.idW, it.ID, d.stateW, it.State, it.Ping))
	if index == m.Index() {
		fmt.Fprint(w, "> "+selectedStyle.Render(name)+meta)
		return
	}
	fmt.Fprint(w, "  "+name+meta)
}

// SelectInstance shows an arrow-key list of instances and returns the chosen
// instance ID. It returns ErrAborted if the user quits without selecting.
func SelectInstance(items []InstanceItem) (string, error) {
	rows := make([]list.Item, len(items))
	var nameW, idW, stateW int
	for i, it := range items {
		rows[i] = it
		name := it.Name
		if name == "" {
			name = "-"
		}
		nameW = max(nameW, len(name))
		idW = max(idW, len(it.ID))
		stateW = max(stateW, len(it.State))
	}
	l := list.New(rows, instanceDelegate{nameW: nameW, idW: idW, stateW: stateW}, 0, 0)
	l.Title = "Select an instance to connect to"
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
