package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// theme is the colors of the screen, for a dark or a light terminal. The background stays
// quiet and color carries meaning: blue for focus, violet for questions, amber for comments
// and warnings, teal for fixes. Unchanged lines keep the terminal's own background, and the
// tinted backgrounds move with it, so they stand out from it as much as they do on the page
// they were made for. Without colors, as with NO_COLOR, pills and bands show in reverse
// video and quiet text faint, so the screen keeps its shape.
type theme struct {
	bg    color.Color // the terminal's background, nil when it did not tell
	base  color.Color // the background the tinted colors are for: bg, or the page's when bg is nil
	dark  bool        // the terminal has a dark background
	plain bool        // the terminal shows no colors

	panel      color.Color // the file tree and the status line
	surface    color.Color // the fill of boxes
	surface2   color.Color // a file header, a selected tree row, keycaps
	cursorline color.Color // the cursor row on an unchanged line
	line       color.Color // dividers
	line2      color.Color // borders of floating boxes, empty diffstat squares

	fg, strong, muted, subtle, faint color.Color // text, from the most to the least readable; strong for names and titles
	accent                           color.Color // focus, the cursor marker, VISUAL
	ask                              color.Color // questions, answers, asking
	pending                          color.Color // comments, warnings, a file changed since last view
	fix                              color.Color // fixes ready
	add, del                         color.Color // signs, counts; del for errors too

	addbg, addnum, addword, addcur color.Color // an added line, its line numbers, its changed words, under the cursor
	delbg, delnum, delword, delcur color.Color // likewise for a deleted line
	hunkbg, hunkfg                 color.Color // the header of a hunk
	selbg, selnum, seladd, seldel  color.Color // a selected unchanged line, its numbers, a selected added and deleted line
	matchbg, matchcur              color.Color // a search match, and the current one

	// sessions are the colors of the sessions, given in the order they first appear in the
	// diff and cycled past the last. They are nil without colors.
	sessions [6]color.Color

	syntax *chroma.Style
}

// newTheme returns the theme for a terminal with the background bg, dark or light, nil when
// the terminal did not tell it. It shows no colors when plain is set.
func newTheme(bg color.Color, dark, plain bool) theme {
	lightDark := lipgloss.LightDark(dark)
	c := func(light, dark string) color.Color {
		return lightDark(lipgloss.Color(light), lipgloss.Color(dark))
	}
	page := c("#fbfbfc", "#0d1015") // the background the tinted colors are made for
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

		panel:      tint("#f3f4f7", "#11151b"),
		surface:    tint("#f0f2f5", "#161b23"),
		surface2:   tint("#e5e8ee", "#1e2530"),
		cursorline: tint("#e8eef8", "#172031"),
		line:       tint("#d9dee5", "#262d39"),
		line2:      tint("#bfc6d1", "#3a4354"),

		fg:      c("#2b323d", "#c9d1dc"),
		strong:  c("#0e1117", "#f1f4f8"),
		muted:   c("#5b6574", "#8a93a3"),
		subtle:  c("#8a93a0", "#5f6979"),
		faint:   tint("#c8ced7", "#343c49"),
		accent:  c("#2c6ad8", "#6fb3ff"),
		ask:     c("#7550dc", "#b49cff"),
		pending: c("#a86f05", "#f0b95b"),
		fix:     c("#0d927e", "#4fd6be"),
		add:     c("#1d8641", "#5cc777"),
		del:     c("#c93547", "#f1707b"),

		addbg:   tint("#eef9f1", "#0f1d16"),
		addnum:  tint("#dbf2e3", "#12261b"),
		addword: tint("#b2e4c2", "#1f4d30"),
		addcur:  tint("#dcf1e3", "#173326"),
		delbg:   tint("#fdeff1", "#22131a"),
		delnum:  tint("#f8dde1", "#2c1720"),
		delword: tint("#f3bcc5", "#5d2431"),
		delcur:  tint("#f6e0e4", "#351c26"),
		hunkbg:  tint("#eef3fb", "#101824"),
		hunkfg:  c("#5470a0", "#7890b6"),
		selbg:   tint("#e0e9fb", "#182844"),
		selnum:  tint("#c8d9f8", "#203760"),
		seladd:  tint("#d7ebf0", "#173039"),
		seldel:  tint("#eadff3", "#28263f"),
		matchbg: tint("#fbe6ad", "#4b3b12"),

		matchcur: c("#e0a020", "#f0b95b"), // bright, not a tint: the current match stands out on any line

		syntax: styles.Get("github"),
	}
	if !plain {
		t.sessions = [...]color.Color{
			c("#b9632c", "#e7a274"), c("#14858d", "#5fc6d0"), c("#b6427a", "#ea8cb3"),
			c("#74821a", "#c6d071"), c("#3c6bd0", "#92b9ff"), c("#7652c8", "#bea8f3"),
		}
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
	if t.plain && (fg == t.muted || fg == t.subtle || fg == t.faint) {
		s = s.Faint(true)
	}
	return s
}

// onBand returns the style of text in fg on the band bg. Without colors the band shows in
// reverse video.
func (t *theme) onBand(fg, bg color.Color) lipgloss.Style {
	return t.paint(fg, bg).Reverse(t.plain)
}

// pill returns the style of a mode pill: fg on bg, bold. Without colors it shows in reverse
// video.
func (t *theme) pill(fg, bg color.Color) lipgloss.Style {
	if t.plain {
		return lipgloss.NewStyle().Bold(true).Reverse(true)
	}
	return t.paint(fg, bg).Bold(true)
}

// chip returns the style of text in fg on a chip of the color tone: tone mixed 18% into the
// surface color. Without colors the chip shows in reverse video.
func (t *theme) chip(fg, tone color.Color) lipgloss.Style {
	if t.plain {
		return lipgloss.NewStyle().Reverse(true)
	}
	return t.paint(fg, mix(tone, t.surface, 0.18))
}

// match returns the style of a search match in text of style st: on the match color, or for
// the current match in the color of the background on a bright one. Without colors a match
// shows in reverse video against its text, the current one bold too.
func (t *theme) match(st lipgloss.Style, current bool) lipgloss.Style {
	switch {
	case t.plain:
		return st.Reverse(!st.GetReverse()).Bold(current)
	case current:
		return st.Foreground(t.base).Background(t.matchcur)
	}
	return st.Background(t.matchbg)
}

// code returns the color of code of token type k, GitHub's syntax color.
func (t *theme) code(k chroma.TokenType) color.Color {
	if e := t.syntax.Get(k); e.Colour.IsSet() {
		return color.RGBA{R: e.Colour.Red(), G: e.Colour.Green(), B: e.Colour.Blue(), A: 0xff}
	}
	return t.fg
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
