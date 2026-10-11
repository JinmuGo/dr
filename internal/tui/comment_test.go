package tui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// commentScreen returns the screen on files in a 125×24 terminal, with what it kept in the
// state directory dir, at the time start.
func commentScreen(files []review.File, dir string) *model {
	m := askScreen(files, Load(dir), &fakeAsk{answer: answer})
	m.now = func() time.Time { return start }
	return m
}

// commentRecords returns the comment records in the log in dir.
func commentRecords(t *testing.T, dir string) []state.Comment {
	t.Helper()
	records, bad, err := state.NewLog(state.LogPath(dir)).Read()
	if err != nil || bad > 0 {
		t.Fatalf("reading the log: %d bad lines, %v", bad, err)
	}
	return records.Comments
}

// texts returns the text of each comment of m.
func texts(m *model) []string {
	var out []string
	for _, c := range m.comments {
		out = append(out, c.Text)
	}
	return out
}

func TestCommentKeys(t *testing.T) {
	// c opens the input box under the line, titled with who the comment goes to and its
	// lines, empty but for a dim placeholder. Enter saves it, and c on its line edits it, the
	// box taking the place of the comment's box and holding its text. Saving it empty removes
	// it. Enter on a new comment with nothing typed, or Esc, makes none. Each add, edit and
	// removal is a record of the log, all with the comment's ID.
	dir := t.TempDir()
	m := commentScreen(testFiles(), dir)
	const title = "COMMENT → claude c1a0 · L2"
	typeKeys(m, "4jc")
	if box := entryBox(t, m); m.drafting == nil || m.entry.text != "" || shownTitle(box) != title || !slices.Equal(shownText(box), []string{commentPlaceholder}) {
		t.Fatalf("c: the box\n%s\nwant an empty one titled %q", strings.Join(box, "\n"), title)
	}
	typeKeys(m, "use errors.Is<enter>")
	if m.drafting != nil || !slices.Equal(texts(m), []string{"use errors.Is"}) {
		t.Fatalf("Enter: box open %t, comments %q; want the box closed and the comment", m.drafting != nil, texts(m))
	}
	added := commentRecords(t, dir)
	want := state.Comment{
		Time: start, ID: m.comments[0].ID, Agent: "claude", Session: claudeSession, Relation: "author", Path: "a.go",
		Lines:  []state.Line{{Kind: "+", Text: "Changed 2"}},
		Before: []state.Line{{Kind: " ", Text: "line 1"}, {Kind: "-", Text: "line 2"}},
		After:  []state.Line{{Kind: " ", Text: "line 3"}, {Kind: " ", Text: "line 4"}},
		Text:   "use errors.Is",
	}
	if len(added) != 1 || len(want.ID) != 16 || !commentEqual(added[0], want) {
		t.Fatalf("records %+v, want %+v", added, want)
	}

	typeKeys(m, "c")
	const edit = "EDIT → claude c1a0 · L2"
	if box := entryBox(t, m); m.drafting == nil || m.drafting.editing == nil || shownTitle(box) != edit ||
		!slices.Equal(shownText(box), []string{"use errors.Is"}) || strings.Contains(screen(t, m), "✎ you → ● claude c1a0 · a.go:2") {
		t.Fatalf("c on the comment: the screen\n%s\nwant the box %q with its text in place of its box", screen(t, m), edit)
	}
	typeKeys(m, "<ctrl+u>use errors.As<enter>")
	typeKeys(m, "c<enter>") // saved as it was, which changes nothing
	if !slices.Equal(texts(m), []string{"use errors.As"}) || len(commentRecords(t, dir)) != 2 {
		t.Errorf("after the edit: comments %q, %d records; want the new text and 2 records", texts(m), len(commentRecords(t, dir)))
	}
	typeKeys(m, "c<ctrl+u><enter>")
	records := commentRecords(t, dir)
	if len(m.comments) != 0 || len(records) != 3 || records[2].Text != "" || records[2].ID != want.ID {
		t.Errorf("saved empty: comments %q, records %+v; want no comment and a third record without text", texts(m), records)
	}
	typeKeys(m, "c<enter>cfix<esc>")
	if len(m.comments) != 0 || m.drafting != nil || len(commentRecords(t, dir)) != 3 {
		t.Errorf("Enter on nothing typed and Esc: comments %q, box open %t; want neither to make one", texts(m), m.drafting != nil)
	}
}

// commentEqual reports whether a and b are the same comment record.
func commentEqual(a, b state.Comment) bool {
	return a.Time.Equal(b.Time) && a.ID == b.ID && a.Agent == b.Agent && a.Session == b.Session && a.Relation == b.Relation &&
		a.Path == b.Path && slices.Equal(a.Lines, b.Lines) && slices.Equal(a.Selected, b.Selected) &&
		slices.Equal(a.Before, b.Before) && slices.Equal(a.After, b.After) && a.Text == b.Text && a.Model == b.Model
}

func TestCommentSelection(t *testing.T) {
	// A selection takes a comment on its lines and ends. The deleted line in it is context,
	// so the range is a.go:2, and c on either line edits the comment, as does a selection of
	// exactly its lines. A selection that overlaps it otherwise is refused.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "3jVjc")
	if title := shownTitle(entryBox(t, m)); title != "COMMENT → claude c1a0 · L2" {
		t.Fatalf("c on a selection: the box titled %q", title)
	}
	typeKeys(m, "fix<enter>")
	if c := m.comments; m.selecting || len(c) != 1 || len(c[0].Lines) != 2 || c[0].Lines[0].Kind != "-" {
		t.Fatalf("after Enter: selecting %t, comments %+v; want the selection ended and a comment on -line 2 and +Changed 2", m.selecting, c)
	}
	for _, tc := range []struct {
		keys, why string
	}{
		{keys: "gg3jc"},
		{keys: "gg4jc"},
		{keys: "gg3jVjc"},
		{keys: "gg2jVjjc", why: "The selection overlaps a comment. Edit it with c on its lines"},
		{keys: "gg4jVjc", why: "The selection overlaps a comment. Edit it with c on its lines"},
	} {
		typeKeys(m, tc.keys)
		switch {
		case tc.why == "" && (m.drafting == nil || m.drafting.editing != m.comments[0] || m.entry.text != "fix"):
			t.Errorf("%q: want the comment edited", tc.keys)
		case tc.why != "" && (m.drafting != nil || m.message != tc.why):
			t.Errorf("%q: message %q, want %q", tc.keys, m.message, tc.why)
		}
		typeKeys(m, "<esc><esc>")
	}
}

func TestCommentRefusals(t *testing.T) {
	// The lines must have changed, have an origin and come from one session, and the status
	// line says so. Lines a Claude subagent wrote go to the session that started it, as the
	// title of the box says.
	for _, tc := range []struct {
		keys, want string
	}{
		{"jc", "Move to a changed line to comment"},  // a hunk header
		{"2jc", "Move to a changed line to comment"}, // an unchanged line
		{"3jc", "Deleted lines alone cannot take a comment"},
		{"16jc", "Lines without an origin cannot take a comment. Fix them with e"},
		{"4jV8jc", "The selection mixes sessions. Select lines from one session"},
		{"31jc", "╭─ COMMENT  → claude c1a0, which started the subagent · L3 "},
		{"30jc", "╭─ COMMENT  → codex 3333 · L2 "},
	} {
		m := commentScreen(askFiles(), t.TempDir())
		typeKeys(m, tc.keys)
		if s := screen(t, m); !strings.Contains(s, tc.want) {
			t.Errorf("%q: the screen\n%s\nhas no %q", tc.keys, s, tc.want)
		}
	}
}

func TestCommentBox(t *testing.T) {
	// The card of a comment comes under the last line of its range, in the unified and the
	// split view: who it goes to and where its lines are, with a pending chip and the key that
	// edits it, then the text and an empty row. Its lines are only drawn: j goes from the line
	// to the next one, the rows are counted without them, search does not see them and a click
	// on them is on no row. The origin card opened on the line comes right under the line,
	// and holds the comment's card after its own rows and an empty one.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcuse errors.Is<enter>")
	box := []string{"▎ ✎ you → ● claude c1a0 · a.go:2    pending ", "▎ use errors.Is", "▎ "}
	check := func(name, row string, after []string) {
		t.Helper()
		lines := strings.Split(screen(t, m), "\n")
		y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, row) })
		for k, want := range after {
			if y < 0 || y+1+k >= len(lines) || !strings.Contains(lines[y+1+k], want) {
				t.Errorf("%s: want %q under %q, then %q:\n%s", name, row, after[:k], want, strings.Join(lines, "\n"))
				return
			}
		}
	}
	check("unified", "Changed 2", append(box, "line 3"))
	typeKeys(m, "j")
	if status := statusLine(t, m); m.cursor != 5 || !strings.Contains(status, "row 6/27") {
		t.Errorf("j: cursor at row %d, status line %q; want row 5, row 6/27", m.cursor, status)
	}
	y := cursorLine(m) + len(box) // the line of row 5, under row 4 and the box
	if m.rowAt(y) != 5 || m.rowAt(y-1) != -1 || m.rowAt(y-len(box)) != -1 || m.rowAt(y-len(box)-1) != 4 {
		t.Errorf("rows at lines %d to %d: %d, %d, %d, %d; want 4, -1 for the box, then 5",
			y-len(box)-1, y, m.rowAt(y-len(box)-1), m.rowAt(y-len(box)), m.rowAt(y-1), m.rowAt(y))
	}
	typeKeys(m, "/errors<enter>")
	if m.cursor != 5 || !strings.HasPrefix(m.message, "Not found") {
		t.Errorf("search for the comment: cursor at row %d, message %q; want it not found", m.cursor, m.message)
	}
	typeKeys(m, "k<enter>")
	check("the origin card", "Changed 2", []string{"▎ ● claude c1a0 #1  a.go:2"})
	lines := strings.Split(screen(t, m), "\n")
	if under := cursorLine(m) + 2 + len(m.originLines()) + 1; under >= len(lines) || !strings.Contains(lines[under], box[0]) {
		t.Errorf("want the comment's card after the origin card's rows and an empty one, on line %d:\n%s", under, strings.Join(lines, "\n"))
	}
	typeKeys(m, "<esc>s")
	if !m.splitShown {
		t.Fatal("s: want the split view")
	}
	check("split", "Changed 2", append(box, "line 3"))
}

func TestCommentWidths(t *testing.T) {
	// The box fits any width, in the dark and the light colors and without colors: screen
	// checks every line is as wide as the screen, so no wide character is split. Control
	// characters show as text.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "Gc한글로 길게 쓴 주석을 고쳐 주세요 \u202e 끝<enter>")
	for _, th := range []theme{newTheme(nil, true, false), newTheme(nil, false, false), newTheme(nil, true, true)} {
		m.theme = th
		for _, width := range []int{125, 70, 41, 30, 9} {
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			s := screen(t, m)
			if strings.Contains(s, "\u202e") {
				t.Errorf("width %d: the control character reached the screen", width)
			}
			if width >= 70 && (!strings.Contains(s, `\u202e`) || !strings.Contains(s, "한글로")) {
				t.Errorf("width %d: want the comment shown, the control character as text:\n%s", width, s)
			}
		}
	}
}

func TestCommentCounts(t *testing.T) {
	// The file tree shows each file's pending comments by its name and the file header by
	// its diffstat, in the dark and the light colors and without colors.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcone<enter>gg11jVjctwo<enter>Gcthree<enter>")
	if len(m.comments) != 3 {
		t.Fatalf("comments %q, want 3", texts(m))
	}
	for _, th := range []theme{newTheme(nil, true, false), newTheme(nil, false, false), newTheme(nil, true, true)} {
		m.theme = th
		lines := strings.Split(screen(t, m), "\n")
		tree := func(name string) string {
			for _, l := range lines {
				if list, _, _ := strings.Cut(l, "│"); strings.Contains(list, name) {
					return list
				}
			}
			return ""
		}
		for _, tc := range []struct{ name, count string }{{"a.go", "✎2"}, {"new.go", "✎1"}, {"b.bin", ""}} {
			if got := tree(tc.name); tc.count != "" && !strings.Contains(got, tc.name+" "+tc.count) || tc.count == "" && strings.Contains(got, "✎") {
				t.Errorf("tree row of %s: %q, want %q", tc.name, got, tc.count)
			}
		}
		for _, tc := range []struct {
			file  int
			count string
		}{{0, "✎ 2"}, {1, ""}, {2, "✎ 1"}} {
			got := ansi.Strip(m.drawHeader(m.files[tc.file], 104, false))
			if tc.count != "" && !strings.Contains(got, tc.count+"    +") || tc.count == "" && strings.Contains(got, "✎") {
				t.Errorf("header of %s: %q, want %q before the counts", m.files[tc.file].path, got, tc.count)
			}
		}
	}
}

func TestCommentReopen(t *testing.T) {
	// On the next open each comment is as its newest record has it, on its lines. A removed
	// comment stays removed. A comment whose lines changed has no place: it shows dim in the
	// origin box of its file's header, and is neither counted nor pending. One on a file no
	// longer in the diff is left out.
	dir := t.TempDir()
	m := commentScreen(testFiles(), dir)
	typeKeys(m, "4jcone<enter>gg11jVjctwo<enter>Gcthree<enter>c<ctrl+u>four<enter>gg11jc<ctrl+u><enter>")
	if !slices.Equal(texts(m), []string{"one", "four"}) {
		t.Fatalf("comments %q, want one and four", texts(m))
	}
	if err := state.NewLog(state.LogPath(dir)).Append(state.Comment{
		ID: "0000000000000001", Agent: "claude", Session: claudeSession, Path: "gone.go", Lines: []state.Line{{Kind: "+", Text: "x"}}, Text: "gone",
	}); err != nil {
		t.Fatal(err)
	}
	again := commentScreen(testFiles(), dir)
	if !slices.Equal(texts(again), []string{"one", "four"}) {
		t.Fatalf("reopened: comments %q, want one and four", texts(again))
	}
	for i, c := range again.comments {
		if c.first != m.comments[i].first || c.last != m.comments[i].last || c.under != m.comments[i].under {
			t.Errorf("reopened: %q on lines %d to %d, want %d to %d", c.Text, c.first, c.last, m.comments[i].first, m.comments[i].last)
		}
	}

	files := testFiles()
	for i, l := range files[0].Lines {
		if l.Text == "Changed 2" {
			files[0].Lines[i].Text = "Changed two"
		}
	}
	changed := commentScreen(files, dir)
	if c := changed.comments; len(c) != 2 || c[0].placed() || !c[1].placed() {
		t.Fatalf("after a change: comments %q, want one without a place and four on its line", texts(changed))
	}
	if n := changed.commentCount("a.go"); n != 0 {
		t.Errorf("a.go counts %d comments, want 0", n)
	}
	typeKeys(changed, "<enter>")
	if text := boxText(changed); !strings.Contains(text, "EARLIER COMMENTS · 1\n> Changed 2\n✎ you → ● claude c1a0   no place in the diff\none") {
		t.Errorf("the header's box:\n%s", text)
	}
	rows := changed.originLines()
	k := slices.IndexFunc(rows, func(r cardRow) bool { return chunksText(r.left) == "EARLIER COMMENTS · 1" })
	for _, r := range rows[k+1:] {
		for _, c := range r.left {
			if strings.TrimSpace(c.text) != "" && (c.color != changed.theme.muted || c.bold) {
				t.Errorf("%+v is not dim", c)
			}
		}
	}
	if g := changed.pendingComments(); len(g) != 1 || len(g[0].comments) != 1 || g[0].comments[0].text != "four" {
		t.Errorf("pending %+v, want four alone", g)
	}
	typeKeys(changed, "<esc>4jc")
	if changed.drafting == nil || changed.drafting.editing != nil {
		t.Error("c on the line of a comment without a place: want a new comment")
	}
}

func TestPendingComments(t *testing.T) {
	// The pending comments come grouped by the session they go to, in the order they were
	// made, each with its range and lines in the current file, its diff lines and its text.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcone<enter>gg11jVjctwo<enter>Gcthree<enter>")
	groups := m.pendingComments()
	type got struct {
		agent, path string
		first, last int
		current     []string
		diff        int
		text        string
	}
	var all []got
	for _, g := range groups {
		for _, c := range g.comments {
			all = append(all, got{g.agent, c.path, c.first, c.last, c.current, len(c.diff), c.text})
		}
	}
	want := []got{
		{"claude", "a.go", 2, 2, []string{"Changed 2"}, 1, "one"},
		{"claude", "c/new.go", 4, 4, []string{"\tvar x = 1"}, 1, "three"},
		{"omp", "a.go", 14, 14, []string{"line 14"}, 2, "two"},
	}
	if len(groups) != 2 || groups[0].session != claudeSession || groups[1].session != ompSession ||
		!slices.EqualFunc(all, want, func(a, b got) bool {
			return a.agent == b.agent && a.path == b.path && a.first == b.first && a.last == b.last &&
				slices.Equal(a.current, b.current) && a.diff == b.diff && a.text == b.text
		}) {
		t.Errorf("got %d groups\n%+v\nwant claude's and then omp's\n%+v", len(groups), all, want)
	}
}

func TestRebuildComments(t *testing.T) {
	// After a rebuild the comments of earlier runs and of this one go to their lines in the
	// new diff, though rows above them came, and stay pending there.
	line := func(kind, text string) state.Line { return state.Line{Kind: kind, Text: text} }
	earlier := state.Records{Comments: []state.Comment{{
		ID: "0000000000000001", Agent: "claude", Session: claudeSession, Relation: "author", Path: "a.go", Text: "earlier",
		Before: []state.Line{line(" ", "line 1"), line("-", "line 2")}, Lines: []state.Line{line("+", "Changed 2")},
		After: []state.Line{line(" ", "line 3"), line(" ", "line 4")},
	}}}
	m := editScreen(testFiles(), Saved{Log: earlier, Reload: reloading(editedFiles())}, &fakeEditor{})
	typeKeys(m, "11jVjcnow<enter>")
	before := m.comments[1].under
	typeKeys(m, "e")
	if findRow(m, "+Line 5 edited") < 0 {
		t.Fatal("the diff was not rebuilt")
	}
	if c := m.comments[1]; c.under == before || !slices.Equal(m.commentsAt(findRow(m, " line 14")), []*comment{c}) {
		t.Errorf("now is under line %d, was under %d; line 14 has %d comments, want now", c.under, before, len(m.commentsAt(findRow(m, " line 14"))))
	}
	if cs := m.commentsAt(findRow(m, "+Changed 2")); len(cs) != 1 || cs[0].Text != "earlier" {
		t.Errorf("+Changed 2 has comments %v, want earlier", cs)
	}
	var pending []string
	for _, g := range m.pendingComments() {
		for _, c := range g.comments {
			pending = append(pending, c.text+" "+lineRange(c.first, c.last))
		}
	}
	if !slices.Equal(pending, []string{"earlier 2", "now 14"}) {
		t.Errorf("pending %q, want earlier on line 2 and now on line 14", pending)
	}
}

func TestRebuildCommentLines(t *testing.T) {
	// A comment whose own lines changed in a rebuild has no place: it goes to the box of its
	// file's header as an earlier comment, and is neither counted nor pending.
	files := testFiles()
	for i, l := range files[0].Lines {
		if l.Text == "Changed 2" {
			files[0].Lines[i].Text = "Changed two"
		}
	}
	m := editScreen(testFiles(), Saved{Reload: reloading(files)}, &fakeEditor{})
	typeKeys(m, "4jcfix<enter>")
	if len(m.pendingComments()) != 1 {
		t.Fatal("the comment is not pending before the rebuild")
	}
	typeKeys(m, "e")
	if len(m.comments) != 1 || m.comments[0].placed() || len(m.pendingComments()) != 0 || m.commentCount("a.go") != 0 {
		t.Fatalf("after the rebuild: %d comments, placed %t, %d pending groups; want one without a place and none pending",
			len(m.comments), len(m.comments) == 1 && m.comments[0].placed(), len(m.pendingComments()))
	}
	typeKeys(m, "gg<enter>")
	if text := boxText(m); !strings.Contains(text, "EARLIER COMMENTS · 1\n> Changed 2\n") {
		t.Errorf("the header's box:\n%s", text)
	}
}

func TestRebuildClosesComment(t *testing.T) {
	// A rebuild closes the input box of c. When no changed file is left, c does nothing.
	m := editScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, &fakeEditor{})
	typeKeys(m, "4jcfix")
	send(m, editedMsg{})
	if m.drafting != nil || m.entry.text != "" || findRow(m, "+Line 5 edited") < 0 {
		t.Errorf("box open %t with %q after the rebuild, want it closed", m.drafting != nil, m.entry.text)
	}

	m = editScreen(testFiles(), Saved{Reload: reloading(nil)}, &fakeEditor{})
	typeKeys(m, "4jcfix<enter>e")
	empty := screen(t, m)
	if len(m.files) != 0 || !strings.Contains(empty, noChanges) {
		t.Fatalf("%d files on the screen\n%s\nwant none", len(m.files), empty)
	}
	typeKeys(m, "c")
	if m.drafting != nil || screen(t, m) != empty {
		t.Errorf("c with no changed file: box open %t, screen\n%s\nwant nothing to change", m.drafting != nil, screen(t, m))
	}
}

func TestCommentSplitRange(t *testing.T) {
	// In the split view a block -oldA -oldB +newA +newB pairs oldA with newA and oldB with
	// newB. The second row alone takes a comment on newB: newA, between its lines in the
	// diff, is not in its range and need not come from its session, while every line it
	// selected must. A selection whose range holds a changed line of another session, or of
	// no origin, is refused.
	codex := &agent.Edit{Agent: "codex", Session: "c0dex000-3333", Turn: 1, Time: start.Add(5 * time.Minute), Cwd: "/work"}
	// block returns f.go with the block, all of it claude #1's but line k of the diff, which
	// e wrote. Its split rows are the header, the hunk header, x, oldA|newA, oldB|newB and y.
	block := func(k int, e *agent.Edit) []review.File {
		lines := parseDiff("x", "-oldA", "-oldB", "+newA", "+newB", "y")
		for i := 1; i <= 4; i++ {
			lines[i].Edit = claude1
		}
		lines[k].Edit = e
		return []review.File{{Path: "f.go", Status: 'M', Lines: lines}}
	}
	const newB = "╭─ COMMENT  → claude c1a0 · L3 "
	for _, tc := range []struct {
		name, keys string
		k          int
		e          *agent.Edit
		want       string
	}{
		{"the second row, newA another session's", "s4jc", 3, codex, newB},
		{"the second row, newA of no origin", "s4jc", 3, nil, newB},
		{"the second row, oldB another session's", "s4jc", 2, codex, "The selection mixes sessions. Select lines from one session"},
		{"both rows, newA another session's", "s3jVjc", 3, codex, "The selection mixes sessions. Select lines from one session"},
		{"both rows, newA of no origin", "s3jVjc", 3, nil, "Lines without an origin cannot take a comment. Fix them with e"},
	} {
		m := commentScreen(block(tc.k, tc.e), t.TempDir())
		typeKeys(m, tc.keys)
		if s := screen(t, m); !m.splitShown || !strings.Contains(s, tc.want) {
			t.Errorf("%s: the screen\n%s\nhas no %q", tc.name, s, tc.want)
		}
	}

	m := commentScreen(block(3, codex), t.TempDir())
	typeKeys(m, "s4jcfix<enter>")
	g := m.pendingComments()
	if len(g) != 1 || g[0].session != claudeSession || g[0].comments[0].first != 3 || g[0].comments[0].last != 3 ||
		!slices.Equal(g[0].comments[0].current, []string{"newB"}) {
		t.Fatalf("pending %+v, want the comment on newB alone", g)
	}
	if s := screen(t, m); !strings.Contains(s, "· f.go:3    pending") {
		t.Errorf("the box does not show f.go:3:\n%s", s)
	}
	// In the unified view newA is not a line of the comment: c there makes a new comment for
	// codex. oldB and newB are its lines.
	typeKeys(m, "sgg5jc")
	if title := shownTitle(entryBox(t, m)); m.drafting == nil || m.drafting.editing != nil || title != "COMMENT → codex 3333 · L2" {
		t.Errorf("c on newA: the box titled %q, want a new comment for codex", title)
	}
	for _, keys := range []string{"<esc>gg4jc", "<esc>gg6jc"} {
		typeKeys(m, keys)
		if m.drafting == nil || m.drafting.editing != m.comments[0] {
			t.Errorf("%q: want the comment edited", keys)
		}
	}
}

func TestCommentAcrossHunks(t *testing.T) {
	// A selection across two hunks of a file covers the lines the diff hides between them:
	// the comment's lines in the current file run from its first line to its last, and its
	// fix request quotes them all.
	files := testFiles()
	f := &fakeAsk{answer: answer}
	m := fixScreen(files, Saved{}, f, writeTree(t, files), fixClock())
	typeKeys(m, "4jV5jcwhole<enter>") // rows 4 to 9: +Changed 2 to line 12, over a hunk header
	want := []string{"Changed 2"}
	for n := 3; n <= 12; n++ {
		want = append(want, "line "+strconv.Itoa(n))
	}
	g := m.pendingComments()
	if len(g) != 1 || g[0].comments[0].first != 2 || g[0].comments[0].last != 12 || !slices.Equal(g[0].comments[0].current, want) {
		t.Fatalf("pending %+v, want lines 2 to 12: %q", g, want)
	}
	typeAll(m, ":submit<enter>")
	quoted := "\n1. a.go:2-12\n>" + strings.Join(want, "\n>") + "\nRequest: whole\n"
	if asked := f.asked(); len(asked) != 1 || !strings.Contains(asked[0].Prompt, quoted) {
		t.Errorf("asked %+v, want %q", asked, quoted)
	}
}
