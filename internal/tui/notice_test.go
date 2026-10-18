package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// noticeLines returns the notice floating at the top right of the diff on m's screen, a
// line for each line inside its border, without its colors and its margins, or nil when no
// notice shows.
func noticeLines(t *testing.T, m *model) []string {
	t.Helper()
	_, w := m.layout()
	if len(m.rows) == 0 {
		w = m.width
	}
	width := min(maxNoticeWidth, w-4)
	x := m.width - width - 2
	lines := strings.Split(screen(t, m), "\n")
	var out []string
	found := false
	for _, l := range lines[m.bandLines():m.diffHeight()] {
		l = ansi.Cut(l, x, x+width)
		switch {
		case !found && strings.HasPrefix(l, "╭─") && strings.HasSuffix(l, "─╮"):
			found = true
		case found && strings.HasPrefix(l, "╰"):
			return out
		case found:
			l = strings.TrimSuffix(strings.TrimPrefix(l, "│ "), " │")
			out = append(out, strings.TrimRight(l, " "))
		}
	}
	return nil
}

// noticeText returns the text of the notice on m's screen on one line: its lines without
// their indent, joined with spaces, so that a line it wrapped reads whole; "" when no notice
// shows.
func noticeText(t *testing.T, m *model) string {
	t.Helper()
	lines := noticeLines(t, m)
	for i, l := range lines {
		lines[i] = strings.TrimLeft(l, " ")
	}
	return strings.Join(lines, " ")
}

// helpLines returns the lines of the key help's box on m's screen without their colors, from
// its top border to its bottom border, or nil when it does not show.
func helpLines(t *testing.T, m *model) []string {
	t.Helper()
	var box []string
	x, w := 0, 0
	for _, l := range strings.Split(screen(t, m), "\n") {
		if i := strings.Index(l, "╭─ Keys "); i >= 0 && box == nil {
			x, w = ansi.StringWidth(l[:i]), ansi.StringWidth(l[i:strings.Index(l, "╮")+len("╮")])
		}
		if w > 0 {
			box = append(box, ansi.Cut(l, x, x+w))
			if strings.HasPrefix(box[len(box)-1], "╰") {
				return box
			}
		}
	}
	return nil
}

func TestHelp(t *testing.T) {
	// ? opens the key help over the screen, in groups of keys in two columns from 80 cells
	// on, every key with what it does, today's keys among them. Any key closes it and does
	// nothing else.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	for _, width := range []int{125, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		typeKeys(m, "gg4j?")
		box := helpLines(t, m)
		if len(box) == 0 || !strings.Contains(box[0], " any key closes ") || !strings.HasPrefix(box[len(box)-1], "╰") {
			t.Fatalf("%d×24: the screen has no whole help:\n%s", width, screen(t, m))
		}
		text := strings.Join(box, "\n")
		var want []string
		for _, column := range helpColumns {
			for _, g := range column {
				want = append(want, g.name)
				for _, k := range g.keys {
					key := " " + k[0] + " "
					want = append(want, key+strings.Repeat(" ", helpLabel-ansi.StringWidth(key))+k[1])
				}
			}
		}
		for _, s := range append(want, " d u J K  half", " a        ask", " ga       open", " ^        pick a model") {
			if !strings.Contains(text, s) {
				t.Errorf("%d×24: the help has no %q:\n%s", width, s, text)
			}
		}
		if !slices.ContainsFunc(box, func(l string) bool { return strings.Contains(l, "MOVE") && strings.Contains(l, "ASK AND FIX") }) {
			t.Errorf("%d×24: want MOVE and ASK AND FIX side by side:\n%s", width, text)
		}
		typeKeys(m, "j")
		if m.help || m.cursor != 4 || helpLines(t, m) != nil {
			t.Errorf("%d×24, j: help open %t, cursor at row %d; want the help closed and the cursor kept on row 4", width, m.help, m.cursor)
		}
	}

	// On a narrower screen the groups come in one column, and a screen too short for the box
	// shows its top and as much as fits, then its bottom border.
	m.Update(tea.WindowSizeMsg{Width: 79, Height: 24})
	typeKeys(m, "?")
	box := helpLines(t, m)
	at := func(name string) int {
		return slices.IndexFunc(box, func(l string) bool { return strings.Contains(l, name) })
	}
	if len(box) != m.height || at("MOVE") < 0 || at("ASK AND FIX") <= at("MOUSE") || !strings.HasPrefix(box[len(box)-1], "╰") {
		t.Errorf("at 79×24: want one column cut to the screen:\n%s", strings.Join(box, "\n"))
	}
}

func TestHelpScrim(t *testing.T) {
	// Behind the help the screen is faint, without its colors, with colors or without.
	for _, plain := range []bool{false, true} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
		m.theme = newTheme(nil, true, plain)
		typeKeys(m, "?")
		lines := strings.Split(m.View().Content, "\n")
		last := lines[len(lines)-1] // the key row, which the box does not cover
		if want := m.theme.paint(m.theme.faint, nil).Render(ansi.Strip(last)); last != want {
			t.Errorf("plain %t: key row %q, want it faint %q", plain, last, want)
		}
		if helpLines(t, m) == nil {
			t.Errorf("plain %t: no help box", plain)
		}
	}
}

// commandLines returns the command list on m's screen, the lines from its top border to its
// bottom border right above the status line, without their colors, or nil when none shows.
func commandLines(t *testing.T, m *model) []string {
	t.Helper()
	lines := strings.Split(screen(t, m), "\n")[:m.diffHeight()]
	w := min(maxCommandsWidth, m.width-2)
	if !strings.HasPrefix(ansi.Cut(lines[len(lines)-1], 1, 1+w), "╰") {
		return nil
	}
	for top := len(lines) - 1; top >= 0; top-- {
		if l := ansi.Cut(lines[top], 1, 1+w); strings.HasPrefix(l, "╭─ commands ") {
			var box []string
			for _, l := range lines[top+1 : len(lines)-1] {
				box = append(box, strings.TrimRight(strings.TrimSuffix(strings.TrimRight(ansi.Cut(l, 1, 1+w), " "), "│"), " "))
			}
			return box
		}
	}
	return nil
}

func TestCommandList(t *testing.T) {
	// : opens a list of the commands above the status line, each with what it does now. What
	// is typed filters it, ↑ ↓ choose, Tab completes the input and Enter runs the highlighted
	// command once something is typed; with nothing typed Enter closes the input and runs
	// nothing. A name that is no command keeps today's error.
	files, dir := testFiles(), t.TempDir()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]}]}`}}
	m := fixScreen(files, Load(dir), f, writeTree(t, files), fixClock())
	typeKeys(m, ":")
	if got, want := commandLines(t, m), []string{"│▌:submit     No pending comments", "│ :undo       No fix to revert"}; !slices.Equal(got, want) {
		t.Errorf(":: list %q, want %q", got, want)
	}
	typeKeys(m, "<esc>4jcone<enter>gg11jVjctwo<enter>:")
	if got := commandLines(t, m); len(got) != 2 || got[0] != "│▌:submit     Send 2 pending comments to 2 sessions" {
		t.Errorf("two comments: list %q", got)
	}
	for _, keys := range []string{"<enter>", ":<down><enter>"} {
		typeKeys(m, keys)
		if m.cmdline.open || len(f.asked()) != 0 || len(m.pendingComments()) != 2 || m.notice != nil || m.message != "" {
			t.Errorf("%q with nothing typed: input open %t, %d asked, %d pending groups, notice %q, message %q; want nothing run",
				keys, m.cmdline.open, len(f.asked()), len(m.pendingComments()), noticeText(t, m), m.message)
		}
	}
	typeKeys(m, ":u")
	if got := commandLines(t, m); !slices.Equal(got, []string{"│▌:undo       No fix to revert"}) {
		t.Errorf(":u: list %q, want undo alone", got)
	}
	typeKeys(m, "<bs>s")
	th := &m.theme
	if s := m.View().Content; !strings.Contains(s, th.onBand(th.ask, th.surface2).Bold(true).Underline(true).Render("s")) {
		t.Errorf(":s: the typed part of submit is not lit")
	}
	typeKeys(m, "<tab>")
	if m.cmdline.input != "submit" || !strings.HasPrefix(statusLine(t, m), " COMMAND   :submit") {
		t.Errorf("tab: input %q, want submit", m.cmdline.input)
	}
	cmd := typeAndKeep(m, "<ctrl+u>su<enter>")
	if m.cmdline.open || cmd == nil || !strings.HasPrefix(noticeText(t, m), "✓ Sent 2 fix requests to ") {
		t.Fatalf(":su Enter: input open %t, notice %q; want :submit run", m.cmdline.open, noticeText(t, m))
	}
	run(m, cmd)
	typeAll(m, "gg4jy:")
	if got := commandLines(t, m); len(got) != 2 || got[1] != "│ :undo       Revert the last fix dr applied" {
		t.Errorf("after y: list %q", got)
	}
	typeKeys(m, "<down><tab><enter>")
	if r := records(t, dir); len(r.Undone) != 1 || !strings.HasPrefix(noticeText(t, m), "✓ Reverted the fix for a.go:2") {
		t.Errorf("↓ Tab Enter: %d undone, notice %q; want :undo run", len(r.Undone), noticeText(t, m))
	}
	typeKeys(m, ":ship")
	if got := commandLines(t, m); got != nil {
		t.Errorf(":ship: list %q, want none", got)
	}
	typeKeys(m, "<enter>")
	if status := statusLine(t, m); !strings.HasPrefix(status, " NORMAL   ✗ Not a dr command: ship") {
		t.Errorf(":ship Enter: status line %q", status)
	}
	typeKeys(m, ":<esc>")
	if m.cmdline.open || commandLines(t, m) != nil {
		t.Errorf("Esc: input open %t, want the list closed", m.cmdline.open)
	}
}

func TestNotices(t *testing.T) {
	// Each notice has the icon of its tone, its text and what to do next, and stays until
	// the next key. Esc only dismisses it; another key dismisses it and does its own work.
	files, dir := testFiles(), t.TempDir()
	root := writeTree(t, files)
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]},{"comment":2,"new_lines":["\tvar x = 2"]}]}`}}
	m := fixScreen(files, Load(dir), f, root, fixClock())
	cmd := typeAndKeep(m, "4jcone<enter>Gcthree<enter>:submit<enter>")
	if got := noticeLines(t, m); !slices.Equal(got, []string{"✓ Sent 1 fix request to claude c1a0 (2 comments)", "  The fixes come under the comments."}) {
		t.Errorf(":submit: notice %q", got)
	}
	typeKeys(m, "gg")
	run(m, cmd)
	send(m, tickMsg{}) // a message other than a key keeps it
	if got := noticeLines(t, m); !slices.Equal(got, []string{"✓ Fixes from claude c1a0 on a.go:2 and 1 more", "  ⏎ to read   esc dismiss"}) {
		t.Fatalf("fixes: notice %q", got)
	}
	typeKeys(m, "<esc>")
	if m.cursor != 0 || m.notice != nil || noticeLines(t, m) != nil {
		t.Errorf("Esc: cursor at row %d, notice %q; want it dismissed and the cursor kept", m.cursor, noticeLines(t, m))
	}
	typeKeys(m, "Gx")
	if got := noticeLines(t, m); !slices.Equal(got, []string{"✓ Dropped the fix for c/new.go:4", "  The comment is pending again."}) {
		t.Errorf("x: notice %q", got)
	}
	typeKeys(m, "gg")
	if m.cursor != 0 || noticeLines(t, m) != nil {
		t.Errorf("gg: cursor at row %d, notice %q; want the notice gone and the cursor moved", m.cursor, noticeLines(t, m))
	}
	// A fix that cannot be written is an error, and other messages stay on the status line.
	m.asker.Root = t.TempDir() // a.go is not there
	typeAll(m, "4jy")
	if got := noticeLines(t, m); len(got) != 2 || !strings.HasPrefix(got[0], "✗ Didn't apply: ") || got[1] != "  Nothing was written." || m.message != "" {
		t.Errorf("y on a missing file: notice %q, message %q", got, m.message)
	}
	typeKeys(m, "ggy")
	if m.message != "No fix on this line" || noticeLines(t, m) != nil {
		t.Errorf("y on a header: message %q, notice %q; want the warning on the status line", m.message, noticeLines(t, m))
	}
	// A drop the log cannot save is an error too: the fix is gone from the screen.
	blocked := filepath.Join(t.TempDir(), "file")
	writeFile(t, blocked, "")
	m.log = state.NewLog(filepath.Join(blocked, "log.jsonl")) // under a file, so it cannot be written
	typeKeys(m, "4jx")
	if got := noticeLines(t, m); len(got) < 2 || !strings.HasPrefix(got[0], "✗ Could not save the drop: ") ||
		!strings.HasPrefix(noticeText(t, m), "✗ Could not save the drop: ") || !strings.HasSuffix(noticeText(t, m), " The fix is dropped on screen; saving it failed, so the next dr shows it again.") ||
		commentOf(m, "one").fix != nil || m.message != "" {
		t.Errorf("x with a log that cannot be written: notice %q, message %q", got, m.message)
	}
	// A notice of fixes whose card shows is not needed: the cursor on the comment sees them.
	m = fixScreen(files, Load(t.TempDir()), &fakeAsk{err: errors.New("rate limited")}, root, fixClock())
	typeAll(m, "4jcone<enter>:submit<enter>")
	if got := noticeText(t, m); !strings.HasPrefix(got, "✓ Sent 1 fix request") {
		t.Errorf("no fix for the comment under the cursor: notice %q, want the one of :submit still", got)
	}
}

func TestFixesLogFails(t *testing.T) {
	// Fixes that come while the log cannot be written show all the same, and an error
	// notice says that saving them failed, which fixes came, and that Enter goes to them,
	// also while the : line is open. The status line stays clear.
	for _, keys := range []string{"gg", "gg:su"} {
		files := testFiles()
		f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"]}]}`}}
		m := fixScreen(files, Load(t.TempDir()), f, writeTree(t, files), fixClock())
		cmd := typeAndKeep(m, "4jcone<enter>:submit<enter>")
		typeKeys(m, keys)
		blocked := filepath.Join(t.TempDir(), "file")
		writeFile(t, blocked, "")
		m.log = state.NewLog(filepath.Join(blocked, "log.jsonl")) // under a file, so it cannot be written
		run(m, cmd)
		got := noticeText(t, m)
		if !strings.HasPrefix(got, "✗ Could not save the fixes: ") ||
			!strings.HasSuffix(got, " Fix from claude c1a0 on a.go:2: shown now; saving them failed.   ⏎ to read   esc dismiss") ||
			commentOf(m, "one").fix == nil || m.message != "" {
			t.Errorf("%q: notice %q, message %q; want the error notice with the fix that came", keys, got, m.message)
		}
		if m.cmdline.open {
			if status := statusLine(t, m); !strings.HasPrefix(status, " COMMAND   :su") {
				t.Errorf("%q: status line %q, want the : line kept", keys, status)
			}
			continue
		}
		typeKeys(m, "<enter>")
		if m.cursor != 4 {
			t.Errorf("%q, Enter: cursor at row %d, want the fix's row 4", keys, m.cursor)
		}
	}
}

func TestNoticeOverInput(t *testing.T) {
	// A notice that comes while an input is open floats over it. Esc dismisses the notice
	// alone and keeps the input with what is typed; a second Esc closes the input. Enter
	// belongs to the input: it saves the comment, runs the command or searches, and does not
	// go to the answer the notice tells of.
	for _, tc := range []struct {
		name, keys string                // typed into the input after the answer's question went
		open       func(m *model) bool   // the input is open with what keys typed
		entered    func(m *model) string // after Enter, why what Enter did is wrong, "" when right
	}{
		{
			name: "comment box", keys: "<esc>Gcfix it",
			open: func(m *model) bool { return m.drafting != nil && m.entry.text == "fix it" },
			entered: func(m *model) string {
				if c := commentOf(m, "fix it"); c == nil || m.drafting != nil || m.originBox.open || m.cursor != 26 {
					return "want the comment saved and the cursor kept"
				}
				return ""
			},
		},
		{
			name: ": line", keys: "<esc>G:und",
			open: func(m *model) bool { return m.cmdline.open && m.cmdline.input == "und" },
			entered: func(m *model) string {
				if m.cmdline.open || m.message != "No fix to undo" || m.originBox.open {
					return "want :undo run"
				}
				return ""
			},
		},
		{
			name: "search", keys: "<esc>G/line 3",
			open: func(m *model) bool { return m.search.open && m.search.input == "line 3" },
			entered: func(m *model) string {
				if m.search.open || !strings.Contains(rowName(m, m.cursor), "line 3") || m.originBox.open {
					return "want the search run"
				}
				return ""
			},
		},
	} {
		for _, keys := range []string{"<esc>", "<enter>"} {
			m := commentScreen(testFiles(), t.TempDir())
			cmd := typeAndKeep(m, "4ja<enter>")
			typeKeys(m, tc.keys)
			send(m, cmd())
			if m.notice == nil || !tc.open(m) {
				t.Fatalf("%s: notice %q, input open %t; want both", tc.name, noticeLines(t, m), tc.open(m))
			}
			typeKeys(m, keys)
			switch keys {
			case "<esc>":
				if m.notice != nil || !tc.open(m) {
					t.Errorf("%s, Esc: notice %q, input open %t; want the notice gone and the input kept", tc.name, noticeLines(t, m), tc.open(m))
				}
				if typeKeys(m, "<esc>"); m.drafting != nil || m.cmdline.open || m.search.open {
					t.Errorf("%s, a second Esc: the input is still open", tc.name)
				}
			case "<enter>":
				if why := tc.entered(m); why != "" {
					t.Errorf("%s, Enter: cursor at row %d, box open %t, message %q; %s", tc.name, m.cursor, m.originBox.open, m.message, why)
				}
			}
		}
	}
}

// bandScreen returns the screen on testFiles in a 125×24 terminal with both bands: the
// session logs of codex could not be read, and another dr holds the lock.
func bandScreen() *model {
	m := newModel(testFiles(), []agent.Problem{{Agent: "codex", Path: "rollout-1.jsonl", Bad: 1}}, Saved{Lock: state.ErrLocked}, Asker{})
	m.tick = noTick
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	return m
}

func TestBands(t *testing.T) {
	// Bands across the top of the diff tell of unreadable session logs and of the lock, and
	// a chip on the status line says read-only. They take lines of the diff: moves, scrolling
	// and clicks still find the rows.
	m := bandScreen()
	list, _ := m.layout()
	lines := strings.Split(screen(t, m), "\n")
	band := func(y int) string { return strings.TrimRight(ansi.Cut(lines[y], list+1, m.width), " ") }
	if !strings.HasPrefix(band(0), " ! Some codex records could not be read, so their lines show as unknown.") ||
		!strings.HasSuffix(band(0), "dr blame shows which") || band(1) != " ⊘ "+lockedBand {
		t.Errorf("bands %q and %q", band(0), band(1))
	}
	if status := statusLine(t, m); !strings.Contains(status, " read-only ") || strings.Contains(status, "unreadable") {
		t.Errorf("status line %q, want the read-only chip and no note on the logs", status)
	}
	if m.bandLines() != 2 || m.rowsHeight() != m.diffHeight()-2 || cursorLine(m) != 2 || !strings.Contains(lines[2], "▾ a.go") {
		t.Errorf("rows from line %d, %d high; want the header of a.go on line 2 under the bands", cursorLine(m), m.rowsHeight())
	}
	for _, keys := range []string{"G", "gg25j", "gg}}}", "G<enter>"} {
		typeKeys(m, "<esc>"+keys)
		if y := cursorLine(m); y < 2 || y >= m.diffHeight() || !strings.Contains(strings.Split(screen(t, m), "\n")[y], "▌") {
			t.Errorf("%q: cursor row on line %d, want it on the screen under the bands", keys, y)
		}
	}
	typeKeys(m, "<esc>")
	// A click on the Viewed box of the header on line y marks its file, and one on a band
	// does nothing.
	file := m.rows[m.cursor].file
	m.jumpTo(m.headRow(file))
	m.scroll()
	y := cursorLine(m)
	send(m, tea.MouseClickMsg{X: m.width - 2, Y: 1, Button: tea.MouseLeft})
	if m.files[file].viewed {
		t.Fatal("a click on the band marked a file viewed")
	}
	send(m, tea.MouseClickMsg{X: m.width - 2, Y: y, Button: tea.MouseLeft})
	if !m.files[file].viewed {
		t.Errorf("a click on the Viewed box on line %d did not mark %s viewed", y, m.files[file].path)
	}

	// Without colors the bands and the chip are in reverse video.
	m = bandScreen()
	m.theme = newTheme(nil, true, true)
	if s := m.View().Content; !strings.Contains(s, m.theme.chip(nil, nil).Render(" read-only ")) ||
		!strings.Contains(strings.Split(s, "\n")[0], "\x1b[7m") {
		t.Errorf("without colors: want the band and the chip reversed:\n%q", strings.Split(s, "\n")[0])
	}
}

func TestUnreadEdits(t *testing.T) {
	// When a file has edits dr could not read, the band names their agent, and a changed
	// line without an origin names the edits in its origin card instead of a person.
	files := testFiles()
	files[0].Unread = []string{"omp ast_edit"}
	m := newModel(files, nil, Saved{}, Asker{})
	m.tick = noTick
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	list, _ := m.layout()
	lines := strings.Split(screen(t, m), "\n")
	if band := strings.TrimRight(ansi.Cut(lines[0], list+1, m.width), " "); !strings.HasPrefix(band, " ! Some omp records could not be read") {
		t.Errorf("band %q, want one for omp", band)
	}
	typeKeys(m, "16j<enter>")
	text := cardText(m)
	if !strings.Contains(text, "UNKNOWN\nNot in any session record dr can read. dr could not read the omp ast_edit edits to this") ||
		strings.Contains(text, "A person changed it") {
		t.Errorf("origin card\n%s\nwant it to name the unread omp ast_edit edits", text)
	}
}
