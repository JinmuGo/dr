package tui

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
)

// keyGlyphs are the key presses, written as for keyPresses, of the keys the key help and
// the key row name with a glyph or a word.
var keyGlyphs = map[string]string{
	"⏎": "<enter>", "⇧⏎": "<shift+enter>", "⌥⏎": "<alt+enter>", "esc": "<esc>", "tab": "<tab>",
	"↑": "<up>", "↓": "<down>", "^C": "<ctrl+c>", "^J": "<ctrl+j>", "^N": "<ctrl+n>", "^P": "<ctrl+p>", "^U": "<ctrl+u>",
	"any key": "x",
}

// labelPresses returns the key presses of each key a label of the key help or the key row
// names: "j k" is j and k, ":submit" types the command and runs it.
func labelPresses(t *testing.T, label string) []string {
	t.Helper()
	if p, ok := keyGlyphs[label]; ok {
		return []string{p}
	}
	var out []string
	for _, k := range strings.Fields(label) {
		switch p, ok := keyGlyphs[k]; {
		case ok:
			out = append(out, p)
		case len(k) > 1 && k[0] == ':':
			out = append(out, k+"<enter>")
		case strings.IndexFunc(k, func(r rune) bool { return r > unicode.MaxASCII || unicode.IsSpace(r) }) >= 0:
			t.Fatalf("label %q: no key press for %q; add it to keyGlyphs", label, k)
		default:
			out = append(out, k)
		}
	}
	return out
}

// snapshot returns all of m's state as text, to tell whether a key changed it. The model
// finder's state is behind a pointer, which the text of m shows only as an address.
func snapshot(m *model) string {
	return fmt.Sprintf("%+v %+v", m, m.entry.picker)
}

func TestKeysHandled(t *testing.T) {
	// Every key that the key help or the key row names does something in its mode: it
	// returns a command or changes the screen's state. A key no mode takes does neither.
	screenOf := func(files []review.File, keys string) func(t *testing.T) *model {
		return func(t *testing.T) *model {
			m := pickScreen(files, t.TempDir(), &fakeAsk{answer: answer}, agentModels())
			typeKeys(m, keys)
			return m
		}
	}
	// The groups of the key help, by the mode their keys work in. MOUSE names no key.
	helpModes := map[string]string{"MOVE": "NORMAL", "REVIEW": "NORMAL", "ASK AND FIX": "NORMAL", "OTHER": "NORMAL", "IN A BOX": "ASK", "MOUSE": ""}
	help := make(map[string][]string)
	for _, column := range helpColumns {
		for _, g := range column {
			mode, ok := helpModes[g.name]
			if !ok {
				t.Fatalf("help group %q: no mode for its keys; add it to helpModes", g.name)
			}
			for _, k := range g.keys {
				help[mode] = append(help[mode], k[0])
			}
		}
	}
	for _, tc := range []struct {
		name   string
		mode   string // as the pill names it
		screen func(t *testing.T) *model
		first  map[string]string // keys typed before a press that needs them to have work
		inert  string            // a key that does nothing there
	}{
		// On a line between two changes with a search query, so { }, n, N and Esc have work.
		{"normal", "NORMAL", screenOf(testFiles(), "/line 1<enter>12G"), nil, "Z"},
		{"visual", "VISUAL", screenOf(testFiles(), "4jV"), nil, "Z"},
		{"origin", "ORIGIN", screenOf(longFiles(), "G<enter>j"), nil, "Z"}, // a box with text to scroll both ways
		{"ask", "ASK", screenOf(testFiles(), "4jawhy"), nil, ""},
		{"comment", "COMMENT", screenOf(testFiles(), "4jcfix it"), nil, ""},
		{"model finder", "ASK", screenOf(pickFiles(), "31Ga^o"), map[string]string{"<up>": "<down>", "<ctrl+p>": "<down>"}, ""},
		{"search", "SEARCH", screenOf(testFiles(), "/ab"), nil, ""},
		{"command", "COMMAND", screenOf(testFiles(), ":"), map[string]string{"<up>": "<down>"}, ""},
		{"help", "HELP", screenOf(testFiles(), "?"), nil, ""},
		{"empty", "NORMAL", screenOf(nil, ""), nil, "Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.screen(t)
			if got := m.mode().String(); got != tc.mode {
				t.Fatalf("mode %s, want %s", got, tc.mode)
			}
			var labels []string
			for _, k := range m.keycaps() {
				labels = append(labels, k.key)
			}
			if tc.name == "normal" || tc.name == "ask" {
				labels = append(labels, help[tc.mode]...)
			}
			for _, label := range labels {
				for _, presses := range labelPresses(t, label) {
					m := tc.screen(t)
					typeKeys(m, tc.first[presses])
					before := snapshot(m)
					var cmd tea.Cmd
					for _, k := range keyPresses(presses) {
						if _, c := m.Update(k); c != nil {
							cmd = c
						}
					}
					if cmd == nil && snapshot(m) == before {
						t.Errorf("%s (%q): did nothing", presses, label)
					}
				}
			}
			if tc.inert != "" {
				m := tc.screen(t)
				before := snapshot(m)
				if _, cmd := m.Update(keyPresses(tc.inert)[0]); cmd != nil || snapshot(m) != before {
					t.Errorf("%s: did something, want nothing; the test cannot tell a key that works", tc.inert)
				}
			}
		})
	}
}
