package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// card is a block that hangs under a row of the diff, from the rail column to the right edge:
// the origin card and the comment cards. Every row starts with a band in the card's color,
// which goes on from the rail of the row above. The header comes first, on a lighter fill,
// then the body, and an empty row of the body ends the card.
type card struct {
	band color.Color
	head cardRow
	rows []cardRow
}

// cardRow is a row of a card: chunks at the left, and chunks at the right edge that show only
// when they fit two cells after the left ones. A rule row is a dashed line across the card,
// and a row with a sign is a line of a small diff: its sign and its code on the background of
// its kind of line, or, for the rest of a line too long for one row, its code alone.
type cardRow struct {
	left, right []chunk
	rule        bool
	sign        byte    // '+' or '-' on a line of a small diff, 0 on other rows
	code        []piece // the code of a line of a small diff
	more        bool    // the code goes on from the row above, without the sign
}

// chunk is a piece of a card row: text in a color, bold or italic, or a chip, text on its
// color mixed into the fill. A chunk that may be cut is cut with … when the row is too
// narrow for it.
type chunk struct {
	text               string // made visible
	color              color.Color
	bold, italic, chip bool
	cut                bool
}

// Parts of a card row and the glyphs of a card.
const (
	cardBand  = "▎" // the band at the left edge of every row
	cardRule  = "╌" // the rule between a thread and the rest of an origin card
	cardThumb = "┃" // the thumb of a card's scroll bar
	cardTrack = "│" // the track of a card's scroll bar
	keyGap    = "   "

	diffSignWidth = 3 // a cell of space, the sign and a space before the code of a small diff
)

// chunksWidth returns how many cells chunks take on the screen, a chip with a space on each
// side.
func chunksWidth(chunks []chunk) int {
	n := 0
	for _, c := range chunks {
		n += ansi.StringWidth(c.text)
		if c.chip {
			n += 2
		}
	}
	return n
}

// cardLeft returns the cell of a diff w cells wide where a card under a row of file f
// starts: the rail column, before the sign and its space, but no further right than a third
// of the diff.
func (m *model) cardLeft(f file, w int) int {
	return max(min(m.codeColumn(f)-3, w/3), 0)
}

// textWidth returns how wide a card w cells wide wraps its text: inside the band and the
// scroll bar, each with a cell of space, up to maxReadWidth.
func textWidth(w int) int {
	return max(min(w-4, maxReadWidth), 1)
}

// drawCard draws c, hung from cell x of a diff w cells wide: its header, at most n rows of
// its body from row top, and an empty row. When the body has more rows than show, a scroll
// bar in the last column tells which part shows.
func (m *model) drawCard(c card, x, w, top, n int) []string {
	width := max(w-x, 0)
	return m.frame(c, x, width, m.cardBody(c, width), top, n)
}

// blockLine is a line drawn in a card's body, one cell short of the card's width: the last
// cell is for the scroll bar, on bg.
type blockLine struct {
	text string
	bg   color.Color
}

// frame draws c's header, hung from cell x and width cells wide, at most n lines of body from
// line top with the scroll bar in the last column when body has more lines than show, and
// c's last row.
func (m *model) frame(c card, x, width int, body []blockLine, top, n int) []string {
	t := &m.theme
	pad := strings.Repeat(" ", x)
	total := len(body)
	shown := max(min(total, n), 0)
	top = max(min(top, total-shown), 0)
	lines := make([]string, 0, shown+2)
	lines = append(lines, pad+m.cardHead(c, width))
	thumb, size := scrollThumb(top, shown, total)
	for i, b := range body[top : top+shown] {
		bar := t.paint(nil, b.bg).Render(" ")
		switch {
		case total <= shown:
		case i >= thumb && i < thumb+size:
			bar = t.paint(t.muted, b.bg).Render(cardThumb)
		default:
			bar = t.paint(t.faint, b.bg).Render(cardTrack)
		}
		lines = append(lines, pad+b.text+bar)
	}
	return append(lines, pad+m.cardEnd(c, width))
}

// fullCard returns every line of c, width cells wide: its header, its body and its last row.
// The lines of the header are on the header's fill, the others on the body's.
func (m *model) fullCard(c card, width int) []blockLine {
	t := &m.theme
	lines := []blockLine{{text: m.cardHead(c, width), bg: t.surface2}}
	for _, b := range m.cardBody(c, width) {
		lines = append(lines, blockLine{text: b.text + t.paint(nil, b.bg).Render(" "), bg: b.bg})
	}
	return append(lines, blockLine{text: m.cardEnd(c, width), bg: t.surface})
}

// cardHead draws the header of c, width cells wide: the band and the header row.
func (m *model) cardHead(c card, width int) string {
	t := &m.theme
	head := t.paint(nil, t.surface2)
	l := newLine(width)
	l.add(cardBand, t.paint(c.band, t.surface2))
	l.add(" ", head)
	l.add(m.drawCardRow(c.head, max(width-3, 0), t.surface2, true), lipgloss.NewStyle())
	return l.fill(head)
}

// cardBody draws the rows of c's body for a card width cells wide, each a line one cell
// short of it: the band, the row, and a cell of space.
func (m *model) cardBody(c card, width int) []blockLine {
	t := &m.theme
	body := t.paint(nil, t.surface)
	lines := make([]blockLine, len(c.rows))
	for i, r := range c.rows {
		l := newLine(max(width-1, 0))
		l.add(cardBand, t.paint(c.band, t.surface))
		l.add(" ", body)
		l.add(m.drawCardRow(r, max(width-4, 0), t.surface, false), lipgloss.NewStyle())
		lines[i] = blockLine{text: l.fill(body), bg: t.surface}
	}
	return lines
}

// cardEnd draws the last row of c, width cells wide: the band on an empty row.
func (m *model) cardEnd(c card, width int) string {
	t := &m.theme
	l := newLine(width)
	l.add(cardBand, t.paint(c.band, t.surface))
	return l.fill(t.paint(nil, t.surface))
}

// scrollThumb returns where the thumb of a scroll bar starts on its track of shown rows, and
// how long it is, when the rows from top show of total.
func scrollThumb(top, shown, total int) (start, size int) {
	if total <= shown || shown == 0 {
		return 0, 0
	}
	size = max(1, (shown*shown+total/2)/total)
	return (top*(shown-size) + (total-shown)/2) / (total - shown), size
}

// drawCardRow draws r w cells wide on bg, the fill of its card's header when head is set or
// else of its body. Without colors the header is bold.
func (m *model) drawCardRow(r cardRow, w int, bg color.Color, head bool) string {
	t := &m.theme
	fill := t.paint(nil, bg)
	switch {
	case r.rule:
		return t.paint(t.line, bg).Render(strings.Repeat(cardRule, w))
	case r.sign != 0:
		return m.drawDiffRow(r, w)
	}
	right := r.right
	if chunksWidth(r.left)+2+chunksWidth(right) > w {
		right = nil
	}
	return spread(m.segs(r.left, bg, head), m.segs(right, bg, head), w, fill)
}

// segs returns chunks as segs of a row on bg, bold without colors on a header, and nil for
// no chunks, so that spread keeps no room for them. Line breaks and tabs draw as spaces, so
// the row stays one row.
func (m *model) segs(chunks []chunk, bg color.Color, head bool) []seg {
	if len(chunks) == 0 {
		return nil
	}
	t := &m.theme
	out := make([]seg, len(chunks))
	for i, c := range chunks {
		text, bold := oneLine.Replace(c.text), c.bold || head && t.plain
		if c.chip {
			out[i] = seg{text: " " + text + " ", st: t.chip(c.color, c.color).Bold(bold), cut: c.cut}
			continue
		}
		out[i] = seg{text: text, st: t.paint(c.color, bg).Bold(bold).Italic(c.italic), cut: c.cut}
	}
	return out
}

// oneLine turns line breaks and tabs into spaces for text drawn on a single row.
var oneLine = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// drawDiffRow draws r, a line of a small diff, w cells wide: a cell of space, its sign, blank
// on a row that goes on from the one above, and its code syntax colored on the background of
// its kind of line, its changed words on the stronger one.
func (m *model) drawDiffRow(r cardRow, w int) string {
	t := &m.theme
	bg, word, sign := t.addbg, t.addword, t.add
	if r.sign == '-' {
		bg, word, sign = t.delbg, t.delword, t.del
	}
	mark := string(r.sign)
	if r.more {
		mark = " "
	}
	l := newLine(w)
	l.add(" ", t.paint(nil, bg))
	l.add(mark+" ", t.paint(sign, bg))
	for _, p := range r.code {
		on := bg
		if p.changed {
			on = word
		}
		l.add(p.text, t.paint(t.code(p.kind), on))
	}
	return l.fill(t.paint(nil, bg))
}

// label returns the row of a section label of a card: its title in small capitals, quiet and
// bold, and a count or a number after it when extra is not "".
func (m *model) label(title, extra string) cardRow {
	t := &m.theme
	r := cardRow{left: []chunk{{text: title, color: t.subtle, bold: true}}}
	if extra != "" {
		r.left = append(r.left, chunk{text: " · " + extra, color: t.subtle})
	}
	return r
}

// wrapped returns the rows of text wrapped to width, each line of the text on its own, in
// the color and the style of c.
func wrapped(text string, c chunk, width int) []cardRow {
	lines := strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
	rows := make([]cardRow, len(lines))
	for i, s := range lines {
		c.text = strings.TrimRight(s, " ")
		rows[i] = cardRow{left: []chunk{c}}
	}
	return rows
}

// noted returns the rows of a note, text after ↳ wrapped to width in the color and the
// style of c, its lines after the first under the text.
func (m *model) noted(text string, c chunk, width int) []cardRow {
	rows := wrapped(text, c, max(width-2, 1))
	for i := range rows {
		mark := chunk{text: "  "}
		if i == 0 {
			mark = chunk{text: "↳ ", color: m.theme.subtle}
		}
		rows[i].left = append([]chunk{mark}, rows[i].left...)
	}
	return rows
}

// keys returns the keys of a card's header, each key bold and what it does quiet after it.
func (m *model) keys(pairs ...[2]string) []chunk {
	t := &m.theme
	var out []chunk
	for i, p := range pairs {
		if i > 0 {
			out = append(out, chunk{text: keyGap})
		}
		out = append(out, chunk{text: p[0], color: t.strong, bold: true}, chunk{text: " " + p[1], color: t.muted})
	}
	return out
}

// fitKeys returns the first of options, the keys of a header from the most to the fewest,
// that fits two cells after left in a header w cells wide, and nil when none does.
func fitKeys(left []chunk, w int, options ...[]chunk) []chunk {
	for _, o := range options {
		if chunksWidth(left)+2+chunksWidth(o) <= w {
			return o
		}
	}
	return nil
}

// joinGroups returns the rows of groups with an empty row between two groups, leaving out
// the empty ones.
func joinGroups(groups ...[]cardRow) []cardRow {
	var rows []cardRow
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if rows != nil {
			rows = append(rows, cardRow{})
		}
		rows = append(rows, g...)
	}
	return rows
}

// dim returns rows with every chunk quiet and not bold, for what has no place in the diff now.
func (m *model) dim(rows []cardRow) []cardRow {
	for i := range rows {
		for _, side := range [][]chunk{rows[i].left, rows[i].right} {
			for k := range side {
				side[k].color, side[k].bold = m.theme.muted, false
			}
		}
	}
	return rows
}
