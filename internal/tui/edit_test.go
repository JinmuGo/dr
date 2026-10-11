package tui

import (
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// fakeEditor stands for tea.ExecProcess: it keeps the commands it gets and, without running
// them, returns the command that tells the screen the editor closed with err.
type fakeEditor struct {
	cmds []*exec.Cmd
	err  error
}

func (f *fakeEditor) execute(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	f.cmds = append(f.cmds, c)
	return func() tea.Msg { return fn(f.err) }
}

// editScreen returns the screen on files in a 125×24 terminal whose editor is ed. It asks
// through a fakeAsk that answers with answer.
func editScreen(files []review.File, saved Saved, ed *fakeEditor) *model {
	m := askScreen(files, saved, &fakeAsk{answer: answer})
	m.execute = ed.execute
	return m
}

// reloading returns a Reload that reads files.
func reloading(files []review.File) func() ([]review.File, []agent.Problem, error) {
	return func() ([]review.File, []agent.Problem, error) { return files, nil, nil }
}

// editedFiles returns testFiles after a person changed line 5 of a.go to "Line 5 edited".
// The lines keep their numbers, but the -line 5 and +Line 5 edited rows push the rows of
// a.go after them down.
func editedFiles() []review.File {
	var base, current strings.Builder
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&base, "line %d\n", i)
		switch i {
		case 2:
			current.WriteString("Changed 2\n")
		case 5:
			current.WriteString("Line 5 edited\n")
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
	files := testFiles()
	files[0].Lines = a
	return files
}

// rowName names row i of m: a file header by its path, and a line row by its line on the
// right, " " for an unchanged and "+" for an added one, or by "-" and its deleted line.
func rowName(m *model, i int) string {
	r := m.rows[i]
	switch {
	case r.kind == fileRow:
		return m.files[r.file].key
	case r.kind != lineRow:
		return "not a line"
	case r.right >= 0 && r.left == r.right:
		return " " + m.lines[r.right].Text
	case r.right >= 0:
		return "+" + m.lines[r.right].Text
	}
	return "-" + m.lines[r.left].Text
}

// findRow returns the row of m that rowName names name, or -1.
func findRow(m *model, name string) int {
	for i := range m.rows {
		if rowName(m, i) == name {
			return i
		}
	}
	return -1
}

func TestEditorLine(t *testing.T) {
	// e opens the editor at the line of the current file the cursor row stands for, in the
	// file under the worktree root.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	replaced := []review.File{{Path: "f.txt", Status: 'M', Lines: parseDiff("x", "-d1", "-d2", "+a1", "+a2", "y")}}
	atEnd := []review.File{{Path: "f.txt", Status: 'M', Lines: parseDiff("x", "y", "-z")}}
	for _, tc := range []struct {
		name  string
		files []review.File
		keys  string
		want  string // the line argument
		path  string
	}{
		{"added line", testFiles(), "4j", "+2", "/repo/a.go"},
		{"added line after a deletion", testFiles(), "16j", "+18", "/repo/a.go"},
		{"unchanged line", testFiles(), "9j", "+12", "/repo/a.go"},
		{"deleted line: the next current line", testFiles(), "12j", "+15", "/repo/a.go"},
		{"deleted line paired with an added one", testFiles(), "3j", "+2", "/repo/a.go"},
		{"deleted line at the end: the line before", atEnd, "4j", "+2", "/repo/f.txt"},
		{"file header: its first changed line", testFiles(), "", "+2", "/repo/a.go"},
		{"header of a new file", testFiles(), "21j", "+1", "/repo/c/new.go"},
		{"hunk header: its first line", testFiles(), "8j", "+12", "/repo/a.go"},
		{"selection: the cursor line", testFiles(), "3jVjj", "+3", "/repo/a.go"},
		{"unified, second of two deleted lines", replaced, "4j", "+2", "/repo/f.txt"},
		{"split, its row has a line on the right", replaced, "4js", "+3", "/repo/f.txt"},
		{"split, deleted line alone on its row", testFiles(), "12js", "+15", "/repo/a.go"},
	} {
		ed := &fakeEditor{}
		m := editScreen(tc.files, Saved{}, ed)
		typeKeys(m, tc.keys+"e")
		want := []string{"sh", "-c", `vi "$@"`, "vi", tc.want, tc.path}
		if len(ed.cmds) != 1 || !slices.Equal(ed.cmds[0].Args, want) {
			var got [][]string
			for _, c := range ed.cmds {
				got = append(got, c.Args)
			}
			t.Errorf("%s: ran %q, want %q", tc.name, got, want)
		}
	}
}

func TestEditorChoice(t *testing.T) {
	// $VISUAL comes first, then $EDITOR, then vi. An editor with arguments runs whole
	// through sh.
	for _, tc := range []struct {
		visual, editor string
		want           string
	}{
		{"nvim", "nano", "nvim"},
		{"", "code --wait", "code --wait"},
		{"", "", "vi"},
	} {
		t.Setenv("VISUAL", tc.visual)
		t.Setenv("EDITOR", tc.editor)
		ed := &fakeEditor{}
		m := editScreen(testFiles(), Saved{}, ed)
		typeKeys(m, "4je")
		want := []string{"sh", "-c", tc.want + ` "$@"`, tc.want, "+2", "/repo/a.go"}
		if len(ed.cmds) != 1 || !slices.Equal(ed.cmds[0].Args, want) {
			t.Errorf("VISUAL %q, EDITOR %q: ran %v, want %q", tc.visual, tc.editor, ed.cmds, want)
		}
	}
}

func TestNothingToEdit(t *testing.T) {
	// A deleted file, a binary file and a directory have no lines to edit: no editor opens,
	// and the status line says why.
	files := []review.File{
		{Path: "b.bin", Status: 'A', NoDiff: review.Binary},
		{Path: "gone.go", Status: 'D', Lines: parseDiff("-a", "-b")},
		{Path: "sub", Status: 'A', NoDiff: review.Directory},
	}
	for _, keys := range []string{"", "j", "2j", "3j", "4j", "5j", "6j", "7j"} { // the headers, their lines and notes
		ed := &fakeEditor{}
		m := editScreen(files, Saved{Reload: reloading(testFiles())}, ed)
		typeKeys(m, keys+"e")
		if len(ed.cmds) != 0 || m.message == "" || len(m.files) != 3 {
			t.Errorf("%q: on %s ran %d commands with %q in the status line; want none and why", keys, rowName(m, m.cursor), len(ed.cmds), m.message)
		}
	}
}

func TestRebuildCursor(t *testing.T) {
	// After the editor closes the diff is read again. The cursor stays on its line of the
	// current file, a deleted line on that deleted line, a header on its header, though
	// rows above them came, and it stays as far from the top of the screen.
	for _, tc := range []struct {
		keys string
		want string // rowName of the cursor row
	}{
		{"16j", "+new after 18"},
		{"16js", "+new after 18"},
		{"9j", " line 12"},
		{"12j", "-line 15"},
		{"12js", "-line 15"},
		{"21j", "c/new.go"},
	} {
		ed := &fakeEditor{}
		m := editScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, ed)
		typeKeys(m, tc.keys)
		before, onScreen := m.cursor, cursorLine(m)
		typeKeys(m, "e")
		if got := rowName(m, m.cursor); got != tc.want || m.cursor == before || cursorLine(m) != onScreen || m.message != "" {
			t.Errorf("%q: the cursor is on row %d, %q, line %d of the screen, with %q in the status line; want it on %q, moved from row %d, on line %d and no message",
				tc.keys, m.cursor, got, cursorLine(m), m.message, tc.want, before, onScreen)
		}
		if findRow(m, "+Line 5 edited") < 0 {
			t.Errorf("%q: the diff was not rebuilt", tc.keys)
		}
	}
}

func TestRebuildNearest(t *testing.T) {
	// When the cursor's line left the diff, the cursor goes to the nearest row of its file.
	// When its file left the diff, it goes to the header of the next file, or of the last.
	// A file that joined the diff shows.
	for _, tc := range []struct {
		name  string
		keys  string
		files []review.File
		want  string
	}{
		{"file gone, the next one", "16j", testFiles()[1:], "b.bin"},
		{"file gone, the next one after a header", "19j", []review.File{testFiles()[0], testFiles()[2]}, "c/new.go"},
		{"file gone, the last one", "23j", testFiles()[:2], "b.bin"},
		{"line gone from its file", "16j", []review.File{{Path: "a.go", Status: 'M', Lines: parseDiff("line 1", "-line 2", "+Changed 2", "line 3")}}, " line 3"},
	} {
		m := editScreen(testFiles(), Saved{Reload: reloading(tc.files)}, &fakeEditor{})
		typeKeys(m, tc.keys)
		send(m, editedMsg{}) // as the editor closing, since e opens none on b.bin
		if got := rowName(m, m.cursor); got != tc.want || len(m.files) != len(tc.files) {
			t.Errorf("%s: the cursor is on %q with %d files; want it on %q with %d", tc.name, got, len(m.files), tc.want, len(tc.files))
		}
	}

	ed := &fakeEditor{}
	m := editScreen(testFiles(), Saved{Reload: reloading(askFiles())}, ed)
	typeKeys(m, "e")
	if len(m.files) != 5 || findRow(m, "d.go") < 0 || !slices.ContainsFunc(m.tree, func(r treeRow) bool { return r.file == 4 }) {
		t.Errorf("after d.go and e.go joined: %d files, d.go at row %d; want 5 files and both in the tree", len(m.files), findRow(m, "d.go"))
	}
}

func TestRebuildKeeps(t *testing.T) {
	// A rebuild keeps the split view, the file tree's width and fold, the search and the
	// viewed marks, which follow their hashes: a.go, edited, unfolds as changed since last
	// view, and c/new.go stays viewed.
	ed := &fakeEditor{}
	m := editScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, ed)
	send(m, drag(20, 30)...)
	send(m, drag(30, 5)...)
	typeKeys(m, "21jvggvs/line 1<enter>gg")
	send(m, editedMsg{})
	if !m.splitShown || m.search.query != "line 1" || m.treeSize != 30 || !m.folded {
		t.Errorf("split %t, query %q, tree %d cells wide, folded %t; want split, \"line 1\", 30, folded", m.splitShown, m.search.query, m.treeSize, m.folded)
	}
	if a, c := m.files[0], m.files[2]; a.viewed || !a.changed || !c.viewed || c.changed || findRow(m, "+Line 5 edited") < 0 {
		t.Errorf("a.go viewed %t, changed %t; c/new.go viewed %t, changed %t; want a.go unfolded and changed, c/new.go viewed", a.viewed, a.changed, c.viewed, c.changed)
	}
}

func TestRebuildCloses(t *testing.T) {
	// A rebuild closes the origin box, a selection and the input box of a with what is typed
	// in it, the one a opened from the origin box too, which then stays closed.
	for _, keys := range []string{"4j<enter>", "4jVj", "4jawhy<alt+enter>this", "4j<enter>awhy"} {
		m := editScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, &fakeEditor{})
		typeKeys(m, keys)
		send(m, editedMsg{})
		if m.reading || m.selecting || m.typing() || m.entry.text != "" || findRow(m, "+Line 5 edited") < 0 || m.View().Cursor != nil {
			t.Errorf("%q: origin box open %t, selecting %t, input box open %t with %q after the rebuild", keys, m.reading, m.selecting, m.typing(), m.entry.text)
		}
	}
}

func TestRebuildQuestions(t *testing.T) {
	// The questions of earlier runs and of this one go to their lines in the new diff. A
	// question still waiting keeps waiting there, and its answer lands there.
	line := func(kind, text string) state.Line { return state.Line{Kind: kind, Text: text} }
	earlier := state.Records{
		Questions: []state.Question{{
			ID: "q1", Agent: "claude", Session: claudeSession, Path: "a.go", Text: "Why Changed?",
			Before: []state.Line{line(" ", "line 1"), line("-", "line 2")}, Lines: []state.Line{line("+", "Changed 2")},
			After: []state.Line{line(" ", "line 3"), line(" ", "line 4")},
		}},
		Answers: map[string]state.Answer{"q1": {Question: "q1", Text: "Because."}},
	}
	m := editScreen(testFiles(), Saved{Log: earlier, Reload: reloading(editedFiles())}, &fakeEditor{})
	cmd := typeAndKeep(m, "16ja<enter>")
	asked := slices.Clone(m.questions[1].lines)
	typeKeys(m, "<esc>e")
	row := findRow(m, "+new after 18")
	if q := m.questions[1]; slices.Equal(q.lines, asked) || !slices.Equal(m.questionsAt(row), []*question{q}) || !q.pending {
		t.Fatalf("after the rebuild the question is on lines %v, was on %v; +new after 18 has questions %v, want the one waiting", q.lines, asked, m.questionsAt(row))
	}
	if qs := m.questionsAt(findRow(m, "+Changed 2")); len(qs) != 1 || qs[0].ID != "q1" {
		t.Errorf("+Changed 2 has questions %v, want q1", qs)
	}
	send(m, cmd())
	q := m.questions[1]
	if q.pending || q.answer == nil || q.answer.Text != answer.Text || m.notice == nil || m.notice.question != q {
		t.Fatalf("the answer did not land: pending %t, answer %v, notice %q", q.pending, q.answer, noticeLines(t, m))
	}
	typeKeys(m, "<enter>")
	if !m.reading || m.cursor != row || !strings.Contains(boxText(m), "토큰 버킷이라") {
		t.Errorf("Enter: box open %t at row %d with\n%s\nwant it open at row %d with the answer", m.reading, m.cursor, boxText(m), row)
	}
}

func TestRebuildQuestionLeaves(t *testing.T) {
	// A question still waiting about a file that left the diff has no place; its answer
	// comes all the same, and a notice tells of it, with nothing to read.
	m := editScreen(testFiles(), Saved{Reload: reloading(testFiles()[:2])}, &fakeEditor{})
	cmd := typeAndKeep(m, "23ja<enter>")
	typeKeys(m, "<esc>e")
	send(m, cmd())
	q := m.questions[0]
	want := []string{"● Answer from claude c1a0 #2 on c/new.go", "  Its file left the diff."}
	if got := noticeLines(t, m); q.file >= 0 || q.answer == nil || !slices.Equal(got, want) || m.notice.question != nil {
		t.Errorf("file %d, answer %v, notice %q; want no place, the answer, and the notice %q with nothing to read", q.file, q.answer, got, want)
	}

	// When the question's file comes back without its lines, it waits under the header,
	// where Enter on the notice goes.
	m = editScreen(testFiles(), Saved{}, &fakeEditor{})
	files := testFiles()
	files[2].Lines = parseDiff("+other")
	m.reload = reloading(files)
	cmd = typeAndKeep(m, "23ja<enter>")
	typeKeys(m, "<esc>e")
	send(m, cmd())
	typeKeys(m, "<enter>")
	if q := m.questions[0]; q.lines != nil || m.cursor != findRow(m, "c/new.go") || !m.reading || !strings.Contains(boxText(m), "토큰 버킷이라") {
		t.Errorf("lines %v, box open %t on %q with\n%s\nwant it open on the header of c/new.go with the answer", q.lines, m.reading, rowName(m, m.cursor), boxText(m))
	}
}

func TestRebuildFails(t *testing.T) {
	// When the files cannot be read again, the diff stays and the status line says why. An
	// editor that fails still rebuilds the diff, and the status line tells of the failure.
	fail := errors.New("git: not a repository")
	for _, tc := range []struct {
		name    string
		reload  func() ([]review.File, []agent.Problem, error)
		editErr error
		rebuilt bool
	}{
		{"read fails", func() ([]review.File, []agent.Problem, error) { return nil, nil, fail }, nil, false},
		{"editor fails", reloading(editedFiles()), errors.New("exit status 1"), true},
		{"no way to read", nil, errors.New("exit status 1"), false},
	} {
		ed := &fakeEditor{err: tc.editErr}
		m := editScreen(testFiles(), Saved{Reload: tc.reload}, ed)
		typeKeys(m, "16je")
		rebuilt := findRow(m, "+Line 5 edited") >= 0
		if rebuilt != tc.rebuilt || m.message == "" || rowName(m, m.cursor) != "+new after 18" || len(m.files) != 3 {
			t.Errorf("%s: rebuilt %t with the cursor on %q and %q in the status line; want rebuilt %t, the cursor kept, and why", tc.name, rebuilt, rowName(m, m.cursor), m.message, tc.rebuilt)
		}
	}
}

func TestRebuildOutOfOrder(t *testing.T) {
	// Two readings of the diff are asked for, and the older one comes last: the diff keeps
	// the files of the newer one.
	var reads int
	reload := func() ([]review.File, []agent.Problem, error) {
		reads++
		if reads == 1 {
			return testFiles(), nil, nil
		}
		return editedFiles(), nil, nil
	}
	m := editScreen(testFiles(), Saved{Reload: reload}, &fakeEditor{})
	_, older := m.Update(editedMsg{})
	_, newer := m.Update(editedMsg{})
	olderMsg, newerMsg := older(), newer()
	send(m, newerMsg)
	if findRow(m, "+Line 5 edited") < 0 {
		t.Fatal("the newer reading did not rebuild the diff")
	}
	send(m, olderMsg)
	if findRow(m, "+Line 5 edited") < 0 {
		t.Error("the older reading, which came last, rebuilt the diff with its files")
	}
}

func TestRebuildEmpty(t *testing.T) {
	// When no changed file is left, as after the last change was undone, the diff says so,
	// and the status line has only its pill and the questions out. An answer still in flight
	// comes all the same, and a notice tells of it. Then keys and the mouse do nothing, but ?
	// opens the help and q quits.
	ed := &fakeEditor{}
	m := editScreen(testFiles(), Saved{Reload: reloading(nil)}, ed)
	cmd := typeAndKeep(m, "4ja<enter>")
	typeKeys(m, "<esc>e")
	if s := screen(t, m); len(m.files) != 0 || strings.Count(s, noChanges) != 1 || strings.Contains(statusLine(t, m), noChanges) {
		t.Fatalf("%d files on the screen\n%s\nwant none, and %q in the diff alone", len(m.files), s, noChanges)
	}
	send(m, cmd())
	const told = "● Answer from claude c1a0 #1 on a.go Its file left the diff."
	if q := m.questions[0]; q.answer == nil || noticeText(t, m) != told {
		t.Errorf("answer %v, notice %q; want the answer and %q", q.answer, noticeText(t, m), told)
	}

	typeKeys(m, "<esc>")
	empty := screen(t, m)
	check := func(name string, msg tea.Msg) {
		t.Helper()
		if _, cmd := m.Update(msg); cmd != nil || screen(t, m) != empty || m.splitOn || len(ed.cmds) != 1 {
			t.Errorf("%s: command %v, split %t, %d editors run, screen\n%s\nwant nothing to change", name, cmd, m.splitOn, len(ed.cmds), screen(t, m))
		}
	}
	for _, keys := range []string{"j", "k", "G", "gg", "}", "{", "l", "h", "v", "V", "<enter>", "a", "J", "K", "e", "s", "/", "n", "5j"} {
		for _, k := range keyPresses(keys) {
			check(keys, k)
		}
	}
	for name, msg := range map[string]tea.Msg{
		"wheel down":    tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelDown},
		"wheel up":      tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelUp},
		"click":         tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseLeft},
		"click at edge": tea.MouseClickMsg{X: 120, Y: 0, Button: tea.MouseLeft},
		"drag":          tea.MouseMotionMsg{X: 30, Y: 5, Button: tea.MouseLeft},
		"release":       tea.MouseReleaseMsg{X: 30, Y: 5, Button: tea.MouseLeft},
		"tick":          tickMsg{},
	} {
		check(name, msg)
	}

	typeKeys(m, "?")
	if s := screen(t, m); !strings.Contains(s, "╭─ Keys ") {
		t.Errorf("?: the screen has no help:\n%s", s)
	}
	typeKeys(m, "x")
	if s := screen(t, m); s != empty {
		t.Errorf("a key after ?: the screen\n%s\nwant the help closed", s)
	}
	if _, cmd := m.Update(keyPresses("q")[0]); cmd == nil {
		t.Fatal("q: no command, want quit")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q: got %T, want tea.QuitMsg", cmd())
	}
}

func TestCtrlCAfterHandBack(t *testing.T) {
	// Codex exits on one ctrl+c; the second press of a double tap reaches dr right after the
	// terminal comes back and must not quit it. A second later ctrl+c quits, and q always does.
	quits := func(m *model, key string) bool {
		_, cmd := m.Update(keyPresses(key)[0])
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	m := editScreen(testFiles(), Saved{}, &fakeEditor{})
	now := start
	m.now = func() time.Time { return now }
	send(m, editedMsg{})
	if quits(m, "<ctrl+c>") {
		t.Error("ctrl+c right after the hand back: got quit, want nothing")
	}
	if !quits(m, "q") {
		t.Error("q right after the hand back: got no quit, want quit")
	}
	now = now.Add(time.Second)
	if !quits(m, "<ctrl+c>") {
		t.Error("ctrl+c a second after the hand back: got no quit, want quit")
	}
}

func TestCtrlCWhileHandedOver(t *testing.T) {
	// Bubble Tea reads keys again before it sends editedMsg, so a ctrl+c meant for the editor
	// or the agent can reach dr before it, and must not quit it.
	quits := func(m *model, key string) bool {
		_, cmd := m.Update(keyPresses(key)[0])
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	for _, tc := range []struct {
		keys string
		m    *model
	}{
		{"e", editScreen(testFiles(), Saved{}, &fakeEditor{})},
		{"ga", openScreen(testFiles(), Saved{}, nil, &fakeEditor{}, &fakeOpen{})},
	} {
		m := tc.m
		now := start
		m.now = func() time.Time { return now }
		typeKeys(m, "4j")
		var cmd tea.Cmd
		for _, k := range keyPresses(tc.keys) {
			_, cmd = m.Update(k) // keep the command that hands the terminal over unrun
		}
		if cmd == nil {
			t.Fatalf("%s: got no command, want the one that hands the terminal over", tc.keys)
		}
		now = now.Add(time.Minute)
		if quits(m, "<ctrl+c>") {
			t.Errorf("%s: ctrl+c before the hand back: got quit, want nothing", tc.keys)
		}
		send(m, editedMsg{})
		if quits(m, "<ctrl+c>") {
			t.Errorf("%s: ctrl+c right after the hand back: got quit, want nothing", tc.keys)
		}
		now = now.Add(time.Second)
		if !quits(m, "<ctrl+c>") {
			t.Errorf("%s: ctrl+c a second after the hand back: got no quit, want quit", tc.keys)
		}
	}
}

// laterFiles returns testFiles after claude c1a0 rewrote c/new.go in a later turn, #3,
// an edit newer than any of the session's in testFiles.
func laterFiles() []review.File {
	later := *claude2
	later.Turn, later.Time = 3, start.Add(10*time.Minute)
	files := testFiles()
	for i := range files[2].Lines {
		files[2].Lines[i].Edit = &later
	}
	return files
}

func TestRebuildFork(t *testing.T) {
	// A fork knows the session's work up to the base of the question it answered. After a
	// rebuild that brings a newer edit of the session, asking goes to a new fork; after one
	// that does not, it goes on in the fork.
	for _, tc := range []struct {
		name  string
		files []review.File
		fork  string
	}{
		{"a newer edit", laterFiles(), ""},
		{"no newer edit", editedFiles(), "fork-1"},
	} {
		f := &fakeAsk{answer: answer}
		m := askScreen(testFiles(), Saved{Reload: reloading(tc.files)}, f)
		m.execute = (&fakeEditor{}).execute
		typeKeys(m, "4ja<enter><esc>ea<enter>")
		if got := f.asked(); len(got) != 2 || got[0].Fork != "" || got[1].Fork != tc.fork {
			t.Errorf("%s: asked %+v, want a new fork and then %q", tc.name, got, tc.fork)
		}
	}
}

func TestRebuildLateAnswer(t *testing.T) {
	// An answer that comes after a rebuild brought a newer edit of the session keeps its
	// fork for the base its question was asked with, so the next question goes to a new fork.
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{Reload: reloading(laterFiles())}, f)
	m.execute = (&fakeEditor{}).execute
	cmd := typeAndKeep(m, "4ja<enter>")
	typeKeys(m, "<esc>e")
	send(m, cmd())
	typeKeys(m, "a<enter>")
	if got := f.asked(); len(got) != 2 || got[1].Fork != "" {
		t.Errorf("asked %+v, want the second question in a new fork", got)
	}
}
