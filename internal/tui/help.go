package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// helpGroup is a group of keys in the key help: its name and each key with what it does.
type helpGroup struct {
	name string
	keys [][2]string
}

// helpColumns are the groups of the key help, in its two columns, left and right. What a key
// does is short, so that both columns fit on a screen 80 cells wide.
var helpColumns = [2][]helpGroup{
	{
		{"MOVE", [][2]string{
			{"j k", "line down, up · 5j"}, {"d u J K", "half a page down, up"}, {"gg G", "top, bottom · 12G"},
			{"} {", "next, previous change"}, {"l h", "next, previous file · 3l"}, {"/ n N", "search, next, previous"},
		}},
		{"REVIEW", [][2]string{
			{"⏎", "origin; j k scroll it"}, {"v", "mark the file viewed"}, {"V", "select lines"},
			{"s", "split or unified"},
		}},
		{"MOUSE", [][2]string{
			{"drag", "resize the tree"}, {"wheel", "scroll three lines"}, {"click", "a file, or a Viewed box"},
			{"⇧ drag", "select text (⌥ on macOS)"},
		}},
	},
	{
		{"ASK AND FIX", [][2]string{
			{"a", "ask who wrote it"}, {"ga", "open it in the agent"}, {"c", "leave a change comment"},
			{":submit", "send every comment"}, {"y x", "apply, drop a fix"}, {":undo", "revert the last fix"},
			{"e", "edit in $EDITOR"},
		}},
		{"IN A BOX", [][2]string{
			{"⏎", "send or save"}, {"⇧⏎", "new line (⌥⏎, ^J)"}, {"^", "pick a model, word start"},
			{"esc ^C", "cancel"},
		}},
		{"OTHER", [][2]string{{"?", "this help"}, {"esc", "clear a count or search"}, {"q", "quit"}}},
	},
}

// Columns of the key help, in cells: what a key does starts helpLabel cells after its
// keycap, the first column helpMargin cells inside the box's left border, and the second
// helpGap cells after the first.
const (
	helpLabel  = 10
	helpMargin = 3
	helpGap    = 3
)

// drawHelp draws the key help over lines, the whole screen, in a box in its middle, and dims
// what is behind it: faint, without its colors. A screen too small for the box shows what of
// it fits, its bottom border last.
func (m *model) drawHelp(lines []string) {
	t := &m.theme
	for y, l := range lines {
		lines[y] = t.paint(t.faint, nil).Render(ansi.Strip(l))
	}
	box, w := m.helpBox(m.width)
	if len(box) > len(lines) {
		box = append(box[:len(lines)-1], box[len(box)-1])
	}
	x, y := (m.width-w)/2, (len(lines)-len(box))/2
	for k, b := range box {
		lines[y+k] = overlay(lines[y+k], b, x, w, m.width)
	}
}

// helpBox draws the key help in a rounded box in the color of floating boxes on their fill,
// no wider than room, and returns its lines and its width. "Keys" heads it at the left of
// its top border and "any key closes" at the right, and the groups of helpColumns come in
// two columns, or in one when two leave no cell of the screen on each side of the box.
func (m *model) helpBox(room int) ([]string, int) {
	t := &m.theme
	label := 0
	for _, column := range helpColumns {
		for _, g := range column {
			for _, k := range g.keys {
				label = max(label, ansi.StringWidth(k[1]))
			}
		}
	}
	colWidth := helpLabel + label
	bodies := [][][]seg{m.helpColumn(helpColumns[0]), m.helpColumn(helpColumns[1])}
	w := 2 + helpMargin + colWidth + helpGap + colWidth + 2 // the borders and a margin at the right
	if w+2 > room {                                         // a cell of the screen on each side
		bodies = [][][]seg{m.helpColumn(slices.Concat(helpColumns[0], helpColumns[1]))}
		w = 2 + helpMargin + colWidth + 2
	}
	w = min(w, room)
	base := t.paint(nil, t.surface)
	border := t.paint(t.line2, t.surface)
	row := func(y int) string {
		l := newLine(w - 2)
		l.add(strings.Repeat(" ", helpMargin), base)
		for i, body := range bodies {
			if i > 0 {
				l.add(strings.Repeat(" ", helpGap), base)
			}
			cell := newLine(colWidth)
			if y >= 0 && y < len(body) {
				for _, s := range body[y] {
					cell.add(s.text, s.st)
				}
			}
			l.add(cell.fill(base), lipgloss.NewStyle())
		}
		return border.Render("│") + l.fill(base) + border.Render("│")
	}
	top := newLine(w - 1)
	top.add("╭─", border)
	top.add(" Keys ", t.paint(t.strong, t.surface).Bold(true))
	const closes, end = " any key closes ", "───"
	if top.free >= ansi.StringWidth(closes+end)+1 {
		top.add(strings.Repeat("─", top.free-ansi.StringWidth(closes+end)), border)
		top.add(closes, t.paint(t.subtle, t.surface))
	}
	top.add(strings.Repeat("─", top.free), border)
	lines := []string{top.fill(border) + border.Render("╮"), row(-1)}
	rows := 0
	for _, body := range bodies {
		rows = max(rows, len(body))
	}
	for y := range rows {
		lines = append(lines, row(y))
	}
	lines = append(lines, row(-1), border.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
	return lines, w
}

// helpColumn returns the lines of a column of the key help with groups: the name of each
// group, quiet and bold, then each key on a keycap with what it does, and an empty line
// between two groups.
func (m *model) helpColumn(groups []helpGroup) [][]seg {
	t := &m.theme
	base := t.paint(nil, t.surface)
	var lines [][]seg
	for i, g := range groups {
		if i > 0 {
			lines = append(lines, nil)
		}
		lines = append(lines, []seg{{text: g.name, st: t.paint(t.subtle, t.surface).Bold(true)}})
		for _, k := range g.keys {
			key := " " + k[0] + " "
			lines = append(lines, []seg{
				{text: key, st: t.onBand(t.strong, t.surface2)},
				{text: strings.Repeat(" ", max(helpLabel-ansi.StringWidth(key), 1)), st: base},
				{text: k[1], st: t.paint(t.fg, t.surface)},
			})
		}
	}
	return lines
}
