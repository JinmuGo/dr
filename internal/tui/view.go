package tui

import (
	"fmt"
	"image/color"
	"math/bits"
	"path"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/agent"
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

// diffLines draws the rows on the screen, w cells wide and h lines high. A blank line comes
// above the header of each file but the top row, as GitHub leaves a gap between files; it
// is only drawn, and moving through the rows does not see it. While the top row is inside a
// file, the header of that file stays on the top line above it. The cards of the comments on
// lines come under the row of their last line of the current file, and the open input box
// of a or c under the row it is about, before them; they are only drawn too. The open origin
// card comes right under the cursor row, before its comments, and pushes the rows after it
// down.
func (m *model) diffLines(w, h int) []string {
	blank := strings.Repeat(" ", w)
	lines := make([]string, 0, h+1)
	if m.sticky() {
		lines = append(lines, m.drawHeader(m.files[m.rows[m.top].file], w, false))
	}
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
	}
	return lines[:h]
}

// sticky reports whether the header of the file of the top row stays on the top line of the
// diff: whether the top row is inside a file, not its header, and the diff has a line for
// the header besides the cursor's.
func (m *model) sticky() bool {
	return m.diffHeight() >= 2 && m.top < len(m.rows) && m.rows[m.top].kind != fileRow
}

// drawRow draws row i of the diff, w cells wide, the way GitHub shows a pull request's
// files: a band for the header of a file, a tinted row for the header of a hunk with its
// text at the code column, and a line with its line numbers, its rail and its sign, or in
// the split view a line on each side of a middle line. The cursor row has a bar at its left
// edge and its own background, a shade of the colors of the diff; a row with questions about
// it has a ? there instead. Rows of a selection are tinted blue. Every match of the search
// in a line is lit, the current match brighter: in the split view on the left when the left
// side holds the query. The first line of a run of changes that runLabels marks ends with
// the label of its origin, on its side in the split view.
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
	case hunkRow, noteRow:
		bg, text, fg := t.hunkbg, r.text, t.hunkfg
		if r.kind == noteRow {
			bg, text, fg = nil, f.noDiff, t.muted
		}
		if cursor {
			bg = t.cursorline
		}
		l.add(cursorBar(cursor), t.paint(t.accent, bg).Reverse(cursor && t.plain))
		l.add(strings.Repeat(" ", code-1), t.paint(nil, bg)) // up to the code
		l.add(text, t.paint(fg, bg))
		return l.fill(t.paint(nil, bg))
	}
	bar := span{text: " "}
	switch {
	case cursor:
		bar = span{text: "▌", color: t.accent}
	case m.hasQuestions(i):
		bar = span{text: "?", color: t.ask, bold: true}
	}
	selected := m.isSelected(i)
	current := i == m.currentRow() // the row holds the current search match, its first
	if !m.splitShown {
		k := r.main()
		line := m.lines[k]
		s := sideText{
			bar:     bar,
			numbers: fmt.Sprintf(" %*s %*s ", f.digits, lineNumber(line.Old), f.digits, lineNumber(line.New)),
			rail:    m.rail(k),
			change:  ' ',
			pieces:  m.pieces(k, r.right < 0),
		}
		s.marks, s.current = m.sideMarks(s.pieces, current)
		switch {
		case r.left < 0:
			s.change = '+'
		case r.right < 0:
			s.change = '-'
		}
		s.labeled, s.label = m.labeled[k], line.Edit
		return l.fill(m.side(l, s, cursor, selected))
	}
	right := newLine((w - 2) / 2) // as wide as the left side less the bar, which the left has
	left := newLine(w - 1 - right.free)
	leftChange, rightChange := byte(' '), byte(' ')
	if r.isChange() {
		leftChange, rightChange = '-', '+'
	}
	var leftFill, rightFill lipgloss.Style
	var leftMarks [][2]int // the search's current match is on the right when the left has none
	if r.left >= 0 {
		s := sideText{
			bar: bar, numbers: fmt.Sprintf(" %*s ", f.digits, lineNumber(m.lines[r.left].Old)),
			rail: m.rail(r.left), change: leftChange, pieces: m.pieces(r.left, true),
			labeled: m.labeled[r.left], label: m.lines[r.left].Edit,
		}
		s.marks, s.current = m.sideMarks(s.pieces, current)
		leftMarks = s.marks
		leftFill = m.side(left, s, cursor, selected)
	} else {
		leftFill = m.emptySide(left, bar, cursor, selected)
	}
	if r.right >= 0 {
		s := sideText{
			numbers: fmt.Sprintf(" %*s ", f.digits, lineNumber(m.lines[r.right].New)),
			rail:    m.rail(r.right), change: rightChange, pieces: m.pieces(r.right, false),
			labeled: m.labeled[r.right], label: m.lines[r.right].Edit,
		}
		s.marks, s.current = m.sideMarks(s.pieces, current && leftMarks == nil)
		rightFill = m.side(right, s, cursor, selected)
	} else {
		rightFill = m.emptySide(right, span{}, cursor, selected)
	}
	var middle color.Color
	if cursor {
		middle = t.cursorline
	}
	return left.fill(leftFill) + t.paint(t.line, middle).Render("│") + right.fill(rightFill)
}

// statSquare is a square of the diffstat: Nerd Font's fa-stop, narrower than its cell, so
// that squares side by side keep a gap as on GitHub. A terminal without Nerd Font symbols
// shows boxes there.
const statSquare = "\uf04d"

// drawHeader draws the header of file f, w cells wide, as GitHub heads a file: a fold mark,
// ▸ when the file is viewed and folds to its header, then the path, the file name the
// brightest text of the screen and its directory quieter, quieter still when folded, and
// whether the file is new or deleted, then a dot in the color of each session that changed
// it and ┆ when it has changed lines without an origin. A chip tells that the diff changed
// since the file was marked viewed. At the right edge come how many comments the file has,
// the counts of added and deleted lines with the diffstat, and the Viewed box. The comments
// and the counts show only when they leave the path its room, the comments first, the dots
// and then the chip only when they fit too, and the box always shows. A ? at the left edge
// tells of earlier questions about the file in its origin box. The search's matches in the
// path are lit as in a line.
func (m *model) drawHeader(f file, w int, cursor bool) string {
	t := &m.theme
	on := func(fg color.Color) lipgloss.Style { return t.onBand(fg, t.surface2) }
	bar := seg{text: " ", st: on(nil)}
	switch {
	case cursor:
		bar = seg{text: "▌", st: on(t.accent)}
	case len(f.earlier) > 0:
		bar = seg{text: "?", st: on(t.ask).Bold(true)}
	}
	mark, dirColor, nameColor := "▾ ", t.muted, t.strong
	if f.viewed {
		mark, dirColor, nameColor = "▸ ", t.subtle, t.muted
	}
	dir, name := path.Split(f.path)
	row, marks, current := f.head, m.marks(f.path), -1
	if m.splitShown {
		row = f.splitHead
	}
	if marks != nil && row == m.currentRow() {
		current = 0
	}
	left := append([]seg{bar, {text: mark, st: on(t.subtle)}},
		m.markSegs([]seg{{text: dir, st: on(dirColor), cut: true}, {text: name, st: on(nameColor).Bold(true)}}, marks, current)...)
	switch f.status {
	case 'A':
		left = append(left, seg{text: " new", st: on(t.add)})
	case 'D':
		left = append(left, seg{text: " deleted", st: on(t.del)})
	}
	var comments, counts []seg
	if n := m.commentCount(f.key); n > 0 {
		comments = []seg{{text: commentMark + " " + strconv.Itoa(n), st: on(t.pending)}}
	}
	if f.noDiff == "" {
		counts = m.counts(f, on)
	}
	check, box := "☐", on(t.fg)
	if f.viewed {
		check, box = "☑", on(t.muted)
	}
	keep := segsWidth(left) + 2 // and the gap before the right
	right := append(fitGroups([]group{
		{segs: comments, rank: 1, keep: keep},
		{segs: counts, rank: 2, keep: keep},
		{segs: []seg{{text: check + viewedBox, st: box}}},
	}, w-1, seg{text: "    ", st: on(nil)}), seg{text: " ", st: on(nil)}) // a margin at the right edge
	if dots := m.sessionDots(f, on); dots != nil && keep+segsWidth(dots)+segsWidth(right) <= w {
		left = append(left, dots...)
		keep += segsWidth(dots)
	}
	if f.changed {
		chip := []seg{{text: "  ", st: on(nil)}, {text: " Changed since last view ", st: t.chip(t.pending, t.pending)}}
		if keep+segsWidth(chip)+segsWidth(right) <= w {
			left = append(left, chip...)
		}
	}
	return spread(left, right, w, on(nil))
}

// sessionDots returns the dots of a file header, in the style on gives each color: two
// spaces, then a ● in the color of each session that changed f, in the order of their
// colors, and ┆ when f has changed lines without an origin. It returns nil when f has no
// changed lines.
func (m *model) sessionDots(f file, on func(color.Color) lipgloss.Style) []seg {
	if len(f.sessions) == 0 && !f.unknown {
		return nil
	}
	dots := []seg{{text: "  ", st: on(nil)}}
	for _, s := range f.sessions {
		dots = append(dots, seg{text: "●", st: on(m.sessionColor(s))})
	}
	if f.unknown {
		dots = append(dots, seg{text: "┆", st: on(m.theme.subtle)})
	}
	return dots
}

// counts returns the counts of f's added and deleted lines, bold in their colors or quiet
// when 0, then its diffstat, each in the style on gives its color. Without colors the
// squares of the diffstat differ in shape instead: ■ added, ▨ deleted, □ neither.
func (m *model) counts(f file, on func(color.Color) lipgloss.Style) []seg {
	t := &m.theme
	plus, minus := on(t.subtle), on(t.subtle)
	if f.added > 0 {
		plus = on(t.add).Bold(true)
	}
	if f.deleted > 0 {
		minus = on(t.del).Bold(true)
	}
	added, deleted, empty := statSquare, statSquare, statSquare
	if t.plain {
		added, deleted, empty = "■", "▨", "□"
	}
	a, d, rest := diffstat(f.added, f.deleted)
	return []seg{
		{text: "+" + strconv.Itoa(f.added), st: plus},
		{text: " ", st: on(nil)},
		{text: "-" + strconv.Itoa(f.deleted), st: minus},
		{text: " ", st: on(nil)},
		{text: strings.Repeat(added, a), st: on(t.add)},
		{text: strings.Repeat(deleted, d), st: on(t.del)},
		{text: strings.Repeat(empty, rest), st: on(t.line2)},
	}
}

// codeColumn returns the column where the code of f starts in a row of the diff, on the left
// side in the split view: after the cursor bar, the line numbers, the rail and the sign.
func (m *model) codeColumn(f file) int {
	if m.splitShown {
		return f.digits + 6
	}
	return 2*f.digits + 7
}

// sideText is what side writes of a line.
type sideText struct {
	bar     span
	numbers string
	rail    span
	change  byte    // '-', '+' or ' ' for a deleted, an added or an unchanged line
	pieces  []piece // the code
	// labeled tells that the line ends with the label of its origin, label: the edit that
	// wrote or deleted the line, nil for none.
	labeled bool
	label   *agent.Edit
	// marks are where the search query is in the code, as byte offsets of the text of
	// pieces, and current which of them is the current match, -1 for none.
	marks   [][2]int
	current int
}

// Labels of runs of changes start labelGap cells after the code, and show when at least
// minLabel cells, and the whole dot and tag, fit before the margin at the right edge. The
// request's summary shows after the tag when at least minSummary cells are left for it.
const (
	labelGap   = 4
	minLabel   = 14
	minSummary = 8
)

// side writes a line on l: its bar, then its numbers on the background of its line numbers,
// then its rail, sign, code and label on the background of its kind of line, with the
// search's matches lit in the code. The cursor row has a shade of its line's background,
// and a selected line the blue one of its kind, which without colors shows in reverse
// video. It returns the style that fills the rest of the line.
func (m *model) side(l *line, s sideText, cursor, selected bool) lipgloss.Style {
	t := &m.theme
	var numberBg, lineBg, wordBg, signColor color.Color // an unchanged line's sign is blank
	numberColor, cursorBg, selectBg := t.faint, t.cursorline, t.selbg
	switch s.change {
	case '+':
		numberBg, lineBg, wordBg, cursorBg, selectBg = t.addnum, t.addbg, t.addword, t.addcur, t.seladd
		signColor, numberColor = t.add, t.subtle
	case '-':
		numberBg, lineBg, wordBg, cursorBg, selectBg = t.delnum, t.delbg, t.delword, t.delcur, t.seldel
		signColor, numberColor = t.del, t.subtle
	}
	switch {
	case selected:
		numberBg, lineBg, numberColor = t.selnum, selectBg, t.fg
	case cursor:
		numberBg, lineBg = cursorBg, cursorBg
	}
	if cursor {
		numberColor = t.strong
	}
	reverse := selected && t.plain
	on := func(fg, bg color.Color) lipgloss.Style { return t.paint(fg, bg).Reverse(reverse) }
	l.add(s.bar.text, t.paint(s.bar.color, numberBg).Bold(s.bar.bold).Reverse(cursor && t.plain))
	l.add(s.numbers, t.paint(numberColor, numberBg).Bold(cursor).Reverse(cursor && t.plain || reverse))
	l.add(s.rail.text, on(s.rail.color, lineBg))
	l.add(string(s.change)+" ", on(signColor, lineBg))
	code := make([]seg, len(s.pieces))
	for i, p := range s.pieces {
		bg := lineBg
		if p.changed {
			bg = wordBg
		}
		code[i] = seg{text: p.text, st: on(t.code(p.kind), bg)}
	}
	for _, c := range m.markSegs(code, s.marks, s.current) {
		l.add(c.text, c.st)
	}
	if s.labeled {
		m.addLabel(l, s.label, func(fg color.Color) lipgloss.Style { return on(fg, lineBg) })
	}
	return on(nil, lineBg)
}

// addLabel writes on l, labelGap cells after what it holds, the label of the origin of a
// line e wrote or deleted, nil for none: its dot and tag in its color, then what it asked
// for in a line, cut with … to fit. The tag shows alone when the diff is narrower than
// minSplitWidth, where the split view gives way to the unified one, or when less than
// minSummary cells are left for what it asked for, and the label not at all when less than
// minLabel cells, or less than the dot and the tag, are left for it before the margin at the
// right edge. on gives the style of each color.
func (m *model) addLabel(l *line, e *agent.Edit, on func(color.Color) lipgloss.Style) {
	room := l.free - labelGap - 1
	dot, tag, about, c := m.originOf(e)
	head := dot + " " + tag
	if room < max(minLabel, ansi.StringWidth(head)) {
		return
	}
	l.add(strings.Repeat(" ", labelGap), on(nil))
	l.add(head, on(c))
	_, diff := m.layout()
	if rest := room - ansi.StringWidth(head) - 2; diff >= minSplitWidth && rest >= minSummary && about != "" {
		l.add("  "+ansi.Truncate(about, rest, "…"), on(m.theme.subtle))
	}
}

// emptySide writes bar on l for a side of the split view without a line, and returns the
// style that fills it: a quiet band, as GitHub shades a side without a line, blue when its
// row is selected.
func (m *model) emptySide(l *line, bar span, cursor, selected bool) lipgloss.Style {
	t := &m.theme
	bg := t.panel
	switch {
	case selected:
		bg = t.selbg
	case cursor:
		bg = t.cursorline
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
// deleted lines and for neither. The size of the change sets how many are colored,
// ceil(log2(changed+1)) up to five, each side takes the floor of its share of them, and a
// side with changes at least one: +27 -11 is 3, 1 and 1.
func diffstat(added, deleted int) (a, d, rest int) {
	total := added + deleted
	if total == 0 {
		return 0, 0, 5
	}
	n := min(5, bits.Len(uint(total))) // ceil(log2(total+1))
	a, d = n*added/total, n*deleted/total
	if added > 0 && a == 0 {
		a = 1
	}
	if deleted > 0 && d == 0 {
		d = 1
	}
	return a, d, 5 - a - d
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

// seg is a piece of a row of the screen: text in a style, which row may cut to fit when cut
// is set.
type seg struct {
	text string
	st   lipgloss.Style
	cut  bool
}

// segsWidth returns how many cells segs take.
func segsWidth(segs []seg) int {
	n := 0
	for _, s := range segs {
		n += ansi.StringWidth(s.text)
	}
	return n
}

// group is a group of segs at the right of a row. Groups of rank 0 always show; the others
// take the room that is left, lower ranks first, each leaving keep cells of the row to its
// left part.
type group struct {
	segs       []seg
	rank, keep int
}

// fitGroups returns the segs of the groups that fit in w cells, with gap between them and
// in the order of groups. A group that does not fit is left out whole.
func fitGroups(groups []group, w int, gap seg) []seg {
	order := make([]int, len(groups))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return groups[a].rank - groups[b].rank })
	shown := make([]bool, len(groups))
	gapWidth := ansi.StringWidth(gap.text)
	used := -gapWidth // the first group has no gap before it
	for _, i := range order {
		g := groups[i]
		if g.segs == nil {
			continue
		}
		if width := used + gapWidth + segsWidth(g.segs); g.rank == 0 || width <= w-g.keep {
			shown[i], used = true, width
		}
	}
	var out []seg
	for i, g := range groups {
		if !shown[i] {
			continue
		}
		if out != nil {
			out = append(out, gap)
		}
		out = append(out, g.segs...)
	}
	return out
}

// spread draws a line w cells wide on fill, with left at its left edge and right at its
// right edge, at least two cells apart. When they do not fit, the segs of left that may be
// cut are cut, the last first, with … for what is cut; what still does not fit is cut at the
// end of left.
func spread(left, right []seg, w int, fill lipgloss.Style) string {
	rightWidth := min(segsWidth(right), w)
	room := w - rightWidth
	if right != nil {
		room = max(room-2, 0)
	}
	left = slices.Clone(left)
	over := segsWidth(left) - room
	for i := len(left) - 1; i >= 0 && over > 0; i-- {
		if !left[i].cut {
			continue
		}
		n := ansi.StringWidth(left[i].text)
		left[i].text = ansi.Truncate(left[i].text, max(n-over, 0), "…")
		over -= n - ansi.StringWidth(left[i].text)
	}
	l := newLine(room)
	for _, s := range left {
		l.add(s.text, s.st)
	}
	r := newLine(rightWidth)
	for _, s := range right {
		r.add(s.text, s.st)
	}
	return l.fill(fill) + fill.Render(strings.Repeat(" ", w-rightWidth-room)) + r.fill(fill)
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
