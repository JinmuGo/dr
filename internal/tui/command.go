package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// command is a dr command typed after :, with what it does now as the command list tells it.
type command struct {
	name string
	run  func(*model) tea.Cmd
	does func(*model) string
}

// commands are the dr commands, in the order the command list shows them.
var commands = []command{
	{name: "submit", run: (*model).submit, does: (*model).submitDoes},
	{name: "undo", run: (*model).undo, does: (*model).undoDoes},
}

// Sizes of the command list, in cells: it is no wider than maxCommandsWidth, and what each
// command does starts commandGap cells after the longest name.
const (
	maxCommandsWidth = 64
	commandGap       = 5
)

// submitDoes says what :submit does now: how many pending comments it sends to how many
// sessions, or that there are none.
func (m *model) submitDoes() string {
	var sessions []session
	n := 0
	for _, c := range m.comments {
		if !c.pending() {
			continue
		}
		n++
		if s := (session{c.Agent, c.Session}); !slices.Contains(sessions, s) {
			sessions = append(sessions, s)
		}
	}
	if n == 0 {
		return "No pending comments"
	}
	return "Send " + countOf(n, "pending comment") + " to " + countOf(len(sessions), "session")
}

// undoDoes says what :undo does now: revert the last fix dr applied, or nothing when no fix
// it applied is in place.
func (m *model) undoDoes() string {
	if len(m.records.InPlace()) == 0 {
		return "No fix to revert"
	}
	return "Revert the last fix dr applied"
}

// commandRows returns the commands whose names start with what is typed in the : input, the
// rows of the command list.
func (m *model) commandRows() []command {
	typed := strings.TrimSpace(m.input)
	var rows []command
	for _, c := range commands {
		if strings.HasPrefix(c.name, typed) {
			rows = append(rows, c)
		}
	}
	return rows
}

// typeCommand handles a key while the : input is open: ↑ and ↓ move the highlight of the
// command list, Tab completes the input to the highlighted name, Enter runs the highlighted
// command when something is typed, or what is typed when no name starts with it, and closes
// the input without running anything when nothing is, Esc closes the input, and other keys
// edit it, which moves the highlight back to the first row. It returns the command the dr
// command starts.
func (m *model) typeCommand(k, text string) tea.Cmd {
	rows := m.commandRows()
	switch k {
	case keyEnter:
		name := strings.TrimSpace(m.input)
		if name != "" && len(rows) > 0 {
			name = rows[m.choice].name
		}
		m.commanding, m.input = false, ""
		return m.runCommand(name)
	case keyEsc:
		m.commanding, m.input = false, ""
	case keyUp:
		m.choice = max(m.choice-1, 0)
	case keyDown:
		m.choice = max(min(m.choice+1, len(rows)-1), 0)
	case keyTab:
		if len(rows) > 0 {
			name := rows[m.choice].name
			m.input = name
			m.choice = slices.IndexFunc(m.commandRows(), func(c command) bool { return c.name == name })
		}
	default:
		m.edit(k, text)
		m.choice = 0
	}
	return nil
}

// openCommand opens the : input, empty, with the first row of the command list highlighted.
func (m *model) openCommand() {
	m.commanding, m.input, m.choice = true, "", 0
}

// runCommand runs the dr command name and returns the command it starts. Nothing typed does
// nothing, and a name that is no dr command is an error on the status line.
func (m *model) runCommand(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	for _, c := range commands {
		if c.name == name {
			return c.run(m)
		}
	}
	m.fail("Not a dr command: " + review.Visible(name))
	return nil
}

// drawCommands draws the command list over lines, the lines of the screen above the status
// line, right above it, while the : input is open and some name starts with what is typed.
func (m *model) drawCommands(lines []string) {
	rows := m.commandRows()
	w := min(maxCommandsWidth, m.width-2)
	if !m.commanding || len(rows) == 0 || w < 4 {
		return
	}
	box := m.commandBox(rows, w)
	box = box[max(len(box)-len(lines), 0):] // a screen too short shows the bottom of the list
	y := len(lines) - len(box)
	for k, l := range box {
		lines[y+k] = overlay(lines[y+k], l, 1, w, m.width)
	}
}

// commandBox draws the command list, w cells wide, in a rounded border in the color of
// floating boxes, with the title "commands" on its top border: a row for each command of
// rows, its name after a : with the part typed in the ask color, bold and underlined, then
// what it does now. The highlighted row is on the selection band with ▌, reversed without
// colors.
func (m *model) commandBox(rows []command, w int) []string {
	t := &m.theme
	border := t.paint(t.line2, t.surface)
	top := newLine(w - 1)
	top.add("╭─", border)
	top.add(" commands ", t.paint(t.muted, t.surface))
	top.add(strings.Repeat("─", top.free), border)
	lines := []string{top.fill(border) + border.Render("╮")}
	names := 0
	for _, c := range commands {
		names = max(names, ansi.StringWidth(c.name))
	}
	typed := strings.TrimSpace(m.input)
	for i, c := range rows {
		bg, on := t.surface, t.paint
		if i == m.choice {
			bg, on = t.surface2, t.onBand
		}
		l := newLine(w - 3) // inside the border, less a cell of margin at the right
		if i == m.choice {
			l.add("▌", on(t.accent, bg))
		} else {
			l.add(" ", on(nil, bg))
		}
		l.add(":", on(t.muted, bg))
		l.add(typed, on(t.ask, bg).Bold(true).Underline(true))
		l.add(c.name[len(typed):], on(t.strong, bg))
		l.add(strings.Repeat(" ", names-ansi.StringWidth(c.name)+commandGap), on(nil, bg))
		l.add(c.does(m), on(t.fg, bg))
		lines = append(lines, border.Render("│")+l.fill(on(nil, bg))+on(nil, bg).Render(" ")+border.Render("│"))
	}
	return append(lines, border.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
}
