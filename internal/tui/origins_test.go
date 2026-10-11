package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

const codexSession = "58e2d1a9-2a61-4f0e-b3c4-c6f747d09b1c"

var (
	codex1   = &agent.Edit{Agent: "codex", Session: codexSession, Turn: 1, Time: start.Add(4 * time.Minute), Prompt: "Complete assignment thoroughly:\n# Target\nadd Sweep"}
	claudeFx = &agent.Edit{Agent: "claude", Session: claudeSession, Turn: 1, Time: start.Add(5 * time.Minute), Fix: &agent.Fix{ID: "f1", Comment: "use min()"}}
)

// changedFile returns a file at path whose lines one to n were all rewritten, the old and
// the new line k by edits[k-1], nil for no origin.
func changedFile(path string, edits ...*agent.Edit) review.File {
	var base, current strings.Builder
	for k := range edits {
		base.WriteString("old " + string(rune('a'+k)) + "\n")
		current.WriteString("new " + string(rune('a'+k)) + "\n")
	}
	lines := origin.Blame(base.String(), current.String(), nil)
	for i, l := range lines {
		lines[i].Edit = edits[max(l.Old, l.New)-1]
	}
	return review.File{Path: path, Status: 'M', Lines: lines}
}

func TestSessionColors(t *testing.T) {
	// Sessions take colors in the order they first appear in the diff, then those asked
	// and those commented to. A reload keeps the colors given and gives the next to a new
	// session, though it comes earlier in the diff. Past the sixth, colors cycle.
	m := New(testFiles(), nil, Saved{}, Asker{}).(*model)
	claude, omp, codex := session{"claude", claudeSession}, session{"omp", ompSession}, session{"codex", codexSession}
	asked := session{"claude", "0a5c0000-0000-0000-0000-00000000a5c0"}
	if m.colors[claude] != 0 || m.colors[omp] != 1 || len(m.colors) != 2 {
		t.Fatalf("colors %v, want claude 0 and omp 1", m.colors)
	}
	m.questions = append(m.questions, &question{Question: state.Question{Agent: asked.agent, Session: asked.id}})
	m.setFiles(append([]review.File{changedFile("0.go", codex1)}, testFiles()...))
	if m.colors[claude] != 0 || m.colors[omp] != 1 || m.colors[codex] != 2 || m.colors[asked] != 3 {
		t.Errorf("after a reload: colors %v, want claude 0, omp 1, codex 2 and the session asked 3", m.colors)
	}
	m.colors[asked] = 7
	if got := m.sessionColor(asked); got != m.theme.sessions[1] {
		t.Errorf("color 7 is %v, want the second, %v", got, m.theme.sessions[1])
	}
	if plain := newTheme(nil, true, true); plain.sessions != [6]color.Color{} {
		t.Errorf("without colors the sessions have colors %v", plain.sessions)
	}
}

func TestRunLabels(t *testing.T) {
	// A run is changed lines in a row from one origin; an unchanged line ends it. The first
	// run of each origin in a hunk has the label on its first line.
	a, b := claude1, subagent
	lines := []origin.Line{
		{Old: 1, New: 1},
		{Old: 2, Edit: a}, // the first run of a
		{New: 2, Edit: a},
		{Old: 3, New: 3},
		{New: 4, Edit: a}, // a later run of a
		{New: 5, Edit: b}, // b between runs of a
		{New: 6, Edit: a},
		{New: 7}, // the first run of no origin
		{New: 8},
		{Old: 4, New: 9},
		{New: 10}, // a later run of no origin
		{New: 11, Edit: claudeFx},
	}
	want := []bool{false, true, false, false, false, true, false, true, false, false, false, true}
	if got := runLabels(lines); !slices.Equal(got, want) {
		t.Errorf("runLabels = %v, want %v", got, want)
	}
}

func TestRunLabelDraw(t *testing.T) {
	// The label starts 4 cells after the code: the dot and the tag in the origin's color,
	// then the summary of the request, cut to fit. With less than 8 cells for the summary
	// the tag shows alone, and when the whole dot and tag do not fit nothing does. Row 3 is
	// "-line 2" by claude #1, its code at column 11 and 6 cells wide.
	m := New(testFiles(), nil, Saved{}, Asker{}).(*model)
	for _, tc := range []struct {
		row, w int
		want   string
	}{
		{3, 80, "- line 2    ● claude c1a0 #1  rate limiter를 짜줘"},
		{3, 48, "- line 2    ● claude c1a0 #1  rate li…"},
		{3, 47, "- line 2    ● claude c1a0 #1"},
		{3, 38, "- line 2    ● claude c1a0 #1"},
		{3, 37, "- line 2"},
		{4, 80, "+ Changed 2"}, // the run of row 3 goes on
		{16, 80, "+ new after 18    ┆ unknown  not in any session record"},
	} {
		got := strings.TrimRight(ansi.Strip(m.drawRow(tc.row, tc.w)), " ")
		if !strings.HasSuffix(got, tc.want) || ansi.StringWidth(m.drawRow(tc.row, tc.w)) != tc.w {
			t.Errorf("row %d at %d cells: got %q, want it to end with %q", tc.row, tc.w, got, tc.want)
		}
	}

	// A long tag: request 12, of a session whose id grew to 5 characters to differ from
	// another's. Row 2 is "-old a", its code at column 9 and 5 cells wide.
	long := &agent.Edit{Agent: "claude", Session: "01a1287a-1111-4000-8000-0000000xc1a0", Turn: 12, Prompt: "twelve"}
	other := &agent.Edit{Agent: "claude", Session: "01a1287a-1111-4000-8000-0000000yc1a0", Turn: 1, Prompt: "one"}
	m.setFiles([]review.File{changedFile("e.go", long, other)})
	for _, tc := range []struct {
		w    int
		want string
	}{
		{37, "- old a    ● claude xc1a0 #12"},
		{36, "- old a"},
	} {
		if got := strings.TrimRight(ansi.Strip(m.drawRow(2, tc.w)), " "); !strings.HasSuffix(got, tc.want) {
			t.Errorf("a long tag at %d cells: got %q, want it to end with %q", tc.w, got, tc.want)
		}
	}

	m.setFiles([]review.File{changedFile("d.go", claudeFx)})
	if got := ansi.Strip(m.drawRow(2, 60)); !strings.Contains(got, "◆ claude c1a0 fix  use min()") {
		t.Errorf("a fixed line %q has no label of its fix", got)
	}
}

func TestSplitLabels(t *testing.T) {
	// In the split view a deleted line's label shows on the left and an added line's on the
	// right, each by its run in the unified order: the first row pairs lines of two requests,
	// and the later run of claude #1 has no label on either side.
	lines := []origin.Line{
		{Text: "old a", Old: 1, Edit: claude1},
		{Text: "new a", New: 1, Edit: subagent},
		{Text: "same", Old: 2, New: 2},
		{Text: "old c", Old: 3, Edit: claude1},
		{Text: "new c", New: 3, Edit: claude1},
	}
	m := New([]review.File{{Path: "s.go", Status: 'M', Lines: lines}}, nil, Saved{}, Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	if typeKeys(m, "s"); !m.splitShown {
		t.Fatal("no split view at 200 cells")
	}
	at := func(text string) string {
		i := slices.IndexFunc(m.rows, func(r row) bool { return r.kind == lineRow && r.left >= 0 && m.lines[r.left].Text == text })
		return ansi.Strip(m.drawRow(i, 160))
	}
	left, right, _ := strings.Cut(at("old a"), "│")
	if !strings.Contains(left, "- old a    ● claude c1a0 #1  rate limiter를 짜줘") || !strings.Contains(right, "+ new a    ● omp a1f2 #1  line 15를 지워줘") {
		t.Errorf("the first split row %q|%q, want claude's label on the left and omp's on the right", left, right)
	}
	if row := at("old c"); strings.Contains(row, "●") {
		t.Errorf("the later run of claude #1 %q has a label", row)
	}
}

func TestRail(t *testing.T) {
	// A changed line has a rail between its line numbers and its sign, ▎ in the color of its
	// origin or ┆ for none, and an unchanged line a blank there, in both views. The code,
	// and the text of a hunk header, start after the rail and the sign.
	m := New(append(testFiles(), changedFile("d.go", claudeFx)), nil, Saved{}, Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	f := m.files[0]
	if at := strings.Index(ansi.Strip(m.drawRow(1, 120)), "@@"); at != m.codeColumn(f) {
		t.Errorf("unified hunk header at column %d, want the code column %d", at, m.codeColumn(f))
	}

	for _, tc := range []struct {
		row        int
		rail, code string
		color      color.Color
	}{
		{2, " ", "line 1", nil},
		{3, "▎", "line 2", m.sessionColor(session{"claude", claudeSession})},
		{12, "▎", "line 15", m.sessionColor(session{"omp", ompSession})},
		{16, "┆", "new after 18", m.theme.subtle},
	} {
		line := ansi.Strip(m.drawRow(tc.row, 120))
		at := m.codeColumn(f)
		if rail := ansi.Cut(line, at-3, at-2); rail != tc.rail || !strings.HasPrefix(ansi.Cut(line, at, 120), tc.code) {
			t.Errorf("unified row %d %q: rail %q and code at %d, want %q before %q", tc.row, line, rail, at, tc.rail, tc.code)
		}
		if got := m.rail(m.rows[tc.row].main()).color; got != tc.color {
			t.Errorf("unified row %d: rail color %v, want %v", tc.row, got, tc.color)
		}
	}
	if fix := m.files[3]; m.rail(fix.first+1).color != m.theme.fix {
		t.Errorf("the rail of a fixed line is %v, want the color of fixes", m.rail(fix.first+1).color)
	}

	typeKeys(m, "s")
	if !m.splitShown {
		t.Fatal("no split view at 200 cells")
	}
	if at := strings.Index(ansi.Strip(m.drawRow(f.splitHead+1, 120)), "@@"); at != m.codeColumn(f) {
		t.Errorf("split hunk header at column %d, want the code column %d", at, m.codeColumn(f))
	}
	for _, tc := range []struct{ left, want string }{
		{"line 1", "1    line 1"},
		{"line 2", "2 ▎- line 2"},
		{"line 15", "15 ▎- line 15"},
	} {
		i := slices.IndexFunc(m.rows, func(r row) bool { return r.kind == lineRow && r.left >= 0 && m.lines[r.left].Text == tc.left })
		line := ansi.Strip(m.drawRow(i, 120))
		at := strings.Index(line, tc.left)
		if !strings.HasPrefix(strings.TrimLeft(line, " ▌"), tc.want) || ansi.StringWidth(line[:max(at, 0)]) != m.codeColumn(f) {
			t.Errorf("split row of %q: %q, want %q with the code at column %d", tc.left, line, tc.want, m.codeColumn(f))
		}
	}
	i := slices.IndexFunc(m.rows, func(r row) bool { return r.kind == lineRow && r.right >= 0 && m.lines[r.right].Text == "new after 18" })
	if line := ansi.Strip(m.drawRow(i, 120)); !strings.Contains(line, "┆+ new after 18") {
		t.Errorf("split row of a line without an origin: %q, want ┆ before its sign", line)
	}
}

func TestLegend(t *testing.T) {
	// The legend lists the sessions in the order of their colors with their requests and how
	// many changed lines each wrote, a subagent's parent under it, then the lines without an
	// origin. What does not fit in half the tree is counted as +N more.
	m := New(append(testFiles(), changedFile("d.go", claudeFx)), nil, Saved{}, Asker{}).(*model)
	text := func(lines []string) []string {
		var out []string
		for _, l := range lines {
			out = append(out, strings.Join(strings.Fields(ansi.Strip(l)), " "))
		}
		return out
	}
	full := []string{"Sessions", "● claude c1a0 #1 #2 fix 8", "● omp a1f2 #1 1", "↳ subagent of omp a1f1", "┆ unknown 1"}
	if got := text(m.legendLines(30, 20)); !slices.Equal(got, full) {
		t.Errorf("legend %q, want %q", got, full)
	}
	cut := []string{"", "Sessions", "● claude c1a0 #1 #2 fix 8", "+2 more"}
	if got := text(m.legendLines(30, 8)); !slices.Equal(got, cut) {
		t.Errorf("legend in 8 lines %q, want %q", got, cut)
	}
	for _, l := range m.legendLines(30, 8) {
		if ansi.StringWidth(l) != 30 {
			t.Errorf("legend line %q is not 30 cells wide", ansi.Strip(l))
		}
	}

	// A click on the legend does nothing; one on a file goes to it.
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	h := m.diffHeight()
	m.Update(tea.MouseClickMsg{X: 2, Y: h - 1, Button: tea.MouseLeft})
	if m.cursor != 0 {
		t.Errorf("a click on the legend moved the cursor to row %d", m.cursor)
	}
	m.Update(tea.MouseClickMsg{X: 4, Y: treeHead + 1, Button: tea.MouseLeft})
	if m.cursor != m.files[1].head {
		t.Errorf("a click on b.bin moved the cursor to row %d, want %d", m.cursor, m.files[1].head)
	}
}

func TestHeaderDots(t *testing.T) {
	// A header has a dot for each session that changed its file in the order of their
	// colors, whatever order its lines have, then ┆ for lines without an origin.
	m := New([]review.File{changedFile("a.go", claude1), changedFile("b.go", subagent, claude1, nil)}, nil, Saved{}, Asker{}).(*model)
	on := func(fg color.Color) lipgloss.Style { return m.theme.paint(fg, nil) }
	var got []color.Color
	for _, s := range m.sessionDots(m.files[1], on) {
		if strings.TrimSpace(s.text) != "" {
			got = append(got, s.st.GetForeground())
		}
	}
	want := []color.Color{m.theme.sessions[0], m.theme.sessions[1], m.theme.subtle}
	if !slices.Equal(got, want) {
		t.Errorf("dots of b.go in %v, want %v", got, want)
	}
}

func TestStatusOrigin(t *testing.T) {
	// The status line names the origin of a changed cursor line in its color: the session's,
	// teal for a fix, quiet for none.
	m := New(append(testFiles(), changedFile("d.go", claudeFx)), nil, Saved{}, Asker{}).(*model)
	on := func(fg color.Color) lipgloss.Style { return m.theme.paint(fg, nil) }
	for _, tc := range []struct {
		row       int
		tag, text string
		color     color.Color
	}{
		{3, "claude c1a0 #1", "rate limiter를 짜줘", m.theme.sessions[m.colors[session{"claude", claudeSession}]]},
		{12, "omp a1f2 #1", "line 15를 지워줘", m.theme.sessions[m.colors[session{"omp", ompSession}]]},
		{16, "unknown", noRecord, m.theme.subtle},
		{m.files[3].head + 3, "claude c1a0 fix", "use min()", m.theme.fix},
	} {
		m.cursor = tc.row
		segs := m.cursorSegs(on)
		k := slices.IndexFunc(segs, func(s seg) bool { return s.text == tc.tag })
		if k < 0 || segs[k].st.GetForeground() != tc.color || !strings.Contains(segs[len(segs)-1].text, tc.text) {
			t.Errorf("row %d: status %v, want %q in %v and %q", tc.row, segs, tc.tag, tc.color, tc.text)
		}
	}
	if m.colors[session{"omp", ompSession}] != 1 {
		t.Errorf("omp has color %d, want 1", m.colors[session{"omp", ompSession}])
	}
}
