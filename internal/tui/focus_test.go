package tui

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// focusScreen returns the screen on testFiles in a terminal width cells wide and 24 lines
// high, with keys typed into it. In the unified view a.go has rows 0 to 18, b.bin its header
// on row 19 and its note on row 20, and c/new.go its header on row 21 and its rows to 26.
func focusScreen(width int, keys string) *model {
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
	typeKeys(m, keys)
	return m
}

func TestFocusMove(t *testing.T) {
	// Space opens the focus view on the cursor's file, the cursor off its header. The keys
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
		m := focusScreen(125, tc.keys)
		if m.focus.open != tc.open || m.cursor != tc.cursor {
			t.Errorf("%q: focus open %t, cursor at row %d; want %t and row %d", tc.keys, m.focus.open, m.cursor, tc.open, tc.cursor)
		}
	}
}

func TestFocusSplit(t *testing.T) {
	// At 110 cells the diff beside the file tree is too narrow for the split view, but the
	// focus view, which has no tree, is wide enough. s there splits the view, and closing it
	// goes back to the unified view on the same line.
	m := focusScreen(110, "s4j")
	if m.splitShown {
		t.Fatal("s: split view shown beside the file tree, want the unified view")
	}
	typeKeys(m, " ")
	if !m.splitShown {
		t.Fatal("s, space: unified view in the focus view, want the split view")
	}
	typeKeys(m, " ")
	if m.splitShown || m.cursor != 4 {
		t.Errorf("s, space, space: split view %t, cursor at row %d; want the unified view on row 4", m.splitShown, m.cursor)
	}
	m = focusScreen(110, "4j s")
	if !m.splitShown {
		t.Error("space, s: unified view in the focus view, want the split view")
	}
}

func TestFocusBox(t *testing.T) {
	// The focus view names the file and where it is among the files in its top border and
	// shows the file's rows, not those of the other files, in the mode FOCUS.
	m := focusScreen(125, "4j l")
	text := screen(t, m)
	lines := strings.Split(text, "\n")
	if top := lines[focusMarginY]; !strings.Contains(top, "╭─ b.bin ") || !strings.Contains(top, " 2/3 ") {
		t.Errorf("top border %q, want the path and 2/3", top)
	}
	if !strings.Contains(text, "Binary") || strings.Contains(text, "line 1 ") {
		t.Errorf("want the note of b.bin and no line of a.go:\n%s", text)
	}
	if got := m.mode().String(); got != "FOCUS" {
		t.Errorf("mode %s, want FOCUS", got)
	}

	// A search's matches light the diff but not the focus view, and they light the diff again
	// once it closes.
	for _, keys := range []string{"/line<enter>", "s/line<enter>"} {
		m = focusScreen(125, keys)
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
			t.Errorf("%q, space: a lit match in the focus view", keys)
		}
		typeKeys(m, " ")
		if !lit() || m.search.query != "line" {
			t.Errorf("%q, space, space: query %q, want the matches lit again", keys, m.search.query)
		}
	}

	// v folds the file to its header, and the box says how to unfold it; v again unfolds it.
	m = focusScreen(125, " v")
	if !m.files[0].viewed || !strings.Contains(screen(t, m), "v unfolds it") {
		t.Errorf("v: viewed %t, want a.go viewed and the note:\n%s", m.files[0].viewed, screen(t, m))
	}
	typeKeys(m, "v")
	if m.files[0].viewed || m.cursor != 1 {
		t.Errorf("vv: viewed %t, cursor at row %d; want a.go unfolded and the cursor on row 1", m.files[0].viewed, m.cursor)
	}
}
