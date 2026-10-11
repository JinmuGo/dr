package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// Widths of the parts of the screen, in cells. The file tree is hidden to keep the diff at
// least minDiffWidth cells wide. The split view takes minSplitWidth: each side then has a
// little over 40 cells of code, somewhat less than hunk keeps its two columns for, so that
// a 160-cell screen shows it beside the file tree.
const (
	minDiffWidth  = 60
	minSplitWidth = 100
	minListWidth  = 20  // the file tree can be dragged no narrower, and folds below half of it
	maxListWidth  = 40  // the file tree is no wider until it is dragged
	maxReadWidth  = 100 // the origin box wraps its text no wider
)

// noChanges is what the empty screen says when no changed file is left. undoHere is why it
// opened there while fixes dr applied are in place, and emptyRule how many cells the rule
// above them takes.
const (
	noChanges = "No changed files"
	undoHere  = "dr also opens here when a fix it applied is the only change, so :undo still works."
	emptyRule = 24
)

// View draws the screen: the file tree and the diff side by side, with the origin box open
// under the cursor row when Enter opened it, the status line under them and the key row
// last, and what floats over them. The terminal's cursor shows where the text typed in the
// open model finder or input box of a or c goes in, or else after the search or the : input
// on the status line. It takes the mouse, for dragging the file tree's edge and for the
// wheel.
func (m *model) View() tea.View {
	v := tea.NewView(m.draw())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	switch typed := m.inputText(); {
	case m.typing() && m.entry.picker != nil && len(m.rows) > 0:
		v.Cursor = tea.NewCursor(m.finderCursor())
	case m.typing():
		if x, y, ok := m.entryCursor(); ok {
			v.Cursor = tea.NewCursor(x, y)
		}
	case typed != "":
		v.Cursor = tea.NewCursor(m.pillWidth()+ansi.StringWidth(typed), m.diffHeight())
	}
	return v
}

// inputPrompt returns what the status line shows before the open input, "" when none is
// open.
func (m *model) inputPrompt() string {
	switch {
	case m.searching:
		return "/"
	case m.commanding:
		return ":"
	}
	return ""
}

// inputText returns what the status line shows of the open search or : input after the
// mode pill: its prompt and what is typed. When they do not fit on the line with the cursor
// after them, their start is cut, so what is typed and the cursor stay on the screen. It
// returns "" when no input is open.
func (m *model) inputText() string {
	prompt := m.inputPrompt()
	if prompt == "" {
		return ""
	}
	return keepEnd(prompt+review.Visible(m.input), m.width-m.pillWidth()-1) // the cursor after it
}

// draw returns the screen, m.height lines of m.width cells: the diff, or noChanges when no
// changed file is left, the status line and, on a screen of minKeyRowHeight rows or more,
// the key row. Over them float the notice at the top right of the diff, the command list
// above the status line while the : input is open, and the key help over all of it.
func (m *model) draw() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	h := m.diffHeight()
	_, w := m.layout()
	var lines []string
	if len(m.rows) == 0 {
		lines, w = m.emptyLines(h), m.width
	} else {
		lines = m.mainLines(h)
	}
	m.drawNotice(lines, w)
	m.drawCommands(lines)
	lines = append(lines, m.status())
	if m.height >= minKeyRowHeight {
		lines = append(lines, m.keyRow())
	}
	if m.help {
		m.drawHelp(lines)
	}
	return strings.Join(lines, "\n")
}

// mainLines returns the h lines of the screen above the status line: the file tree, folded
// to a handle or hidden as layout says, and beside it the diff, the bands at its top, then
// the rows with the model finder floating over them while it is open.
func (m *model) mainLines(h int) []string {
	listWidth, diffWidth := m.layout()
	var list []string
	if listWidth > 0 {
		list = m.fileTree(listWidth, h)
	}
	rows := m.diffLines(diffWidth, m.rowsHeight())
	if m.typing() && m.entry.picker != nil {
		m.drawFinder(rows, diffWidth)
	}
	diff := append(m.bandRows(diffWidth), rows...)
	separator := m.theme.paint(m.theme.line, nil).Render("│")
	lines := make([]string, h)
	for y := range lines {
		switch {
		case m.folded && y == 0:
			lines[y] = m.theme.paint(m.theme.accent, nil).Render("▸")
		case m.folded:
			lines[y] = separator
		case list != nil:
			lines[y] = list[y] + separator
		}
		lines[y] += diff[y]
	}
	return lines
}

// emptyLines returns the h lines of the screen above the status line when no changed file
// is left, under the bands. In the middle of the diff, which takes the whole width, come a
// faint rule, noChanges, what the diff compared, and, while fixes dr applied are in place,
// why dr opened on this screen.
func (m *model) emptyLines(h int) []string {
	t := &m.theme
	lines := make([]string, h)
	for y := range lines {
		lines[y] = strings.Repeat(" ", m.width)
	}
	put := func(y int, text string, st lipgloss.Style) {
		if y >= h || text == "" {
			return
		}
		l := newLine(m.width)
		l.add(strings.Repeat(" ", max((m.width-ansi.StringWidth(text))/2, 0)), t.paint(nil, nil))
		l.add(text, st)
		lines[y] = l.fill(t.paint(nil, nil))
	}
	y := max(h/2-3, 0)
	put(y, strings.Repeat("─", emptyRule), t.paint(t.faint, nil))
	put(y+2, noChanges, t.paint(t.strong, nil).Bold(true))
	put(y+3, m.base.same(), t.paint(t.muted, nil))
	if len(m.records.InPlace()) > 0 {
		put(y+5, undoHere, t.paint(t.subtle, nil))
	}
	copy(lines, m.bandRows(m.width))
	return lines
}

// same returns what the empty screen says the diff compared, "" when the screen was not
// told.
func (b Base) same() string {
	if b.Commit == "" {
		return ""
	}
	what := review.Visible(b.Ref) + " (" + b.Commit[:min(len(b.Commit), 7)] + ")"
	if b.MergeBase {
		what = "The merge-base with " + what
	}
	return what + " and the working tree are the same."
}

// layout returns the widths of the file tree and the diff. The file tree is as wide as it
// was dragged, or as its rows up to maxListWidth, and it is hidden, 0 wide, when the diff
// would be narrower than minDiffWidth beside it. Folded, it is 0 wide too, and a handle of a
// cell takes its place.
func (m *model) layout() (list, diff int) {
	if m.folded {
		return 0, max(m.width-1, 0)
	}
	list = min(m.listWidth, maxListWidth)
	if m.treeSize > 0 {
		list = m.treeSize
	}
	if diff = m.width - list - 1; diff >= minDiffWidth {
		return list, diff
	}
	return 0, m.width
}
