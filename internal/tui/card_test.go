package tui

import (
	"image/color"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// cellOf returns the cell where glyph first shows on line, a line of the screen without its
// styles, or -1.
func cellOf(line, glyph string) int {
	i := strings.Index(line, glyph)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(line[:i])
}

func TestCardBand(t *testing.T) {
	// The card hangs under its row from the rail column, in the unified and the split view:
	// its band goes on from the rail of the row, in the color of the origin, the session's,
	// teal for a fix dr applied and quiet for none, blue under a file header and amber for a
	// comment. Every row of the card starts with it.
	fixed, _ := fixFiles()
	for _, tc := range []struct {
		name, keys, rail string
		files            []review.File
		band             func(m *model) color.Color
	}{
		{name: "a session's line", keys: "4j", rail: "▎", files: testFiles(), band: func(m *model) color.Color { return m.sessionColor(session{"claude", claudeSession}) }},
		{name: "another session's line", keys: "12j", rail: "▎", files: testFiles(), band: func(m *model) color.Color { return m.sessionColor(session{"omp", ompSession}) }},
		{name: "a line without origin", keys: "16j", rail: "┆", files: testFiles(), band: func(m *model) color.Color { return m.theme.subtle }},
		{name: "a fixed line", keys: "4j", rail: "▎", files: fixed, band: func(m *model) color.Color { return m.theme.fix }},
		{name: "split", keys: "s3j", rail: "▎", files: testFiles(), band: func(m *model) color.Color { return m.sessionColor(session{"claude", claudeSession}) }},
		{name: "a file header", files: testFiles(), band: func(m *model) color.Color { return m.theme.accent }},
	} {
		m := New(tc.files, nil, Saved{}, Asker{}).(*model)
		m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
		typeKeys(m, tc.keys+"<enter>")
		th := &m.theme
		if got, want := m.originCard(40).band, tc.band(m); got != want {
			t.Errorf("%s: band %v, want %v", tc.name, got, want)
		}
		if raw := m.View().Content; !strings.Contains(raw, th.paint(tc.band(m), th.surface2).Render(cardBand)) ||
			!strings.Contains(raw, th.paint(tc.band(m), th.surface).Render(cardBand)) {
			t.Errorf("%s: the screen has no band in the card's color on its header and its body", tc.name)
		}
		lines := strings.Split(screen(t, m), "\n")
		y := cursorLine(m)
		x := cellOf(lines[y+1], cardBand)
		if tc.rail != "" {
			if rail := cellOf(lines[y], tc.rail); rail != x {
				t.Errorf("%s: the card's band at cell %d, want it under the rail at %d:\n%s", tc.name, x, rail, strings.Join(lines, "\n"))
			}
		}
		for k := y + 1; k <= y+m.boxHeight(); k++ {
			if got := cellOf(lines[k], cardBand); got != x {
				t.Errorf("%s: line %d of the card has its band at %d, want %d:\n%s", tc.name, k-y, got, x, strings.Join(lines, "\n"))
			}
		}
	}
	// A comment's card hangs from the rail too, in amber.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcfix<enter>")
	lines := strings.Split(screen(t, m), "\n")
	y := cursorLine(m)
	if rail, band := cellOf(lines[y], "▎"), cellOf(lines[y+1], cardBand); rail != band || m.commentCard(m.comments[0], 80).band != m.theme.pending {
		t.Errorf("the comment's band at cell %d in %v, want amber at the rail, %d", band, m.commentCard(m.comments[0], 80).band, rail)
	}
}

func TestCardHeader(t *testing.T) {
	// The header of the origin card has the dot and the tag of the origin in its color and
	// bold, then where the row is, quiet.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "4j")
	th, own := &m.theme, m.sessionColor(session{"claude", claudeSession})
	want := []chunk{{text: "● ", color: own}, {text: "claude c1a0 #1", color: own, bold: true}, {text: "  a.go:2", color: th.muted, cut: true}}
	if got := m.originCard(100).head.left; !slices.Equal(got, want) {
		t.Errorf("header %+v, want %+v", got, want)
	}
	// The keys at its right edge show only as far as they fit, the one that closes it last
	// to go; a comment's card keeps its first keys.
	for _, tc := range []struct {
		width int
		want  string
	}{
		{100, "a ask   ⏎ close"},
		{38, "⏎ close"},
		{20, ""},
	} {
		if got := chunksText(m.originCard(tc.width).head.right); got != tc.want {
			t.Errorf("%d cells wide: keys %q, want %q", tc.width, got, tc.want)
		}
	}
	m = commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcfix<enter>")
	c := m.comments[0]
	c.fix = &state.Fix{Agent: "claude", Session: claudeSession, Lines: []string{"Changed 2"}, New: []string{"Changed two"}}
	for _, tc := range []struct {
		width int
		want  string
	}{
		{120, "y apply   x drop   c edit"},
		{70, "y apply   x drop"},
		{60, "y apply"},
	} {
		if got := chunksText(m.commentCard(c, tc.width).head.right); got != tc.want {
			t.Errorf("comment %d cells wide: keys %q, want %q", tc.width, got, tc.want)
		}
	}
}

func TestCardSections(t *testing.T) {
	// Sections have labels in small capitals, quiet and bold; the note is quiet and slanted.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	typeKeys(m, "4j")
	th := &m.theme
	rows := m.originLines()
	i := slices.IndexFunc(rows, func(r cardRow) bool { return chunksText(r.left) == "NOTE" })
	if i < 0 || rows[i].left[0] != (chunk{text: "NOTE", color: th.subtle, bold: true}) ||
		rows[i+1].left[0] != (chunk{text: "토큰 버킷으로 바꿀게요", color: th.muted, italic: true}) {
		t.Errorf("the note section:\n%s", boxText(m))
	}
	if r := m.label("THREAD", "2"); !slices.Equal(r.left, []chunk{{text: "THREAD", color: th.subtle, bold: true}, {text: " · 2", color: th.subtle}}) {
		t.Errorf("label %+v", r.left)
	}
	// Text keeps its line breaks, each line wrapped on its own.
	if got := rowsText(wrapped(review.Visible("first line\n\nthird"), chunk{}, 20)); got != "first line\n\nthird" {
		t.Errorf("wrapped %q", got)
	}
}

func TestThreadRows(t *testing.T) {
	// The answer's header of a thread: the dot and the name of who was asked in the session's
	// color, quiet for a new session; how long the answer has been coming in violet while it
	// waits; a quiet no answer chip when dr closed before it came; ^ and the model when it was
	// picked. Another session tells how it relates to the lines, quiet and slanted.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	m.now = func() time.Time { return start.Add(4 * time.Second) }
	th, own := &m.theme, m.sessionColor(session{"claude", claudeSession})
	asked := state.Question{Agent: "claude", Session: claudeSession, Text: "why?", Time: start, Relation: string(review.Editor)}
	rows := m.exchange(&question{Question: asked, pending: true}, 80)
	if got := rowsText(rows); got != "you  just now\nwhy?\n● claude c1a0  "+askingFor(4*time.Second)+"\n↳ edited this file, not these lines" {
		t.Errorf("waiting:\n%s", got)
	}
	if who := rows[2].left; who[0].color != own || who[3] != (chunk{text: askingFor(4 * time.Second), color: th.ask}) {
		t.Errorf("waiting: %+v", who)
	}
	if rel := rows[3].left; !slices.Equal(rel, []chunk{{text: "↳ ", color: th.subtle}, {text: "edited this file, not these lines", color: th.subtle, italic: true}}) {
		t.Errorf("relation %+v", rel)
	}
	rows = m.exchange(&question{Question: asked}, 80)
	if who := rows[2].left; who[3] != (chunk{text: "no answer", color: th.muted, chip: true}) || rows[4].left[0] != (chunk{text: "dr closed before it came", color: th.muted}) {
		t.Errorf("closed: %+v then %+v", who, rows[4].left)
	}
	picked := asked
	picked.Model, picked.Relation = "sonnet", string(review.Author)
	rows = m.exchange(&question{Question: picked, answer: &state.Answer{Text: "[guess] a", Model: "claude-sonnet-5", TookMS: 7000}}, 80)
	if got := rows[2].right; !slices.Equal(got, []chunk{{text: "^claude-sonnet-5 · 7s", color: th.subtle}}) {
		t.Errorf("picked: note %+v", got)
	}
	fresh := state.Question{Agent: "omp", Text: "why?", Relation: string(review.NewSession)}
	if who := m.exchange(&question{Question: fresh, pending: true}, 80)[2].left; who[1] != (chunk{text: "omp, new session", color: th.muted, bold: true}) {
		t.Errorf("new session: %+v", who)
	}
}

func TestCommentCardStates(t *testing.T) {
	// The chip in a comment card's header tells its state in its color: pending in amber, the
	// fix request in violet while it waits, a fix ready in teal, a fix that keeps the lines
	// quiet, and no fix in red. A fix shows as a small diff: the comment's lines deleted and
	// the new ones added, syntax colored, the words that differ marked.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcfix<enter>")
	th, c := &m.theme, m.comments[0]
	ready := &state.Fix{Agent: "claude", Session: claudeSession, Path: "a.go", Lines: []string{"Changed 2"}, New: []string{"Changed two"}}
	for _, tc := range []struct {
		name    string
		request string
		fix     *state.Fix
		want    chunk
	}{
		{name: "pending", want: chunk{text: "pending", color: th.pending, chip: true}},
		{name: "asking", request: "r", want: chunk{text: askingFor(0), color: th.ask, chip: true}},
		{name: "fix ready", fix: ready, want: chunk{text: "fix ready", color: th.fix, chip: true}},
		{name: "keeps", fix: &state.Fix{Keep: true, Note: "fine"}, want: chunk{text: "keeps the lines", color: th.muted, chip: true}},
		{name: "no fix", fix: &state.Fix{Error: "rate limited"}, want: chunk{text: "no fix", color: th.del, chip: true}},
	} {
		c.request, c.asked, c.fix = tc.request, start, tc.fix
		head := m.commentCard(c, 120).head.left
		if got := head[len(head)-1]; got != tc.want {
			t.Errorf("%s: chip %+v, want %+v", tc.name, got, tc.want)
		}
	}
	c.request, c.fix = "", ready
	rows := m.commentCard(c, 120).rows
	i := slices.IndexFunc(rows, func(r cardRow) bool { return r.sign != 0 })
	if i < 0 || i+1 >= len(rows) || rows[i].sign != '-' || rows[i+1].sign != '+' {
		t.Fatalf("no small diff in\n%s", rowsText(rows))
	}
	changed := func(code []piece) []string {
		var out []string
		for _, p := range code {
			if p.changed {
				out = append(out, p.text)
			}
		}
		return out
	}
	if old, cur := changed(rows[i].code), changed(rows[i+1].code); !slices.Equal(old, []string{"2"}) || !slices.Equal(cur, []string{"two"}) {
		t.Errorf("marked %q and %q, want 2 and two", old, cur)
	}
	p := rows[i+1].code[len(rows[i+1].code)-1]
	if line := m.drawCardRow(rows[i+1], 40, th.surface, false); !strings.Contains(line, th.paint(th.code(p.kind), th.addword).Render("two")) ||
		!strings.Contains(line, th.paint(th.add, th.addbg).Render("+ ")) || ansi.StringWidth(line) != 40 {
		t.Errorf("the added line %q, want two on the word color of added lines", line)
	}
}

func TestCardScroll(t *testing.T) {
	// A card taller than the room shows the rows that fit, with a scroll bar in its last
	// column: the thumb quiet, the track faint. The thumb follows the rows shown. A card
	// that fits has no bar.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	th := &m.theme
	k := card{band: th.accent, head: cardRow{left: []chunk{{text: "head"}}}}
	for i := range 20 {
		k.rows = append(k.rows, cardRow{left: []chunk{{text: strings.Repeat("x", i)}}})
	}
	bar := func(top, n int) string {
		lines := m.drawCard(k, 2, 30, top, n)
		if len(lines) != min(n, 20)+2 {
			t.Fatalf("%d lines, want %d", len(lines), min(n, 20)+2)
		}
		var b strings.Builder
		for _, l := range lines[1 : len(lines)-1] {
			if w := ansi.StringWidth(l); w != 30 {
				t.Errorf("a line %d cells wide, want 30", w)
			}
			l = ansi.Strip(l)
			b.WriteString(l[strings.LastIndex(l, " ")+1:])
		}
		return b.String()
	}
	for _, tc := range []struct {
		top, n int
		want   string
	}{
		{0, 5, "┃││││"},
		{8, 5, "││┃││"},
		{15, 5, "││││┃"},
		{40, 5, "││││┃"}, // past the end
		{0, 10, "┃┃┃┃┃│││││"},
		{10, 10, "│││││┃┃┃┃┃"},
		{0, 20, ""},
	} {
		if got := bar(tc.top, tc.n); got != tc.want {
			t.Errorf("rows from %d, %d shown: bar %q, want %q", tc.top, tc.n, got, tc.want)
		}
	}
	raw := strings.Join(m.drawCard(k, 2, 30, 0, 5), "\n")
	if !strings.Contains(raw, th.paint(th.muted, th.surface).Render(cardThumb)) || !strings.Contains(raw, th.paint(th.faint, th.surface).Render(cardTrack)) {
		t.Errorf("the bar is not in quiet and faint colors: %q", raw)
	}
	// In the diff the bar follows the origin card's scroll.
	m = New(longFiles(), nil, Saved{}, Asker{}).(*model)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	typeKeys(m, "G<enter>")
	thumb := func() int {
		lines := strings.Split(screen(t, m), "\n")
		return slices.IndexFunc(lines, func(l string) bool { return strings.HasSuffix(l, cardThumb) })
	}
	first := thumb()
	typeKeys(m, "G")
	if last := thumb(); first < 0 || last <= first {
		t.Errorf("the thumb at line %d, then %d after G; want it lower", first, last)
	}
}

func TestCardPlain(t *testing.T) {
	// Without colors a card keeps its shape: the same glyphs in the same cells, the header
	// bold and the chips in reverse video.
	shapes := make([]string, 2)
	for i, plain := range []bool{false, true} {
		m := commentScreen(testFiles(), t.TempDir())
		m.theme = newTheme(nil, true, plain)
		typeKeys(m, "4jcfix<enter><enter>")
		y := cursorLine(m)
		lines := strings.Split(screen(t, m), "\n")
		shapes[i] = strings.Join(lines[y+1:y+1+m.commentHeight(m.cursor)+m.boxHeight()], "\n")
		if !plain {
			continue
		}
		th := &m.theme
		raw := m.View().Content
		for name, want := range map[string]string{
			"the chip":             lipgloss.NewStyle().Reverse(true).Bold(true).Render(" pending "),
			"the header's quiet":   th.paint(th.muted, th.surface2).Bold(true).Render(" · a.go:2"),
			"the origin's header":  th.paint(th.muted, th.surface2).Bold(true).Render("  a.go:2"),
			"the band on its body": th.paint(m.sessionColor(session{"claude", claudeSession}), th.surface).Render(cardBand),
		} {
			if !strings.Contains(raw, want) {
				t.Errorf("no colors: the screen has no %s %q", name, want)
			}
		}
	}
	if shapes[0] != shapes[1] {
		t.Errorf("the shape without colors differs:\n%s\nwant\n%s", shapes[1], shapes[0])
	}
}

func TestCardRow(t *testing.T) {
	// A row is as wide as it is drawn. The chunks at its right edge show only when they fit
	// two cells after the left ones; a wide character that does not fit is left out whole,
	// and line breaks and tabs draw as spaces.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	r := cardRow{left: []chunk{{text: "claude 세션", bold: true}}, right: []chunk{{text: "model · 7s"}}}
	broken := cardRow{left: []chunk{{text: "claude c1a0\nde00"}, {text: "ok\t", color: m.theme.add, chip: true}}}
	for _, tc := range []struct {
		r     cardRow
		width int
		want  string
	}{
		{r, 30, "claude 세션         model · 7s"},
		{r, 23, "claude 세션  model · 7s"},
		{r, 22, "claude 세션"},
		{r, 8, "claude"},
		{cardRow{left: []chunk{{text: "claude 세션"}}}, 11, "claude 세션"}, // as wide as the row, with nothing at the right
		{broken, 30, "claude c1a0 de00 ok"},
	} {
		line := m.drawCardRow(tc.r, tc.width, m.theme.surface, false)
		if got := strings.TrimRight(ansi.Strip(line), " "); got != tc.want || ansi.StringWidth(line) != tc.width {
			t.Errorf("%d cells: got %q, %d cells; want %q", tc.width, got, ansi.StringWidth(line), tc.want)
		}
	}
}

func TestMiniDiffWraps(t *testing.T) {
	// A fix's line too long for its card goes on in the rows under it, on the background of
	// its kind of line with the sign cell blank, so lines that differ only at their end show
	// both ends.
	m := commentScreen(testFiles(), t.TempDir())
	typeKeys(m, "4jcfix<enter>")
	th, c := &m.theme, m.comments[0]
	const head = "total := computeEveryTokenInTheBucket(rate, burst, now) + "
	c.fix = &state.Fix{Agent: "claude", Session: claudeSession, Path: "a.go", Lines: []string{head + "1"}, New: []string{head + "2"}}
	var old, cur []cardRow
	for _, r := range m.commentCard(c, 40).rows {
		switch r.sign {
		case '-':
			old = append(old, r)
		case '+':
			cur = append(cur, r)
		}
	}
	for _, tc := range []struct {
		rows []cardRow
		want string
		bg   color.Color
	}{
		{old, head + "1", th.delbg},
		{cur, head + "2", th.addbg},
	} {
		var text string
		for i, r := range tc.rows {
			text += join(r.code)
			if r.more != (i > 0) {
				t.Errorf("row %d of %q: goes on %t", i, tc.want, r.more)
			}
			line := m.drawCardRow(r, 36, th.surface, false)
			if w := ansi.StringWidth(line); w != 36 {
				t.Errorf("row %d of %q is %d cells wide, want 36", i, tc.want, w)
			}
			if i > 0 && (!strings.HasPrefix(ansi.Strip(line), "   ") || !strings.Contains(line, th.paint(th.del, tc.bg).Render("  ")) && !strings.Contains(line, th.paint(th.add, tc.bg).Render("  "))) {
				t.Errorf("row %d of %q: %q, want a blank sign on the line's background", i, tc.want, line)
			}
		}
		if len(tc.rows) < 2 || text != tc.want {
			t.Errorf("rows %q, want %q over more than one row", text, tc.want)
		}
		last := tc.rows[len(tc.rows)-1].code
		if p := last[len(last)-1]; !p.changed || p.text != tc.want[len(tc.want)-1:] {
			t.Errorf("%q ends with %+v, want its last character marked", tc.want, p)
		}
	}
}

func TestCardAboveTallComment(t *testing.T) {
	// Enter on a row with a comment opens the origin card right under the row, before the
	// comment's card, with all the room for its rows: a comment taller than the screen does
	// not push it off.
	m := commentScreen(testFiles(), t.TempDir())
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 16})
	typeKeys(m, "4jc1<alt+enter>2<alt+enter>3<alt+enter>4<alt+enter>5<alt+enter>6<alt+enter>7<alt+enter>8<alt+enter>9<alt+enter>10<alt+enter>11<alt+enter>12<enter><enter>")
	lines := strings.Split(screen(t, m), "\n")
	y := cursorLine(m)
	n := min(len(m.originLines()), m.diffHeight()-4)
	if !m.reading || y < 0 || y+n+2 >= m.diffHeight() || !strings.Contains(lines[y], "Changed 2") ||
		!strings.Contains(lines[y+1], "▎ ● claude c1a0 #1  a.go:2") || !strings.HasSuffix(strings.TrimRight(lines[y+n+2], " "), cardBand) {
		t.Errorf("want the row, the origin card's header, %d rows of its body and its last row on the screen:\n%s", n, strings.Join(lines, "\n"))
	}
}

func TestQuotedLinesWrap(t *testing.T) {
	// A long quoted line of an earlier question or comment goes on in the rows under it,
	// under its text, so its end shows at any width.
	dir := t.TempDir()
	long := "x := " + strings.Repeat("abcdefghij", 10) + "END"
	log := state.NewLog(filepath.Join(dir, logFile))
	lines := []state.Line{{Kind: "+", Text: long}}
	if err := log.Append(state.Question{Time: start, ID: "000000000000000q", Agent: "claude", Session: claudeSession, Relation: "author", Path: "a.go", Lines: lines, Text: "why?"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(state.Comment{Time: start, ID: "000000000000000c", Agent: "claude", Session: claudeSession, Relation: "author", Path: "a.go", Lines: lines, Text: "fix"}); err != nil {
		t.Fatal(err)
	}
	m := commentScreen(testFiles(), dir)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	typeKeys(m, "gg<enter>")
	text, s := boxText(m), screen(t, m)
	for _, label := range []string{"EARLIER QUESTIONS · 1\n", "EARLIER COMMENTS · 1\n"} {
		at := strings.Index(text, label)
		if at < 0 {
			t.Fatalf("no %q in\n%s", label, text)
		}
		rows := strings.Split(text[at+len(label):], "\n")
		var joined, last string
		for i, r := range rows {
			mark := "> "
			if i > 0 {
				mark = "  "
			}
			if !strings.HasPrefix(r, mark) {
				break
			}
			joined, last = joined+r[len(mark):], r
		}
		if joined != long || len(rows) < 2 || !strings.HasPrefix(rows[1], "  ") {
			t.Errorf("%s: quoted %q over rows\n%s\nwant %q over more than one row", label, joined, strings.Join(rows[:3], "\n"), long)
		}
		if !strings.HasSuffix(last, "END") || !strings.Contains(s, "▎ "+last) {
			t.Errorf("%s: the end of the quoted line, %q, is not on the screen:\n%s", label, last, s)
		}
	}
}

func TestCardScrollsComments(t *testing.T) {
	// With the origin card open, the cards of the row's comments scroll in it after its own
	// rows, as one with them: G brings the end of a fix taller than the screen into view, the
	// thumb at the bottom, the status line counting every line, and gg the origin's rows
	// back. After Esc, y applies the fix as before.
	repo, home := agentRepo(t,
		map[string]string{"omp/notes.md": "a\nold\nc\n"},
		map[string]string{"omp/notes.md": "a\nB\nc\nd\n"},
		map[string]string{".omp/agent/sessions/spike/1.jsonl": "omp-01a1f000-0000-7000-8000-000000000001.jsonl"})
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = `"L` + strconv.Itoa(i+1) + `"`
	}
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-9", Text: `{"fixes":[{"comment":1,"new_lines":[` + strings.Join(lines, ",") + `]}]}`}}
	m, _ := repoScreen(t, repo, home, f, fixClock())
	typeAll(m, "4jclonger<enter>:submit<enter>")
	if c := commentOf(m, "longer"); c == nil || c.fix == nil || len(c.fix.New) != 60 {
		t.Fatalf("comment %+v, want a fix of 60 lines", c)
	}
	typeKeys(m, "<enter>G")
	s := strings.Split(screen(t, m), "\n")
	y, rows, n := cursorLine(m), m.boxRows(), m.blockHeight()
	if n <= len(m.originLines())+60 || !strings.Contains(s[y+rows], "+ L60") || !strings.HasSuffix(strings.TrimRight(s[y+1+rows], " "), cardThumb) ||
		!strings.Contains(s[len(s)-2], "origin "+strconv.Itoa(n)+"/"+strconv.Itoa(n)) {
		t.Errorf("G: %d lines, want the last line of the fix at the bottom with the thumb, and origin %d/%d:\n%s", n, n, n, strings.Join(s, "\n"))
	}
	typeKeys(m, "gg")
	s = strings.Split(screen(t, m), "\n")
	if first := chunksText(m.originLines()[0].left); !strings.Contains(s[y+1], "▎ ● omp") || !strings.Contains(s[y+2], first) || strings.Contains(strings.Join(s, "\n"), "+ L60") {
		t.Errorf("gg: want the origin's first row %q under the header:\n%s", first, strings.Join(s, "\n"))
	}
	typeKeys(m, "<esc>")
	typeAll(m, "y")
	if got := noticeText(t, m); got != "✓ Applied the fix for omp/notes.md:2 :undo reverts it" {
		t.Errorf("y after Esc: notice %q, want the fix applied", got)
	}
}
