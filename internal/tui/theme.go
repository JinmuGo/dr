package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// theme is the colors of the screen: GitHub's diff colors for a dark or a light terminal.
// Unchanged lines keep the terminal's own background, and the tinted backgrounds move with
// it, so they stand out from it as much as they do on GitHub. Without colors, as with
// NO_COLOR, bands and selected lines show in reverse video and quiet text faint, so the
// screen keeps its shape.
type theme struct {
	bg    color.Color // the terminal's background, nil when it did not tell
	base  color.Color // the background the tinted colors are for: bg, or GitHub's when bg is nil
	dark  bool        // the terminal has a dark background
	plain bool        // the terminal shows no colors

	text, muted, subtle, accent          color.Color
	title                                color.Color // a file name in its header, the most readable text
	added, deleted, warning              color.Color // signs, counts and notes
	border                               color.Color
	band                                 color.Color // titles and the status line
	header                               color.Color // the band of a file's header
	cursor                               color.Color // background of the cursor row of an unchanged line
	lift                                 color.Color // mixed into a background under the cursor
	addLine, addNumber, addWord          color.Color // an added line, its line numbers, its changed words
	deleteLine, deleteNumber, deleteWord color.Color
	hunkLine, hunkNumber                 color.Color
	// selectLine and selectNumber are an unchanged line of a selection and its line numbers,
	// GitHub's selected-line yellow. selected tints the other backgrounds of a selection.
	selectLine, selectNumber color.Color

	syntax *chroma.Style
}

// newTheme returns the theme for a terminal with the background bg, dark or light, nil when
// the terminal did not tell it. It shows no colors when plain is set.
func newTheme(bg color.Color, dark, plain bool) theme {
	lightDark := lipgloss.LightDark(dark)
	c := func(light, dark string) color.Color {
		return lightDark(lipgloss.Color(light), lipgloss.Color(dark))
	}
	page := c("#ffffff", "#0d1117") // the background GitHub's colors are made for
	tint := func(light, dark string) color.Color {
		if bg == nil {
			return c(light, dark)
		}
		return shift(c(light, dark), page, bg)
	}
	base := page
	if bg != nil {
		base = bg
	}
	t := theme{
		bg:    bg,
		base:  base,
		dark:  dark,
		plain: plain,

		text:    c("#1f2328", "#e6edf3"),
		muted:   c("#59636e", "#8b949e"),
		subtle:  c("#818b98", "#6e7681"),
		accent:  c("#0969da", "#58a6ff"),
		added:   c("#1a7f37", "#3fb950"),
		deleted: c("#d1242f", "#f85149"),
		warning: c("#9a6700", "#d29922"),
		lift:    c("#000000", "#ffffff"),
		title:   c("#000000", "#ffffff"),

		border:       tint("#d1d9e0", "#30363d"),
		band:         tint("#f6f8fa", "#161b22"),
		header:       tint("#e6eaef", "#262c36"),
		cursor:       tint("#eaeef2", "#1f242c"),
		addLine:      tint("#dafbe1", "#12261e"),
		addNumber:    tint("#aceebb", "#1c4428"),
		addWord:      tint("#aceebb", "#1f5a2f"),
		deleteLine:   tint("#ffebe9", "#301b1e"),
		deleteNumber: tint("#ffcecb", "#542426"),
		deleteWord:   tint("#ffc1c0", "#782d2d"),
		hunkLine:     tint("#ddf4ff", "#121d2f"),
		hunkNumber:   tint("#b6e3ff", "#172c48"),
		selectLine:   tint("#fff8c5", "#272215"), // GitHub's attention background: #fff8c5, #bb8009 at 15%
		selectNumber: tint("#eedcab", "#533d11"), // its attention border: #d4a72c and #bb8009 at 40%

		syntax: styles.Get("github"),
	}
	if dark {
		t.syntax = styles.Get("github-dark")
	}
	return t
}

// paint returns the style of text in fg on bg. A nil color is the terminal's own. Bubble
// Tea drops the colors a terminal cannot show, so without colors quiet text turns faint.
func (t *theme) paint(fg, bg color.Color) lipgloss.Style {
	s := lipgloss.NewStyle()
	if fg != nil {
		s = s.Foreground(fg)
	}
	if bg != nil {
		s = s.Background(bg)
	}
	if t.plain && (fg == t.muted || fg == t.subtle) {
		s = s.Faint(true)
	}
	return s
}

// onBand returns the style of text in fg on the band bg, lighter or darker under the cursor
// when cursor is set. Without colors the band shows in reverse video.
func (t *theme) onBand(fg, bg color.Color, cursor bool) lipgloss.Style {
	if cursor {
		bg = t.under(bg)
	}
	return t.paint(fg, bg).Reverse(t.plain)
}

// under returns bg as it shows under the cursor: a little lighter on a dark terminal and a
// little darker on a light one. Under a row without a background it is the cursor color.
func (t *theme) under(bg color.Color) color.Color {
	if bg == nil {
		return t.cursor
	}
	return mix(t.lift, bg, 0.07)
}

// selected returns bg as it shows on a selected row, nil being the terminal's own: moved as
// far as tint is from base, so a selected added or deleted line keeps its green or red under
// the yellow.
func (t *theme) selected(bg, tint color.Color) color.Color {
	if bg == nil {
		bg = t.base
	}
	return shift(bg, t.base, tint)
}

// code returns the color of code of token type k, GitHub's syntax color.
func (t *theme) code(k chroma.TokenType) color.Color {
	if e := t.syntax.Get(k); e.Colour.IsSet() {
		return color.RGBA{R: e.Colour.Red(), G: e.Colour.Green(), B: e.Colour.Blue(), A: 0xff}
	}
	return t.text
}

// mix returns the color f of the way from b to a.
func mix(a, b color.Color, f float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	channel := func(x, y uint32) uint8 {
		return uint8(float64(x>>8)*f + float64(y>>8)*(1-f) + 0.5)
	}
	return color.RGBA{R: channel(ar, br), G: channel(ag, bg), B: channel(ab, bb), A: 0xff}
}

// shift returns c moved by the difference from the color from to the color to.
func shift(c, from, to color.Color) color.Color {
	cr, cg, cb, _ := c.RGBA()
	fr, fg, fb, _ := from.RGBA()
	tr, tg, tb, _ := to.RGBA()
	channel := func(c, f, t uint32) uint8 {
		return uint8(min(max(int(c>>8)+int(t>>8)-int(f>>8), 0), 0xff))
	}
	return color.RGBA{R: channel(cr, fr, tr), G: channel(cg, fg, tg), B: channel(cb, fb, tb), A: 0xff}
}
