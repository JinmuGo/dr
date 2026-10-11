package tui

import (
	"image/color"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

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

// spansWidth returns the width of spans in cells.
func spansWidth(spans []span) int {
	w := 0
	for _, s := range spans {
		w += ansi.StringWidth(s.text)
	}
	return w
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

// wrapChunks returns chunks broken into lines of at most w cells, after the spaces where it
// can, each piece keeping the style of its chunk. A word longer than a line is cut where the
// line ends, without splitting a wide character.
func wrapChunks(chunks []chunk, w int) [][]chunk {
	w = max(w, 1)
	var lines [][]chunk
	var line []chunk
	used := 0
	put := func(c chunk, text string) {
		c.text = text
		line = append(line, c)
		used += ansi.StringWidth(text)
	}
	next := func() {
		lines = append(lines, line)
		line, used = nil, 0
	}
	for _, c := range chunks {
		for _, word := range strings.SplitAfter(c.text, " ") {
			if word == "" {
				continue
			}
			if used > 0 && used+ansi.StringWidth(strings.TrimRight(word, " ")) > w {
				next()
			}
			for ansi.StringWidth(word) > w-used && ansi.StringWidth(strings.TrimRight(word, " ")) > w-used {
				part := ansi.Truncate(word, w-used, "")
				if part == "" && used == 0 { // a character wider than the line
					_, size := utf8.DecodeRuneInString(word)
					part = word[:size]
				}
				put(c, part)
				next()
				word = word[len(part):]
			}
			if word != "" {
				put(c, word)
			}
		}
	}
	if line != nil || lines == nil {
		lines = append(lines, line)
	}
	return lines
}
