package tui

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// viewedScreen returns the screen on testFiles in a 125×24 terminal, with files marked
// viewed before, which keeps what it saves in the state directory dir.
func viewedScreen(t *testing.T, dir string, files state.Viewed) *model {
	t.Helper()
	m := New(testFiles(), nil, Saved{Dir: dir, Viewed: files}, Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	return m
}

// send sends msgs to m and runs each command they return, as Bubble Tea does, sending back
// what it returns: the save of the viewed files after v.
func send(m *model, msgs ...tea.Msg) {
	for len(msgs) > 0 {
		_, cmd := m.Update(msgs[0])
		msgs = msgs[1:]
		if cmd != nil {
			msgs = append([]tea.Msg{cmd()}, msgs...)
		}
	}
}

// typeKeys types keys, written as for keyPresses, into m like send.
func typeKeys(m *model, keys string) {
	for _, k := range keyPresses(keys) {
		send(m, k)
	}
}

// statusLine returns the status line of m without its colors.
func statusLine(t *testing.T, m *model) string {
	t.Helper()
	lines := strings.Split(screen(t, m), "\n")
	return lines[len(lines)-1]
}

func TestViewedFold(t *testing.T) {
	// v on a.go folds it to its header. The rows are then its header, b.bin and its note,
	// and c/new.go with its hunk header and its 4 lines, in the split view too, and moving
	// does not see the rows that folded. Folding c/new.go leaves the other 22 rows, 21 in
	// the split view.
	for _, tc := range []struct {
		keys         string
		split        bool
		rows, cursor int
		status       string // in the status line
	}{
		{keys: "v", rows: 9, cursor: 0, status: "row 1/9"},
		{keys: "5jv", rows: 9, cursor: 0}, // from inside the file to its header
		{keys: "vv", rows: 27, cursor: 0}, // unfolded with the cursor on its header
		{keys: "5jvv", rows: 27, cursor: 0},
		{keys: "vj", rows: 9, cursor: 1},
		{keys: "v3j", rows: 9, cursor: 3},
		{keys: "v}", rows: 9, cursor: 5}, // the first change of c/new.go
		{keys: "vG{", rows: 9, cursor: 5},
		{keys: "vl", rows: 9, cursor: 1},
		{keys: "vGh", rows: 9, cursor: 3},
		{keys: "vG3h", rows: 9, cursor: 0}, // h stops on the folded header
		{keys: "v/line 1<enter>", rows: 9, cursor: 0, status: "Not found: line 1"},
		{keys: "v/new.go<enter>", rows: 9, cursor: 3}, // a header still has its path
		{keys: "Gv", rows: 22, cursor: 21},
		{keys: "sv", split: true, rows: 9, cursor: 0},
		{keys: "s4jv", split: true, rows: 9, cursor: 0},
		{keys: "svv", split: true, rows: 26, cursor: 0},
		{keys: "sv}", split: true, rows: 9, cursor: 5},
		{keys: "sGv", split: true, rows: 21, cursor: 20},
		{keys: "s5jvs", rows: 9, cursor: 0}, // back to the unified view, still folded
	} {
		m := viewedScreen(t, t.TempDir(), nil)
		typeKeys(m, tc.keys)
		if m.splitShown != tc.split || len(m.rows) != tc.rows || m.cursor != tc.cursor {
			t.Errorf("%q: split view %t with %d rows and the cursor at row %d, want %t, %d and row %d",
				tc.keys, m.splitShown, len(m.rows), m.cursor, tc.split, tc.rows, tc.cursor)
		}
		if m.cursor < m.top || cursorLine(m) >= m.diffHeight() {
			t.Errorf("%q: cursor at row %d, line %d of the screen from row %d, is off the screen", tc.keys, m.cursor, cursorLine(m), m.top)
		}
		if status := statusLine(t, m); !strings.Contains(status, tc.status) {
			t.Errorf("%q: status line %q has no %q", tc.keys, status, tc.status)
		}
	}
}

func TestHeader(t *testing.T) {
	// The path is at the left edge of a file header. At the right edge, a cell from it, come
	// that the diff changed since the file was viewed, the counts with the diffstat, and the
	// Viewed box, which always shows. The rest shows only when it leaves the path its room.
	files := testFiles()
	for _, tc := range []struct {
		name        string
		file, w     int
		viewed      state.Viewed
		prefix      string
		suffix, not string
	}{
		{name: "not viewed", w: 80, prefix: "  ▾ a.go ", suffix: " +2 -2 ■■■■□  ☐ Viewed "},
		{name: "viewed", w: 80, viewed: state.Viewed{"a.go": files[0].Hash(contextLines)}, prefix: "  ▸ a.go ", suffix: " +2 -2 ■■■■□  ☑ Viewed "},
		{
			name: "changed", w: 80, viewed: state.Viewed{"a.go": "1111"},
			prefix: "  ▾ a.go ", suffix: " Changed since last view  +2 -2 ■■■■□  ☐ Viewed ",
		},
		{name: "no line diff", file: 1, w: 80, prefix: "  ▾ b.bin ", suffix: " ☐ Viewed ", not: "+0"},
		{name: "no room to say it changed", w: 40, viewed: state.Viewed{"a.go": "1111"}, prefix: "  ▾ a.go ", suffix: " +2 -2 ■■■■□  ☐ Viewed ", not: "Changed"},
		{name: "no room for the counts", w: 20, prefix: "  ▾ a.go ", suffix: " ☐ Viewed ", not: "+2"},
	} {
		m := New(files, nil, Saved{Viewed: tc.viewed}, Asker{}).(*model)
		line := ansi.Strip(m.drawHeader(m.files[tc.file], tc.w, false))
		if !strings.HasPrefix(line, tc.prefix) || !strings.HasSuffix(line, tc.suffix) || tc.not != "" && strings.Contains(line, tc.not) || ansi.StringWidth(line) != tc.w {
			t.Errorf("%s: got %q, %d cells, want %q at the left, %q at the right, no %q, %d cells", tc.name, line, ansi.StringWidth(line), tc.prefix, tc.suffix, tc.not, tc.w)
		}
	}
}

func TestViewedSave(t *testing.T) {
	// a.go was marked viewed with the diff it has now, so it opens folded. c/new.go was
	// marked with another diff, so it opens unfolded and says its diff changed. gone.go is
	// not in this diff and stays saved.
	files := testFiles()
	a, c := files[0].Hash(contextLines), files[2].Hash(contextLines)
	dir := filepath.Join(t.TempDir(), "dr")
	path := filepath.Join(dir, "viewed.json")
	m := viewedScreen(t, dir, state.Viewed{"a.go": a, "c/new.go": "1111", "gone.go": "2222"})
	if len(m.rows) != 9 || !m.files[0].viewed || m.files[2].viewed || !m.files[2].changed {
		t.Fatalf("got %d rows, a.go viewed %t, c/new.go viewed %t and changed %t; want 9 rows, a.go viewed, c/new.go changed",
			len(m.rows), m.files[0].viewed, m.files[2].viewed, m.files[2].changed)
	}
	if line := ansi.Strip(m.drawRow(3, 125)); !strings.Contains(line, "Changed since last view") {
		t.Errorf("the header of c/new.go %q does not say its diff changed", line)
	}
	for _, step := range []struct {
		keys string
		want state.Viewed
	}{
		{"3jv", state.Viewed{"a.go": a, "c/new.go": c, "gone.go": "2222"}}, // marked again with its diff now
		{"ggv", state.Viewed{"c/new.go": c, "gone.go": "2222"}},
		{"v", state.Viewed{"a.go": a, "c/new.go": c, "gone.go": "2222"}},
	} {
		typeKeys(m, step.keys)
		if got, err := state.Load(path); err != nil || !maps.Equal(got, step.want) {
			t.Errorf("after %q: saved %v, %v, want %v", step.keys, got, err, step.want)
		}
	}
	if m.files[2].changed {
		t.Error("c/new.go still says its diff changed after v")
	}
}

func TestViewedErrors(t *testing.T) {
	// What could not be read from the state directory is said until the next key: a file
	// of viewed files that is not JSON, and lines of the log that are not records.
	dir := t.TempDir()
	for name, data := range map[string]string{"viewed.json": `{"a.go": "11`, "log.jsonl": "not a record\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New(testFiles(), nil, Load(dir), Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	const read = "could not read viewed state: parse " // the path, then why
	const log = "; skipped 1 unreadable line of log.jsonl"
	if status := statusLine(t, m); !strings.HasPrefix(m.message, read) || !strings.HasSuffix(m.message, log) || !strings.Contains(status, read) {
		t.Errorf("message %q, in the status line %q, want %q, then %q", m.message, status, read, log)
	}
	if typeKeys(m, "j"); strings.Contains(statusLine(t, m), "could not") {
		t.Errorf("status line %q still has the problem after a key", statusLine(t, m))
	}

	// A save that fails is said in the status line, and the file stays folded.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m = viewedScreen(t, filepath.Join(file, "dr"), nil)
	typeKeys(m, "v")
	const save = "could not save viewed state: mkdir "
	if status := statusLine(t, m); !strings.Contains(status, save) || !m.files[0].viewed || len(m.rows) != 9 {
		t.Errorf("status line %q, a.go viewed %t with %d rows; want %q, a.go viewed with 9 rows", status, m.files[0].viewed, len(m.rows), save)
	}
}

func TestSaver(t *testing.T) {
	// Saves that end out of order leave the newest in the file.
	path := filepath.Join(t.TempDir(), "viewed.json")
	s := &saver{path: path}
	for _, save := range []struct {
		version int
		v       state.Viewed
	}{{2, state.Viewed{"a.go": "2222"}}, {1, state.Viewed{"a.go": "1111"}}} {
		if err := s.save(save.version, save.v); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := state.Load(path); err != nil || got["a.go"] != "2222" {
		t.Errorf("got %v, %v, want version 2", got, err)
	}
}

func TestTreeLines(t *testing.T) {
	// The file tree shows a status icon and the name of each file, no line counts, and a
	// check at the right edge of a viewed file. Its title counts the viewed files.
	files := append(testFiles(), review.File{Path: "gone.go", Status: 'D', Lines: origin.Blame("x\n", "", nil)})
	m := New(files, nil, Saved{Viewed: state.Viewed{"a.go": files[0].Hash(contextLines)}}, Asker{}).(*model)
	want := []string{"1 / 4 files viewed", "± a.go ✓", "+ b.bin", "▾ c", "+ new.go", "− gone.go"}
	got := m.fileTree(m.listWidth, len(want))
	for i, line := range got {
		if text := strings.Join(strings.Fields(strings.Trim(ansi.Strip(line), "▌")), " "); text != want[i] || ansi.StringWidth(line) != m.listWidth {
			t.Errorf("tree line %d: got %q, %d cells, want %q, %d cells", i, text, ansi.StringWidth(line), want[i], m.listWidth)
		}
	}
}

func TestClick(t *testing.T) {
	// On the 125-cell screen the file tree is 20 cells wide: its title on line 0, then a.go,
	// b.bin, c and new.go. The Viewed box of a header is at columns 116 to 123. The header
	// of b.bin, row 19, is on line 20, under the blank line above it.
	click := func(x, y int) tea.Msg { return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft} }
	for _, tc := range []struct {
		name         string
		keys         string
		x, y         int
		cursor, rows int
		viewed       []bool
	}{
		{name: "a file of the tree", x: 5, y: 2, cursor: 19, rows: 27},
		{name: "a file in a directory", x: 5, y: 4, cursor: 21, rows: 27},
		{name: "a directory", x: 5, y: 3, cursor: 0, rows: 27},
		{name: "the title", x: 5, y: 0, cursor: 0, rows: 27},
		{name: "the Viewed box", x: 116, y: 0, cursor: 0, rows: 9, viewed: []bool{true, false, false}},
		{name: "its right end", x: 123, y: 0, cursor: 0, rows: 9, viewed: []bool{true, false, false}},
		{name: "beside it", x: 115, y: 0, cursor: 0, rows: 27},
		{name: "the Viewed box of another file", keys: "3j", x: 120, y: 20, cursor: 19, rows: 26, viewed: []bool{false, true, false}},
		{name: "the blank line above it", x: 120, y: 19, cursor: 0, rows: 27},
		{name: "a hunk header", x: 120, y: 1, cursor: 0, rows: 27},
		{name: "the status line", x: 120, y: 23, cursor: 0, rows: 27},
		{name: "the help open", keys: "?", x: 120, y: 0, cursor: 0, rows: 27},
		{name: "unfolded again", keys: "v", x: 120, y: 0, cursor: 0, rows: 27},
	} {
		m := viewedScreen(t, t.TempDir(), nil)
		typeKeys(m, tc.keys)
		send(m, click(tc.x, tc.y))
		viewed := []bool{m.files[0].viewed, m.files[1].viewed, m.files[2].viewed}
		if tc.viewed == nil {
			tc.viewed = make([]bool, 3)
		}
		if m.cursor != tc.cursor || len(m.rows) != tc.rows || !slices.Equal(viewed, tc.viewed) {
			t.Errorf("%s: cursor at row %d of %d, viewed %v; want row %d of %d, viewed %v", tc.name, m.cursor, len(m.rows), viewed, tc.cursor, tc.rows, tc.viewed)
		}
	}
}
