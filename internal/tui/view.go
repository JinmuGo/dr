package tui

import (
	"fmt"
	"image/color"
	"path"
	"slices"
	"strconv"
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

// noChanges is what the diff and the status line say when no changed file is left.
const noChanges = "No changed files"

// helpKeys is the key help: each key and what it does.
var helpKeys = [][2]string{
	{"j k", "Down, up a line. Arrows work too. Takes a count (5j)"},
	{"gg G", "Top, bottom. With a count, go to that row (12G)"},
	{"d u J K", "Half a page down, up"},
	{"} {", "Next, previous change"},
	{"l h", "Next, previous file. Arrows work too. Takes a count (3l)"},
	{"/ n N", "Search, next, previous. Lowercase queries ignore case"},
	{"Enter", "Open the origin under the line. j k d u J K gg G scroll, a asks a follow-up, q Esc Enter close"},
	{"s", "Switch between split and unified. Narrow screens stay unified"},
	{"v", "Mark the file viewed or not. Viewed files fold to their header"},
	{"V", "Select lines, as in vim. Moves extend it in the file. V or Esc ends it"},
	{"a", "Ask the session that wrote the line or the selection, typed in a box under the line. Lines without an origin go to the session that last edited the file, or a new session"},
	{"e", "Edit the line in your editor ($VISUAL or $EDITOR)"},
	{"ga", "Open a fork of the session that wrote the line in its agent, for changes bigger than a comment; dr redraws when you leave it"},
	{"c", "Leave a change comment on the line or the selection for the session that wrote it, typed in a box under the line, or edit the comment there. Saved empty, it goes. Nothing is sent yet"},
	{"a c box", "Enter send or save · Shift-Enter or Alt-Enter new line · ^ pick a model · Esc or ctrl+c cancel"},
	{":submit", "Send the pending comments to the sessions that wrote their lines"},
	{"y x", "Apply, drop the fix under the comment"},
	{":undo", "Revert the last fix dr applied"},
	{"Esc", "Clear a pending count or key"},
	{"?", "Help"},
	{"q", "Quit"},
	{"Mouse", "Drag the line between tree and diff to resize the tree, all the way left to fold it. The wheel scrolls. Click a file in the tree to go to it, or the Viewed box on a file header to mark it. Hold Shift or Option to select text"},
}

// View draws the screen: the file tree and the diff side by side, with the origin box open
// under the cursor row when Enter opened it, and the status line under them. The terminal's
// cursor shows where the text typed in the open input box of a or c goes in, or else after
// the search or the : input on the status line. It takes the mouse, for dragging the file
// tree's edge and for the wheel.
func (m *model) View() tea.View {
	v := tea.NewView(m.draw())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	switch typed := m.inputText(); {
	case m.typing():
		if x, y, ok := m.entryCursor(); ok {
			v.Cursor = tea.NewCursor(x, y)
		}
	case typed != "":
		v.Cursor = tea.NewCursor(1+ansi.StringWidth(typed), m.height-1)
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

// inputText returns what the status line shows of the open search or : input: its prompt
// and what is typed. When they do not fit on the line with the cursor after them, their
// start is cut, so what is typed and the cursor stay on the screen. It returns "" when no
// input is open.
func (m *model) inputText() string {
	prompt := m.inputPrompt()
	if prompt == "" {
		return ""
	}
	return keepEnd(prompt+review.Visible(m.input), m.width-2) // a space before it and the cursor after it
}

// keepEnd returns s cut at the start to w cells, with … for what is cut. A wide character is
// not split: one that the cut goes through goes too.
func keepEnd(s string, w int) string {
	n := ansi.StringWidth(s)
	if n <= w {
		return s
	}
	if w < 1 {
		return ""
	}
	cut := ansi.TruncateLeft(s, n-w+1, "…") // the … takes a cell
	if ansi.StringWidth(cut) > w {
		cut = ansi.TruncateLeft(s, n-w+2, "…")
	}
	return cut
}

// draw returns the screen, m.height lines of m.width cells.
func (m *model) draw() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if len(m.rows) == 0 {
		return m.drawEmpty()
	}
	h := m.diffHeight()
	listWidth, diffWidth := m.layout()
	var list []string
	if listWidth > 0 {
		list = m.fileTree(listWidth, h)
	}
	diff := m.diffLines(diffWidth, h)
	if m.help {
		m.drawHelp(diff, diffWidth)
	}
	separator := m.theme.paint(m.theme.border, nil).Render("│")
	var b strings.Builder
	for y := range h {
		switch {
		case m.folded && y == 0:
			b.WriteString(m.theme.paint(m.theme.accent, nil).Render("▸"))
		case m.folded:
			b.WriteString(separator)
		case list != nil:
			b.WriteString(list[y] + separator)
		}
		b.WriteString(diff[y] + "\n")
	}
	b.WriteString(m.status())
	return b.String()
}

// drawEmpty returns the screen when no changed file is left: noChanges in the middle of the
// diff, which takes the whole width, the key help over it when it is open, and the status
// line.
func (m *model) drawEmpty() string {
	t := &m.theme
	h := m.diffHeight()
	lines := make([]string, h)
	for y := range lines {
		lines[y] = strings.Repeat(" ", m.width)
	}
	l := newLine(m.width)
	l.add(strings.Repeat(" ", max((m.width-ansi.StringWidth(noChanges))/2, 0)), t.paint(nil, nil))
	l.add(noChanges, t.paint(t.muted, nil))
	lines[(h-1)/2] = l.fill(t.paint(nil, nil))
	if m.help {
		m.drawHelp(lines, m.width)
	}
	return strings.Join(lines, "\n") + "\n" + m.status()
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

// diffLines draws the rows on the screen, w cells wide and h lines high. A blank line comes
// above the header of each file but the top row, as GitHub leaves a gap between files; it
// is only drawn, and moving through the rows does not see it. The boxes of the comments on
// lines come under the row of their last line of the current file, and the open input box
// of a or c under the row it is about, after them; they are only drawn too. The open origin
// box comes right under the cursor row and its comments, and pushes the rows after it down.
func (m *model) diffLines(w, h int) []string {
	blank := strings.Repeat(" ", w)
	lines := make([]string, 0, h+1)
	for i := m.top; len(lines) < h; i++ {
		if i >= len(m.rows) {
			lines = append(lines, blank)
			continue
		}
		if i > m.top && m.rows[i].kind == fileRow {
			lines = append(lines, blank)
		}
		lines = append(lines, m.drawRow(i, w))
		lines = append(lines, m.underLines(i, w)...)
		if m.reading && i == m.cursor {
			lines = append(lines, m.box(w)...)
		}
	}
	return lines[:h]
}

// drawRow draws row i of the diff, w cells wide, the way GitHub shows a pull request's
// files: a band for the header of a file, a tinted row for the header of a hunk, and a line
// with its line numbers and its sign, or in the split view a line on each side of a middle
// line. The cursor row has a bar at its left edge and a lighter background, so it keeps the
// colors of the diff; a row with questions about it has a ? there instead. Rows of a
// selection are tinted yellow.
func (m *model) drawRow(i, w int) string {
	t := &m.theme
	r := m.rows[i]
	f := m.files[r.file]
	cursor := i == m.cursor
	code := m.codeColumn(f)
	l := newLine(w)
	switch r.kind {
	case fileRow:
		return m.drawHeader(f, w, cursor)
	case hunkRow:
		numberBg, lineBg := t.hunkNumber, t.hunkLine
		if cursor {
			numberBg, lineBg = t.under(numberBg), t.under(lineBg)
		}
		l.add(cursorBar(cursor), t.paint(t.accent, numberBg).Reverse(cursor && t.plain))
		l.add(strings.Repeat(" ", code-3), t.paint(nil, numberBg).Reverse(cursor && t.plain)) // up to the sign
		l.add(r.text, t.paint(t.muted, lineBg))
		return l.fill(t.paint(nil, lineBg))
	case noteRow:
		var bg color.Color
		if cursor {
			bg = t.cursor
		}
		l.add(cursorBar(cursor), t.paint(t.accent, bg).Reverse(cursor && t.plain))
		l.add(strings.Repeat(" ", code-1), t.paint(nil, bg))
		l.add(f.noDiff, t.paint(t.muted, bg))
		return l.fill(t.paint(nil, bg))
	}
	bar := span{text: " "}
	switch {
	case cursor:
		bar = span{text: "▌", color: t.accent}
	case m.hasQuestions(i):
		bar = span{text: "?", color: t.muted}
	}
	selected := m.isSelected(i)
	if !m.splitShown {
		line := m.lines[r.main()]
		numbers := fmt.Sprintf(" %*s %*s ", f.digits, lineNumber(line.Old), f.digits, lineNumber(line.New))
		change := byte(' ')
		switch {
		case r.left < 0:
			change = '+'
		case r.right < 0:
			change = '-'
		}
		return l.fill(m.side(l, bar, numbers, change, m.pieces(r.main(), change == '-'), cursor, selected))
	}
	right := newLine((w - 2) / 2) // as wide as the left side less the bar, which the left has
	left := newLine(w - 1 - right.free)
	leftChange, rightChange := byte(' '), byte(' ')
	if r.isChange() {
		leftChange, rightChange = '-', '+'
	}
	var leftFill, rightFill lipgloss.Style
	if r.left >= 0 {
		numbers := fmt.Sprintf(" %*s ", f.digits, lineNumber(m.lines[r.left].Old))
		leftFill = m.side(left, bar, numbers, leftChange, m.pieces(r.left, true), cursor, selected)
	} else {
		leftFill = m.emptySide(left, bar, cursor, selected)
	}
	if r.right >= 0 {
		numbers := fmt.Sprintf(" %*s ", f.digits, lineNumber(m.lines[r.right].New))
		rightFill = m.side(right, span{}, numbers, rightChange, m.pieces(r.right, false), cursor, selected)
	} else {
		rightFill = m.emptySide(right, span{}, cursor, selected)
	}
	var middle color.Color
	if cursor {
		middle = t.cursor
	}
	return left.fill(leftFill) + t.paint(t.border, middle).Render("│") + right.fill(rightFill)
}

// drawHeader draws the header of file f, w cells wide, as GitHub heads a file: a fold mark,
// ▸ when the file is viewed and folds to its header, then the path, the file name the
// brightest text of the screen and its directory quieter. At the right edge come that the
// diff changed since the file was marked viewed, the counts of added and deleted lines with
// the diffstat, how many comments the file has, and the Viewed box. The first three
// show only when they leave the path its room, the comments before the counts, and the box
// always shows. A ? at the left edge tells of earlier questions about the file in its
// origin box.
func (m *model) drawHeader(f file, w int, cursor bool) string {
	t := &m.theme
	band := func(fg color.Color) lipgloss.Style { return t.onBand(fg, t.header, cursor) }
	mark := " ▾ "
	if f.viewed {
		mark = " ▸ "
	}
	var changed, counts []span
	if f.changed {
		changed = []span{{text: "Changed since last view", color: t.warning}}
	}
	if f.noDiff == "" {
		added, deleted, rest := diffstat(f.added, f.deleted)
		counts = []span{
			{text: fmt.Sprintf("+%d", f.added), color: t.added},
			{text: " "},
			{text: fmt.Sprintf("-%d", f.deleted), color: t.deleted},
			{text: " "},
			{text: strings.Repeat("■", added), color: t.added},
			{text: strings.Repeat("■", deleted), color: t.deleted},
			{text: strings.Repeat("□", rest), color: t.subtle},
		}
	}
	check := span{text: "☐", color: t.muted}
	if f.viewed {
		check = span{text: "☑", color: t.accent}
	}
	left := 1 + ansi.StringWidth(mark) + ansi.StringWidth(f.path) // with the cursor bar
	right := fitParts([]statusPart{
		{spans: changed, rank: 1, keep: left},
		{spans: counts, rank: 3, keep: left},
		{spans: m.commentSpans(f.key), rank: 2, keep: left},
		{spans: []span{check, {text: viewedBox, color: t.text}}},
	}, w-2) // a space before the right part and a margin after it, as on a band
	rightWidth := 2 + spansWidth(right)
	l := newLine(w - rightWidth)
	bar := span{text: " "}
	switch {
	case cursor:
		bar = span{text: "▌", color: t.accent}
	case len(f.earlier) > 0:
		bar = span{text: "?", color: t.muted}
	}
	l.add(bar.text, band(bar.color))
	l.add(mark, band(t.muted))
	dir, name := path.Split(f.path)
	l.add(dir, band(t.muted).Bold(true))
	l.add(name, band(t.title).Bold(true))
	r := newLine(min(rightWidth, w))
	r.add(" ", band(nil))
	for _, s := range right {
		r.add(s.text, band(s.color).Bold(s.bold))
	}
	return l.fill(band(nil)) + r.fill(band(nil))
}

// codeColumn returns the column where the code of f starts in a row of the diff, on the left
// side in the split view: after the cursor bar, the line numbers and the sign.
func (m *model) codeColumn(f file) int {
	if m.splitShown {
		return f.digits + 5
	}
	return 2*f.digits + 6
}

// side writes a line on l: bar, then numbers on the background of its line numbers, then
// its sign and pieces on the background of its kind of line. change is '-', '+' or ' ' for
// a deleted, an added or an unchanged line. A selected line is tinted, and without colors
// shows in reverse video. It returns the style that fills the rest of the line.
func (m *model) side(l *line, bar span, numbers string, change byte, pieces []piece, cursor, selected bool) lipgloss.Style {
	t := &m.theme
	var numberBg, lineBg, wordBg, signColor color.Color // an unchanged line's sign is blank
	numberColor := t.subtle
	switch change {
	case '+':
		numberBg, lineBg, wordBg = t.addNumber, t.addLine, t.addWord
		signColor, numberColor = t.added, t.text
	case '-':
		numberBg, lineBg, wordBg = t.deleteNumber, t.deleteLine, t.deleteWord
		signColor, numberColor = t.deleted, t.text
	}
	if selected {
		numberBg, lineBg, wordBg = t.selected(numberBg, t.selectNumber), t.selected(lineBg, t.selectLine), t.selected(wordBg, t.selectLine)
		numberColor = t.text
	}
	if cursor {
		numberBg, lineBg, wordBg, numberColor = t.under(numberBg), t.under(lineBg), t.under(wordBg), t.text
	}
	reverse := selected && t.plain
	l.add(bar.text, t.paint(bar.color, numberBg).Reverse(cursor && t.plain))
	l.add(numbers, t.paint(numberColor, numberBg).Bold(cursor).Reverse(cursor && t.plain || reverse))
	l.add(string(change)+" ", t.paint(signColor, lineBg).Reverse(reverse))
	for _, p := range pieces {
		bg := lineBg
		if p.changed {
			bg = wordBg
		}
		l.add(p.text, t.paint(t.code(p.kind), bg).Reverse(reverse))
	}
	return t.paint(nil, lineBg).Reverse(reverse)
}

// emptySide writes bar on l for a side of the split view without a line, and returns the
// style that fills it: a quiet band, as GitHub shades a side without a line, tinted when its
// row is selected.
func (m *model) emptySide(l *line, bar span, cursor, selected bool) lipgloss.Style {
	t := &m.theme
	bg := t.band
	if selected {
		bg = t.selected(bg, t.selectLine)
	}
	if cursor {
		bg = t.under(bg)
	}
	l.add(bar.text, t.paint(bar.color, bg).Reverse(cursor && t.plain))
	return t.paint(nil, bg).Reverse(selected && t.plain)
}

// cursorBar returns the left edge of a row: a bar on the cursor row.
func cursorBar(cursor bool) string {
	if cursor {
		return "▌"
	}
	return " "
}

// diffstat returns how many of GitHub's five diffstat squares stand for added lines, for
// deleted lines and for neither. A file with fewer than five changed lines has a square for
// each, and a side with changes has at least one.
func diffstat(added, deleted int) (a, d, rest int) {
	total := added + deleted
	if total < 5 {
		return added, deleted, 5 - total
	}
	a = (added*5 + total/2) / total
	switch {
	case a == 0 && added > 0:
		a = 1
	case a == 5 && deleted > 0:
		a = 4
	}
	return a, 5 - a, 0
}

// lineNumber returns n as text, "" for 0.
func lineNumber(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// span is text in a color on a band.
type span struct {
	text  string
	color color.Color
	bold  bool
}

// band draws a band w cells wide with left at its left edge, cut to fit, and right at its
// right edge.
func (m *model) band(left, right []span, w int) string {
	t := &m.theme
	style := func(s span) lipgloss.Style { return t.onBand(s.color, t.band, false).Bold(s.bold) }
	base := style(span{})
	rightWidth := 0
	if right != nil {
		rightWidth = 2 + spansWidth(right) // with a space before it and a margin after it
	}
	l := newLine(w - rightWidth)
	l.add(" ", base)
	for _, s := range left {
		l.add(s.text, style(s))
	}
	out := l.fill(base)
	if right == nil {
		return out
	}
	r := newLine(min(rightWidth, w))
	r.add(" ", base)
	for _, s := range right {
		r.add(s.text, style(s))
	}
	return out + r.fill(base)
}

// status draws the status line under the diff. The left shows the open search or : input,
// or a message, or the size of the selection, or else the file under the cursor, or
// noChanges when no changed file is left, cut to the room the right leaves it. The right
// shows the note on the session logs dr could not fully read, how far the open origin box
// is read when its text does not fit, y and x on a line of a comment with a fix under it,
// that the diff is too narrow for the split view s asked for, the origin of a changed row
// under the cursor row, the row number, and a hint of keys, which the open input box of a or c
// leaves out as its title tells its keys. An open input goes first: the right leaves out
// what it must to make room for it and the cursor after it, the hint first and the row
// number last.
func (m *model) status() string {
	t := &m.theme
	left := []span{{text: noChanges, color: t.text}}
	hint := []span{{text: "?", color: t.accent, bold: true}, {text: " help", color: t.muted}}
	var problems, position, narrow, tag, row, fixKeys []span
	if len(m.rows) > 0 {
		r := m.rows[m.cursor]
		left = []span{
			{text: fmt.Sprintf("file %d/%d  ", r.file+1, len(m.files)), color: t.muted},
			{text: m.files[r.file].path, color: t.text},
		}
		hint = append([]span{{text: "Enter", color: t.accent, bold: true}, {text: " origin  ", color: t.muted}}, hint...)
		switch {
		case m.typing():
			hint = nil
		case m.reading:
			hint = nil
			if n, rows := len(m.originLines()), m.boxRows(); n > rows {
				position = []span{{text: fmt.Sprintf("origin %d/%d", min(m.readTop+rows, n), n), color: t.muted}}
				hint = []span{{text: "j k", color: t.accent, bold: true}, {text: " scroll", color: t.muted}}
			}
		}
		if m.splitOn && !m.splitShown {
			narrow = []span{{text: "too narrow for split", color: t.warning}}
		}
		if c := m.commentOn(m.cursor); c != nil && c.fix != nil {
			fixKeys = []span{{text: "y", color: t.accent, bold: true}, {text: " apply · ", color: t.muted}, {text: "x", color: t.accent, bold: true}, {text: " drop", color: t.muted}}
		}
		if r.isChange() {
			tag = []span{{text: "unknown", color: t.warning}}
			if e := m.lines[r.main()].Edit; e != nil {
				tag = []span{{text: review.Tag(e, m.short), color: t.accent}}
			}
		}
		row = []span{{text: fmt.Sprintf("row %d/%d", m.cursor+1, len(m.rows)), color: t.muted}}
	}
	typed := m.inputText()
	switch {
	case typed != "":
		left = []span{{text: typed, color: t.text}}
	case m.message != "" && m.news:
		left = []span{{text: m.message, color: t.accent}}
	case m.message != "":
		left = []span{{text: m.message, color: t.warning}}
	case m.selecting:
		left = []span{{text: "VISUAL", color: t.accent, bold: true}, {text: " " + countOf(len(m.selectedRows()), "line"), color: t.text}}
	}
	if m.problems != "" {
		problems = []span{{text: m.problems, color: t.warning}}
	}
	fit := func() []span {
		return fitParts([]statusPart{
			{spans: problems, rank: 3},
			{spans: position, rank: 1},
			{spans: fixKeys, rank: 1},
			{spans: narrow, rank: 2},
			{spans: tag},
			{spans: row},
			{spans: hint, rank: 4, keep: 40}, // 40 cells left for the file path
		}, m.width-2) // the band puts a space before the right part and a margin after it
	}
	right := fit()
	if typed != "" {
		need := 2 + spansWidth(left) // with the space before it and the cursor after it
		for _, drop := range []*[]span{&hint, &problems, &narrow, &fixKeys, &position, &tag, &row} {
			if right == nil || need <= m.width-2-spansWidth(right) {
				break
			}
			*drop = nil
			right = fit()
		}
	}
	return m.band(left, right, m.width)
}

// warn shows text in the status line, in the warning color, until the next key.
func (m *model) warn(text string) {
	m.message, m.news = text, false
}

// tell shows news in the status line, in the accent color, until the next key.
func (m *model) tell(text string) {
	m.message, m.news = text, true
}

// statusPart is a part of the right of the status line or of a file header. Parts of rank 0
// always show; the others take the room that is left, lower ranks first, each leaving keep
// cells of the line to the left part.
type statusPart struct {
	spans      []span
	rank, keep int
}

// fitParts returns the spans of the parts that fit in w cells, two cells apart and in the
// order of parts. A part that does not fit is left out whole.
func fitParts(parts []statusPart, w int) []span {
	order := make([]int, len(parts))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return parts[a].rank - parts[b].rank })
	shown := make([]bool, len(parts))
	used := -2 // the first part has no gap before it
	for _, i := range order {
		p := parts[i]
		if p.spans == nil {
			continue
		}
		if width := used + 2 + spansWidth(p.spans); p.rank == 0 || width <= w-p.keep {
			shown[i], used = true, width
		}
	}
	var out []span
	for i, p := range parts {
		if !shown[i] {
			continue
		}
		if out != nil {
			out = append(out, span{text: "  "})
		}
		out = append(out, p.spans...)
	}
	return out
}

// drawHelp draws the key help over lines, the diff w cells wide, in a box in its middle. A
// box that would leave only a sliver of the diff beside it takes the whole width.
func (m *model) drawHelp(lines []string, w int) {
	keyWidth, textWidth := helpWidths()
	width := min(keyWidth+2+textWidth+4, 84) // with the border and a space inside it
	if width > w-6 {
		width = w
	}
	box := m.helpBox(width)
	x, y := (w-width)/2, max((len(lines)-len(box))/2, 0)
	for k, b := range box {
		if y+k < len(lines) {
			lines[y+k] = overlay(lines[y+k], b, x, width, w)
		}
	}
}

// helpWidths returns the widths of the widest key and of the widest text of the key help.
func helpWidths() (keyWidth, textWidth int) {
	for _, k := range helpKeys {
		keyWidth = max(keyWidth, ansi.StringWidth(k[0]))
		textWidth = max(textWidth, ansi.StringWidth(k[1]))
	}
	return keyWidth, textWidth
}

// helpBox draws the key help in a box w cells wide: a border with a title, each key with
// what it does, and how to close it. It returns nil when w is too narrow for it.
func (m *model) helpBox(w int) []string {
	t := &m.theme
	keyWidth, _ := helpWidths()
	inner := w - 4 // inside the border and a space on each side
	textWidth := inner - keyWidth - 2
	if textWidth < 10 {
		return nil
	}
	base := t.paint(nil, t.band)
	border := t.paint(t.border, t.band)
	row := func(parts ...span) string {
		l := newLine(inner + 2)
		l.add(" ", base)
		for _, p := range parts {
			l.add(p.text, t.paint(p.color, t.band).Bold(p.bold))
		}
		return border.Render("│") + l.fill(base) + border.Render("│")
	}
	const title = "Help"
	lines := []string{
		border.Render("╭─ ") + t.paint(t.text, t.band).Bold(true).Render(title) +
			border.Render(" "+strings.Repeat("─", inner-1-ansi.StringWidth(title))+"╮"), // as wide as the rows
		row(),
	}
	for _, k := range helpKeys {
		key := k[0] + strings.Repeat(" ", keyWidth-ansi.StringWidth(k[0])+2)
		for _, text := range strings.Split(lipgloss.NewStyle().Width(textWidth).Render(k[1]), "\n") {
			lines = append(lines, row(span{text: key, color: t.accent, bold: true}, span{text: text, color: t.text}))
			key = strings.Repeat(" ", keyWidth+2)
		}
	}
	lines = append(lines,
		row(),
		row(span{text: "Press any key to close", color: t.muted}),
		border.Render("╰"+strings.Repeat("─", inner+2)+"╯"),
	)
	return lines
}

// overlay returns base, a line w cells wide, with top, a line topWidth cells wide, written
// over it from cell x.
func overlay(base, top string, x, topWidth, w int) string {
	left := ansi.Truncate(base, x, "")
	right := ansi.TruncateLeft(base, x+topWidth, "")
	if ansi.StringWidth(right) > w-x-topWidth { // a wide character across the edge of top
		right = " " + ansi.TruncateLeft(base, x+topWidth+1, "")
	}
	return left + strings.Repeat(" ", max(x-ansi.StringWidth(left), 0)) + ansi.ResetStyle + top + right
}

// line builds a screen line of a fixed width from styled text.
type line struct {
	b    strings.Builder
	free int // cells not written yet
}

// newLine returns a line w cells wide.
func newLine(w int) *line {
	return &line{free: max(w, 0)}
}

// add writes s in style st, cut to the cells not written yet. A wide character that does
// not fit is left out.
func (l *line) add(s string, st lipgloss.Style) {
	if s == "" || l.free == 0 {
		return
	}
	w := ansi.StringWidth(s)
	if w > l.free {
		s = ansi.Truncate(s, l.free, "")
		w = ansi.StringWidth(s)
	}
	l.b.WriteString(st.Render(s))
	l.free -= w
}

// fill writes spaces in style st over the cells not written yet and returns the line.
func (l *line) fill(st lipgloss.Style) string {
	if l.free > 0 {
		l.b.WriteString(st.Render(strings.Repeat(" ", l.free)))
		l.free = 0
	}
	return l.b.String()
}
