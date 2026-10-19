package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestThemes(t *testing.T) {
	// Each named theme has its chroma style, and DR_THEME paints the screen with its colors
	// on any terminal: dark or light as the theme is, its tints mixed into the terminal's
	// background.
	for name, p := range palettes {
		if _, ok := styles.Registry[p.syntax]; !ok {
			t.Errorf("%s: no chroma style %q", name, p.syntax)
		}
		m := newModel(testFiles(), nil, Saved{Theme: name}, Asker{})
		m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})
		if m.message != "" || m.theme.dark != p.dark || !same(m.theme.accent, lipgloss.Color(p.blue)) || m.theme.syntax.Name != p.syntax {
			t.Errorf("%s: message %q, dark %t, accent %v, syntax %s; want the theme's", name, m.message, m.theme.dark, m.theme.accent, m.theme.syntax.Name)
		}
		if !same(m.theme.base, lipgloss.Color("#ffffff")) {
			t.Errorf("%s: tints mixed into %v, want the terminal's background", name, m.theme.base)
		}
	}
}

func TestUnknownTheme(t *testing.T) {
	// A name dr does not know keeps the default colors, and the status line lists the names.
	m := newModel(testFiles(), nil, Saved{Theme: "solarized", Problem: "could not read viewed state"}, Asker{})
	if !strings.Contains(m.message, "could not read viewed state; unknown DR_THEME \"solarized\", one of github, catppuccin-latte") ||
		!same(m.theme.accent, newTheme(nil, true, false).accent) {
		t.Errorf("message %q, accent %v; want the default theme and the names", m.message, m.theme.accent)
	}
}

// same reports whether a and b are the same color.
func same(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}
