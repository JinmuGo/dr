package tui

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// entryBox returns the lines of the open input box on the screen of m without colors, from
// its top border to its bottom border, each from the box's left edge, or nil when no box
// shows.
func entryBox(t *testing.T, m *model) []string {
	t.Helper()
	lines := strings.Split(screen(t, m), "\n")
	r := m.entryRow()
	if r < 0 || r < m.top {
		return nil
	}
	_, w := m.layout()
	left, top := m.width-w+m.entryLeft(w), m.lineOf(r)+1
	var box []string
	for _, l := range lines[top:min(top+m.entryHeight(w), m.diffHeight())] {
		box = append(box, ansi.Cut(l, left, m.width))
	}
	return box
}

// boxTitle matches the top border of the input box: its pill, its title, and the model.
var boxTitle = regexp.MustCompile(`^[╭┏][─━] (ASK|COMMENT|EDIT)  (.*?) [─━]+(?: (\^ .*?) [─━])?[╮┓]$`)

// shownTitle returns what the top border of box, as entryBox returns it, says: the pill and
// the title, "ASK claude c1a0 #1 · L2".
func shownTitle(box []string) string {
	if len(box) == 0 {
		return ""
	}
	s := boxTitle.FindStringSubmatch(box[0])
	if s == nil {
		return ""
	}
	return s[1] + " " + s[2]
}

// shownModel returns what the top border of box says of the model, "" when it says nothing.
func shownModel(box []string) string {
	if s := boxTitle.FindStringSubmatch(box[0]); s != nil {
		return s[3]
	}
	return ""
}

// shownNote returns what the bottom border of box says sending does.
func shownNote(box []string) string {
	return strings.Trim(box[len(box)-1], "╰╯┗┛─━ ")
}

// shownText returns the rows of text that box, as entryBox returns it, shows, without the
// borders, the mark before the first row and the spaces after them.
func shownText(box []string) []string {
	if len(box) < 2 {
		return nil
	}
	var rows []string
	for _, l := range box[1 : len(box)-1] {
		rows = append(rows, strings.TrimRight(strings.TrimRight(ansi.Cut(l, 4, ansi.StringWidth(l)), "│┃"), " "))
	}
	return rows
}

// beforeCursor returns the line of the screen of m that the terminal's cursor is on, and
// what that line shows before the cursor, without colors; "" for both when m shows no
// cursor.
func beforeCursor(t *testing.T, m *model) (line, before string) {
	t.Helper()
	c := m.View().Cursor
	if c == nil {
		return "", ""
	}
	line = strings.Split(screen(t, m), "\n")[c.Y]
	return line, ansi.Cut(line, 0, c.X)
}

func TestEntryTitles(t *testing.T) {
	// The top border has a pill of what the box does, ASK, COMMENT or EDIT, and the title:
	// whom it asks or whom the comment goes to, and the lines in the current file, or in base
	// when all are deleted. The bottom border says what sending does. The keys are on the key
	// row, not in the box.
	for _, tc := range []struct {
		name, keys  string
		title, note string
	}{
		{"a on a line", "4ja", "ASK claude c1a0 #1 · L2", "asks a read-only fork; the session is not changed"},
		{"a on lines", "2jVjja", "ASK claude c1a0 #1 · L1-2", "asks a read-only fork; the session is not changed"},
		{"a on a deleted line", "34Ga", "ASK codex 3333, which edited this file · L3", "asks a read-only fork; the session is not changed"},
		{"c on a line", "4jc", "COMMENT → claude c1a0 · L2", "saved as pending; :submit asks for its fix"},
		{"c on lines", "11jVjc", "COMMENT → omp a1f2 · L14", "saved as pending; :submit asks for its fix"},
		{"c on a comment", "4jcfix<enter>c", "EDIT → claude c1a0 · L2", "save it empty to remove it"},
	} {
		m := commentScreen(askFiles(), t.TempDir())
		typeKeys(m, tc.keys)
		box := entryBox(t, m)
		if got, note := shownTitle(box), shownNote(box); got != tc.title || note != tc.note {
			t.Errorf("%s: title %q and note %q, want %q and %q", tc.name, got, note, tc.title, tc.note)
		}
	}
}

func TestEntryFollowUp(t *testing.T) {
	// a in the origin box of a line with questions follows them up: the title says so, and
	// the bottom border that the question goes on in the fork that answered, which has seen
	// them. a on the line after that goes on in the same fork too.
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: "[chose] a", Took: 7 * time.Second}}
	m := askScreen(testFiles(), Saved{}, f)
	run(m, typeAndKeep(m, "4jawhy<enter>"))
	typeKeys(m, "a")
	box := entryBox(t, m)
	if got, note := shownTitle(box), shownNote(box); got != "ASK claude c1a0 #1 · follow-up · L2" || note != "continues the same fork; it has seen 1 earlier question" {
		t.Errorf("a in the origin box: title %q and note %q", got, note)
	}
	typeKeys(m, "<esc><esc>a")
	if note := shownNote(entryBox(t, m)); note != "continues the same fork; it has seen 1 earlier question" {
		t.Errorf("a on the line: note %q", note)
	}
}

func TestEntryLook(t *testing.T) {
	// The input box is the only box with an outline in color: violet to ask, amber to
	// comment, rounded. ❯ or ✎ comes before the text, and the cursor is a block in the box's
	// color. Nothing typed, the placeholder shows dim with its first character under the
	// cursor. Without colors, as with NO_COLOR, no color reaches the terminal, and heavy lines
	// and the reversed cursor set the box apart.
	for _, tc := range []struct {
		name string
		th   theme
	}{
		{"dark", newTheme(nil, true, false)},
		{"light", newTheme(nil, false, false)},
		{"no colors", newTheme(nil, true, true)},
	} {
		m := commentScreen(testFiles(), t.TempDir())
		m.theme = tc.th
		typeKeys(m, "4jcuse errors.Is<enter>gg12ja")
		th := &m.theme
		f := m.entryFrame()
		border, cursor, dim := th.paint(th.ask, th.surface), th.onBand(th.surface, th.ask), th.paint(th.subtle, th.surface)
		raw := m.View().Content
		for name, want := range map[string]string{
			"the top border":    border.Render(f.tl + f.h),
			"a side border":     border.Render(f.v + " "),
			"the bottom border": border.Render(f.br),
			"the pill":          th.pill(th.base, th.ask).Render(" ASK "),
			"the tag":           th.paint(m.sessionColor(session{"omp", ompSession}), th.surface).Bold(true).Render("omp a1f2 #1"),
			"the mark":          th.paint(th.ask, th.surface).Bold(true).Render("❯ "),
			"the cursor":        cursor.Render(whyDeleted[:1]),
			"the placeholder":   dim.Render(whyDeleted[1:]),
		} {
			if !strings.Contains(raw, want) {
				t.Errorf("%s: the screen has no %s %q", tc.name, name, want)
			}
		}
		top := f.tl + f.h + " ASK  omp a1f2 #1 · L15 "
		if s := screen(t, m); !strings.Contains(s, top) || !strings.Contains(s, f.bl+f.h+f.h) {
			t.Errorf("%s: the screen\n%s\nwant the input box starting %q", tc.name, s, top)
		}
		typeKeys(m, "why<left>")
		if raw := m.View().Content; !strings.Contains(raw, cursor.Render("y")) || !strings.Contains(raw, th.paint(th.strong, th.surface).Render("wh")) {
			t.Errorf("%s: the screen does not show the cursor on the y of %q", tc.name, m.entry.text)
		}
		typeKeys(m, "<esc>gg4jc")
		if raw := m.View().Content; !strings.Contains(raw, th.paint(th.pending, th.surface).Render(f.tl+f.h)) ||
			!strings.Contains(raw, th.pill(th.base, th.pending).Render(" EDIT ")) || !strings.Contains(raw, th.paint(th.pending, th.surface).Bold(true).Render("✎ ")) {
			t.Errorf("%s: the comment's box is not amber with ✎", tc.name)
		}
		if !th.plain {
			if f != roundFrame {
				t.Errorf("%s: the box's border is %+v, want rounded", tc.name, f)
			}
			continue
		}
		var out bytes.Buffer
		w := &colorprofile.Writer{Forward: &out, Profile: colorprofile.ASCII}
		if _, err := w.WriteString(m.View().Content); err != nil {
			t.Fatal(err)
		}
		if s := out.String(); strings.Contains(s, "38;") || strings.Contains(s, "48;") || !strings.Contains(s, "┏━") ||
			!strings.Contains(s, "┃") || !strings.Contains(s, "┛") || !strings.Contains(s, "\x1b[7m ") {
			t.Errorf("%s: as the terminal gets it\n%q\nwant no colors, the heavy lines and the reversed cursor", tc.name, s)
		}
	}
}

func TestEntryWrap(t *testing.T) {
	// The text wraps at the box's inner width by display width, a wide character never split,
	// and the box grows with it. The cursor shows where the text goes in: after the last
	// character, or at the start of the next row where a row wraps.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 24}) // the box is 60 cells wide, its text 53
	typeKeys(m, "4ja"+strings.Repeat("한", 30))
	box := entryBox(t, m)
	if got, want := shownText(box), []string{strings.Repeat("한", 26), strings.Repeat("한", 4)}; !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	if line, before := beforeCursor(t, m); !strings.Contains(line, "│   한한한한") || !strings.HasSuffix(before, "│   한한한한") {
		t.Errorf("cursor after %q on %q, want it after the fourth 한 of the second row", before, line)
	}
	typeKeys(m, "<left><left><left><left>")
	if line, before := beforeCursor(t, m); !strings.Contains(line, "│   한한한한") || !strings.HasSuffix(before, "│   ") {
		t.Errorf("cursor after %q on %q, want it at the start of the second row", before, line)
	}
	if raw := m.View().Content; !strings.Contains(raw, m.theme.onBand(m.theme.surface, m.theme.ask).Render("한")) {
		t.Error("the cursor's cell is not the first 한 of the second row, on a block")
	}
	typeKeys(m, "<up>")
	if line, before := beforeCursor(t, m); !strings.Contains(line, "│ ❯ "+strings.Repeat("한", 26)) || !strings.HasSuffix(before, "│ ❯ ") {
		t.Errorf("Up: cursor after %q on %q, want it at the start of the first row", before, line)
	}
}

func TestEntryRows(t *testing.T) {
	// The box grows with its text up to 10 rows, then scrolls in it to keep the cursor's row
	// in view. On a short screen it shows fewer.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	var keys []string
	for i := range 15 {
		keys = append(keys, string(rune('a'+i)))
	}
	typeKeys(m, "4ja"+strings.Join(keys, "<alt+enter>"))
	for _, step := range []struct {
		keys, cursor string
		rows         []string
	}{
		{"", "│   o", keys[5:]},
		{strings.Repeat("<up>", 12), "│   c", keys[2:12]},
		{"<down>", "│   d", keys[2:12]},
	} {
		typeKeys(m, step.keys)
		box := entryBox(t, m)
		if _, before := beforeCursor(t, m); !slices.Equal(shownText(box), step.rows) || len(box) != 12 || !strings.HasSuffix(before, step.cursor) {
			t.Errorf("after %q: the box\n%s\nwith %q before the cursor; want the rows %q", step.keys, strings.Join(box, "\n"), before, step.rows)
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 10}) // 9 lines of the diff: the header on top, the row, the borders and 5 rows
	if box := entryBox(t, m); len(box) != 7 || !slices.Contains(shownText(box), "d") {
		t.Errorf("on a short screen: the box\n%s\nwant 5 rows with the cursor's", strings.Join(box, "\n"))
	}
}

func TestEntrySplit(t *testing.T) {
	// In the split view the box hangs from the code column of the left side, under the row
	// of the lines. A resize that leaves the split view keeps it under the same line.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "s3jawhy") // -line 2 and +Changed 2
	lines := strings.Split(screen(t, m), "\n")
	y := cursorLine(m)
	if !m.splitShown || !strings.Contains(lines[y], "Changed 2") || !strings.Contains(lines[y+1], "╭─ ASK  claude c1a0 #1 · L2 ") ||
		!strings.Contains(lines[y+2], "│ ❯ why") || !strings.Contains(lines[y+3], "╰─") || !strings.Contains(lines[y+4], "line 3") {
		t.Errorf("split view %t: want the row, the box with the text and then line 3:\n%s", m.splitShown, strings.Join(lines, "\n"))
	}
	if _, before := beforeCursor(t, m); !strings.HasSuffix(before, "│ ❯ why") {
		t.Errorf("cursor after %q, want it after why", before)
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	lines = strings.Split(screen(t, m), "\n")
	if row := lines[cursorLine(m)]; m.splitShown || !strings.Contains(row, "Changed 2") || !strings.Contains(lines[cursorLine(m)+1], "╭─ ASK ") {
		t.Errorf("after leaving the split view: want the box under +Changed 2:\n%s", strings.Join(lines, "\n"))
	}
	m = commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "s3jc")
	if title := shownTitle(entryBox(t, m)); !m.splitShown || title != "COMMENT → claude c1a0 · L2" {
		t.Errorf("c in the split view: the box titled %q", title)
	}
}

func TestEntryNarrow(t *testing.T) {
	// A narrow box leaves out the model first, then cuts the title with …, and at last shows
	// its border alone; the note on its bottom border goes when it does not fit. Its text
	// wraps to the room left, a character a row at the least. screen checks every line is as
	// wide as the screen.
	for _, tc := range []struct {
		width       int
		top, bottom string
		rows        []string
	}{
		{125, "╭─ ASK  claude c1a0 #1 · L2 ───", "─ asks a read-only fork; the session is not changed ─╯", []string{"한글 ab"}},
		{70, "╭─ ASK  claude c1a0 #1 · L2 ───", "─ asks a read-only fork; the session is not changed ─╯", []string{"한글 ab"}},
		{40, "╭─ ASK  claude c1a0 #1 · … ─╮", "╰───────────────────────────╯", []string{"한글 ab"}},
		{12, "╭──────╮", "╰──────╯", []string{"한", "글", "", "a", "b"}}, // the space is a row of its own
	} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
		m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
		typeKeys(m, "4ja한글 ab")
		box := entryBox(t, m)
		if len(box) < 3 || !strings.HasPrefix(box[0], tc.top) || !strings.HasSuffix(box[len(box)-1], tc.bottom) || !slices.Equal(shownText(box), tc.rows) {
			t.Errorf("width %d: the box\n%s\nwant it to start with %q, end with %q and show %q", tc.width, strings.Join(box, "\n"), tc.top, tc.bottom, tc.rows)
		}
	}
	// A title is cut before a wide character that does not fit whole.
	if got := cutChunks([]chunk{{text: "ab"}, {text: "한글"}}, 3); got[0].text != "ab" || got[1].text != "" {
		t.Errorf("cut to 3 cells: %+v, want ab alone", got)
	}
}

func TestEntryScrollsIntoView(t *testing.T) {
	// The screen scrolls to show the whole box with its row, as it grows too.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 12})
	typeKeys(m, "Ga")
	for _, keys := range []string{"", "a<alt+enter>b<alt+enter>c<alt+enter>d<alt+enter>e"} {
		typeKeys(m, keys)
		lines := strings.Split(screen(t, m), "\n")
		top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╭─ ASK ") })
		bottom := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "╰─") })
		if top < 1 || !strings.Contains(lines[top-1], "var x = 1") || bottom < 0 || bottom >= m.diffHeight() {
			t.Errorf("after %q: want the last row and the whole box under it:\n%s", keys, strings.Join(lines, "\n"))
		}
	}
}

func TestEntryKeys(t *testing.T) {
	// Every key goes to the box: Shift+Enter, Alt+Enter and Ctrl+J start a new line, and
	// keys of the diff type their text. The mouse does nothing. Enter sends the question as
	// typed, line breaks too; the log keeps them, and the thread shows each line.
	dir := t.TempDir()
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{Dir: dir}, f)
	typeKeys(m, "4jaa<shift+enter>b<alt+enter>c<ctrl+j>dj?q")
	send(m, tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseLeft}, tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelDown})
	if box := entryBox(t, m); m.cursor != 4 || m.help || !slices.Equal(shownText(box), []string{"a", "b", "c", "dj?q"}) {
		t.Errorf("cursor at row %d, help open %t, the box\n%s\nwant row 4 and four lines", m.cursor, m.help, strings.Join(box, "\n"))
	}
	typeKeys(m, "<enter>")
	want := review.Prompt(review.Author, "a.go", []origin.Line{{Text: "Changed 2", New: 2}}, "a\nb\nc\ndj?q")
	if got := f.asked(); len(got) != 1 || got[0].Prompt != want {
		t.Errorf("asked %+v, want the four lines", got)
	}
	if qs := Load(dir).Log.Questions; len(qs) != 1 || qs[0].Text != "a\nb\nc\ndj?q" {
		t.Errorf("question records %+v, want the text with its line breaks", qs)
	}
	if s := screen(t, m); !strings.Contains(s, "▎ a ") || !strings.Contains(s, "▎ dj?q") || !m.originBox.open {
		t.Errorf("the thread does not show each line:\n%s", s)
	}
}

func TestEntryCtrlC(t *testing.T) {
	// Ctrl+C in the box cancels it as Esc does: nothing is sent or saved, and a from the
	// origin box brings the origin box back. A second Ctrl+C quits.
	for _, tc := range []struct {
		name, keys string
		origin     bool // the origin box opens again
	}{
		{name: "a", keys: "4jawhy"},
		{name: "a from the origin box", keys: "4j<enter>awhy", origin: true},
		{name: "c", keys: "4jcfix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			f := &fakeAsk{answer: answer}
			m := askScreen(testFiles(), Load(dir), f)
			typeKeys(m, tc.keys)
			if !m.typing() {
				t.Fatalf("%q: want the box open", tc.keys)
			}
			ctrlC := keyPresses("<ctrl+c>")[0]
			if _, cmd := m.Update(ctrlC); cmd != nil {
				if _, ok := cmd().(tea.QuitMsg); ok {
					t.Fatal("Ctrl+C in the box: got tea.QuitMsg, want the box closed")
				}
			}
			if m.typing() || m.originBox.open != tc.origin || len(f.asked()) != 0 || entryBox(t, m) != nil {
				t.Errorf("Ctrl+C: box open %t, origin box open %t, %d questions; want the box closed, origin box open %t, none asked",
					m.typing(), m.originBox.open, len(f.asked()), tc.origin)
			}
			if l := Load(dir).Log; len(l.Questions) != 0 || len(l.Comments) != 0 {
				t.Errorf("Ctrl+C: records %+v, want none", l)
			}
			_, cmd := m.Update(ctrlC)
			if cmd == nil {
				t.Fatal("second Ctrl+C: no command, want quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("second Ctrl+C: got %T, want tea.QuitMsg", cmd())
			}
		})
	}
}

func TestEntryPaste(t *testing.T) {
	// A paste goes into the open box at the cursor, made clean; with no box open, it goes
	// nowhere.
	m := commentScreen(testFiles(), t.TempDir())
	send(m, tea.PasteMsg{Content: "lost"})
	typeKeys(m, "4jc<<left>")
	send(m, tea.PasteMsg{Content: "use\r\nerrors.Is\tnow"})
	if want := "use\nerrors.Is    now<"; m.entry.text != want {
		t.Errorf("text %q after the paste, want %q", m.entry.text, want)
	}
	if box := entryBox(t, m); !slices.Equal(shownText(box), []string{"use", "errors.Is    now<"}) {
		t.Errorf("the box\n%s\nwant both lines", strings.Join(box, "\n"))
	}
}

func TestEntryComment(t *testing.T) {
	// A comment keeps its line breaks: in the log, in its box, which shows each line, and in
	// the input box that edits it, which holds them with the cursor at the end.
	dir := t.TempDir()
	m := commentScreen(testFiles(), dir)
	typeKeys(m, "4jcone<alt+enter>two<enter>")
	if records := commentRecords(t, dir); len(records) != 1 || records[0].Text != "one\ntwo" {
		t.Fatalf("records %+v, want the comment with its line break", records)
	}
	lines := strings.Split(screen(t, m), "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "▎ one") })
	if at < 0 || !strings.Contains(lines[at+1], "▎ two") {
		t.Errorf("the comment's box does not show each line:\n%s", strings.Join(lines, "\n"))
	}
	typeKeys(m, "c")
	if _, before := beforeCursor(t, m); !slices.Equal(shownText(entryBox(t, m)), []string{"one", "two"}) || !strings.HasSuffix(before, "│   two") {
		t.Errorf("c on the comment: the box\n%s\nwith %q before the cursor", strings.Join(entryBox(t, m), "\n"), before)
	}
	typeKeys(m, "<up><end>!<enter>")
	if !slices.Equal(texts(m), []string{"one!\ntwo"}) {
		t.Errorf("comments %q, want the first line edited", texts(m))
	}
}

func TestEntryAboveTallComment(t *testing.T) {
	// The box comes right under its row, before the row's comments: a comment taller than
	// the screen does not push it off. The cursor sits on its text row, and typed text shows.
	m := commentScreen(testFiles(), t.TempDir())
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 12})
	typeKeys(m, "4jc1<alt+enter>2<alt+enter>3<alt+enter>4<alt+enter>5<alt+enter>6<alt+enter>7<alt+enter>8<alt+enter>9<alt+enter>10<enter>awhy")
	lines := strings.Split(screen(t, m), "\n")
	y := cursorLine(m)
	if y < 0 || y+3 >= m.diffHeight() || !strings.Contains(lines[y], "Changed 2") || !strings.Contains(lines[y+1], "╭─ ASK ") ||
		!strings.Contains(lines[y+2], "│ ❯ why") || !strings.Contains(lines[y+3], "╰─") {
		t.Fatalf("want the row, then the whole box with the text:\n%s", strings.Join(lines, "\n"))
	}
	if c := m.View().Cursor; c == nil || c.Y != y+2 {
		t.Errorf("cursor %+v, want it on line %d, the box's text row", c, y+2)
	}
	if _, before := beforeCursor(t, m); !strings.HasSuffix(before, "│ ❯ why") {
		t.Errorf("cursor after %q, want it after why", before)
	}
}

func TestEntryGrowsAboveComment(t *testing.T) {
	// With a comment shorter than the screen under the row, the box grows to its row cap
	// and the cursor's row stays on the screen.
	m := commentScreen(testFiles(), t.TempDir())
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 12})
	typeKeys(m, "4jcone<alt+enter>two<alt+enter>three<enter>a")
	for i := range 15 {
		if i > 0 {
			typeKeys(m, "<alt+enter>")
		}
		typeKeys(m, string(rune('a'+i)))
		c := m.View().Cursor
		if c == nil || c.Y >= m.diffHeight() {
			t.Fatalf("after %d lines: cursor %+v, want it on the screen:\n%s", i+1, c, screen(t, m))
		}
		mark := "│   "
		if i == 0 {
			mark = "│ ❯ "
		}
		if _, before := beforeCursor(t, m); !strings.HasSuffix(before, mark+string(rune('a'+i))) {
			t.Fatalf("after %d lines: cursor after %q", i+1, before)
		}
	}
	if box := entryBox(t, m); len(box) != m.entryRows()+2 || !strings.HasSuffix(box[len(box)-1], "╯") {
		t.Errorf("the box\n%s\nwant %d rows and its bottom border", strings.Join(box, "\n"), m.entryRows())
	}
}

func TestEntryBeforeOtherComment(t *testing.T) {
	// c on a split row whose line ends two comments edits the first: the box comes right
	// under the row, the other comment's box follows it, and the edited one is hidden.
	lines := parseDiff("x", "-oldA", "-oldB", "+newA", "+newB", "y")
	for i := 1; i <= 4; i++ {
		lines[i].Edit = claude1
	}
	m := commentScreen([]review.File{{Path: "f.go", Status: 'M', Lines: lines}}, t.TempDir())
	typeKeys(m, "gg6jcalpha<enter>s")
	if len(m.comments) != 1 || !m.splitShown {
		t.Fatalf("%d comments, split view %t; want one comment in the split view", len(m.comments), m.splitShown)
	}
	other := *m.comments[0] // a second comment on the same line, as saved records can hold
	other.Text, other.n = "beta", 1
	m.comments = append(m.comments, &other)
	typeKeys(m, "gg4jc")
	if m.drafting == nil || m.drafting.editing != m.comments[0] {
		t.Fatal("want the first comment edited")
	}
	s := strings.Split(screen(t, m), "\n")
	y := cursorLine(m)
	bottom := slices.IndexFunc(s, func(l string) bool { return strings.Contains(l, "╰─") })
	at := slices.IndexFunc(s, func(l string) bool { return strings.Contains(l, "▎ beta") })
	if !strings.Contains(s[y], "newB") || !strings.Contains(s[y+1], "╭─ EDIT ") || bottom <= y+1 || at <= bottom ||
		slices.ContainsFunc(s, func(l string) bool { return strings.Contains(l, "▎ alpha") }) {
		t.Errorf("want the row, the box, then beta's box and no box for alpha:\n%s", strings.Join(s, "\n"))
	}
	if c := m.View().Cursor; c == nil || c.Y != y+2 {
		t.Errorf("cursor %+v, want it on line %d, the box's text row", c, y+2)
	}
}
