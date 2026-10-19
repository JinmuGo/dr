package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Margins of the peek view, in cells: it leaves peekMarginX cells of the screen on each
// side and peekMarginY lines above and below, when the screen has room for them.
const (
	peekMarginX = 2
	peekMarginY = 1
)

// peekView is the peek view space opens: the diff of the cursor's file alone, in a box
// that floats over the screen. The cursor moves in it and stays where it was when the box
// closes.
type peekView struct {
	open bool
	top  int // first row in the box
}

// openPeek opens the peek view on the cursor's file, the cursor off its header, which
// the box's title shows.
func (m *model) openPeek() {
	m.peek.open = true
	m.enterFile()
}

// enterFile moves the cursor from the header of its file to the file's first row under it,
// when it has one, and scrolls the peek view to the top of the file.
func (m *model) enterFile() {
	first, _ := m.peekRows()
	m.cursor = max(m.cursor, first)
	m.peek.top = first
}

// peekRows returns the first and the last row the peek view shows: the rows of the
// cursor's file under its header, or the header alone when the file folds to it.
func (m *model) peekRows() (first, last int) {
	head, last := m.fileSpan()
	if last > head {
		return head + 1, last
	}
	return head, head
}

// fileSpan returns the row of the header of the cursor's file and its last row.
func (m *model) fileSpan() (head, last int) {
	f := m.rows[m.cursor].file
	head = m.headRow(f)
	last = head
	for last+1 < len(m.rows) && m.rows[last+1].file == f {
		last++
	}
	return head, last
}

// peekKey handles a key while the peek view is open: the keys of the diff that move,
// inside the file, s for the split view, h and l for the previous and the next file, v to
// mark the file viewed or not, ? for the help, and space, Esc and q to close it. Esc first
// clears a count or a g waiting for its key. n and N do nothing, since the search runs
// through the whole diff. It returns the command that saves the viewed files after v.
func (m *model) peekKey(k string) tea.Cmd {
	waiting := m.count > 0 || m.pending != ""
	k = m.pending + k
	count := m.count
	m.count, m.pending = 0, ""
	switch k {
	case keyEsc:
		m.peek.open = waiting // Esc only clears what waits
	case keyPeek, keyQuit:
		m.peek.open = false
	case keySplit:
		m.splitOn = !m.splitOn // Update shows the view that fits the box
	case keyNextFile, keyRight:
		m.peekFile(max(count, 1))
	case keyPrevFile, keyLeft:
		m.peekFile(-max(count, 1))
	case keyViewed:
		cmd := m.toggleViewed(m.rows[m.cursor].file)
		m.enterFile()
		return cmd
	case keyHelp:
		m.help = true
	case keyNextMatch, keyPrevMatch:
	default:
		m.moveKey(k, count)
	}
	return nil
}

// peekFile shows the file n files after the cursor's, before it when n is negative, or the
// last or the first file when there are fewer.
func (m *model) peekFile(n int) {
	m.otherFile(n)
	m.enterFile()
}

// peekBox returns where the peek view floats over the diff, h lines high: its left
// column, its top line, its width and its height, borders included.
func (m *model) peekBox(h int) (x, y, w, bh int) {
	if m.width >= 2*peekMarginX+minDiffWidth {
		x = peekMarginX
	}
	if h >= 2*peekMarginY+8 {
		y = peekMarginY
	}
	return x, y, m.width - 2*x, h - 2*y
}

// peekWidth returns how wide the rows in the peek view are.
func (m *model) peekWidth() int {
	_, _, w, _ := m.peekBox(m.diffHeight())
	return max(w-2, 0)
}

// peekScroll scrolls the peek view as little as it takes to show the cursor row, with no
// blank lines at its bottom while the file has rows above to show.
func (m *model) peekScroll() {
	_, _, _, h := m.peekBox(m.diffHeight())
	first, last := m.peekRows()
	body := max(h-2, 1)
	m.peek.top = max(min(m.peek.top, m.cursor, last-body+1), first, m.cursor-body+1)
}

// drawPeek draws the peek view over lines, the h lines of the diff, and dims what is
// behind it, as the key help does. Its top border names the file and where it is among the
// files, and inside come the file's rows as the diff draws them, without the boxes under
// them. A file folded to its header says how to unfold it. A screen too small for the box
// shows the diff as it is.
func (m *model) drawPeek(lines []string) {
	t := &m.theme
	x, y, w, h := m.peekBox(len(lines))
	if w < 4 || h < 3 {
		return
	}
	for i, l := range lines {
		lines[i] = t.paint(t.faint, nil).Render(ansi.Strip(l))
	}
	file := m.rows[m.cursor].file
	title := seg{text: " " + m.files[file].path + " ", st: t.paint(t.strong, t.surface).Bold(true)}
	where := seg{text: " " + strconv.Itoa(file+1) + "/" + strconv.Itoa(len(m.files)) + " ", st: t.paint(t.subtle, t.surface)}
	box := []string{t.floatTop(w, title, where)}
	first, last := m.peekRows()
	folded := m.rows[first].kind == fileRow
	for k := range h - 2 {
		i := m.peek.top + k
		if !folded && i <= last {
			box = append(box, t.floatSide(m.drawRow(i, w-2)))
			continue
		}
		l := newLine(w - 2)
		if folded && k == 0 {
			l.add(" Viewed, so it folds. v unfolds it.", t.paint(t.muted, nil))
		}
		box = append(box, t.floatSide(l.fill(t.paint(nil, nil))))
	}
	box = append(box, t.floatBottom(w))
	overlayBox(lines, box, x, y, w, m.width)
}
