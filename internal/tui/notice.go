package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// notice is news that floats at the top right of the diff until the next key: what :submit,
// y, x or :undo did, or an answer or fixes that came while their card was closed.
type notice struct {
	tone     messageTone
	text     []chunk   // the first line, after the icon of its tone
	warnings []string  // what went wrong besides, each after a !
	hint     []chunk   // what to do next, under them
	question *question // Enter opens the origin card on its answer
	fixes    *comment  // Enter goes to the card of its fixes
}

// warn adds warning to the warnings of n, which then reads as a warning; "" adds nothing.
func (n *notice) warn(warning string) {
	if warning != "" {
		n.tone, n.warnings = toneWarn, append(n.warnings, warning)
	}
}

// maxNoticeWidth is how wide a notice is at most, in cells.
const maxNoticeWidth = 60

// readHint returns the hint of a notice that Enter reads: Enter to read it, Esc to dismiss.
func (m *model) readHint() []chunk {
	return m.keys([2]string{"⏎", "to read"}, [2]string{"esc", "dismiss"})
}

// quiet returns text as a chunk in the muted color, the hint of a notice that says what
// follows from it.
func (m *model) quiet(text string) []chunk {
	return []chunk{{text: text, color: m.theme.muted}}
}

// drawNotice draws the notice over lines, the lines of the screen above the status line,
// at the top right of the diff, which is w cells wide and ends at the right edge: two lines
// under the bands, or higher when the screen is too short for it there.
func (m *model) drawNotice(lines []string, w int) {
	if m.notice == nil {
		return
	}
	width := min(maxNoticeWidth, w-4)
	x := m.width - width - 2 // two cells from the right edge
	if width < 12 {          // no room for its margins: as wide as the diff
		width = min(w, m.width)
		x = m.width - width
	}
	box := m.noticeBox(m.notice, width)
	box = box[:min(len(box), len(lines))]
	y := max(min(m.bandLines()+2, len(lines)-len(box)), 0)
	for k, l := range box {
		lines[y+k] = overlay(lines[y+k], l, x, width, m.width)
	}
}

// noticeBox draws n, w cells wide, in a rounded border in the color of floating boxes on the
// fill of boxes: its first line after the icon of its tone, each warning after a ! in the
// warning color, then its hint, each wrapped to the box.
func (m *model) noticeBox(n *notice, w int) []string {
	t := &m.theme
	border := t.paint(t.line2, t.surface)
	base := t.paint(nil, t.surface)
	inner := max(w-4, 1) // inside the border and a cell of margin on each side
	var rows [][]chunk
	indented := func(chunks []chunk, first chunk) {
		for i, l := range wrapChunks(chunks, inner-ansi.StringWidth(first.text)) {
			mark := chunk{text: strings.Repeat(" ", ansi.StringWidth(first.text))}
			if i == 0 {
				mark = first
			}
			rows = append(rows, append([]chunk{mark}, l...))
		}
	}
	icon, c := t.icon(n.tone)
	indented(n.text, chunk{text: icon + " ", color: c, bold: true})
	for _, warning := range n.warnings {
		indented([]chunk{{text: warning, color: t.pending}}, chunk{text: "  ! ", color: t.pending, bold: true})
	}
	if n.hint != nil {
		indented(n.hint, chunk{text: "  "})
	}
	lines := []string{border.Render("╭" + strings.Repeat("─", max(w-2, 0)) + "╮")}
	for _, r := range rows {
		l := newLine(inner)
		for _, s := range m.segs(r, t.surface, false) {
			l.add(s.text, s.st)
		}
		lines = append(lines, border.Render("│")+base.Render(" ")+l.fill(base)+base.Render(" ")+border.Render("│"))
	}
	return append(lines, border.Render("╰"+strings.Repeat("─", max(w-2, 0))+"╯"))
}

// band is a line across the top of the diff that tells why the screen cannot be trusted
// whole, for as long as that holds: an icon and text in the band's tone, and a quiet note at
// the right.
type band struct {
	icon, text, right string
	tone              color.Color
}

// bands returns the bands of the screen: one when the session logs of some agents could not
// be read, and one when this dr does not hold the lock on the worktree.
func (m *model) bands() []band {
	var bands []band
	if len(m.unreadable) > 0 {
		bands = append(bands, band{
			icon: "!", tone: m.theme.pending, right: "dr blame shows which",
			text: "Some " + strings.Join(m.unreadable, ", ") + " records could not be read, so their lines show as unknown.",
		})
	}
	if m.lockErr != nil {
		bands = append(bands, band{icon: "⊘", tone: m.theme.muted, text: lockBand(m.lockErr)})
	}
	return bands
}

// bandRows draws the bands that bandLines leaves room for, each w cells wide: its icon in its
// tone, bold, and its text, cut to fit, on its tone mixed into the fill of boxes, and its
// note at the right when the text fits beside it. Without colors a band shows in reverse
// video.
func (m *model) bandRows(w int) []string {
	t := &m.theme
	bands := m.bands()[:m.bandLines()]
	rows := make([]string, len(bands))
	for i, b := range bands {
		on := func(fg color.Color) lipgloss.Style { return t.chip(fg, b.tone) }
		left := []seg{{text: " " + b.icon + " ", st: on(b.tone).Bold(true)}, {text: b.text, st: on(t.fg), cut: true}}
		var right []seg
		if b.right != "" && segsWidth(left)+2+ansi.StringWidth(b.right)+1 <= w {
			right = []seg{{text: b.right, st: on(t.subtle)}, {text: " ", st: on(nil)}}
		}
		rows[i] = spread(left, right, w, on(nil))
	}
	return rows
}
