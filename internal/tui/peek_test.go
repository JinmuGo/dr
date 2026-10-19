package tui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// peekScreen returns the screen on testFiles in a terminal width cells wide and 24 lines
// high, with keys typed into it. In the unified view a.go has rows 0 to 18, b.bin its header
// on row 19 and its note on row 20, and c/new.go its header on row 21 and its rows to 26.
func peekScreen(width int, keys string) *model {
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	typeKeys(m, keys)
	return m
}

func TestPeekMove(t *testing.T) {
	// Space opens the peek view on the cursor's file, the cursor off its header. The keys
	// that move stay in the file, h and l go to the previous and the next file, and space,
	// Esc and q close the view with the cursor where it moved to; q does not quit.
	for _, tc := range []struct {
		keys   string
		open   bool
		cursor int
	}{
		{keys: " ", open: true, cursor: 1},
		{keys: "4j ", open: true, cursor: 4},
		{keys: "4j G", open: true, cursor: 18},
		{keys: "4j 30j", open: true, cursor: 18},
		{keys: "4j gg", open: true, cursor: 1},
		{keys: "4j }}}}}", open: true, cursor: 16}, // the last change of a.go
		{keys: " l", open: true, cursor: 20},       // the note of b.bin
		{keys: " 2l", open: true, cursor: 22},
		{keys: " 9l", open: true, cursor: 22},
		{keys: " llh", open: true, cursor: 20},
		{keys: "4j 3j ", cursor: 7},
		{keys: "4j 3j<esc>", cursor: 7},
		{keys: "4j 3j2<esc>", open: true, cursor: 7}, // Esc clears the count first
		{keys: "4j 3jq", cursor: 7},
	} {
		m := peekScreen(125, tc.keys)
		if m.peek.open != tc.open || m.cursor != tc.cursor {
			t.Errorf("%q: focus open %t, cursor at row %d; want %t and row %d", tc.keys, m.peek.open, m.cursor, tc.open, tc.cursor)
		}
	}
}

func TestPeekSplit(t *testing.T) {
	// At 110 cells the diff beside the file tree is too narrow for the split view, but the
	// peek view, which has no tree, is wide enough. s there splits the view, and closing it
	// goes back to the unified view on the same line.
	m := peekScreen(110, "s4j")
	if m.splitShown {
		t.Fatal("s: split view shown beside the file tree, want the unified view")
	}
	typeKeys(m, " ")
	if !m.splitShown {
		t.Fatal("s, space: unified view in the peek view, want the split view")
	}
	typeKeys(m, " ")
	if m.splitShown || m.cursor != 4 {
		t.Errorf("s, space, space: split view %t, cursor at row %d; want the unified view on row 4", m.splitShown, m.cursor)
	}
	m = peekScreen(110, "4j s")
	if !m.splitShown {
		t.Error("space, s: unified view in the peek view, want the split view")
	}
}

func TestPeekBox(t *testing.T) {
	// The peek view names the file and where it is among the files in its top border and
	// shows the file's rows, not those of the other files, in the mode FOCUS.
	m := peekScreen(125, "4j l")
	text := screen(t, m)
	lines := strings.Split(text, "\n")
	if top := lines[peekMarginY]; !strings.Contains(top, "╭─ b.bin ") || !strings.Contains(top, " 2/3 ") {
		t.Errorf("top border %q, want the path and 2/3", top)
	}
	if !strings.Contains(text, "Binary") || strings.Contains(text, "line 1 ") {
		t.Errorf("want the note of b.bin and no line of a.go:\n%s", text)
	}
	if got := m.mode().String(); got != "PEEK" {
		t.Errorf("mode %s, want FOCUS", got)
	}

	// A search's matches light the diff but not the peek view, and they light the diff again
	// once it closes.
	for _, keys := range []string{"/line<enter>", "s/line<enter>"} {
		m = peekScreen(125, keys)
		lit := func() bool { // the background of a match or of the current one is on the screen
			for _, c := range []color.Color{m.theme.matchbg, m.theme.matchcur} {
				r, g, b, _ := c.RGBA()
				if strings.Contains(m.View().Content, fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)) {
					return true
				}
			}
			return false
		}
		if !lit() {
			t.Fatalf("%q: no lit match in the diff", keys)
		}
		typeKeys(m, " ")
		if lit() {
			t.Errorf("%q, space: a lit match in the peek view", keys)
		}
		typeKeys(m, " ")
		if !lit() || m.search.query != "line" {
			t.Errorf("%q, space, space: query %q, want the matches lit again", keys, m.search.query)
		}
	}

	// v folds the file to its header, and the box says how to unfold it; v again unfolds it.
	m = peekScreen(125, " v")
	if !m.files[0].viewed || !strings.Contains(screen(t, m), "v unfolds it") {
		t.Errorf("v: viewed %t, want a.go viewed and the note:\n%s", m.files[0].viewed, screen(t, m))
	}
	typeKeys(m, "v")
	if m.files[0].viewed || m.cursor != 1 {
		t.Errorf("vv: viewed %t, cursor at row %d; want a.go unfolded and the cursor on row 1", m.files[0].viewed, m.cursor)
	}
}

func TestFocusMove(t *testing.T) {
	// f keeps the diff on the cursor's file, its header included. The keys that move stay
	// in the file, h and l go to the previous and the next file, a search goes to a match in
	// another file, and f again lets the keys leave the file.
	for _, tc := range []struct {
		keys   string
		cursor int
	}{
		{keys: "fG", cursor: 18},
		{keys: "f30j", cursor: 18},
		{keys: "f4jgg", cursor: 0},
		{keys: "f4j3G", cursor: 2},
		{keys: "f4j}}}}}", cursor: 16},
		{keys: "fl", cursor: 19},
		{keys: "fll", cursor: 21},
		{keys: "fllh", cursor: 19},
		{keys: "f9l", cursor: 21},
		{keys: "fGl", cursor: 19},
		{keys: "fl5k", cursor: 19},
		{keys: "f/package<enter>", cursor: 23},
		{keys: "fGfj", cursor: 19},
	} {
		m := peekScreen(125, tc.keys)
		if m.cursor != tc.cursor {
			t.Errorf("%q: cursor at row %d, want row %d", tc.keys, m.cursor, tc.cursor)
		}
	}
}

func TestFocusDraw(t *testing.T) {
	// The focused diff shows the cursor's file alone, with the files in the tree still, and
	// the status line says the diff is focused. The mode stays NORMAL, so a still asks.
	diff := func(m *model) string {
		list, _ := m.layout()
		var out []string
		for _, l := range strings.Split(screen(t, m), "\n")[:m.diffHeight()] {
			out = append(out, ansi.Cut(l, list+1, m.width))
		}
		return strings.Join(out, "\n")
	}
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 40})
	if d := diff(m); !strings.Contains(d, "a.go") || !strings.Contains(d, "b.bin") {
		t.Fatalf("want every file in the diff:\n%s", d)
	}
	typeKeys(m, "fl")
	d := diff(m)
	if !strings.Contains(d, "b.bin") || strings.Contains(d, "a.go") || strings.Contains(d, "c/new.go") {
		t.Errorf("fl: want b.bin alone in the diff:\n%s", d)
	}
	if s := statusLine(t, m); !strings.Contains(s, "NORMAL") || !strings.Contains(s, "f focus") {
		t.Errorf("fl: status line %q, want NORMAL and f focus", s)
	}
	typeKeys(m, "f")
	if d := diff(m); !strings.Contains(d, "a.go") || strings.Contains(statusLine(t, m), "f focus") {
		t.Errorf("flf: want every file again and no f focus:\n%s", d)
	}
}
