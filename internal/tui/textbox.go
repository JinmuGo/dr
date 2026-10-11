package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// textBox is the text typed in the input box of a and c, and the cursor in it. The cursor is
// a byte offset of the text at the start of a grapheme cluster or at the end, so a wide
// character, or one an input method composed, is never split. The text's lines are parted by
// "\n", and the box shows them wrapped at its width, in rows that Up and Down move between,
// as tuicr's comment input does. With the text goes the model picked for it with ^, as OMP
// tags a message with the model it picks.
type textBox struct {
	text   string
	cursor int
	column int     // the cell Up and Down keep the cursor at
	kept   bool    // the last key was Up or Down, which keep column
	top    int     // the first row the box shows when the text has more rows than fit
	pick   string  // the model picked with ^, "" for the session's own
	picker *picker // the model list open under the box while ^ picks a model, nil when closed
}

// textRow is a row the box shows: the bytes of the text from start to end, without the line
// break that ends a line.
type textRow struct{ start, end int }

// newTextBox returns a box that holds text, with the cursor at its end.
func newTextBox(text string) textBox {
	return textBox{text: text, cursor: len(text)}
}

// edit handles key k, a key's name as tea.Key.String gives it, with text, the text the key
// types. width is how wide the box wraps the text, the rows Up and Down move by.
//
// Shift+Enter, Alt+Enter and Ctrl+J start a new line. Backspace and Delete delete the
// character before and after the cursor, a line break too. Left and Right move a character,
// Up and Down a row, keeping the cell they started at across rows that are shorter. Home,
// End, Ctrl+A and Ctrl+E go to the start and the end of the line, Alt or Ctrl with Left or
// Right, and Alt+B and Alt+F, a word. Ctrl+U deletes to the start of the line, and Ctrl+W
// the word before the cursor. Other keys type their text, made clean as a paste is.
func (b *textBox) edit(k, text string, width int) {
	if k != "up" && k != "down" {
		b.kept = false
	}
	switch k {
	case "shift+enter", "alt+enter", "ctrl+j":
		b.insert("\n")
	case "backspace":
		b.remove(b.before(b.cursor), b.cursor)
	case "delete":
		b.remove(b.cursor, b.after(b.cursor))
	case "left":
		b.cursor = b.before(b.cursor)
	case "right":
		b.cursor = b.after(b.cursor)
	case "up":
		b.vertical(-1, width)
	case "down":
		b.vertical(1, width)
	case "home", "ctrl+a":
		b.cursor = b.lineStart()
	case "end", "ctrl+e":
		b.cursor = b.lineEnd()
	case "alt+left", "ctrl+left", "alt+b":
		b.cursor = b.wordLeft()
	case "alt+right", "ctrl+right", "alt+f":
		b.cursor = b.wordRight()
	case "ctrl+u":
		b.remove(b.lineStart(), b.cursor)
	case "ctrl+w":
		b.remove(b.wordLeft(), b.cursor)
	default:
		b.insert(clean(text))
	}
}

// paste inserts s, the text of a paste, at the cursor, made clean.
func (b *textBox) paste(s string) {
	b.kept = false
	b.insert(clean(s))
}

// clean returns s as the box takes it: each line break, "\r\n" and "\r" too, as "\n", a tab
// as four spaces, and no other control characters.
func clean(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\r' || r == '\n':
			b.WriteByte('\n')
		case r == '\t':
			b.WriteString("    ")
		case !unicode.IsControl(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// insert writes s at the cursor and moves the cursor after it.
func (b *textBox) insert(s string) {
	b.text = b.text[:b.cursor] + s + b.text[b.cursor:]
	b.cursor += len(s)
	b.snap()
}

// remove deletes the text from byte from to byte to and puts the cursor there.
func (b *textBox) remove(from, to int) {
	b.text = b.text[:from] + b.text[to:]
	b.cursor = from
	b.snap()
}

// snap moves the cursor to the end of the cluster it is in, when an edit joined the
// characters around it into one, such as a letter typed before a combining mark.
func (b *textBox) snap() {
	i := b.lineStart()
	for i < b.cursor {
		i = b.next(i)
	}
	b.cursor = i
}

// next returns the end of the cluster that starts at byte i, before the end of the text: a
// line break alone, or a grapheme cluster of the line.
func (b *textBox) next(i int) int {
	if b.text[i] == '\n' {
		return i + 1
	}
	end := len(b.text)
	if n := strings.IndexByte(b.text[i:], '\n'); n >= 0 {
		end = i + n
	}
	cluster, _ := ansi.FirstGraphemeCluster(b.text[i:end], ansi.GraphemeWidth)
	return i + max(len(cluster), 1)
}

// after returns the end of the cluster at byte i, or the end of the text at its end.
func (b *textBox) after(i int) int {
	if i >= len(b.text) {
		return len(b.text)
	}
	return b.next(i)
}

// before returns the start of the cluster before byte i, or 0 at the start of the text.
func (b *textBox) before(i int) int {
	if i == 0 {
		return 0
	}
	start := strings.LastIndexByte(b.text[:i], '\n') + 1
	if start == i {
		return i - 1 // the line break that ends the line before
	}
	prev := start
	for j := start; j < i; j = b.next(j) {
		prev = j
	}
	return prev
}

// lineStart returns the start of the line the cursor is on.
func (b *textBox) lineStart() int {
	return strings.LastIndexByte(b.text[:b.cursor], '\n') + 1
}

// lineEnd returns the end of the line the cursor is on, before its line break.
func (b *textBox) lineEnd() int {
	if n := strings.IndexByte(b.text[b.cursor:], '\n'); n >= 0 {
		return b.cursor + n
	}
	return len(b.text)
}

// starts returns the start of each cluster of the text, then its end, and which of them the
// cursor is at.
func (b *textBox) starts() ([]int, int) {
	var starts []int
	at := 0
	for i := 0; i < len(b.text); i = b.next(i) {
		if i <= b.cursor {
			at = len(starts)
		}
		starts = append(starts, i)
	}
	if b.cursor == len(b.text) {
		at = len(starts)
	}
	return append(starts, len(b.text)), at
}

// space reports whether the cluster at byte i is white space, a line break too.
func (b *textBox) space(i int) bool {
	r, _ := utf8.DecodeRuneInString(b.text[i:])
	return unicode.IsSpace(r)
}

// wordLeft returns the start of the word before the cursor, past the white space before it,
// or 0 when there is none, as tuicr moves a word left.
func (b *textBox) wordLeft() int {
	starts, k := b.starts()
	for k > 0 && b.space(starts[k-1]) {
		k--
	}
	for k > 0 && !b.space(starts[k-1]) {
		k--
	}
	return starts[k]
}

// wordRight returns the start of the next word after the cursor, or the end of the text when
// there is none, as tuicr moves a word right.
func (b *textBox) wordRight() int {
	starts, k := b.starts()
	end := len(starts) - 1
	for k < end && !b.space(starts[k]) {
		k++
	}
	for k < end && b.space(starts[k]) {
		k++
	}
	return starts[k]
}

// rows returns the rows the text shows in at width cells: each line, wrapped before the
// cluster that would go past width. A cluster wider than width has a row of its own, and an
// empty line is an empty row.
func (b *textBox) rows(width int) []textRow {
	var rows []textRow
	start, used := 0, 0
	for i := 0; ; {
		if i == len(b.text) {
			return append(rows, textRow{start, i})
		}
		if b.text[i] == '\n' {
			rows = append(rows, textRow{start, i})
			i++
			start, used = i, 0
			continue
		}
		j := b.next(i)
		w := cells(b.text[i:j])
		if used > 0 && used+w > width {
			rows = append(rows, textRow{start, i})
			start, used = i, 0
		}
		used += w
		i = j
	}
}

// at returns which of rows the cursor is on and its cell in that row. Where a row wraps,
// the cursor at its end is at the start of the next row, as in tuicr.
func (b *textBox) at(rows []textRow) (row, column int) {
	for k, r := range rows {
		if r.start > b.cursor {
			break
		}
		row = k
	}
	return row, cells(b.text[rows[row].start:b.cursor])
}

// vertical moves the cursor a row up (dir -1) or down (dir 1) in the rows of the text at
// width cells, to the cell it started at in the first of a run of Up and Down keys, or the
// nearest before it. The cursor stays on the first and the last row.
func (b *textBox) vertical(dir, width int) {
	rows := b.rows(width)
	from, column := b.at(rows)
	k := from + dir
	if k < 0 || k >= len(rows) {
		return
	}
	if !b.kept {
		b.column, b.kept = column, true
	}
	r := rows[k]
	to, prev, used := r.start, r.start, 0
	for to < r.end {
		next := b.next(to)
		w := cells(b.text[to:next])
		if used+w > b.column {
			break
		}
		prev, to, used = to, next, used+w
	}
	if to == r.end && k+1 < len(rows) && rows[k+1].start == r.end {
		to = prev // the end of a row that wraps is the start of the next
	}
	b.cursor = to
}

// follow scrolls the rows the box shows, at most height of them at width cells, as little
// as it takes to show the cursor's row, and leaves no empty rows at the bottom while there
// are rows above to show.
func (b *textBox) follow(width, height int) {
	rows := b.rows(width)
	row, _ := b.at(rows)
	b.top = max(min(b.top, row), row-height+1)
	b.top = max(min(b.top, len(rows)-height), 0)
}

// layout returns the rows the box shows at width cells, at most height of them from its
// first, which of those the cursor is on, -1 when none is, and its cell in that row.
func (b *textBox) layout(width, height int) (shown []textRow, row, column int) {
	rows := b.rows(width)
	row, column = b.at(rows)
	top := max(min(b.top, len(rows)-height), 0)
	shown = rows[top:min(top+height, len(rows))]
	row -= top
	if row < 0 || row >= len(shown) {
		row = -1
	}
	return shown, row, column
}

// cells returns how many cells s takes as the box shows it, as display writes it.
func cells(s string) int {
	return ansi.StringWidth(display(s))
}
