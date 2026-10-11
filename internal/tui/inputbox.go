package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// maxEntryRows is how many rows of its text the input box of a and c shows at most. A longer
// text scrolls in it to keep the cursor's row in view.
const maxEntryRows = 10

// commentPlaceholder is what the input box of c shows dim while nothing is typed.
const commentPlaceholder = "How should these lines change?"

// typing reports whether the input box of a or c is open.
func (m *model) typing() bool {
	return m.asking != nil || m.drafting != nil
}

// entryRow returns the row the open input box is drawn under, -1 when none is open.
func (m *model) entryRow() int {
	switch {
	case m.asking != nil:
		return m.asking.row
	case m.drafting != nil:
		return m.drafting.row
	}
	return -1
}

// typeBox handles a key while the input box is open: Enter sends the question or saves the
// comment, Esc or ctrl+c closes the box, and other keys edit the text, as textBox.edit says.
// A ^ typed at the start of the text or after white space opens the model list, and while it
// is open pickKey takes the keys first. It returns the command that asks, or that lists the
// agent's models.
func (m *model) typeBox(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if m.entry.picker != nil && m.pickKey(k) {
		return nil
	}
	switch k {
	case "enter":
		if m.asking != nil {
			return m.send()
		}
		m.saveComment()
	case "esc", "ctrl+c":
		m.closeBox()
	default:
		_, w := m.layout()
		m.entry.edit(k, msg.Text, m.entryWidth(w))
		if m.entry.picker != nil {
			m.keepPicker()
		} else if at, ok := m.entry.opensPicker(); ok && msg.Text == "^" {
			return m.openPicker(at)
		}
	}
	return nil
}

// closeBox closes the input box without sending or saving what is typed in it. When a
// closed the origin box to open it, the origin box opens again as it was.
func (m *model) closeBox() {
	if t := m.asking; t != nil && t.reading {
		m.reading, m.readTop = true, t.readTop
	}
	m.asking, m.drafting, m.entry = nil, nil, textBox{}
}

// entryLeft returns the cell of a diff w cells wide where the input box starts: the code
// column of its row's file, as the origin box hangs, but no further right than a third of
// the diff.
func (m *model) entryLeft(w int) int {
	return min(m.codeColumn(m.files[m.rows[m.entryRow()].file]), w/3)
}

// entryWidth returns how wide the input box wraps its text in a diff w cells wide: inside
// "┃ " and " ┃", less a cell for the cursor after a full row.
func (m *model) entryWidth(w int) int {
	return max(w-m.entryLeft(w)-5, 1)
}

// entryRows returns how many rows of its text the input box shows at most: maxEntryRows, or
// fewer on a screen too short for them with the box's row, its two borders and the file
// header that stays on top.
func (m *model) entryRows() int {
	return max(min(maxEntryRows, m.diffHeight()-4), 1)
}

// entryHeight returns how many lines of the screen the open input box takes in a diff w
// cells wide, its borders included.
func (m *model) entryHeight(w int) int {
	shown, _, _ := m.entry.layout(m.entryWidth(w), m.entryRows())
	return len(shown) + 2
}

// entryLines draws the open input box in a diff w cells wide the way tuicr draws its comment
// input, under the row it is about and from the code column to the right edge, but in heavy
// lines in the accent color, which set it apart from the other boxes with colors or without.
// Its top border holds its title, and inside are the rows of the text it shows, with the
// cursor's cell reversed in the accent color, or while nothing is typed the cursor and a
// dim placeholder.
func (m *model) entryLines(w int) []string {
	t := &m.theme
	x := m.entryLeft(w)
	width := w - x
	pad := strings.Repeat(" ", x)
	border := t.paint(t.accent, t.surface).Bold(true)
	base, text := t.paint(nil, t.surface), t.paint(t.fg, t.surface)
	cursor := t.paint(t.accent, t.surface).Reverse(true)
	shown, at, _ := m.entry.layout(m.entryWidth(w), m.entryRows())
	lines := make([]string, 0, len(shown)+2)
	lines = append(lines, pad+m.entryTop(width))
	s, c := m.entry.text, m.entry.cursor
	for k, r := range shown {
		l := newLine(width - 1) // the right border comes after
		l.add("┃ ", border)
		switch {
		case k != at:
			l.add(display(s[r.start:r.end]), text)
		case s == "":
			l.add(" ", cursor)
			l.add(m.entryPlaceholder(), t.paint(t.muted, t.surface))
		case c < r.end:
			next := m.entry.next(c)
			l.add(display(s[r.start:c]), text)
			l.add(display(s[c:next]), cursor)
			l.add(display(s[next:r.end]), text)
		default:
			l.add(display(s[r.start:c]), text)
			l.add(" ", cursor)
		}
		lines = append(lines, pad+l.fill(base)+border.Render("┃"))
	}
	bottom := newLine(width - 1)
	bottom.add("┗"+strings.Repeat("━", width), border)
	return append(lines, pad+bottom.fill(border)+border.Render("┛"))
}

// entryTop draws the top border of the input box, w cells wide, with the title in it: what
// the box does and for whom in the accent color, the lines it is about, the model picked
// with ^, and the keys that send or save, start a new line, pick a model and cancel, dim. A
// narrow box leaves out the keys first, and then cuts the rest with …, never through a wide
// character.
func (m *model) entryTop(w int) string {
	t := &m.theme
	border := t.paint(t.accent, t.surface).Bold(true)
	action, lines := m.entryTitle()
	title := []span{{text: action, color: t.accent, bold: true}, {text: " · ", color: t.muted}, {text: lines, color: t.fg}}
	if pick := m.entry.pick; pick != "" {
		title = append(title, span{text: " · ", color: t.muted}, span{text: "^" + review.Visible(pick), color: t.fg})
	}
	hint := span{text: m.entryHint(), color: t.muted}
	room := w - 6 // "┏━ ", a space after the title, a ━ at least and "┓"
	switch n := spansWidth(title); {
	case n+ansi.StringWidth(hint.text) <= room:
		title = append(title, hint)
	case n > room:
		title = append(cutSpans(title, room-1), span{text: "…", color: t.muted})
	}
	l := newLine(w - 1) // "┓" comes after
	l.add("┏━", border)
	if room > 0 {
		l.add(" ", border)
		for _, s := range title {
			l.add(s.text, t.paint(s.color, t.surface).Bold(s.bold))
		}
		l.add(" ", border)
	}
	l.add(strings.Repeat("━", l.free), border)
	return l.fill(border) + border.Render("┓")
}

// entryTitle returns what the input box does and for whom, "Ask claude c0875bbe #1" or
// "Comment for claude c0875bbe", and where its lines are, "L48" or "L19-21".
func (m *model) entryTitle() (action, lines string) {
	if t := m.asking; t != nil {
		return "Ask " + t.label, m.lineLabel(t.lines)
	}
	in := m.drafting
	action = "Comment for " + in.who
	if in.editing != nil {
		action = "Edit comment for " + in.who
	}
	return action, "L" + lineRange(in.from, in.to)
}

// entryHint returns the keys the title of the input box tells of: Enter, which sends a
// question or saves a comment, the key that starts a new line, Shift-Enter when the
// terminal tells it from Enter and Alt-Enter otherwise, ^, which picks a model, and Esc.
func (m *model) entryHint() string {
	verb, newline := "save", "Alt-Enter"
	if m.asking != nil {
		verb = "send"
	}
	if m.shiftEnter {
		newline = "Shift-Enter"
	}
	return " (Enter:" + verb + " " + newline + ":newline ^:model Esc:cancel)"
}

// entryPlaceholder returns what the input box shows dim while nothing is typed: for a what
// Enter then asks, and for c what to write.
func (m *model) entryPlaceholder() string {
	if m.asking != nil {
		return m.asking.placeholder
	}
	return commentPlaceholder
}

// lineLabel returns where lines, lines of model.lines in the order of the diff, are, as the
// title of the input box says it: "L48" or "L19-21" in the current file, or in base when all
// of them are deleted lines.
func (m *model) lineLabel(lines []int) string {
	from, to := m.currentRange(lines)
	if from == 0 {
		from, to = m.lines[lines[0]].Old, m.lines[lines[len(lines)-1]].Old
	}
	return "L" + lineRange(from, to)
}

// entryCursor returns the cell of the screen where the text typed in the open input box
// goes in, for the terminal's cursor, so that an input method composes its characters
// there. It reports false when that cell is not on the screen.
func (m *model) entryCursor() (x, y int, ok bool) {
	r := m.entryRow()
	if r < m.top {
		return 0, 0, false
	}
	_, w := m.layout()
	_, row, column := m.entry.layout(m.entryWidth(w), m.entryRows())
	y = m.lineOf(r) + 1 + 1 + row // the row and the top border; its comments' boxes come after
	if row < 0 || y >= m.diffHeight() {
		return 0, 0, false
	}
	x = m.width - w + m.entryLeft(w) + 2 + column // the file tree and the line beside it, and "┃ "
	return min(x, m.width-1), y, true
}

// underHeight returns how many lines of the screen what comes under row i takes, the origin
// box aside: the boxes of its comments, and the open input box with its model list.
func (m *model) underHeight(i int) int {
	n := m.commentHeight(i)
	if i == m.entryRow() {
		_, w := m.layout()
		n += m.entryHeight(w) + m.pickerHeight(w)
	}
	return n
}

// underLines draws what comes under row i in a diff w cells wide, the origin box aside: the
// open input box right under its row, as tuicr places it, and its model list, then the boxes
// of its comments. They are only drawn, and moving through the rows does not see them.
func (m *model) underLines(i, w int) []string {
	var lines []string
	if i == m.entryRow() {
		lines = append(m.entryLines(w), m.pickerLines(w)...)
	}
	return append(lines, m.commentLines(i, w)...)
}

// lineOf returns the line of the diff that shows row i, at or below the top row, as
// diffLines draws it while the origin box is closed.
func (m *model) lineOf(i int) int {
	line := 0
	if m.sticky() {
		line++ // the file header that stays on top
	}
	for k := m.top; k < i; k++ {
		line += 1 + m.underHeight(k)
		if m.rows[k+1].kind == fileRow {
			line++ // the blank line above a file header
		}
	}
	return line
}

// cutSpans returns spans cut to w cells, without splitting a wide character.
func cutSpans(spans []span, w int) []span {
	var out []span
	for _, s := range spans {
		s.text = ansi.Truncate(s.text, max(w, 0), "")
		w -= ansi.StringWidth(s.text)
		out = append(out, s)
	}
	return out
}
