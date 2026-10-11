package tui

import (
	"fmt"
	"image/color"
	"path"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

const (
	claudeSession = "7a1c3e9b-1111-4000-8000-00000000c1a0"
	ompParent     = "01a1287a-2222-4000-8000-00000000a1f1"
	ompSession    = "01a1287a-2222-4000-8000-00000000a1f2"
)

var (
	start   = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	claude1 = &agent.Edit{
		Agent: "claude", Session: claudeSession, Turn: 1, Time: start.Add(time.Minute), Cwd: "/work", Model: "claude-opus-5-5",
		Prompt: "rate limiter를 짜줘\n셸 명령은 실행하지 마", Note: "토큰 버킷으로 바꿀게요", Reply: "다 짰어요",
	}
	claude2 = &agent.Edit{
		Agent: "claude", Session: claudeSession, Turn: 2, Time: start.Add(2 * time.Minute), Cwd: "/work", Model: "claude-opus-5-5",
		Prompt: "새 파일로 나눠줘", Reply: "나눴어요",
	}
	subagent = &agent.Edit{
		Agent: "omp", Session: ompSession, Parent: ompParent, Turn: 1, Time: start.Add(3 * time.Minute), Cwd: "/work",
		Log:    "/home/.omp/agent/sessions/-work/2026-10-09T12-00-00-000Z_" + ompParent + "/0-Task.jsonl",
		Prompt: "line 15를 지워줘", Reply: "지웠어요",
	}
)

// testFiles returns three changed files. In a.go claude #1 rewrote line 2, an OMP subagent
// deleted line 15, and a line after line 18 has no origin. b.bin is binary. claude #2
// wrote the new c/new.go. Their 27 rows are:
//
//	0      a.go
//	1      @@ -1,5 +1,5 @@
//	2      line 1
//	3-4    -line 2, +Changed 2
//	5-7    line 3 to 5
//	8      @@ -12,9 +12,9 @@
//	9-11   line 12 to 14
//	12     -line 15
//	13-15  line 16 to 18
//	16     +new after 18
//	17-18  line 19 to 20
//	19     b.bin
//	20     the binary note
//	21     c/new.go
//	22     @@ -0,0 +1,4 @@
//	23-26  the lines of c/new.go, 25 a long line of wide characters
func testFiles() []review.File {
	var base, current strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&base, "line %d\n", i)
		switch i {
		case 2:
			current.WriteString("Changed 2\n")
		case 15:
		case 18:
			current.WriteString("line 18\nnew after 18\n")
		default:
			fmt.Fprintf(&current, "line %d\n", i)
		}
	}
	a := origin.Blame(base.String(), current.String(), nil)
	edits := map[string]*agent.Edit{"line 2": claude1, "Changed 2": claude1, "line 15": subagent}
	for i, l := range a {
		if l.Old == 0 || l.New == 0 {
			a[i].Edit = edits[l.Text]
		}
	}
	c := origin.Blame("", "package c\n\n// "+strings.Repeat("한글로 길게 쓴 주석이에요. ", 20)+"\n\tvar x = 1\n", nil)
	for i := range c {
		c[i].Edit = claude2
	}
	return []review.File{
		{Path: "a.go", Status: 'M', Lines: a},
		{Path: "b.bin", Status: 'A', NoDiff: review.Binary},
		{Path: "c/new.go", Status: 'A', Lines: c},
	}
}

var namedKeys = map[string]tea.KeyPressMsg{
	"<esc>":         {Code: tea.KeyEscape},
	"<enter>":       {Code: tea.KeyEnter},
	"<shift+enter>": {Code: tea.KeyEnter, Mod: tea.ModShift},
	"<alt+enter>":   {Code: tea.KeyEnter, Mod: tea.ModAlt},
	"<tab>":         {Code: tea.KeyTab},
	"<bs>":          {Code: tea.KeyBackspace},
	"<delete>":      {Code: tea.KeyDelete},
	"<down>":        {Code: tea.KeyDown},
	"<up>":          {Code: tea.KeyUp},
	"<left>":        {Code: tea.KeyLeft},
	"<right>":       {Code: tea.KeyRight},
	"<home>":        {Code: tea.KeyHome},
	"<end>":         {Code: tea.KeyEnd},
	"<alt+left>":    {Code: tea.KeyLeft, Mod: tea.ModAlt},
	"<alt+right>":   {Code: tea.KeyRight, Mod: tea.ModAlt},
	"<ctrl+left>":   {Code: tea.KeyLeft, Mod: tea.ModCtrl},
	"<ctrl+right>":  {Code: tea.KeyRight, Mod: tea.ModCtrl},
	"<alt+b>":       {Code: 'b', Mod: tea.ModAlt},
	"<alt+f>":       {Code: 'f', Mod: tea.ModAlt},
	"<ctrl+a>":      {Code: 'a', Mod: tea.ModCtrl},
	"<ctrl+c>":      {Code: 'c', Mod: tea.ModCtrl},
	"<ctrl+e>":      {Code: 'e', Mod: tea.ModCtrl},
	"<ctrl+j>":      {Code: 'j', Mod: tea.ModCtrl},
	"<ctrl+n>":      {Code: 'n', Mod: tea.ModCtrl},
	"<ctrl+p>":      {Code: 'p', Mod: tea.ModCtrl},
	"<ctrl+u>":      {Code: 'u', Mod: tea.ModCtrl},
	"<ctrl+w>":      {Code: 'w', Mod: tea.ModCtrl},
}

// keyPresses returns the key presses that type keys. In keys, names in angle brackets such
// as <esc> are the keys of namedKeys.
func keyPresses(keys string) []tea.KeyPressMsg {
	var msgs []tea.KeyPressMsg
	for keys != "" {
		if end := strings.Index(keys, ">"); keys[0] == '<' && end > 0 {
			if k, ok := namedKeys[keys[:end+1]]; ok {
				msgs = append(msgs, k)
				keys = keys[end+1:]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(keys)
		msgs = append(msgs, tea.KeyPressMsg{Code: r, Text: string(r)})
		keys = keys[size:]
	}
	return msgs
}

// open starts the screen on testFiles in a 125×24 terminal, wide enough for the split view
// beside the file tree, and types keys into it, written as for keyPresses.
func open(t *testing.T, keys string) *teatest.TestModel {
	t.Helper()
	return openFiles(t, testFiles(), keys)
}

// openFiles starts the screen on files like open.
func openFiles(t *testing.T, files []review.File, keys string) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, newModel(files, nil, Saved{}, Asker{}), teatest.WithInitialTermSize(125, 24))
	for _, k := range keyPresses(keys) {
		tm.Send(k)
	}
	return tm
}

// end sends msgs to the screen, quits it and returns the model after them.
func end(t *testing.T, tm *teatest.TestModel, msgs ...tea.Msg) *model {
	t.Helper()
	for _, msg := range msgs {
		tm.Send(msg)
	}
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	return tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*model)
}

// press types keys into the screen and returns the model after them.
func press(t *testing.T, keys string) *model {
	t.Helper()
	return end(t, open(t, keys))
}

// cursorLine returns the line of the screen that shows the cursor row: a line for each band
// and for each row from the top, one more for the blank line above each file header but the
// top row, and one for the header that stays on top while the top row is inside a file.
func cursorLine(m *model) int {
	y := m.bandLines() + m.cursor - m.top
	if m.sticky() {
		y++
	}
	for i := m.top + 1; i <= m.cursor; i++ {
		if m.rows[i].kind == fileRow {
			y++
		}
	}
	return y
}

func TestFileGap(t *testing.T) {
	// A blank line comes above the header of each file but the first, as GitHub leaves a gap
	// between files. The header shows a fold mark and the path. The blank line is only drawn:
	// j from the last line of a.go goes to the header of b.bin, and k comes back.
	for _, tc := range []struct {
		keys        string
		top, cursor int
	}{
		{keys: "18j", top: 0, cursor: 18},
		{keys: "18jj", top: 0, cursor: 19},
		{keys: "18jjk", top: 0, cursor: 18},
		{keys: "s17j", top: 0, cursor: 17}, // the last line of a.go in the split view
	} {
		m := newModel(testFiles(), nil, Saved{}, Asker{})
		m.Update(tea.WindowSizeMsg{Width: 200, Height: 30})
		for _, k := range keyPresses(tc.keys) {
			m.Update(k)
		}
		if m.top != tc.top || m.cursor != tc.cursor {
			t.Errorf("%q: cursor at row %d with row %d at the top, want row %d with row %d at the top", tc.keys, m.cursor, m.top, tc.cursor, tc.top)
			continue
		}
		list, _ := m.layout()
		var diff []string
		for _, l := range strings.Split(screen(t, m), "\n") {
			diff = append(diff, strings.TrimRight(ansi.Cut(l, list+1, m.width), " "))
		}
		at := func(prefix string) int {
			return slices.IndexFunc(diff, func(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " ▌"), prefix) })
		}
		first, second, third := at("▾ a.go"), at("▾ b.bin"), at("▾ c/new.go")
		if first != 0 || second < 2 || diff[second-1] != "" || !strings.Contains(diff[second-2], "line 20") ||
			third < 2 || diff[third-1] != "" || !strings.Contains(diff[third-2], review.Binary) {
			t.Errorf("%q: want a.go at the top and one blank line above b.bin and c/new.go:\n%s", tc.keys, strings.Join(diff, "\n"))
		}
	}

	// A header at the top row has no blank line above it.
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	for _, k := range keyPresses("Gh") {
		m.Update(k)
	}
	list, _ := m.layout()
	if top := strings.TrimLeft(ansi.Cut(strings.Split(screen(t, m), "\n")[0], list+1, m.width), " ▌"); m.top != 21 || !strings.HasPrefix(top, "▾ c/new.go") {
		t.Errorf("Gh: want the header of c/new.go, row 21, on the first line; got row %d at the top and %q", m.top, top)
	}
}

func TestRows(t *testing.T) {
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 24}) // a diff wide enough for the summary of a label
	if len(m.rows) != 27 {
		t.Fatalf("got %d rows, want 27", len(m.rows))
	}
	for _, tc := range []struct {
		row  int
		kind kind
		text string
	}{
		{0, fileRow, "▾ a.go ●●┆ +2 -2 " + strings.Repeat(statSquare, 5) + " ☐ Viewed"},
		{1, hunkRow, "@@ -1,5 +1,5 @@"},
		{3, lineRow, "2    ▎- line 2    ● claude c1a0 #1  rate limiter를 짜줘"}, // the first line of a run has its label
		{4, lineRow, "2 ▎+ Changed 2"},
		{8, hunkRow, "@@ -12,9 +12,9 @@"}, // two changes whose context lines touch
		{19, fileRow, "▾ b.bin new ☐ Viewed"},
		{20, noteRow, review.Binary},
		{22, hunkRow, "@@ -0,0 +1,4 @@"},
		{26, lineRow, "4 ▎+     var x = 1"}, // a tab is four spaces
	} {
		r := m.rows[tc.row]
		text := strings.Trim(ansi.Strip(m.drawRow(tc.row, 80)), " ▌")
		if r.kind == fileRow {
			text = strings.Join(strings.Fields(text), " ") // the path at the left edge, the rest at the right
		}
		if r.kind != tc.kind || text != tc.text {
			t.Errorf("row %d: got kind %d %q, want kind %d %q", tc.row, r.kind, text, tc.kind, tc.text)
		}
	}
}

func TestTree(t *testing.T) {
	for _, tc := range []struct {
		paths []string
		want  []string // each row as its depth in spaces, then its name, with a slash after a directory
	}{
		{
			paths: []string{"a.go", "b.bin", "c/new.go"},
			want:  []string{"a.go", "b.bin", "c/", " new.go"},
		},
		{
			// a directory that holds nothing but one directory shares a row with it
			paths: []string{"go.mod", "src/main/java/A.java", "src/main/java/B.java", "src/test/T.java", "web/x/y/z.ts"},
			want: []string{
				"go.mod", "src/", " main/java/", "  A.java", "  B.java", " test/", "  T.java", "web/x/y/", " z.ts",
			},
		},
		{
			// files go in the order of the diff, back up out of a directory and into it again
			paths: []string{"agent/agent.go", "agent/testdata/x.jsonl", "agent/zeta.go", "main.go"},
			want:  []string{"agent/", " agent.go", " testdata/", "  x.jsonl", " zeta.go", "main.go"},
		},
	} {
		files := make([]file, len(tc.paths))
		for i, p := range tc.paths {
			files[i] = file{path: p}
		}
		var got []string
		for _, r := range tree(files) {
			row := strings.Repeat(" ", r.depth) + r.name
			if r.file < 0 {
				row += "/"
			} else if r.name != path.Base(tc.paths[r.file]) {
				t.Errorf("%q: row %q is file %d, %q", tc.paths, row, r.file, tc.paths[r.file])
			}
			got = append(got, row)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: got rows %q, want %q", tc.paths, got, tc.want)
		}
	}
}

func TestMarkPair(t *testing.T) {
	for _, tc := range []struct {
		deleted, added string
		want           [2][]string // the marked parts of each line
	}{
		{"return nil, err", "return err", [2][]string{{"nil, "}, nil}},
		{"x := compute(a, b)", "x := compute(a, c)", [2][]string{{"b"}, {"c"}}},
		{"base, err := git.Base(root)", "paths, err := repo.Changed()", [2][]string{}}, // too unlike to mark
		{"", "added", [2][]string{}},
	} {
		a, b := markPair(toPieces(nil, tc.deleted), toPieces(nil, tc.added))
		if got := [2][]string{changed(a), changed(b)}; !slices.Equal(got[0], tc.want[0]) || !slices.Equal(got[1], tc.want[1]) {
			t.Errorf("%q, %q: marked %q, want %q", tc.deleted, tc.added, got, tc.want)
		}
		if join(a) != tc.deleted || join(b) != tc.added {
			t.Errorf("%q, %q: pieces make %q, %q", tc.deleted, tc.added, join(a), join(b))
		}
	}
}

// changed returns the text of each marked piece.
func changed(pieces []piece) []string {
	var out []string
	for _, p := range pieces {
		if p.changed {
			out = append(out, p.text)
		}
	}
	return out
}

func TestMove(t *testing.T) {
	for _, tc := range []struct {
		keys string
		want int
	}{
		{"j", 1},
		{"<down><down><up>", 1},
		{"5j", 5},
		{"5jk", 4},
		{"3k", 0},
		{"100j", 26},
		{"G", 26},
		{"Ggg", 0},
		{"12G", 11},
		{"12gg", 11},
		{"d", 11}, // half of the 23 rows above the status line
		{"ddu", 11},
		{"Gu", 15},
		{"5<esc>j", 1},
		{"5g<esc>G", 26},
		{"?qj", 1}, // any key closes the help, q too
	} {
		m := press(t, tc.keys)
		if m.cursor != tc.want {
			t.Errorf("%q: cursor at row %d, want %d", tc.keys, m.cursor, tc.want)
		}
		if m.cursor < m.top || cursorLine(m) >= m.diffHeight() {
			t.Errorf("%q: cursor at row %d, line %d of the screen from row %d, is off the screen", tc.keys, m.cursor, cursorLine(m), m.top)
		}
	}
}

func TestHalfPageJK(t *testing.T) {
	// J and K move the cursor and the screen as d and u do, without a count as they have
	// none, and scroll the open origin box half of it as they do.
	for _, tc := range []struct{ keys, same string }{
		{"J", "d"},
		{"JJK", "ddu"},
		{"GK", "Gu"},
		{"5J", "5d"},
	} {
		got, want := press(t, tc.keys), press(t, tc.same)
		if got.cursor != want.cursor || got.top != want.top {
			t.Errorf("%q: cursor at row %d, screen from row %d; want row %d from row %d as %q", tc.keys, got.cursor, got.top, want.cursor, want.top, tc.same)
		}
	}
	box := func(keys string) *model {
		m := newModel(longFiles(), nil, Saved{}, Asker{})
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		typeKeys(m, "G<enter>"+keys)
		return m
	}
	for _, tc := range []struct{ keys, same string }{
		{"J", "d"},
		{"JJK", "ddu"},
	} {
		got, want := box(tc.keys), box(tc.same)
		if !got.reading || got.readTop != want.readTop || got.readTop == 0 {
			t.Errorf("%q in the origin box: open %t, text from line %d; want it open from line %d as %q", tc.keys, got.reading, got.readTop, want.readTop, tc.same)
		}
	}
}

func TestJump(t *testing.T) {
	for _, tc := range []struct {
		keys string
		want int
	}{
		{"}", 3},
		{"}}", 12},
		{"3}", 16},
		{"4}", 23},
		{"9}", 23}, // stays at the last change
		{"G{", 23},
		{"4j{", 3}, // from inside a change to its start
		{"G2{", 16},
		{"l", 19},
		{"3l", 21},
		{"<right>", 19},
		{"Gh", 21},
		{"G2h", 19},
		{"G<left>", 21},
		{"5jh", 0},
		{"]f", 0}, // l and h took the place of ]f and [f
	} {
		if m := press(t, tc.keys); m.cursor != tc.want {
			t.Errorf("%q: cursor at row %d, want %d", tc.keys, m.cursor, tc.want)
		}
	}
}

func TestSearch(t *testing.T) {
	for _, tc := range []struct {
		keys    string
		want    int
		message string
	}{
		{"/changed<enter>", 4, ""}, // lower case ignores case
		{"/Changed<enter>", 4, ""},
		{"/CHANGED<enter>", 0, "Not found: CHANGED"},
		{"/line 2<enter>", 3, ""},
		{"/line 2<enter>n", 18, ""},
		{"/line 2<enter>nn", 3, ""}, // around the end
		{"/line 2<enter>N", 18, ""}, // around the start
		{"/line 2<enter>2n", 3, ""},
		{"/new.go<enter>", 21, ""}, // a file path
		{"/xyz<bs><bs><bs>changed<enter>", 4, ""},
		{"/changed<esc>n", 0, ""},
		{"/changed<enter>G/<enter>", 4, ""}, // an empty query searches again
		{"/q<esc>j", 1, ""},                 // q in the input does not quit
	} {
		m := press(t, tc.keys)
		if m.cursor != tc.want || m.message != tc.message {
			t.Errorf("%q: cursor at row %d with message %q, want row %d with %q", tc.keys, m.cursor, m.message, tc.want, tc.message)
		}
	}
}

func TestScroll(t *testing.T) {
	// 7 lines fit above the status line on a screen too short for the key row, so the
	// middle of the screen is 3 lines below its top. The headers of b.bin and c/new.go, rows
	// 19 and 21, take one line more for the blank line above them unless they are the top
	// row, and a top row inside a file one more for its header, which stays on top.
	for _, tc := range []struct {
		keys        string
		cursor, top int
	}{
		{"10j", 10, 5}, // a move scrolls as little as it takes
		{"}", 3, 0},    // a jump on the screen does not scroll
		{"}}", 12, 10},
		{"l", 19, 18},
		{"12G", 11, 9},
		{"/new after<enter>", 16, 14},
		{"/line 2<enter>N", 18, 16}, // around the start
		{"G", 26, 21},               // the last row cannot be in the middle, and row 20 would push it off
		{"Ggg", 0, 0},
		{"Gh", 21, 21},
		{"G2h", 19, 18},
	} {
		m := newModel(testFiles(), nil, Saved{}, Asker{})
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
		for _, k := range keyPresses(tc.keys) {
			m.Update(k)
		}
		if m.cursor != tc.cursor || m.top != tc.top {
			t.Errorf("%q: cursor at row %d with row %d at the top, want row %d with row %d at the top",
				tc.keys, m.cursor, m.top, tc.cursor, tc.top)
		}
	}
}

func TestQuit(t *testing.T) {
	// In the open origin box q closes it, so it takes a second q to quit.
	for _, keys := range []string{"q", "<ctrl+c>", "/abc<ctrl+c>", "?<ctrl+c>", "<enter><ctrl+c>", "<enter>qq"} {
		// WaitFinished fails the test when the screen is still open after the timeout.
		open(t, keys).WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	}
}

func TestOriginText(t *testing.T) {
	for _, tc := range []struct {
		name, keys string
		want, not  []string // want in the order the card shows them, its header first
	}{
		{
			name: "agent line", keys: "4j",
			want: []string{
				"● claude c1a0 #1  a.go:2", "REQUEST · #1\nrate limiter를 짜줘\n셸 명령은 실행하지 마",
				"NOTE\n토큰 버킷으로 바꿀게요", "REPLY\n다 짰어요", "session " + claudeSession,
			},
			not: []string{"This edit deleted the line"},
		},
		{
			name: "deleted line", keys: "12j",
			want: []string{"● omp a1f2 #1", "This edit deleted the line", "line 15를 지워줘", "지웠어요", "session " + ompSession, "↳ subagent of omp a1f1"},
			not:  []string{"NOTE"}, // the edit left no note
		},
		{
			name: "line without origin", keys: "16j",
			want: []string{"┆ unknown  a.go:18", "UNKNOWN\nNot in any session record."},
		},
		{
			name: "unchanged line", keys: "2j",
			want: []string{"a.go:1", "Unchanged line"},
		},
		{
			name: "file header",
			want: []string{
				"▾ a.go  2 requests · 2 sessions", "REQUESTS THAT CHANGED THIS FILE",
				"● claude c1a0 #1  rate limiter를 짜줘", "● omp a1f2 #1     line 15를 지워줘", "┆ unknown         " + noRecord,
			},
		},
		{
			name: "binary file", keys: "l",
			want: []string{review.Binary, "No origin lookup for this file"},
		},
	} {
		text := cardText(press(t, tc.keys))
		at := 0
		for _, s := range tc.want {
			i := strings.Index(text[at:], s)
			if i < 0 {
				t.Errorf("%s: origin\n%s\nhas no %q after its first %d bytes", tc.name, text, s, at)
				break
			}
			at += i + len(s)
		}
		for _, s := range tc.not {
			if strings.Contains(text, s) {
				t.Errorf("%s: origin\n%s\nhas %q", tc.name, text, s)
			}
		}
	}
}

// screen returns the text m draws after checking that it fills a terminal of m.width by
// m.height cells.
func screen(t *testing.T, m *model) string {
	t.Helper()
	s := m.View().Content
	lines := strings.Split(s, "\n")
	if len(lines) != m.height {
		t.Errorf("width %d: got %d lines, want %d", m.width, len(lines), m.height)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != m.width {
			t.Errorf("width %d: line %d is %d cells wide: %q", m.width, i, w, l)
		}
	}
	return ansi.Strip(s)
}

func TestBoxPlace(t *testing.T) {
	// The card opens right under the cursor row and pushes the rows after it down. Its
	// header names the origin and where the row is, and the keys that ask from the card and
	// close it: a only on a changed line. An empty row ends it.
	for _, tc := range []struct {
		keys, row, top, after string
		ask                   bool
	}{
		{"4j<enter>", "Changed 2", "▎ ● claude c1a0 #1  a.go:2", "line 3", true},
		{"16j<enter>", "new after 18", "▎ ┆ unknown  a.go:18", "", true}, // the card ends the screen
		{"2j<enter>", "line 1", "▎ a.go:1", "line 2", false},
		{"<enter>", "▾ a.go", "▎ ▾ a.go", "@@ -1,5 +1,5 @@", false},
		{"G<enter>", "var x = 1", "▎ ● claude c1a0 #2  c/new.go:4", "", true}, // the screen moves up to show the card
	} {
		m := newModel(testFiles(), nil, Saved{}, Asker{})
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
		for _, k := range keyPresses(tc.keys) {
			m.Update(k)
		}
		lines := strings.Split(screen(t, m), "\n")
		y := cursorLine(m)
		bottom := y + m.boxHeight()
		if !strings.Contains(lines[y], tc.row) || !strings.Contains(lines[y+1], tc.top) || !strings.HasSuffix(strings.TrimRight(lines[y+1], " "), "⏎ close") ||
			strings.Contains(lines[y+1], "a ask") != tc.ask || bottom >= m.diffHeight() || !strings.HasSuffix(strings.TrimRight(lines[bottom], " "), "▎") ||
			tc.after != "" && !strings.Contains(lines[bottom+1], tc.after) {
			t.Errorf("%q: want %q, then a card from %q with a ask %t and ⏎ close to its last row on the screen, then %q:\n%s",
				tc.keys, tc.row, tc.top, tc.ask, tc.after, strings.Join(lines, "\n"))
		}
	}
}

// The end of the request and the reply of the line of long.go in longFiles.
const longLast, longReply = "마지막 조건: 셸은 쓰지 마", "조건을 다 지켰어요"

// longFiles returns testFiles and long.go, whose line is from a request longer than the
// origin box shows on a 24-row screen.
func longFiles() []review.File {
	var prompt strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&prompt, "조건 %d\n", i)
	}
	prompt.WriteString(longLast)
	lines := origin.Blame("", "x := 1\n", nil)
	lines[0].Edit = &agent.Edit{
		Agent: "claude", Session: claudeSession, Turn: 3, Time: start.Add(4 * time.Minute),
		Prompt: prompt.String(), Reply: longReply,
	}
	return append(testFiles(), review.File{Path: "long.go", Status: 'A', Lines: lines})
}

func TestBox(t *testing.T) {
	// The last row is the line of long.go.
	files := longFiles()
	const last, reply = longLast, longReply

	// The file tree is on the screen at width 120 and hidden at 70. The card's body has 47
	// rows either way: REQUEST, the 41 lines of the request, an empty row, REPLY and the
	// reply, an empty row and the session. 18 of them fit: the 22 rows above the status line
	// and the key row less the cursor row, one more row, the card's header and its last row.
	// d and u move 9.
	for _, width := range []int{120, 70} {
		m := newModel(files, nil, Saved{}, Asker{})
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		send := func(keys string) (string, tea.Cmd) {
			var cmd tea.Cmd
			for _, k := range keyPresses(keys) {
				_, cmd = m.Update(k)
			}
			return screen(t, m), cmd
		}

		s, _ := send("G")
		cursor, top := m.cursor, m.top
		lines := strings.Split(s, "\n")
		status, keys := lines[m.diffHeight()], lines[m.diffHeight()+1]
		if strings.Contains(s, "조건 2") || !strings.Contains(status, "claude c1a0 #3  조") || width == 120 && !strings.Contains(status, "조건 1") || !strings.Contains(keys, "⏎  origin") {
			t.Errorf("width %d: want the origin and the first line of its request in the status line only, and the key that opens the rest in the key row:\n%s", width, s)
		}

		for _, step := range []struct {
			keys      string
			want, not []string
		}{
			{"<enter>", []string{"▎ ● claude c1a0 #3  long.go:1", "⏎ close", "조건 1", "origin 18/47"}, []string{last, reply}},
			{"k", []string{"REQUEST · #3", "origin 18/47"}, nil}, // the first row stays at the top
			{"j", []string{"origin 19/47"}, []string{"REQUEST"}},
			{"d", []string{"origin 28/47"}, nil},
			{"u", []string{"origin 19/47"}, nil},
			{"G", []string{last, reply, "session " + claudeSession[:20], "origin 47/47"}, nil},
			{"<down>", []string{last, reply, "origin 47/47"}, nil}, // the last row stays at the bottom
			{"gg", []string{"REQUEST · #3", "origin 18/47"}, []string{reply}},
			{"dddd", []string{last, reply, "origin 47/47"}, nil},
		} {
			s, _ := send(step.keys)
			for _, w := range step.want {
				if !strings.Contains(s, w) {
					t.Errorf("width %d, %q: the screen has no %q:\n%s", width, step.keys, w, s)
				}
			}
			for _, w := range step.not {
				if strings.Contains(s, w) {
					t.Errorf("width %d, %q: the screen has %q:\n%s", width, step.keys, w, s)
				}
			}
			if m.cursor != cursor {
				t.Errorf("width %d, %q: the cursor moved to row %d from row %d", width, step.keys, m.cursor, cursor)
			}
		}

		// The box is still open. Each way to close it shows the diff again with the cursor
		// where it was.
		for _, keys := range []string{"q", "<esc>", "<enter>"} {
			s, cmd := send(keys)
			if m.reading || cmd != nil || m.cursor != cursor || m.top != top || !strings.Contains(s, "x := 1") ||
				strings.Contains(s, "▎ ● claude c1a0 #3") {
				t.Errorf("width %d, %q: open %t, command %t, cursor at row %d with row %d at the top; "+
					"want the diff without the box, the cursor at row %d and row %d at the top:\n%s",
					width, keys, m.reading, cmd != nil, m.cursor, m.top, cursor, top, s)
			}
			send("<enter>G") // open it again for the next key
		}
	}
}

func TestTheme(t *testing.T) {
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	for _, tc := range []struct {
		bg   color.Color
		dark bool
	}{
		{color.RGBA{R: 0x0d, G: 0x11, B: 0x17, A: 0xff}, true}, // GitHub's own
		{color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}, true},
		{color.Black, true},
		{color.White, false},
		{color.RGBA{R: 0xfd, G: 0xf6, B: 0xe3, A: 0xff}, false},
	} {
		m.Update(tea.BackgroundColorMsg{Color: tc.bg})
		th := m.theme
		if th.dark != tc.dark {
			t.Errorf("background %v: got dark %t, want %t", tc.bg, th.dark, tc.dark)
		}
		// Bands stand out from the background, lighter on a dark one and darker on a light
		// one, and so do the tinted rows of the diff.
		for name, c := range map[string]color.Color{"panel": th.panel, "header": th.surface2, "cursor": th.cursorline} {
			if lighter := brightness(c) > brightness(tc.bg); lighter != tc.dark {
				t.Errorf("background %v: %s %v is lighter %t, want %t", tc.bg, name, c, lighter, tc.dark)
			}
		}
		for name, c := range map[string]color.Color{"added": th.addbg, "deleted": th.delbg, "hunk": th.hunkbg} {
			if brightness(c) == brightness(tc.bg) {
				t.Errorf("background %v: %s line %v does not stand out", tc.bg, name, c)
			}
		}
	}
}

// brightness returns the sum of the channels of c.
func brightness(c color.Color) uint32 {
	r, g, b, _ := c.RGBA()
	return r + g + b
}

func TestLayout(t *testing.T) {
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	for _, tc := range []struct {
		width int
		list  bool
	}{
		{200, true},
		{100, true},
		{70, false},
	} {
		m.width = tc.width
		list, diff := m.layout()
		separator := 0
		if list > 0 {
			separator = 1
		}
		if (list > 0) != tc.list || list+separator+diff != tc.width || list > 0 && diff < minDiffWidth {
			t.Errorf("width %d: got list %d, diff %d; want list %t, a diff of at least %d next to it",
				tc.width, list, diff, tc.list, minDiffWidth)
		}
	}
}

func TestDraw(t *testing.T) {
	problems := []agent.Problem{{Agent: "codex", Path: "rollout-1.jsonl", Bad: 1}}
	for _, width := range []int{200, 100, 70} {
		// Row 25, split row 24, is a long line of wide characters, cut at the edge of the
		// diff, or of the right side in the split view. The split view needs the 200 cells.
		for _, tc := range []struct{ keys, tag string }{
			{"", ""},
			{"4j", "claude c1a0 #1"},
			{"25j", "claude c1a0 #2"},
			{"?", ""},
			{"4j<enter>", "claude c1a0 #1"},
			{"25j<enter>", "claude c1a0 #2"},
			{"G<enter>", "claude c1a0 #2"},
			{"s3j", "claude c1a0 #1"},
			{"s/line 15<enter><enter>", "omp a1f2 #1"},
			{"s24j", "claude c1a0 #2"},
			{"sG<enter>", "claude c1a0 #2"},
		} {
			m := newModel(testFiles(), problems, Saved{}, Asker{})
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			for _, k := range keyPresses(tc.keys) {
				m.Update(k)
			}
			if split := strings.HasPrefix(tc.keys, "s") && width == 200; m.splitShown != split {
				t.Errorf("width %d, keys %q: split view %t, want %t", width, tc.keys, m.splitShown, split)
				continue
			}
			lines := strings.Split(m.draw(), "\n")
			if len(lines) != 24 {
				t.Errorf("width %d, keys %q: got %d lines, want 24", width, tc.keys, len(lines))
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w != width {
					t.Errorf("width %d, keys %q: line %d is %d cells wide: %q", width, tc.keys, i, w, l)
				}
			}
			// The cursor row shows a bar at the left edge of the diff, unless the help covers it.
			list, _ := m.layout()
			if list > 0 {
				list++ // and the line between the file tree and the diff
			}
			if bar := ansi.Strip(ansi.Cut(lines[cursorLine(m)], list, list+1)); bar != "▌" && !m.help {
				t.Errorf("width %d, keys %q: the cursor row starts the diff with %q, want a bar", width, tc.keys, bar)
			}
			// The band over the diff tells of the session logs dr could not fully read.
			if band := ansi.Strip(ansi.Cut(lines[0], list, width)); !m.help && !strings.HasPrefix(band, " ! Some codex records could not be read") {
				t.Errorf("width %d, keys %q: the diff starts with %q, want the band", width, tc.keys, band)
			}
			// The status line names the origin of a changed row under the cursor, unless the
			// help covers it. On a narrow screen the row number gives way to it and to the note
			// that the diff is too narrow for split.
			status := ansi.Strip(lines[m.diffHeight()])
			var want []string
			if tc.tag != "" {
				want = append(want, tc.tag)
			}
			if width >= 100 && !m.help {
				want = append(want, fmt.Sprintf("row %d/%d", m.cursor+1, len(m.rows)))
			}
			for _, s := range want {
				if !strings.Contains(status, s) {
					t.Errorf("width %d, keys %q: status line %q has no %q", width, tc.keys, status, s)
				}
			}
		}
	}
}

func TestStatusRoom(t *testing.T) {
	// At 70 cells the origin of the cursor row stays whole. How far the box is read comes
	// next and then the file number; the row number does not fit, so it is left out whole.
	// The card's body is 11 rows, 4 of which fit on a 10-row screen under the band on two
	// agents' logs.
	problems := []agent.Problem{{Agent: "claude", Path: "a.jsonl", Bad: 1}, {Agent: "codex", Path: "rollout-1.jsonl", Bad: 1}}
	m := newModel(testFiles(), problems, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 10})
	for _, k := range keyPresses("4j<enter>") {
		m.Update(k)
	}
	s := screen(t, m)
	status := s[strings.LastIndex(s, "\n")+1:]
	for _, want := range []string{"claude c1a0 #1", "origin 4/11", "file 1/3"} {
		if !strings.Contains(status, want) {
			t.Errorf("status line %q has no %q", status, want)
		}
	}
	for _, part := range []string{"records", "row", "scroll"} {
		if strings.Contains(status, part) {
			t.Errorf("status line %q has a part of what does not fit, %q", status, part)
		}
	}
}

func TestOverlay(t *testing.T) {
	// The help box lands across wide characters of the line under it and keeps the line as
	// wide as the screen.
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("한글한글한글한글")
	for _, top := range []string{"[ab]", "[abc]"} {
		for x := range 16 - len(top) + 1 {
			got := overlay(base, top, x, len(top), 16)
			if w, text := ansi.StringWidth(got), ansi.Strip(got); w != 16 || !strings.Contains(text, top) {
				t.Errorf("%q at %d: got %q, %d cells wide", top, x, text, w)
			}
		}
	}
}

// parseDiff returns the lines of a diff written as texts: "-x" a deleted line, "+x" an added
// line and "x" an unchanged one, numbered on each side.
func parseDiff(texts ...string) []origin.Line {
	var lines []origin.Line
	old, now := 0, 0
	for _, s := range texts {
		switch {
		case strings.HasPrefix(s, "-"):
			old++
			lines = append(lines, origin.Line{Text: s[1:], Old: old})
		case strings.HasPrefix(s, "+"):
			now++
			lines = append(lines, origin.Line{Text: s[1:], New: now})
		default:
			old, now = old+1, now+1
			lines = append(lines, origin.Line{Text: s, Old: old, New: now})
		}
	}
	return lines
}

func TestSplitRows(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []origin.Line
		want  []string // the line rows of the split view, left|right
	}{
		{"deleted only", parseDiff("x", "-d", "y"), []string{"x|x", "d|", "y|y"}},
		{"added only", parseDiff("x", "+a", "y"), []string{"x|x", "|a", "y|y"}},
		{
			"two deleted, three added", parseDiff("x", "-v := 1", "-w := 1", "+v := 2", "+w := 2", "+u := 3", "y"),
			[]string{"x|x", "v := 1|v := 2", "w := 1|w := 2", "|u := 3", "y|y"},
		},
		{
			"unchanged between", parseDiff("-d1", "+a1", "x", "-d2", "-d3", "+a2"),
			[]string{"d1|a1", "x|x", "d2|a2", "d3|"},
		},
	} {
		m := newModel([]review.File{{Path: "f.txt", Status: 'M', Lines: tc.lines}}, nil, Saved{}, Asker{})
		var got []string
		for _, r := range m.split {
			if r.kind != lineRow {
				continue
			}
			var left, right string
			if r.left >= 0 {
				left = m.lines[r.left].Text
			}
			if r.right >= 0 {
				right = m.lines[r.right].Text
			}
			got = append(got, left+"|"+right)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	// The word marks pair the lines the way the split view does: the third added line has no
	// partner and no marks.
	m := newModel([]review.File{{Path: "f.txt", Status: 'M', Lines: parseDiff("x", "-v := 1", "-w := 1", "+v := 2", "+w := 2", "+u := 3")}}, nil, Saved{}, Asker{})
	m.colorFile(0)
	for _, tc := range []struct {
		line int // in m.lines
		old  bool
		want []string
	}{
		{1, true, []string{"1"}}, {3, false, []string{"2"}},
		{2, true, []string{"1"}}, {4, false, []string{"2"}},
		{5, false, nil},
	} {
		if got := changed(m.pieces(tc.line, tc.old)); !slices.Equal(got, tc.want) {
			t.Errorf("line %q: marked %q, want %q", m.lines[tc.line].Text, got, tc.want)
		}
	}
}

func TestSplitMove(t *testing.T) {
	// testFiles has 26 split rows: as the 27 unified rows, but -line 2 and +Changed 2 share
	// row 3, so -line 15 is row 11, +new after 18 row 15, b.bin row 18 and c/new.go row 20.
	for _, tc := range []struct {
		keys  string
		split bool
		want  int
	}{
		{"3js", true, 3},     // the deleted line goes to its row
		{"4js", true, 3},     // and so does the added line
		{"3jss", false, 3},   // back to the deleted line it came from
		{"4jss", false, 4},   // and to the added line
		{"3jsjks", false, 3}, // still on the row it came to
		{"3jsjs", false, 5},  // moved to another row: the line on the right
		{"sjjjs", false, 4},  // a row entered elsewhere: the line on the right
		{"12js", true, 11},
		{"12jss", false, 12},
		{"s11js", false, 12}, // or on the left when the right has none
		{"16js", true, 15},
		{"19js", true, 18}, // a header goes to its header
		{"20js", true, 19},
		{"sG", true, 25},
		{"s}", true, 3},
		{"s}}", true, 11},
		{"s3}", true, 15},
		{"s4}", true, 22},
		{"sG{", true, 22},
		{"sl", true, 18},
		{"s3l", true, 20},
		{"sGh", true, 20},
		{"sG2h", true, 18},
		{"s/changed<enter>", true, 3}, // the right side
		{"s/line 2<enter>", true, 3},  // the left side
		{"s/line 2<enter>n", true, 17},
		{"s/new.go<enter>", true, 20},
	} {
		m := press(t, tc.keys)
		if m.splitShown != tc.split || m.cursor != tc.want {
			t.Errorf("%q: split view %t with the cursor at row %d, want %t and row %d", tc.keys, m.splitShown, m.cursor, tc.split, tc.want)
		}
		if m.cursor < m.top || cursorLine(m) >= m.diffHeight() {
			t.Errorf("%q: cursor at row %d, line %d of the screen from row %d, is off the screen", tc.keys, m.cursor, cursorLine(m), m.top)
		}
	}
}

func TestSplitWidth(t *testing.T) {
	// On the deleted line -line 2, unified row 3, the split view shows its pair at row 3. A
	// width too narrow for the split view, from a resize or a drag of the file tree, shows
	// the unified view again with the cursor still on the deleted line. On 125 cells the
	// tree of testFiles, 20 cells, leaves the diff 104; dragged to 30 it leaves 94, and
	// dragged back it stops at its least width, 20, which leaves 104 again.
	m := newModel(testFiles(), nil, Saved{}, Asker{})
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	for _, k := range keyPresses("3js") {
		m.Update(k)
	}
	for _, step := range []struct {
		name  string
		msgs  []tea.Msg
		split bool
	}{
		{"narrower", []tea.Msg{tea.WindowSizeMsg{Width: 90, Height: 24}}, false},
		{"wider", []tea.Msg{tea.WindowSizeMsg{Width: 125, Height: 24}}, true},
		{"narrower again", []tea.Msg{tea.WindowSizeMsg{Width: 90, Height: 24}}, false},
		{"wider again", []tea.Msg{tea.WindowSizeMsg{Width: 125, Height: 24}}, true},
		{"tree dragged wider", drag(20, 30), false},
		{"tree dragged back", drag(30, 17), true},
	} {
		for _, msg := range step.msgs {
			m.Update(msg)
		}
		if m.splitShown != step.split || m.cursor != 3 || m.lines[m.rows[m.cursor].left].Text != "line 2" {
			t.Errorf("%s: split view %t with the cursor at row %d, want %t and row 3 with -line 2", step.name, m.splitShown, m.cursor, step.split)
		}
	}
	if !m.splitShown || m.rows[m.cursor].right < 0 || m.lines[m.rows[m.cursor].right].Text != "Changed 2" {
		t.Errorf("the split row is not the pair of -line 2 and +Changed 2")
	}
}

func TestSplitOrigin(t *testing.T) {
	// The first pair comes from two requests and the second from one. The header names the
	// origin of the main line, and a pair of two origins tells each under its side.
	lines := parseDiff("x", "-old", "+new", "y", "-old 2", "+new 2", "z")
	lines[1].Edit, lines[2].Edit = subagent, claude1
	lines[4].Edit, lines[5].Edit = claude1, claude1
	files := []review.File{{Path: "f.go", Status: 'M', Lines: lines}}
	for _, tc := range []struct {
		keys      string
		want, not []string // want in the order the card's body shows them
	}{
		{
			keys: "s3j",
			want: []string{"DELETED LINE\n● omp a1f2 #1\n", "line 15를 지워줘", "ADDED LINE\n● claude c1a0 #1\n", "rate limiter를 짜줘\n셸 명령은 실행하지 마"},
		},
		{keys: "s5j", want: []string{"REQUEST · #1", "다 짰어요"}, not: []string{"DELETED LINE", "ADDED LINE"}},
		{keys: "4j", want: []string{"REQUEST · #1"}, not: []string{"DELETED LINE", "omp a1f2 #1"}}, // unified: the added line alone
	} {
		m := end(t, openFiles(t, files, tc.keys))
		if head := cardText(m); !strings.HasPrefix(head, "● claude c1a0 #1  f.go:") {
			t.Errorf("%q: the card's header %q, want claude c1a0 #1", tc.keys, strings.SplitN(head, "\n", 2)[0])
		}
		text := boxText(m)
		at := 0
		for _, s := range tc.want {
			i := strings.Index(text[at:], s)
			if i < 0 {
				t.Errorf("%q: origin\n%s\nhas no %q after its first %d bytes", tc.keys, text, s, at)
				break
			}
			at += i + len(s)
		}
		for _, s := range tc.not {
			if strings.Contains(text, s) {
				t.Errorf("%q: origin\n%s\nhas %q", tc.keys, text, s)
			}
		}
	}
}

// drag returns the mouse messages of dragging with the left button from column from to each
// of the columns to, on the sixth row.
func drag(from int, to ...int) []tea.Msg {
	msgs := []tea.Msg{tea.MouseClickMsg{X: from, Y: 5, Button: tea.MouseLeft}}
	for _, x := range to {
		msgs = append(msgs, tea.MouseMotionMsg{X: x, Y: 5, Button: tea.MouseLeft})
	}
	return append(msgs, tea.MouseReleaseMsg{X: to[len(to)-1], Y: 5, Button: tea.MouseLeft})
}

func TestDragTree(t *testing.T) {
	// The file tree of testFiles is 19 cells wide on the 125-cell screen, so the line beside
	// it is column 19. The diff keeps 60 cells, which leaves the tree 64 at most.
	for _, tc := range []struct {
		name   string
		drags  [][]tea.Msg
		list   int
		folded bool
	}{
		{"wider", [][]tea.Msg{drag(19, 25, 30)}, 30, false},
		{"grabbed beside the line", [][]tea.Msg{drag(18, 30)}, 30, false},
		{"grabbed on the other side", [][]tea.Msg{drag(20, 24)}, 24, false},
		{"missed", [][]tea.Msg{drag(22, 30)}, 19, false},
		{"no narrower than the least", [][]tea.Msg{drag(19, 30), drag(30, 12)}, 20, false},
		{"no wider than the diff allows", [][]tea.Msg{drag(19, 100)}, 64, false},
		{"folded", [][]tea.Msg{drag(19, 5)}, 0, true},
		{"opened by the handle", [][]tea.Msg{drag(19, 5), drag(0, 25)}, 25, false},
		{"handle grabbed beside it", [][]tea.Msg{drag(19, 5), drag(1, 30)}, 30, false},
		{"not dragged far enough to open", [][]tea.Msg{drag(19, 5), drag(0, 6)}, 0, true},
		{"the line follows the tree", [][]tea.Msg{drag(19, 30), drag(30, 40)}, 40, false},
	} {
		var msgs []tea.Msg
		for _, d := range tc.drags {
			msgs = append(msgs, d...)
		}
		m := end(t, open(t, ""), msgs...)
		list, diff := m.layout()
		if list != tc.list || m.folded != tc.folded || m.dragging {
			t.Errorf("%s: tree %d cells wide, folded %t, dragging %t; want %d, folded %t", tc.name, list, m.folded, m.dragging, tc.list, tc.folded)
		}
		lines := strings.Split(screen(t, m), "\n")
		switch {
		case m.folded && (diff != 124 || !strings.HasPrefix(lines[0], "▸") || !strings.HasPrefix(lines[1], "│")):
			t.Errorf("%s: want a handle at the left edge and the diff beside it, got a diff %d cells wide:\n%s", tc.name, diff, strings.Join(lines, "\n"))
		case !m.folded && ansi.StringWidth(lines[1][:strings.Index(lines[1], "│")]) != list:
			t.Errorf("%s: want the line between the tree and the diff at column %d:\n%s", tc.name, list, strings.Join(lines, "\n"))
		}
	}
}

func TestWheel(t *testing.T) {
	down := tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelUp}
	if m := end(t, open(t, ""), down, down, up); m.cursor != 3 || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Errorf("wheel down twice and up once: cursor at row %d, mouse mode %v; want row 3 and cell motion", m.cursor, m.View().MouseMode)
	}
	// With the origin box open the wheel moves its text and not the cursor.
	m := end(t, openFiles(t, longFiles(), "G<enter>"), down, down, up, down)
	if m.readTop != 6 || m.cursor != len(m.rows)-1 {
		t.Errorf("wheel in the origin box: its text from line %d with the cursor at row %d, want line 6 and row %d", m.readTop, m.cursor, len(m.rows)-1)
	}
	// The help ignores the wheel.
	if m := end(t, open(t, "?"), down); m.cursor != 0 || !m.help {
		t.Errorf("wheel over the help: cursor at row %d, help %t; want row 0 under the help", m.cursor, m.help)
	}
}
