package tui

import (
	"image/color"
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
// A ^ typed at the start of a word opens the model finder and types nothing, and while the
// finder is open pickKey takes every key. It returns the command that asks, or that lists
// the agent's models.
func (m *model) typeBox(msg tea.KeyPressMsg) tea.Cmd {
	if m.entry.picker != nil {
		m.pickKey(msg)
		return nil
	}
	switch k := msg.String(); k {
	case "enter":
		if m.asking != nil {
			return m.send()
		}
		m.saveComment()
	case "esc", "ctrl+c":
		m.closeBox()
	default:
		if msg.Text == "^" && m.entry.opensPicker() {
			return m.openPicker()
		}
		_, w := m.layout()
		m.entry.edit(k, msg.Text, m.entryWidth(w))
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
// "│ ❯ " and " │", less a cell for the cursor after a full row.
func (m *model) entryWidth(w int) int {
	return max(w-m.entryLeft(w)-7, 1)
}

// entryRows returns how many rows of its text the input box shows at most: maxEntryRows, or
// fewer on a screen too short for them with the box's row, its two borders and the file
// header that stays on top.
func (m *model) entryRows() int {
	return max(min(maxEntryRows, m.rowsHeight()-4), 1)
}

// entryHeight returns how many lines of the screen the open input box takes in a diff w
// cells wide, its borders included.
func (m *model) entryHeight(w int) int {
	shown, _, _ := m.entry.layout(m.entryWidth(w), m.entryRows())
	return len(shown) + 2
}

// entryTone returns the color of the open input box: that of questions for a, of comments
// for c.
func (m *model) entryTone() color.Color {
	if m.asking != nil {
		return m.theme.ask
	}
	return m.theme.pending
}

// frame is the glyphs of a box's border: its corners, top left, top right, bottom left and
// bottom right, and its horizontal and vertical lines.
type frame struct{ tl, tr, bl, br, h, v string }

var (
	roundFrame = frame{"╭", "╮", "╰", "╯", "─", "│"}
	heavyFrame = frame{"┏", "┓", "┗", "┛", "━", "┃"}
)

// entryFrame returns the border of the input box: rounded, or heavy without colors, where
// the outline cannot tell the box that takes the keys by its color.
func (m *model) entryFrame() frame {
	if m.theme.plain {
		return heavyFrame
	}
	return roundFrame
}

// entryLines draws the open input box in a diff w cells wide under the row it is about, from
// the code column to the right edge, in an outline of its color, the only box on the screen
// with one: violet to ask, amber to comment. Its top border holds a pill of what it does,
// its title and the model it goes with, and its bottom border what sending does. ❯ to ask or
// ✎ to comment comes before the text, and the cursor is a block in the box's color; nothing
// typed, a dim placeholder shows, its first character under the cursor. While the model
// finder is open the keys go to it, and the box shows no cursor.
func (m *model) entryLines(w int) []string {
	t := &m.theme
	x := m.entryLeft(w)
	width := w - x
	pad := strings.Repeat(" ", x)
	f, tone := m.entryFrame(), m.entryTone()
	border := t.paint(tone, t.surface)
	base, text, dim := t.paint(nil, t.surface), t.paint(t.strong, t.surface), t.paint(t.subtle, t.surface)
	cursor := t.onBand(t.surface, tone)
	if m.entry.picker != nil {
		cursor = text
	}
	mark := "❯ "
	if m.drafting != nil {
		mark = "✎ "
	}
	shown, at, _ := m.entry.layout(m.entryWidth(w), m.entryRows())
	lines := make([]string, 0, len(shown)+2)
	lines = append(lines, pad+m.entryTop(width))
	s, c := m.entry.text, m.entry.cursor
	for k, r := range shown {
		l := newLine(width - 1) // the right border comes after
		l.add(f.v+" ", border)
		if k == 0 && m.entry.top == 0 {
			l.add(mark, t.paint(tone, t.surface).Bold(true))
		} else {
			l.add("  ", base)
		}
		switch {
		case k != at:
			l.add(display(s[r.start:r.end]), text)
		case s == "":
			placeholder := display(m.entryPlaceholder())
			first, _ := ansi.FirstGraphemeCluster(placeholder, ansi.GraphemeWidth)
			if m.entry.picker != nil {
				l.add(placeholder, dim)
				break
			}
			l.add(first, cursor)
			l.add(placeholder[len(first):], dim)
		case c < r.end:
			next := m.entry.next(c)
			l.add(display(s[r.start:c]), text)
			l.add(display(s[c:next]), cursor)
			l.add(display(s[next:r.end]), text)
		default:
			l.add(display(s[r.start:c]), text)
			l.add(" ", cursor)
		}
		lines = append(lines, pad+l.fill(base)+border.Render(f.v))
	}
	bottom := newLine(width - 1)
	bottom.add(f.bl, border)
	if note := " " + m.entryNote() + " "; ansi.StringWidth(note)+3 <= width {
		bottom.add(strings.Repeat(f.h, width-3-ansi.StringWidth(note)), border)
		bottom.add(note, dim)
	}
	bottom.add(strings.Repeat(f.h, bottom.free), border)
	return append(lines, pad+bottom.fill(border)+border.Render(f.br))
}

// entryTop draws the top border of the input box, w cells wide: a pill of what the box does,
// ASK, COMMENT or EDIT, its title, and at the right the model it goes with. A narrow box
// leaves out the model first, and then cuts the title with …, never through a wide
// character.
func (m *model) entryTop(w int) string {
	t := &m.theme
	f, tone := m.entryFrame(), m.entryTone()
	border := t.paint(tone, t.surface)
	pill := " " + m.entryPill() + " "
	title := m.entryTitle()
	chip, chipColor := m.modelChip()
	chip = " " + chip + " "
	room := w - 4 - ansi.StringWidth(pill) // "╭─" and "─╮"
	titleWidth := spansWidth(title) + 2    // a space on each side
	switch {
	case titleWidth+ansi.StringWidth(chip)+1 <= room: // a line at least between them
	case titleWidth <= room:
		chip = ""
	case room < 4: // the pill alone, or not even that
		chip, title = "", nil
	default:
		chip, title = "", append(cutSpans(title, room-3), span{text: "…", color: t.muted})
	}
	l := newLine(w - 1) // the corner comes after
	l.add(f.tl+f.h, border)
	if room >= 0 {
		l.add(pill, t.pill(t.base, tone))
	}
	if title != nil {
		l.add(" ", border)
		for _, s := range title {
			l.add(s.text, t.paint(s.color, t.surface).Bold(s.bold))
		}
		l.add(" ", border)
	}
	if n := l.free - ansi.StringWidth(chip) - 1; n >= 0 {
		l.add(strings.Repeat(f.h, n), border)
		l.add(chip, t.paint(chipColor, t.surface))
	}
	l.add(strings.Repeat(f.h, l.free), border)
	return l.fill(border) + border.Render(f.tr)
}

// entryPill returns what the pill of the input box says: ASK for a, COMMENT for c, and EDIT
// for c on a comment.
func (m *model) entryPill() string {
	switch {
	case m.asking != nil:
		return "ASK"
	case m.drafting.editing != nil:
		return "EDIT"
	}
	return "COMMENT"
}

// entryTitle returns the title of the input box: for a whom it asks, its tag in the color of
// its origin and how the session relates to the lines, whether it follows up the questions
// in the origin box, and where its lines are, "claude c1a0 #1 · L48"; for c whom the comment
// goes to, its tag in the color of its session, and its range, "→ claude c1a0 · L19-21".
func (m *model) entryTitle() []span {
	t := &m.theme
	sep := span{text: " · ", color: t.muted}
	if a := m.asking; a != nil {
		tag := review.Visible(a.tag)
		c := m.boxColor(a.Ask.Agent, a.Ask.Session)
		if a.Relation == review.Author && a.Edit != nil {
			c = m.originColor(a.Edit) // the tag names its request, a fix dr applied too
		}
		title := []span{{text: tag, color: c, bold: true}, {text: strings.TrimPrefix(review.Visible(a.label), tag), color: t.fg}}
		if a.followUp {
			title = append(title, sep, span{text: "follow-up", color: t.fg})
		}
		return append(title, sep, span{text: m.lineLabel(a.lines), color: t.fg})
	}
	in := m.drafting
	agentName, id := in.target.Ask.Agent, in.target.Ask.Session
	if c := in.editing; c != nil {
		agentName, id = c.Agent, c.Session
	}
	tag := review.Visible(review.Who(agentName, id, m.short))
	return []span{
		{text: "→ ", color: t.subtle}, {text: tag, color: m.boxColor(agentName, id), bold: true}, {text: strings.TrimPrefix(in.who, tag), color: t.fg},
		sep, {text: "L" + lineRange(in.from, in.to), color: t.fg},
	}
}

// boxColor returns the color of the session of agentName with id that the input box sends
// to: its session color, or quiet for a new session, which has none.
func (m *model) boxColor(agentName, id string) color.Color {
	if id == "" {
		return m.theme.subtle
	}
	return m.sessionColor(session{agentName, id})
}

// modelChip returns what the top border of the input box says of the model it goes with,
// and in which color: the session's own, quiet; the one picked with ^ for this question or
// comment alone, in the color of comments; or, while the model finder is open, that a model
// is being picked, in the color of questions.
func (m *model) modelChip() (string, color.Color) {
	t := &m.theme
	what := "this question"
	if m.drafting != nil {
		what = "this comment"
	}
	name, own := m.boxSession()
	switch {
	case m.entry.picker != nil:
		return "^ picking a model…", t.ask
	case m.entry.pick != "":
		return "^ " + review.Visible(m.entry.pick) + " · " + what, t.pending
	case own == "":
		return "^ " + review.Visible(name) + "'s default model", t.muted
	}
	return "^ " + review.Visible(own) + " · session", t.muted
}

// entryNote returns what the bottom border of the input box says sending does. a asks a
// fork of the session with read-only tools, which leaves the session as it is, or goes on
// in the fork that answered before, which saw the earlier questions; with no session to
// fork it starts a new one. c saves the comment as pending until :submit asks for its fix,
// and an edit saved empty removes the comment.
func (m *model) entryNote() string {
	if a := m.asking; a != nil {
		if fork := m.fork(session{a.Ask.Agent, a.Ask.Session}, a.Base); fork != "" {
			n := 0
			for _, q := range m.questions {
				if q.answer != nil && q.answer.Fork == fork {
					n++
				}
			}
			if n == 0 {
				return "continues the same fork; the session is not changed"
			}
			return "continues the same fork; it has seen " + countOf(n, "earlier question")
		}
		if a.Ask.Session == "" {
			return "starts a new read-only session"
		}
		return "asks a read-only fork; the session is not changed"
	}
	if m.drafting.editing != nil {
		return "save it empty to remove it"
	}
	return "saved as pending; :submit asks for its fix"
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
	if row < 0 || y >= m.rowsHeight() {
		return 0, 0, false
	}
	x = m.width - w + m.entryLeft(w) + 4 + column // the file tree and the line beside it, and "│ ❯ "
	return min(x, m.width-1), y + m.bandLines(), true
}

// underHeight returns how many lines of the screen what comes under row i takes, the origin
// card aside: the cards of its comments, which the open origin card holds under the cursor
// row, and the open input box.
func (m *model) underHeight(i int) int {
	n := m.commentHeight(i)
	if i == m.entryRow() {
		_, w := m.layout()
		n += m.entryHeight(w)
	}
	return n
}

// underLines draws what comes under row i in a diff w cells wide: the open input box right
// under its row, as tuicr places it, then the open origin card under the cursor row, then
// the cards of its comments. They are only drawn, and moving through the rows does not see
// them. The model finder floats over the diff and is not among them.
func (m *model) underLines(i, w int) []string {
	var lines []string
	if i == m.entryRow() {
		lines = m.entryLines(w)
	}
	if m.reading && i == m.cursor {
		lines = append(lines, m.box(w)...)
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
