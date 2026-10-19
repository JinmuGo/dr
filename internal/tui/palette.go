package tui

import (
	"image/color"
	"maps"
	"slices"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// palette is the colors of a named theme, which DR_THEME picks: its background and text,
// the colors that carry meaning, the colors of the sessions and the chroma style of its
// code. The rest of the screen's colors are mixed from them.
type palette struct {
	dark                                    bool
	bg, fg, comment                         string
	blue, magenta, yellow, cyan, green, red string
	sessions                                [6]string
	syntax                                  string // the chroma style
}

// palettes are the named themes, by the name DR_THEME gives.
var palettes = map[string]palette{
	"catppuccin-mocha": {
		dark: true, bg: "#1e1e2e", fg: "#cdd6f4", comment: "#7f849c",
		blue: "#89b4fa", magenta: "#cba6f7", yellow: "#f9e2af", cyan: "#94e2d5", green: "#a6e3a1", red: "#f38ba8",
		sessions: [6]string{"#fab387", "#94e2d5", "#f5c2e7", "#f9e2af", "#74c7ec", "#cba6f7"},
		syntax:   "catppuccin-mocha",
	},
	"catppuccin-latte": {
		bg: "#eff1f5", fg: "#4c4f69", comment: "#8c8fa1",
		blue: "#1e66f5", magenta: "#8839ef", yellow: "#df8e1d", cyan: "#179299", green: "#40a02b", red: "#d20f39",
		sessions: [6]string{"#fe640b", "#179299", "#ea76cb", "#df8e1d", "#209fb5", "#8839ef"},
		syntax:   "catppuccin-latte",
	},
	"onedark": {
		dark: true, bg: "#282c34", fg: "#abb2bf", comment: "#7f848e",
		blue: "#61afef", magenta: "#c678dd", yellow: "#e5c07b", cyan: "#56b6c2", green: "#98c379", red: "#e06c75",
		sessions: [6]string{"#d19a66", "#56b6c2", "#c678dd", "#e5c07b", "#61afef", "#98c379"},
		syntax:   "onedark",
	},
	"tokyonight": {
		dark: true, bg: "#1a1b26", fg: "#c0caf5", comment: "#565f89",
		blue: "#7aa2f7", magenta: "#bb9af7", yellow: "#e0af68", cyan: "#7dcfff", green: "#9ece6a", red: "#f7768e",
		sessions: [6]string{"#ff9e64", "#73daca", "#bb9af7", "#e0af68", "#2ac3de", "#7aa2f7"},
		syntax:   "tokyonight-night",
	},
	"gruvbox": {
		dark: true, bg: "#282828", fg: "#ebdbb2", comment: "#928374",
		blue: "#83a598", magenta: "#d3869b", yellow: "#fabd2f", cyan: "#8ec07c", green: "#b8bb26", red: "#fb4934",
		sessions: [6]string{"#fe8019", "#8ec07c", "#d3869b", "#fabd2f", "#83a598", "#b8bb26"},
		syntax:   "gruvbox",
	},
}

// Themes returns the names DR_THEME takes, in order: the default first, then the named
// themes.
func Themes() []string {
	return append([]string{defaultTheme}, slices.Sorted(maps.Keys(palettes))...)
}

// defaultTheme is the theme dr shows when DR_THEME is not set: GitHub's colors, light or
// dark as the terminal is.
const defaultTheme = "github"

// knownTheme reports whether DR_THEME may name name: "" or a name of Themes.
func knownTheme(name string) bool {
	_, ok := palettes[name]
	return ok || name == "" || name == defaultTheme
}

// themeFor returns the theme named name for a terminal with the background bg, dark or
// light, nil when the terminal did not tell it. A name that is not a named theme gives the
// default theme. It shows no colors when plain is set.
func themeFor(name string, bg color.Color, dark, plain bool) theme {
	p, ok := palettes[name]
	if !ok {
		return newTheme(bg, dark, plain)
	}
	return p.theme(bg, plain)
}

// theme returns the theme of p. It is dark or light as p is, whatever the terminal is. Its
// tinted colors are mixed into the terminal's background bg, which unchanged lines keep, or
// into p's when the terminal did not tell it: the surfaces from the text color, the lines
// of a diff from green, red and blue, as the default theme tints them.
func (p palette) theme(bg color.Color, plain bool) theme {
	c := lipgloss.Color
	base := c(p.bg)
	if bg != nil {
		base = bg
	}
	fg := c(p.fg)
	edge := c("#ffffff") // what the strongest text leans to
	if !p.dark {
		edge = c("#000000")
	}
	blue, green, red, yellow := c(p.blue), c(p.green), c(p.red), c(p.yellow)
	on := func(tone color.Color, f float64) color.Color { return mix(tone, base, f) }
	t := theme{
		bg:    bg,
		base:  base,
		dark:  p.dark,
		plain: plain,

		panel:      on(fg, 0.03),
		surface:    on(fg, 0.06),
		surface2:   on(fg, 0.10),
		cursorline: on(blue, 0.10),
		line:       on(fg, 0.14),
		line2:      on(fg, 0.24),

		fg:      fg,
		strong:  mix(edge, fg, 0.45),
		muted:   on(fg, 0.62),
		subtle:  c(p.comment),
		faint:   on(fg, 0.20),
		accent:  blue,
		ask:     c(p.magenta),
		pending: yellow,
		fix:     c(p.cyan),
		add:     green,
		del:     red,

		addbg:   on(green, 0.09),
		addnum:  on(green, 0.15),
		addword: on(green, 0.30),
		addcur:  on(green, 0.18),
		delbg:   on(red, 0.09),
		delnum:  on(red, 0.15),
		delword: on(red, 0.30),
		delcur:  on(red, 0.18),
		hunkbg:  on(blue, 0.07),
		hunkfg:  mix(blue, fg, 0.55),
		selbg:   on(blue, 0.16),
		selnum:  on(blue, 0.26),
		seladd:  mix(blue, on(green, 0.12), 0.14),
		seldel:  mix(blue, on(red, 0.12), 0.14),
		matchbg: on(yellow, 0.28),

		matchcur: yellow,

		syntax: styles.Get(p.syntax),
	}
	if !plain {
		for i, s := range p.sessions {
			t.sessions[i] = c(s)
		}
	}
	return t
}
