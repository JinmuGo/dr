package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

func TestModes(t *testing.T) {
	// The status line starts with a pill naming what the keys do now, and the key row lists
	// those keys, with how to quit at its right edge where q quits.
	for _, tc := range []struct {
		keys, pill, row, right string
	}{
		{"", " NORMAL ", "  a  ask    c  comment    e  edit    ⏎  origin    v  viewed    V  select", "  q  quit "},
		{"4jV", " VISUAL ", "  a  ask about 1 line    c  comment on 1 line    j k  extend    V esc  stop selecting", "  q  quit "},
		{"4j<enter>", " ORIGIN ", "  j k  scroll    a  ask    ⏎ esc  close", ""},
		{"2j<enter>", " ORIGIN ", "  j k  scroll    ⏎ esc  close", ""}, // an unchanged line cannot be asked about
		{"<enter>", " ORIGIN ", "  j k  scroll    ⏎ esc  close", ""},   // nor a file header
		{"4ja", " ASK ", "  ⏎  send    ⌥⏎  new line    ^  pick a model    esc  cancel", ""},
		{"4jc", " COMMENT ", "  ⏎  save    ⌥⏎  new line    ^  pick a model    esc  cancel", ""},
		{"/ab", " SEARCH ", "  ⏎  search    esc  cancel" + strings.Repeat(" ", 20), ""}, // n and N type into the query
		{":ab", " COMMAND ", "  ⏎  run    esc  cancel", ""},
		{"?", " HELP ", "  any key  close", ""},
	} {
		m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{answer: answer})
		typeKeys(m, tc.keys)
		status, keys := statusLine(t, m), keyLine(t, m)
		if !strings.HasPrefix(status, tc.pill+"  ") || !strings.HasPrefix(keys, tc.row) || !strings.HasSuffix(keys, tc.right) ||
			tc.right == "" && strings.Contains(keys, "quit") {
			t.Errorf("%q: status line %q, key row %q; want the pill %q, then keys %q and %q at the right", tc.keys, status, keys, tc.pill, tc.row, tc.right)
		}
	}

	// A terminal that tells Shift-Enter from Enter starts a new line with it.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	m.shiftEnter = true
	typeKeys(m, "4ja")
	if keys := keyLine(t, m); !strings.HasPrefix(keys, "  ⏎  send    ⇧⏎  new line") {
		t.Errorf("key row %q, want Shift-Enter for a new line", keys)
	}

	// The model list adds a chip after the pill, and the key row has its keys.
	m = pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "31Ga^")
	if status, keys := statusLine(t, m), keyLine(t, m); !strings.HasPrefix(status, " ASK  ^ model   ") || !strings.HasPrefix(keys, "  ↑ ↓  move    ⏎ tab  pick    esc  close the list") {
		t.Errorf("model list: status line %q, key row %q; want the chip and the keys of the list", status, keys)
	}
}

func TestStatusInFlight(t *testing.T) {
	// While a question is out the status line counts its seconds with a spinner that turns
	// with them. Then the origin box of its line has a thread, so a asks a follow-up. Pending
	// comments are counted, and the key row tells how to send them.
	m := commentScreen(testFiles(), t.TempDir())
	cmd := typeAndKeep(m, "4ja<enter>")
	for _, s := range []int{5, 6} {
		m.now = func() time.Time { return start.Add(time.Duration(s) * time.Second) }
		want := fmt.Sprintf("%c asking %ds", spinner[s%len(spinner)], s)
		if status := statusLine(t, m); !strings.Contains(status, want) {
			t.Errorf("%ds: status line %q, want %q", s, status, want)
		}
	}
	send(m, cmd())
	if typeKeys(m, "<esc><enter>"); !m.reading || !strings.HasPrefix(keyLine(t, m), "  j k  scroll    a  ask a follow-up") {
		t.Errorf("origin box with a thread: key row %q, want a follow-up", keyLine(t, m))
	}
	typeKeys(m, "<esc>cone<enter>")
	if status, keys := statusLine(t, m), keyLine(t, m); strings.Contains(status, "asking") || !strings.Contains(status, "✎ 1 pending") ||
		!strings.HasSuffix(keys, "  :submit  send 1 ") {
		t.Errorf("a pending comment: status line %q, key row %q; want it counted and :submit at the right", status, keys)
	}
}

func TestStatusCrowded(t *testing.T) {
	// On a narrow screen the pill and the start of a message stay; the right leaves out what
	// does not fit beside them, the row and file numbers first, the questions last.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcone<enter>Gctwo<enter>")
	m.comments[0].fix = &state.Fix{New: []string{"Changed two"}}
	cmd := typeAndKeep(m, "gg4ja<enter>")
	m.now = func() time.Time { return start.Add(5 * time.Second) }
	defer send(m, cmd())
	const message = "Answer from claude c1a0 #1 on a.go:2. Enter to read"
	for _, width := range []int{60, 150} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		m.warn(message)
		status := statusLine(t, m)
		if !strings.HasPrefix(status, " ORIGIN   ! "+message[:minMessage-1]) || !strings.Contains(status, "asking 5s") {
			t.Errorf("width %d: status line %q, want the pill, the start of the message and the question out", width, status)
		}
		if all := []string{"◆ 1 fix ready", "✎ 1 pending", "row 5/27", "file 1/3"}; width == 150 {
			for _, s := range all {
				if !strings.Contains(status, s) {
					t.Errorf("width %d: status line %q has no %q", width, status, s)
				}
			}
		} else if strings.Contains(status, "row") {
			t.Errorf("width %d: status line %q keeps the row number, which goes first", width, status)
		}
	}
}

func TestKeyRowFit(t *testing.T) {
	// The key row leaves out the keys that do not fit whole beside how to quit; none is cut.
	labels := []string{"ask", "comment", "edit", "origin", "viewed", "select", "split", "search", "command", "help"}
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	for width := 40; width <= 150; width++ {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		keys := keyLine(t, m)
		left, ok := strings.CutSuffix(keys, "  q  quit ")
		fields := strings.Fields(left)
		if !ok || len(fields) == 0 || !slices.Contains(labels, fields[len(fields)-1]) {
			t.Errorf("width %d: key row %q, want whole keys and q quit at the right", width, keys)
		}
	}
}

func TestPlainDiffstat(t *testing.T) {
	// Without colors the diffstat squares tell added, deleted and neither by their shape.
	m := New(testFiles(), nil, Saved{}, Asker{}).(*model)
	m.theme = newTheme(nil, true, true)
	if header := ansi.Strip(m.drawHeader(m.files[0], 80, false)); !strings.Contains(header, "+2 -2 ■▨□□□") {
		t.Errorf("header %q, want +2 -2 ■▨□□□", header)
	}
	m.theme = newTheme(nil, true, false)
	if header := ansi.Strip(m.drawHeader(m.files[0], 80, false)); !strings.Contains(header, "+2 -2 "+strings.Repeat(statSquare, 5)) {
		t.Errorf("header %q, want the Nerd Font squares with colors", header)
	}
}

func TestKeyRowRoom(t *testing.T) {
	// The key row shows on a screen of 12 rows or more, under the status line, and the diff
	// gives it a line. A shorter screen leaves it out for the diff.
	for _, tc := range []struct {
		height, diff int
		keys         bool
	}{
		{24, 22, true},
		{12, 10, true},
		{11, 10, false},
		{8, 7, false},
	} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{})
		m.Update(tea.WindowSizeMsg{Width: 125, Height: tc.height})
		lines := strings.Split(screen(t, m), "\n")
		last := lines[len(lines)-1]
		if m.diffHeight() != tc.diff || strings.HasPrefix(last, "  a  ask") != tc.keys || !strings.HasPrefix(lines[tc.diff], " NORMAL ") {
			t.Errorf("height %d: %d lines of the diff, last line %q; want %d, the key row %t, the status line on line %d",
				tc.height, m.diffHeight(), last, tc.diff, tc.keys, tc.diff)
		}
	}
}

func TestDiffstat(t *testing.T) {
	// GitHub's five squares: the size of the change sets how many are colored, each side
	// gets the floor of its share and at least one when it has lines, the rest stay grey.
	for _, tc := range []struct {
		added, deleted, a, d, rest int
	}{
		{27, 11, 3, 1, 1},
		{1, 0, 1, 0, 4},
		{15, 2, 4, 1, 0},
		{0, 0, 0, 0, 5},
		{2, 2, 1, 1, 3},
		{0, 300, 0, 5, 0},
	} {
		if a, d, rest := diffstat(tc.added, tc.deleted); a != tc.a || d != tc.d || rest != tc.rest {
			t.Errorf("+%d -%d: got %d, %d, %d; want %d, %d, %d", tc.added, tc.deleted, a, d, rest, tc.a, tc.d, tc.rest)
		}
	}
}

func TestStickyHeader(t *testing.T) {
	// Once the top row is inside a file, that file's header stays on the top line, and the
	// cursor never goes under it: moving up to the top row scrolls so it shows under the
	// header, and on to the header itself shows the header in its place.
	m := New(testFiles(), nil, Saved{}, Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12}) // 10 lines of the diff
	list, _ := m.layout()
	diff := func() []string {
		var lines []string
		for _, l := range strings.Split(screen(t, m), "\n")[:m.diffHeight()] {
			lines = append(lines, ansi.Cut(l, list+1, m.width))
		}
		return lines
	}
	header := func(lines []string) string { return strings.TrimLeft(lines[0], " ") }
	if typeKeys(m, "12j"); m.top == 0 || !strings.HasPrefix(header(diff()), "▾ a.go") || m.rowAt(0) != 0 {
		t.Fatalf("12j: top row %d, first line %q, row at line 0 %d; want a.go's header on top", m.top, diff()[0], m.rowAt(0))
	}
	for m.cursor > 0 {
		typeKeys(m, "k")
		lines := diff()
		y := cursorLine(m)
		if !strings.HasPrefix(lines[y], "▌") || m.sticky() && (y < 1 || !strings.HasPrefix(header(lines), "▾ a.go")) {
			t.Fatalf("k to row %d with row %d on top: cursor on line %d of\n%s\nwant it under the header", m.cursor, m.top, y, strings.Join(lines, "\n"))
		}
	}
	if m.top != 0 || m.sticky() {
		t.Errorf("on the header: top row %d, header on top %t; want the header in its place", m.top, m.sticky())
	}
	// In another file the header on top is that file's, here on a screen of 5 lines of the
	// diff, with c/new.go's last 4 lines under it.
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 6})
	typeKeys(m, "G")
	if lines := diff(); m.top != 23 || !strings.HasPrefix(header(lines), "▾ c/new.go") || strings.Count(strings.Join(lines, "\n"), "▾ c/new.go") != 1 {
		t.Errorf("G: top row %d, diff\n%s\nwant row 23 at the top under c/new.go's header", m.top, strings.Join(lines, "\n"))
	}
	// A 2-row screen has a single line of the diff, which shows the cursor row, not the header.
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 2})
	typeKeys(m, "k")
	if lines := diff(); m.sticky() || cursorLine(m) != 0 || !strings.HasPrefix(lines[0], "▌") {
		t.Errorf("height 2: header on top %t, cursor on line %d of %q; want the cursor row alone", m.sticky(), cursorLine(m), lines)
	}
}
