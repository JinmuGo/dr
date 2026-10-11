package tui

import (
	"context"
	"errors"
	"image/color"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// fakeAsk stands for agent.Ask: it keeps the questions it gets and gives each answer, or
// fails with err. The agents in installed are installed, all of them when it is nil.
type fakeAsk struct {
	mu        sync.Mutex
	questions []agent.Question
	answer    agent.Answer
	err       error
	installed []string
}

func (f *fakeAsk) ask(_ context.Context, q agent.Question) (agent.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.questions = append(f.questions, q)
	return f.answer, f.err
}

// isInstalled stands for agent.Installed.
func (f *fakeAsk) isInstalled(name string) bool {
	return f.installed == nil || slices.Contains(f.installed, name)
}

// asked returns the questions f got.
func (f *fakeAsk) asked() []agent.Question {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.questions)
}

// answer is an answer that starts with its tag, as the question asks it to.
var answer = agent.Answer{Fork: "fork-1", Text: "[decided] 토큰 버킷이라 float64로 했어요.", Model: "claude-opus-5-5", Took: 7012 * time.Millisecond}

// root is the top directory of the worktree the screens of the tests show.
const root = "/repo"

// askScreen returns the screen on files in a 125×24 terminal that asks through f. It does not
// tick, so the command of the key that asks is the question alone.
func askScreen(files []review.File, saved Saved, f *fakeAsk) *model {
	m := newModel(files, nil, saved, Asker{Ask: f.ask, Installed: f.isInstalled, Root: root})
	m.tick = noTick
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	return m
}

// noTick stands for tick in a test that sends tickMsg itself, if at all.
func noTick() tea.Cmd { return nil }

// typeAndKeep types keys into m like typeKeys, but keeps the command of the last key for the
// test to run, so the question it asks stays in flight until then.
func typeAndKeep(m *model, keys string) tea.Cmd {
	presses := keyPresses(keys)
	for _, k := range presses[:len(presses)-1] {
		send(m, k)
	}
	_, cmd := m.Update(presses[len(presses)-1])
	return cmd
}

// boxText returns the text of the origin card's body of the cursor row, as rowsText writes
// it.
func boxText(m *model) string {
	return rowsText(m.originLines())
}

// cardText returns the text of the origin card of the cursor row as the screen lays it out,
// its header first, as rowsText writes it.
func cardText(m *model) string {
	_, w := m.layout()
	_, width := m.boxLeft(w)
	k := m.originCard(width)
	return rowsText(append([]cardRow{k.head}, k.rows...))
}

// rowsText returns the text of rows, a row a line: its chunks at the left, then two spaces
// and those at the right edge; a rule as "╌", and a line of a small diff as its sign and its
// code.
func rowsText(rows []cardRow) string {
	texts := make([]string, len(rows))
	for i, r := range rows {
		switch {
		case r.rule:
			texts[i] = "╌"
		case r.sign != 0:
			texts[i] = string(r.sign) + " " + join(r.code)
		default:
			texts[i] = chunksText(r.left)
			if right := chunksText(r.right); right != "" {
				texts[i] += "  " + right
			}
		}
	}
	return strings.Join(texts, "\n")
}

// chunksText returns the text of chunks.
func chunksText(chunks []chunk) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.text)
	}
	return b.String()
}

// askFiles returns testFiles and two more files. In d.go, rows 27 to 34, a Codex session
// wrote the added line on row 30, the newest edit of the file, and a Claude subagent the one
// on row 31; the line deleted on row 33 has no origin. e.go, rows 35 to 40, has no origins:
// a deleted line on row 38 and an added one on row 39.
func askFiles() []review.File {
	d := parseDiff("x", "+codex line", "+subagent line", "y", "-old line", "z")
	d[1].Edit = &agent.Edit{
		Agent: "codex", Session: "c0dex000-3333", Turn: 1, Time: start.Add(5 * time.Minute), Cwd: "/work", Model: "gpt-6.1-sol", Effort: "medium",
	}
	d[2].Edit = &agent.Edit{Agent: "claude", Session: "5ub00000-4444", Parent: claudeSession, Turn: 1, Time: start, Cwd: "/work/sub"}
	e := parseDiff("a", "-gone", "+new", "b")
	return append(testFiles(), review.File{Path: "d.go", Status: 'M', Lines: d}, review.File{Path: "e.go", Status: 'M', Lines: e})
}

func TestAskKeyA(t *testing.T) {
	// a asks; K moves half a page and no longer asks, in the diff and in the open origin box.
	for _, keys := range []string{"4jK", "4j<enter>K"} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
		typeKeys(m, keys)
		if m.typing() {
			t.Errorf("%q opened the input box", keys)
		}
	}
}

func TestAskInput(t *testing.T) {
	// a on a changed line opens the input box right under the line, titled with who it asks,
	// where the line is and the keys, with what it asks when nothing is typed dim after the
	// cursor. Typing takes its place, an input method's text too, and the terminal's cursor
	// stays where the text goes in. Backspace and ctrl+u take it back, and Esc closes the
	// box without asking. The status line keeps telling of the cursor row.
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{}, f)
	const title = "ASK claude c1a0 #1 · L2"
	for _, step := range []struct {
		keys, text string
	}{
		{"4ja", whyAdded}, // its first character under the cursor
		{"왜", "왜"},
		{"<bs>", whyAdded},
		{"abc<ctrl+u>", whyAdded},
		{"abc", "abc"},
	} {
		typeKeys(m, step.keys)
		box := entryBox(t, m)
		if lines := strings.Split(screen(t, m), "\n"); !strings.Contains(lines[cursorLine(m)], "Changed 2") ||
			shownTitle(box) != title || !slices.Equal(shownText(box), []string{step.text}) {
			t.Errorf("after %q: the box\n%s\nwant it under the line, titled %q, holding %q", step.keys, strings.Join(box, "\n"), title, step.text)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyExtended, Text: "왜요"}) // an input method commits two characters
	if _, before := beforeCursor(t, m); m.entry.text != "abc왜요" || !strings.HasSuffix(before, "❯ abc왜요") {
		t.Errorf("text %q with %q before the cursor, want %q with the cursor after it", m.entry.text, before, "abc왜요")
	}
	if status := statusLine(t, m); !strings.HasPrefix(status, " ASK ") || !strings.Contains(status, "claude c1a0 #1") ||
		!strings.Contains(status, "row 5/27") || strings.Contains(status, "Enter") {
		t.Errorf("status line %q, want the ASK pill, the origin and the row of the cursor row without the hint", status)
	}
	typeKeys(m, "<esc>")
	if m.asking != nil || m.reading || m.cursor != 4 || len(f.asked()) != 0 || entryBox(t, m) != nil || m.View().Cursor != nil {
		t.Errorf("Esc: box open %t, origin box open %t, cursor at row %d, %d questions; want all closed at row 4 and none asked",
			m.asking != nil, m.reading, m.cursor, len(f.asked()))
	}
	// A deleted line is asked about differently, and goes by its line in base.
	typeKeys(m, "ka")
	if box := entryBox(t, m); shownTitle(box) != title || !slices.Equal(shownText(box), []string{whyDeleted}) {
		t.Errorf("a on a deleted line: the box\n%s", strings.Join(box, "\n"))
	}
}

func TestInputRoom(t *testing.T) {
	// An open search or : input goes first on the status line, after the mode pill. The
	// right leaves out what it must, the questions last, to make room for it and the cursor
	// after it. When the whole line is too narrow, the start of the input is cut, so what is
	// typed and the cursor stay on the screen, and no wide character is split; screen checks
	// every line is as wide as the screen.
	query, long := strings.Repeat("가", 35), strings.Repeat("왜", 30)
	for _, tc := range []struct {
		name       string
		width      int
		keys       string
		start, end string // of the status line without its trailing spaces
		gone       []string
	}{
		{
			name: "the origin makes room", width: 100, keys: "32G/" + query,
			start: " SEARCH   /" + query, end: " 0 of 0", gone: []string{"claude 4444", "row", "file"},
		},
		{
			// Cut after 13 cells, the / and 6 of the 왜.
			name: "only the end of what is typed", width: 60, keys: "32G/" + long,
			start: " SEARCH   …" + strings.Repeat("왜", 24), end: strings.Repeat("왜", 24), gone: []string{"row"},
		},
		{
			name: "a search", width: 60, keys: "/" + strings.Repeat("가나", 15),
			start: " SEARCH   …" + strings.Repeat("가나", 12), end: strings.Repeat("가나", 12),
		},
		{
			name: "a command", width: 60, keys: ":" + strings.Repeat("가나", 15),
			start: " COMMAND   …", end: strings.Repeat("가나", 11),
		},
	} {
		m := askScreen(askFiles(), Saved{}, &fakeAsk{answer: answer})
		m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
		typeKeys(m, tc.keys)
		status := strings.TrimRight(statusLine(t, m), " ")
		if !strings.HasPrefix(status, tc.start) || !strings.HasSuffix(status, tc.end) {
			t.Errorf("%s: status line %q, want it to start with %q and end with %q", tc.name, status, tc.start, tc.end)
		}
		for _, g := range tc.gone {
			if strings.Contains(status, g) {
				t.Errorf("%s: status line %q has %q", tc.name, status, g)
			}
		}
		typed := m.input[len(m.input)-len("가"):] // the last character typed
		want := ansi.StringWidth(status[:strings.LastIndex(status, typed)+len(typed)])
		if c := m.View().Cursor; c == nil || c.X != want || c.Y != m.diffHeight() {
			t.Errorf("%s: cursor %+v, want it at column %d of the status line", tc.name, c, want)
		}
	}
}

func TestAskRefusals(t *testing.T) {
	for _, tc := range []struct {
		keys, want string
		installed  []string // the agents installed, all when nil
	}{
		{keys: "a", want: "Move to a changed line to ask"},    // a file header
		{keys: "ja", want: "Move to a changed line to ask"},   // a hunk header
		{keys: "2ja", want: "Move to a changed line to ask"},  // an unchanged line
		{keys: "20ja", want: "Move to a changed line to ask"}, // why a binary file has no lines
		{keys: "5jVjja", want: "The selection has no changed lines"},
		{keys: "12jV4ja", want: "Some selected lines have no origin"},
		{keys: "3jV9ja", want: "The selection mixes sessions. Select lines from one session"},
		{keys: "40Ga", want: "No agent CLI is installed to ask (claude, codex or omp)", installed: []string{}},
	} {
		f := &fakeAsk{answer: answer, installed: tc.installed}
		m := askScreen(askFiles(), Saved{}, f)
		typeKeys(m, tc.keys)
		if m.message != tc.want || m.tone == toneOK || m.asking != nil {
			t.Errorf("%q: message %q, tone %d, input open %t; want the warning %q and no input", tc.keys, m.message, m.tone, m.asking != nil, tc.want)
		}
		if status := statusLine(t, m); !strings.Contains(status, tc.want) {
			t.Errorf("%q: status line %q has no %q", tc.keys, status, tc.want)
		}
		if typeKeys(m, "<enter>"); len(f.asked()) != 0 {
			t.Errorf("%q: asked %+v", tc.keys, f.asked())
		}
	}
}

func TestAskOthers(t *testing.T) {
	// An OMP subagent is asked itself. Lines that no session dr can fork wrote go to another
	// session, which the input box names with how it relates to them; the question tells it so,
	// and what a asks when nothing is typed does not say it wrote them. The thread in the
	// origin box names who was asked, and says dim how they relate to the lines. The model
	// that answered is compared with the session's, which a new session does not have.
	for _, tc := range []struct {
		name, keys  string // keys to the lines, before a
		installed   []string
		label       string
		placeholder string
		ask         agent.Question  // without the prompt
		rel         review.Relation // what the prompt tells the session of how it relates to the lines
		path        string
		lines       []origin.Line // the lines the prompt quotes
		who, note   string        // the header of the answer, without the badge
		relation    string        // the dim line under it
	}{
		{
			name: "an OMP subagent", keys: "12j",
			label: "omp a1f2 #1", placeholder: whyDeleted,
			ask: agent.Question{Agent: "omp", Session: ompSession, Log: subagent.Log, Cwd: "/work"},
			rel: review.Author, path: "a.go", lines: []origin.Line{{Text: "line 15", Old: 15}},
			who: "omp a1f2", note: "claude-opus-5-5 · 7s",
		},
		{
			name: "the parent of a Claude subagent", keys: "32G",
			label: "claude c1a0, which started the subagent", placeholder: whyAddedOther,
			ask: agent.Question{Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5"},
			rel: review.Parent, path: "d.go", lines: []origin.Line{{Text: "subagent line", New: 3}},
			who: "claude c1a0", note: "claude-opus-5-5 · 7s", relation: "started the subagent that wrote these lines",
		},
		{
			name: "the session that edited the file last", keys: "16j",
			label: "omp a1f2, which edited this file", placeholder: whyAddedOther,
			ask: agent.Question{Agent: "omp", Session: ompSession, Log: subagent.Log, Cwd: "/work"},
			rel: review.Editor, path: "a.go", lines: []origin.Line{{Text: "new after 18", New: 18}},
			who: "omp a1f2", note: "claude-opus-5-5 · 7s", relation: "edited this file, not these lines",
		},
		{
			name: "a deleted line the editor of the file did not write", keys: "34G",
			label: "codex 3333, which edited this file", placeholder: whyDeletedOther,
			ask: agent.Question{Agent: "codex", Session: "c0dex000-3333", Cwd: "/work", Model: "gpt-6.1-sol", Effort: "medium"},
			rel: review.Editor, path: "d.go", lines: []origin.Line{{Text: "old line", Old: 3}},
			who: "codex 3333", note: "claude-opus-5-5, not gpt-6.1-sol · 7s", relation: "edited this file, not these lines",
		},
		{
			name: "a new session", keys: "40G", installed: []string{"omp"},
			label: "omp in a new session", placeholder: whyAddedOther,
			ask: agent.Question{Agent: "omp", Cwd: root},
			rel: review.NewSession, path: "e.go", lines: []origin.Line{{Text: "new", New: 2}},
			who: "omp, new session", note: "claude-opus-5-5 · 7s", relation: "new session, without this code's history",
		},
		{
			name: "a new session of the agent behind the most edits", keys: "39G",
			label: "claude in a new session", placeholder: whyDeletedOther,
			ask: agent.Question{Agent: "claude", Cwd: root},
			rel: review.NewSession, path: "e.go", lines: []origin.Line{{Text: "gone", Old: 2}},
			who: "claude, new session", note: "claude-opus-5-5 · 7s", relation: "new session, without this code's history",
		},
		{
			name: "lines for a new session", keys: "39GVj", installed: []string{"codex", "omp"},
			label: "codex in a new session", placeholder: whyLinesOther,
			ask: agent.Question{Agent: "codex", Cwd: root},
			rel: review.NewSession, path: "e.go", lines: []origin.Line{{Text: "gone", Old: 2}, {Text: "new", New: 2}},
			who: "codex, new session", note: "claude-opus-5-5 · 7s", relation: "new session, without this code's history",
		},
	} {
		f := &fakeAsk{answer: answer, installed: tc.installed}
		m := askScreen(askFiles(), Saved{}, f)
		typeKeys(m, tc.keys+"a")
		if box := entryBox(t, m); !strings.HasPrefix(shownTitle(box), "ASK "+tc.label+" · L") || !slices.Equal(shownText(box), []string{tc.placeholder}) {
			t.Errorf("%s: the box\n%s\nwant it to ask %s %q", tc.name, strings.Join(box, "\n"), tc.label, tc.placeholder)
		}
		typeKeys(m, "<enter>")
		want := tc.ask
		want.Prompt = review.Prompt(tc.rel, tc.path, tc.lines, tc.placeholder)
		if got := f.asked(); len(got) != 1 || got[0] != want {
			t.Errorf("%s: asked\n%+v\nwant\n%+v", tc.name, got, want)
		}
		thread := "THREAD · 1\nyou  just now\n" + tc.placeholder + "\n● " + tc.who + "  decided  " + tc.note + "\n"
		if tc.relation != "" {
			thread += "↳ " + tc.relation + "\n"
		}
		thread += "토큰 버킷이라 float64로 했어요.\n╌\n"
		if text := boxText(m); !strings.HasPrefix(text, thread) {
			t.Errorf("%s: the box\n%s\nwant it to start with\n%s", tc.name, text, thread)
		}
	}
}

func TestAskNewSession(t *testing.T) {
	// Questions for a new session of an agent go on in the session the first one started,
	// about any lines, in this run and the next. The log keeps who was asked.
	dir := t.TempDir()
	f := &fakeAsk{answer: agent.Answer{Fork: "new-1", Text: "추측이에요. [guess]", Model: "anthropic/claude-opus-5-5", Took: time.Second}, installed: []string{"omp"}}
	m := askScreen(askFiles(), Saved{Dir: dir}, f)
	typeKeys(m, "40Ga<enter><esc>39GaWhy gone?<enter>")
	if got := f.asked(); len(got) != 2 || got[0].Fork != "" || got[1] != (agent.Question{Agent: "omp", Cwd: root, Fork: "new-1", Prompt: got[1].Prompt}) {
		t.Errorf("asked %+v, want a new omp session and then the same one", got)
	}
	// A new session has no model of its own that another could answer in place of. The model
	// shows without its provider.
	const thread = "Why gone?\n● omp, new session  guess  claude-opus-5-5 · 1s\n↳ new session, without this code's history\n추측이에요.\n╌\n"
	if text := boxText(m); !strings.Contains(text, thread) {
		t.Errorf("the box:\n%s\nwant the question, who was asked, the answer and its model", text)
	}
	records, bad, err := state.NewLog(state.LogPath(dir)).Read()
	if err != nil || bad != 0 {
		t.Fatal(bad, err)
	}
	for _, q := range records.Questions {
		if q.Agent != "omp" || q.Session != "" || q.Relation != "new" {
			t.Errorf("question record %+v, want one for a new omp session", q)
		}
	}
	if len(records.Questions) != 2 || len(records.Forks) != 1 || records.Forks[0].Agent != "omp" || records.Forks[0].Session != "" || records.Forks[0].Fork != "new-1" {
		t.Errorf("records %+v, want two questions and one new session", records)
	}

	// The next run goes on in it too, and shows who answered the earlier questions.
	f = &fakeAsk{answer: answer, installed: []string{"omp"}}
	m = askScreen(askFiles(), Load(dir), f)
	typeKeys(m, "39G<enter>")
	if text := boxText(m); !strings.Contains(text, thread) {
		t.Errorf("the box of the earlier question:\n%s", text)
	}
	typeKeys(m, "<esc>40Ga<enter>")
	if got := f.asked(); len(got) != 1 || got[0].Fork != "new-1" {
		t.Errorf("asked %+v, want it in new-1", got)
	}
}

func TestAskBusy(t *testing.T) {
	// One question at a time goes to a session; another session can be asked meanwhile.
	f := &fakeAsk{answer: answer}
	m := askScreen(askFiles(), Saved{}, f)
	cmd := typeAndKeep(m, "4ja<enter>")
	typeKeys(m, "<esc>ka")
	const busy = "Still waiting for claude c1a0 to answer"
	if m.message != busy || m.asking != nil {
		t.Errorf("a on the same session: message %q, input open %t; want %q", m.message, m.asking != nil, busy)
	}
	typeKeys(m, "31Ga") // row 30, the Codex line
	if box := entryBox(t, m); m.asking == nil || !strings.HasPrefix(shownTitle(box), "ASK codex 3333 #1 · ") {
		t.Errorf("a on another session: the box\n%s\nwant one that asks codex", strings.Join(box, "\n"))
	}
	typeKeys(m, "<esc>")
	send(m, cmd())
	if typeKeys(m, "4Ga"); m.asking == nil {
		t.Errorf("a after the answer came: message %q, want the input", m.message)
	}
}

func TestAskAnswer(t *testing.T) {
	// The question goes to a new fork of the session, in its directory, with the model that
	// wrote the line. The origin box opens under the line with the thread first: the
	// question, waiting for its answer, and then the answer with the badge of its tag, which
	// the box does not show. A rule parts the thread from the origin of the line. The log
	// keeps the question, the fork and the answer as the agent sent it.
	dir := t.TempDir()
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{Dir: dir}, f)
	m.now = func() time.Time { return start }
	cmd := typeAndKeep(m, "4ja<enter>")
	waiting := "THREAD · 1\nyou  just now\n" + whyAdded + "\n● claude c1a0  " + askingFor(0) + "\n╌\n\nREQUEST · #1\n"
	if text := boxText(m); !m.reading || m.cursor != 4 || !strings.HasPrefix(text, waiting) {
		t.Fatalf("after Enter: box open %t at row %d with\n%s\nwant it open at row 4, starting with\n%s", m.reading, m.cursor, text, waiting)
	}
	if s := screen(t, m); !strings.Contains(s, "asking… 0s") {
		t.Errorf("the screen has no asking… 0s:\n%s", s)
	}
	send(m, cmd())
	want := agent.Question{
		Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5",
		Prompt: review.Prompt(review.Author, "a.go", []origin.Line{{Text: "Changed 2", New: 2}}, whyAdded),
	}
	if got := f.asked(); len(got) != 1 || got[0] != want {
		t.Errorf("asked %+v, want %+v", got, want)
	}
	answered := "THREAD · 1\nyou  just now\n" + whyAdded + "\n● claude c1a0  decided  claude-opus-5-5 · 7s\n토큰 버킷이라 float64로 했어요.\n╌\n\nREQUEST"
	if text := boxText(m); !strings.HasPrefix(text, answered) {
		t.Errorf("the box:\n%s\nwant it to start with\n%s", text, answered)
	}
	s := screen(t, m)
	if !strings.Contains(s, "▎ 토큰 버킷이라 float64로 했어요.") || strings.Contains(s, "[decided]") || strings.Contains(s, "asking…") || m.message != "" {
		t.Errorf("the box shows the answer without its tag, so the status line says nothing, got %q:\n%s", m.message, s)
	}

	// A question on another line of the session goes on in the fork. The box of each line
	// shows only its own questions. An answer without a tag has no badge.
	f.answer.Text = "지운 줄이에요."
	typeKeys(m, "<esc>kaWhy delete it?<enter>")
	if got := f.asked(); len(got) != 2 || got[1].Fork != "fork-1" || !strings.Contains(got[1].Prompt, "a.go, deleted line 2 of the base file.") {
		t.Errorf("the second question: got %+v, want it in fork-1 about the deleted line 2", got)
	}
	if text := boxText(m); !strings.Contains(text, "Why delete it?\n● claude c1a0  claude-opus-5-5 · 7s\n지운 줄이에요.\n") || strings.Contains(text, whyAdded) {
		t.Errorf("the box of row 3:\n%s\nwant only its own question", text)
	}
	records, bad, err := state.NewLog(state.LogPath(dir)).Read()
	if err != nil || bad != 0 {
		t.Fatal(bad, err)
	}
	q := records.Questions[0]
	wantLines := [3][]state.Line{
		{{Kind: " ", Text: "line 1"}, {Kind: "-", Text: "line 2"}},
		{{Kind: "+", Text: "Changed 2"}},
		{{Kind: " ", Text: "line 3"}, {Kind: " ", Text: "line 4"}},
	}
	if len(records.Questions) != 2 || q.Agent != "claude" || q.Session != claudeSession || q.Relation != "author" || q.Path != "a.go" || q.Text != whyAdded || len(q.ID) != 16 ||
		!slices.Equal(q.Before, wantLines[0]) || !slices.Equal(q.Lines, wantLines[1]) || !slices.Equal(q.After, wantLines[2]) {
		t.Errorf("question records %+v, want two, the first about +Changed 2", records.Questions)
	}
	// The fork knows the session up to its newest edit, claude #2.
	fork := state.Fork{Agent: "claude", Session: claudeSession, Fork: "fork-1", Base: claude2.Time}
	if len(records.Forks) != 1 || records.Forks[0].Time.IsZero() {
		t.Fatalf("fork records %+v, want one", records.Forks)
	}
	if got := records.Forks[0]; got.Agent != fork.Agent || got.Session != fork.Session || got.Fork != fork.Fork || !got.Base.Equal(fork.Base) {
		t.Errorf("fork record %+v, want %+v", got, fork)
	}
	if a := records.Answers[q.ID]; a.Fork != "fork-1" || a.Model != "claude-opus-5-5" || a.TookMS != 7012 || a.Text != answer.Text || a.Error != "" {
		t.Errorf("answer record %+v", a)
	}
}

func TestAskModels(t *testing.T) {
	// The header of an answer has the session's dot and name in its color, the badge of its
	// tag as a chip, green, blue or amber, and at its right edge the model that answered,
	// without its provider, and how long it took; in the warning color with the session's
	// model when that is another. A question without an answer says no answer on a red chip,
	// and why under it in red.
	type pick func(*theme) color.Color
	for _, tc := range []struct {
		name       string
		answer     agent.Answer
		err        error
		badge      string
		badgeColor pick // nil for no badge
		note       string
		warn       bool
		body       string
		bodyColor  pick
	}{
		{
			name: "the session's model", answer: answer,
			badge: "decided", badgeColor: func(t *theme) color.Color { return t.add }, note: "claude-opus-5-5 · 7s",
			body: "토큰 버킷이라 float64로 했어요.", bodyColor: func(t *theme) color.Color { return t.fg },
		},
		{
			name: "the session's model from a provider", answer: agent.Answer{Fork: "f", Text: "[chose] a", Model: "anthropic/claude-opus-5-5", Took: 41 * time.Second},
			badge: "chose", badgeColor: func(t *theme) color.Color { return t.accent }, note: "claude-opus-5-5 · 41s",
			body: "a", bodyColor: func(t *theme) color.Color { return t.fg },
		},
		{
			name: "another model", answer: agent.Answer{Fork: "f", Text: "a [guess]", Model: "claude-sonnet-5", Took: 7 * time.Second},
			badge: "guess", badgeColor: func(t *theme) color.Color { return t.pending }, note: "claude-sonnet-5, not claude-opus-5-5 · 7s", warn: true,
			body: "a", bodyColor: func(t *theme) color.Color { return t.fg },
		},
		{
			name: "no model", answer: agent.Answer{Fork: "f", Text: "a"},
			note: "unknown, not claude-opus-5-5 · 0s", warn: true, body: "a", bodyColor: func(t *theme) color.Color { return t.fg },
		},
		{
			name: "no answer", err: errors.New("claude is not installed or not in PATH"),
			badge: "no answer", badgeColor: func(t *theme) color.Color { return t.del },
			body: "claude is not installed or not in PATH", bodyColor: func(t *theme) color.Color { return t.del },
		},
	} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: tc.answer, err: tc.err})
		typeKeys(m, "4ja<enter>")
		th := &m.theme
		rows := m.originLines()
		i := slices.IndexFunc(rows, func(r cardRow) bool { return strings.HasPrefix(chunksText(r.left), "● claude c1a0") })
		if i < 0 || i+1 >= len(rows) {
			t.Fatalf("%s: no answer header in\n%s", tc.name, boxText(m))
		}
		head, own := rows[i].left, m.sessionColor(session{"claude", claudeSession})
		if head[0].color != own || head[1] != (chunk{text: "claude c1a0", color: own, bold: true}) {
			t.Errorf("%s: who answered %+v, want the dot and the name in the session's color", tc.name, head[:2])
		}
		last := head[len(head)-1]
		switch {
		case tc.badgeColor == nil && last.chip:
			t.Errorf("%s: badge %+v, want none", tc.name, last)
		case tc.badgeColor != nil && last != (chunk{text: tc.badge, color: tc.badgeColor(th), chip: true}):
			t.Errorf("%s: badge %+v, want the chip %q", tc.name, last, tc.badge)
		}
		var note []chunk
		if tc.note != "" {
			c := th.subtle
			if tc.warn {
				c = th.pending
			}
			note = []chunk{{text: tc.note, color: c}}
		}
		if !slices.Equal(rows[i].right, note) {
			t.Errorf("%s: note %+v, want %+v", tc.name, rows[i].right, note)
		}
		if body := rows[i+1].left; len(body) != 1 || body[0] != (chunk{text: tc.body, color: tc.bodyColor(th)}) {
			t.Errorf("%s: then %+v, want %q", tc.name, body, tc.body)
		}
	}
}

func TestModelNote(t *testing.T) {
	// The note on an answer and on a fix warns only when it can compare: the session's own
	// model is known, and the one that answered is unknown or another. A model picked with ^
	// is never warned of: the note marks it with ^ and names the model that answered, or the
	// one picked when the CLI did not tell.
	for _, tc := range []struct {
		name, model, own, picked string
		want                     string
		warn                     bool
	}{
		{name: "own unknown", model: "claude-opus-5-5", want: "claude-opus-5-5 · 8s"},
		{name: "both unknown", want: "unknown · 8s"},
		{name: "answering unknown", own: "claude-opus-5-5", want: "unknown, not claude-opus-5-5 · 8s", warn: true},
		{name: "another model", model: "claude-sonnet-5", own: "claude-opus-5-5", want: "claude-sonnet-5, not claude-opus-5-5 · 8s", warn: true},
		{name: "same model", model: "anthropic/claude-opus-5-5", own: "claude-opus-5-5", want: "claude-opus-5-5 · 8s"},
		{name: "picked, another answering", model: "claude-sonnet-5-5", own: "claude-opus-5-5", picked: "sonnet", want: "^claude-sonnet-5-5 · 8s"},
		{name: "picked, answering unknown", own: "claude-opus-5-5", picked: "anthropic/claude-haiku-5-5", want: "^claude-haiku-5-5 · 8s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &model{
				theme:   newTheme(nil, true, false),
				newest:  map[session]*agent.Edit{{"claude", "s"}: {Model: tc.own}},
				records: state.Records{Requests: []state.Request{{ID: "r", Agent: "claude", Session: "s", Model: tc.picked}}},
			}
			want := chunk{text: tc.want, color: m.theme.subtle}
			if tc.warn {
				want.color = m.theme.pending
			}
			q := &question{Question: state.Question{Relation: string(review.Author), Model: tc.picked}, own: tc.own}
			if got := m.answeredBy(q, &state.Answer{Model: tc.model, TookMS: 8000}); got != want {
				t.Errorf("answer note: got %+v, want %+v", got, want)
			}
			if got := m.fixedBy(&state.Fix{Request: "r", Agent: "claude", Session: "s", Model: tc.model, TookMS: 8000}); got != want {
				t.Errorf("fix note: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestAskNotice(t *testing.T) {
	// An answer that comes while the box is closed, or shows another line, is told of in a
	// notice at the top right of the diff until the next key. Enter goes to read it.
	const read = "  ⏎ to read   esc dismiss"
	for _, tc := range []struct {
		name, keys string // after the question on row 4 went
		err        error
		notice     []string
		answer     string // in the box
	}{
		{
			name: "box closed", keys: "<esc>G", notice: []string{"● Answer from claude c1a0 #1 on a.go:2", read},
			answer: "● claude c1a0  decided  claude-opus-5-5 · 7s\n토큰 버킷이라 float64로 했어요.\n╌",
		},
		{
			name: "box on another line", keys: "<esc>2j<enter>", notice: []string{"● Answer from claude c1a0 #1 on a.go:2", read},
			answer: "● claude c1a0  decided  claude-opus-5-5 · 7s\n토큰 버킷이라 float64로 했어요.\n╌",
		},
		{
			name: "file folded since", keys: "<esc>v", notice: []string{"● Answer from claude c1a0 #1 on a.go:2", read},
			answer: "● claude c1a0  decided  claude-opus-5-5 · 7s\n토큰 버킷이라 float64로 했어요.\n╌",
		},
		{
			name: "no answer", keys: "<esc>", err: errors.New("no answer in 3 minutes"), notice: []string{"✗ No answer from claude c1a0 #1 on a.go:2", read},
			answer: "● claude c1a0  no answer\nno answer in 3 minutes\n╌",
		},
	} {
		m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{answer: answer, err: tc.err})
		cmd := typeAndKeep(m, "4ja<enter>")
		typeKeys(m, tc.keys)
		send(m, cmd())
		if got := noticeLines(t, m); !slices.Equal(got, tc.notice) || m.message != "" {
			t.Errorf("%s: notice %q and %q on the status line, want the notice %q", tc.name, got, m.message, tc.notice)
		}
		typeKeys(m, "<enter>")
		want := "THREAD · 1\nyou  just now\n" + whyAdded + "\n" + tc.answer
		if text := boxText(m); m.cursor != 4 || !m.reading || !strings.HasPrefix(text, want) || noticeLines(t, m) != nil {
			t.Errorf("%s: Enter left the cursor at row %d with the box open %t:\n%s\nwant the box of row 4 starting with\n%s", tc.name, m.cursor, m.reading, text, want)
		}
	}
	// Esc only dismisses the notice; another key dismisses it and does its own work, and
	// Enter is Enter again.
	for _, tc := range []struct {
		keys   string
		cursor int
		box    bool
	}{
		{"<esc><enter>", 26, true},
		{"k<enter>", 25, true},
		{"<esc>", 26, false},
	} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
		cmd := typeAndKeep(m, "4ja<enter>")
		typeKeys(m, "<esc>G")
		send(m, cmd())
		typeKeys(m, tc.keys)
		if m.cursor != tc.cursor || m.reading != tc.box || strings.Contains(boxText(m), "THREAD") || noticeLines(t, m) != nil {
			t.Errorf("%q: cursor at row %d, box open %t, notice %q; want row %d, box %t and no notice", tc.keys, m.cursor, m.reading, noticeLines(t, m), tc.cursor, tc.box)
		}
	}
}

func TestAskTick(t *testing.T) {
	// While a question waits for its answer, a tick comes once a second, and the box counts
	// the seconds it has waited. No tick comes while no question waits, nor once the answer
	// came. The header of the question says how long ago it was asked.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	now := start
	m.now = func() time.Time { return now }
	ticks := 0
	m.tick = func() tea.Cmd {
		ticks++
		return nil
	}
	typeKeys(m, "4j<enter>jk<esc>")
	cmd := typeAndKeep(m, "a<enter>")
	if text := boxText(m); ticks != 1 || !strings.Contains(text, "\n● claude c1a0  "+askingFor(0)+"\n") {
		t.Errorf("after the question went: %d ticks, the box\n%s\nwant 1 tick and the question waiting 0s", ticks, text)
	}
	now = now.Add(12 * time.Second)
	typeKeys(m, "j") // keys while the tick is on its way ask for no more
	m.Update(tickMsg{})
	if text := boxText(m); ticks != 2 || !strings.Contains(text, "\nyou  just now\n") || !strings.Contains(text, "\n● claude c1a0  "+askingFor(12*time.Second)+"\n") {
		t.Errorf("after a tick 12s later: %d ticks, the box\n%s\nwant 2 ticks and the question waiting 12s", ticks, text)
	}
	send(m, cmd())
	m.Update(tickMsg{}) // the tick on its way when the answer came
	now = now.Add(4 * time.Minute)
	typeKeys(m, "jk")
	if text := boxText(m); ticks != 2 || !strings.Contains(text, "\nyou  4m\n") || !strings.Contains(text, "\n● claude c1a0  decided  claude-opus-5-5 · 7s\n") {
		t.Errorf("after the answer: %d ticks, the box\n%s\nwant still 2 ticks, the question 4m ago and its answer", ticks, text)
	}
}

func TestAskFromBox(t *testing.T) {
	// a in the open origin box asks about what the newest question there asked about, as
	// GitHub's Reply answers a thread: here the selection of rows 2 to 4, from the box of
	// either row. It closes the origin box and opens the input box under the same row; Enter
	// opens the origin box again, which stays under its row and shows the newest question with
	// its answer when the thread does not fit. In a box without questions a asks about the
	// box's changed line, and in a box with nothing to ask about it says why and the origin
	// box stays. The header of the origin card tells of a where it asks, as a follow-up in a
	// thread.
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{}, f)
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 14}) // the box shows 9 lines of its text
	top := func() string { return strings.Split(screen(t, m), "\n")[cursorLine(m)+1] }
	typeKeys(m, "2jVjja<enter>")
	f.answer.Text = "[chose] 둘 다 버킷 상태라서요."
	if !strings.Contains(top(), "a follow-up   ⏎ close") {
		t.Errorf("the card's header %q does not tell of a", top())
	}
	typeKeys(m, "a")
	if box := entryBox(t, m); m.reading || m.cursor != 4 || !strings.Contains(top(), "╭─ ASK  claude c1a0 #1 · follow-up · L1-2 ") ||
		!slices.Equal(shownText(box), []string{whyLines}) {
		t.Errorf("a in the box: origin box open %t at row %d, the line under it %q; want the input box for the selection there", m.reading, m.cursor, top())
	}
	typeKeys(m, "Why both?<enter>")
	lines := []origin.Line{{Text: "line 1", Old: 1, New: 1}, {Text: "line 2", Old: 2}, {Text: "Changed 2", New: 2}}
	if got := f.asked(); len(got) != 2 || got[1].Fork != "fork-1" || got[1].Prompt != review.Prompt(review.Author, "a.go", lines, "Why both?") {
		t.Errorf("asked %+v, want the follow-up in fork-1 about the selection", got)
	}
	thread := "THREAD · 2\nyou  just now\n" + whyLines + "\n● claude c1a0  decided  claude-opus-5-5 · 7s\n토큰 버킷이라 float64로 했어요.\n\n" +
		"you  just now\nWhy both?\n● claude c1a0  chose  claude-opus-5-5 · 7s\n둘 다 버킷 상태라서요.\n╌\n"
	if text := boxText(m); !m.reading || m.typing() || m.cursor != 4 || !strings.HasPrefix(text, thread) {
		t.Errorf("box open %t at row %d:\n%s\nwant it open at row 4 starting with\n%s", m.reading, m.cursor, text, thread)
	}
	if s := screen(t, m); !strings.Contains(s, "Why both?") || !strings.Contains(s, "둘 다 버킷 상태라서요.") || strings.Contains(s, "THREAD · 2") {
		t.Errorf("the box does not show the newest question and its answer:\n%s", s)
	}
	for _, tc := range []struct {
		keys, top string
	}{
		{keys: "<esc>gg2j<enter>a", top: "╭─ ASK  claude c1a0 #1 · follow-up · L1-2 "}, // an unchanged line of the selection
		{keys: "<esc><esc>gg12j<enter>a", top: "╭─ ASK  omp a1f2 #1 · L15 "},           // no questions
	} {
		typeKeys(m, tc.keys)
		if m.reading || !m.typing() || !strings.Contains(top(), tc.top) {
			t.Errorf("%q: origin box open %t, the line under the row %q; want the input box %q", tc.keys, m.reading, top(), tc.top)
		}
	}
	typeKeys(m, "<esc><esc>gg5j<enter>a")
	if why := "Move to a changed line to ask"; !m.reading || m.typing() || m.message != why || !strings.Contains(top(), "⏎ close") || strings.Contains(top(), "a ask") {
		t.Errorf("a on an unchanged line: origin box open %t, input box open %t, message %q; want the origin box and %q", m.reading, m.typing(), m.message, why)
	}
}

func TestAskFromBoxEsc(t *testing.T) {
	// Esc in the input box that a opened from the origin box brings the origin box back as
	// it was, scrolled as far, without asking.
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Saved{}, f)
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 10}) // the origin box shows 5 lines of its text
	typeKeys(m, "4j<enter>jj")
	if m.readTop != 2 {
		t.Fatalf("the origin box shows its text from line %d, want 2", m.readTop)
	}
	typeKeys(m, "a")
	if m.reading || !m.typing() {
		t.Fatalf("a: origin box open %t, input box open %t; want only the input box", m.reading, m.typing())
	}
	typeKeys(m, "why<esc>")
	if !m.reading || m.typing() || m.readTop != 2 || m.cursor != 4 || len(f.asked()) != 0 {
		t.Errorf("Esc: origin box open %t from line %d at row %d, input box open %t, %d questions; want the origin box as it was and none asked",
			m.reading, m.readTop, m.cursor, m.typing(), len(f.asked()))
	}
}

func TestAgo(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "just now"}, // a clock set back
		{0, "just now"},
		{59 * time.Second, "just now"},
		{time.Minute, "1m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23*time.Hour + 59*time.Minute, "23h"},
		{24 * time.Hour, "1d"},
		{71 * time.Hour, "2d"},
	} {
		if got := ago(tc.d); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestQuestionMark(t *testing.T) {
	// A row with questions has a dim ? where the cursor bar goes, unless the cursor is on it.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "4ja<enter><esc>")
	bar := func(i int) string {
		return firstRune(ansi.Strip(m.drawRow(i, 80)))
	}
	if got := bar(4); got != "▌" {
		t.Errorf("the cursor row starts with %q, want the bar", got)
	}
	typeKeys(m, "j")
	if got := []string{bar(3), bar(4), bar(5)}; !slices.Equal(got, []string{" ", "?", "▌"}) {
		t.Errorf("rows 3 to 5 start with %q, want nothing, a ? and the bar", got)
	}
}

func TestSelect(t *testing.T) {
	// V selects lines from the cursor, as vim does. Moves extend the selection but stay in
	// its file, a.go's lines on rows 2 to 18, or c/new.go's on 23 to 26.
	for _, tc := range []struct {
		keys      string
		selecting bool
		cursor    int
		status    string
	}{
		{keys: "2jV", selecting: true, cursor: 2, status: "VISUAL   1 line · L1"},
		{keys: "2jVjj", selecting: true, cursor: 4, status: "VISUAL   3 lines · L1-2   ● claude c1a0"},
		{keys: "5jVgg", selecting: true, cursor: 2, status: "VISUAL   4 lines"},
		{keys: "2jVG", selecting: true, cursor: 18, status: "VISUAL   ! The selection mixes sessions (claude c1a0, omp a1f2)"}, // not the hunk header
		{keys: "2jV100j", selecting: true, cursor: 18},
		{keys: "2jV3G", selecting: true, cursor: 2},
		{keys: "2jVdd", selecting: true, cursor: 18},
		{keys: "2jV}}}}", selecting: true, cursor: 18}, // the next change is in another file
		{keys: "2jV/new.go<enter>", cursor: 21},        // / ends it
		{keys: "9jVk", selecting: true, cursor: 8, status: "VISUAL   1 line"},
		{keys: "V", selecting: true, cursor: 2},  // from a file header to its first line
		{keys: "jV", selecting: true, cursor: 2}, // from a hunk header to its first line
		{keys: "21jVk", selecting: true, cursor: 23},
		{keys: "lV", cursor: 19, status: "This file has no lines to select"},
		{keys: "2jVjV", cursor: 3},
		{keys: "2jVj<esc>", cursor: 3},
		{keys: "2jVjl", cursor: 19},
		{keys: "2jVjv", cursor: 0},
		{keys: "2jVj<enter>", cursor: 3},
		{keys: "s2jVjj", selecting: true, cursor: 4, status: "VISUAL   3 lines"},
		{keys: "s2jVG", selecting: true, cursor: 17}, // the split view's last line of a.go
		{keys: "2jVjs", cursor: 3},
	} {
		m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{})
		typeKeys(m, tc.keys)
		if m.selecting != tc.selecting || m.cursor != tc.cursor {
			t.Errorf("%q: selecting %t with the cursor at row %d, want %t and row %d", tc.keys, m.selecting, m.cursor, tc.selecting, tc.cursor)
		}
		if status := statusLine(t, m); !strings.Contains(status, tc.status) {
			t.Errorf("%q: status line %q has no %q", tc.keys, status, tc.status)
		}
	}

	// The wheel extends it too, and a click in the tree ends it.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	typeKeys(m, "2jV")
	down := tea.MouseWheelMsg{X: 60, Y: 5, Button: tea.MouseWheelDown}
	send(m, down, down, down, down, down, down, down)
	if !m.selecting || m.cursor != 18 {
		t.Errorf("wheel: selecting %t at row %d, want row 18", m.selecting, m.cursor)
	}
	send(m, tea.MouseClickMsg{X: 5, Y: treeHead + 1, Button: tea.MouseLeft})
	if m.selecting || m.cursor != 19 {
		t.Errorf("tree click: selecting %t at row %d, want it ended at row 19", m.selecting, m.cursor)
	}
}

func TestSelectTint(t *testing.T) {
	// Selected rows are tinted, both sides in the split view, and added and deleted lines
	// stay told apart under the tint.
	for _, keys := range []string{"2jVjj", "s2jVj"} {
		m := askScreen(testFiles(), Saved{}, &fakeAsk{})
		typeKeys(m, keys)
		selected := []string{m.drawRow(3, 120), m.drawRow(2, 120)}
		typeKeys(m, "<esc>")
		plain := []string{m.drawRow(3, 120), m.drawRow(2, 120)}
		for i := range selected {
			if selected[i] == plain[i] || ansi.Strip(selected[i]) != ansi.Strip(plain[i]) {
				t.Errorf("%q: row %d looks the same selected, or reads differently:\n%q\n%q", keys, 3-i, selected[i], plain[i])
			}
		}
	}
	for _, dark := range []bool{true, false} {
		th := newTheme(nil, dark, false)
		unchanged, added, deleted := th.selbg, th.seladd, th.seldel
		if sameColor(added, deleted) || sameColor(added, unchanged) || sameColor(deleted, unchanged) || sameColor(added, th.addbg) || sameColor(deleted, th.delbg) {
			t.Errorf("dark %t: selected lines %v, %v and %v are not told apart", dark, unchanged, added, deleted)
		}
	}
}

func TestSelectTokens(t *testing.T) {
	// A selected row has its numbers on the selection's number color and its code on the
	// selection color of its kind: blue for an unchanged line, with green or red in it for
	// an added or a deleted one. The cursor keeps its marker. Rows 2 to 4 are line 1,
	// -line 2 and +Changed 2, the cursor on the last.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	th := &m.theme
	typeKeys(m, "2jVjj")
	for _, tc := range []struct {
		name string
		row  int
		want []string
	}{
		{"an unchanged line", 2, []string{th.paint(th.fg, th.selnum).Render("  1  1 "), th.paint(nil, th.selbg).Render("  ")}},
		{"a deleted line", 3, []string{th.paint(th.fg, th.selnum).Render("  2    "), th.paint(th.del, th.seldel).Render("- ")}},
		{"an added line under the cursor", 4, []string{
			th.paint(th.accent, th.selnum).Render("▌"), th.paint(th.strong, th.selnum).Bold(true).Render("     2 "), th.paint(th.add, th.seladd).Render("+ "),
		}},
	} {
		raw := m.drawRow(tc.row, 120)
		for _, want := range tc.want {
			if !strings.Contains(raw, want) {
				t.Errorf("%s: row %d %q has no %q", tc.name, tc.row, raw, want)
			}
		}
	}
}

func TestAskSelection(t *testing.T) {
	// a asks about the selected lines and ends the selection. The box opens under the last
	// line. In the split view a row's both sides are selected.
	for _, tc := range []struct {
		keys        string
		cursor      int
		placeholder string
		lines       []origin.Line // the lines the prompt quotes
	}{
		{
			keys: "2jVjja<enter>", cursor: 4, placeholder: whyLines,
			lines: []origin.Line{{Text: "line 1", Old: 1, New: 1}, {Text: "line 2", Old: 2}, {Text: "Changed 2", New: 2}},
		},
		{
			keys: "s3jVa<enter>", cursor: 3, placeholder: whyLines,
			lines: []origin.Line{{Text: "line 2", Old: 2}, {Text: "Changed 2", New: 2}},
		},
		{
			keys: "3jVa<enter>", cursor: 3, placeholder: whyDeleted, // one line selected
			lines: []origin.Line{{Text: "line 2", Old: 2}},
		},
	} {
		f := &fakeAsk{answer: answer}
		m := askScreen(testFiles(), Saved{}, f)
		typeKeys(m, tc.keys)
		want := review.Prompt(review.Author, "a.go", tc.lines, tc.placeholder)
		if got := f.asked(); len(got) != 1 || got[0].Prompt != want {
			t.Errorf("%q: asked %+v, want the prompt %q", tc.keys, got, want)
		}
		if m.selecting || !m.reading || m.cursor != tc.cursor || !strings.HasPrefix(boxText(m), "THREAD · 1\nyou  just now\n"+tc.placeholder+"\n") {
			t.Errorf("%q: selecting %t, box open %t at row %d:\n%s\nwant the box of row %d with the question", tc.keys, m.selecting, m.reading, m.cursor, boxText(m), tc.cursor)
		}
	}
	// The question goes with each line of the selection.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "2jVjja<enter><esc>")
	for _, row := range []int{2, 3, 4} {
		if len(m.questionsAt(row)) != 1 {
			t.Errorf("row %d has %d questions, want the selection's", row, len(m.questionsAt(row)))
		}
	}
}

func TestEarlierQuestions(t *testing.T) {
	// Questions of an earlier run go back to their lines when the lines and two around them
	// are found once in the diff. The others show in the box of their file's header, with
	// their lines quoted, in the look of a thread, all dim. A question dr quit before the
	// answer came says so. A question whose time the log does not know has none.
	dir := t.TempDir()
	log := state.NewLog(state.LogPath(dir))
	line := func(kind, text string) state.Line { return state.Line{Kind: kind, Text: text} }
	for _, r := range []state.Record{
		state.Question{
			ID: "q1", Agent: "claude", Session: claudeSession, Path: "a.go", Text: "Why Changed?",
			Before: []state.Line{line(" ", "line 1"), line("-", "line 2")}, Lines: []state.Line{line("+", "Changed 2")},
			After: []state.Line{line(" ", "line 3"), line(" ", "line 4")},
		},
		state.Fork{Agent: "claude", Session: claudeSession, Fork: "fork-0", Base: claude2.Time},
		state.Answer{Question: "q1", Fork: "fork-0", Model: "claude-opus-5-5", TookMS: 6400, Text: "Because."},
		state.Question{ID: "q2", Agent: "claude", Session: claudeSession, Path: "a.go", Text: "Why gone?", Lines: []state.Line{line("+", "\tgone line")}},
		state.Answer{Question: "q2", Error: "no answer in 3 minutes"},
		state.Question{ID: "q3", Agent: "claude", Session: claudeSession, Path: "c/new.go", Text: "Why x?", Lines: []state.Line{line("+", "\tvar x = 1")}, Before: []state.Line{}},
		state.Question{ID: "q4", Agent: "claude", Session: claudeSession, Path: "gone.go", Text: "Why?", Lines: []state.Line{line("+", "x")}},
	} {
		if err := log.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Load(dir), f)
	for _, tc := range []struct {
		keys      string
		want, not []string
	}{
		{
			keys: "4j<enter>",
			want: []string{"THREAD · 1\nyou\nWhy Changed?\n● claude c1a0  claude-opus-5-5 · 6s\nBecause.\n╌\n\nREQUEST"},
			not:  []string{"Why gone?"},
		},
		{
			keys: "<esc>gg<enter>",
			want: []string{"EARLIER QUESTIONS · 1\n>     gone line\nyou\nWhy gone?\n● claude c1a0  no answer\nno answer in 3 minutes"},
			not:  []string{"Why Changed?"},
		},
		{keys: "<esc>G<enter>", want: []string{"Why x?\n● claude c1a0  no answer\ndr closed before it came\n╌"}},
	} {
		typeKeys(m, tc.keys)
		text := boxText(m)
		for _, w := range tc.want {
			if !strings.Contains(text, w) {
				t.Errorf("%q: the box has no\n%s\nin\n%s", tc.keys, w, text)
			}
		}
		for _, w := range tc.not {
			if strings.Contains(text, w) {
				t.Errorf("%q: the box has %q:\n%s", tc.keys, w, text)
			}
		}
	}
	// The earlier questions are dim, and the header tells of them with a ?.
	typeKeys(m, "<esc>gg<enter>")
	rows := m.originLines()
	k := slices.IndexFunc(rows, func(r cardRow) bool { return chunksText(r.left) == "EARLIER QUESTIONS · 1" })
	if k < 0 {
		t.Fatalf("no earlier questions in\n%s", boxText(m))
	}
	for _, r := range rows[k+1:] {
		for _, c := range slices.Concat(r.left, r.right) {
			if strings.TrimSpace(c.text) != "" && (c.color != m.theme.muted || c.bold) {
				t.Errorf("%+v is not dim", c)
			}
		}
	}
	typeKeys(m, "<esc>j")
	if got := firstRune(ansi.Strip(m.drawRow(0, 80))); got != "?" {
		t.Errorf("the header of a.go starts with %q, want a ?", got)
	}
	// The fork of the earlier run goes on: the session has no edit newer than its base.
	typeKeys(m, "2ja<enter>")
	if got := f.asked(); len(got) != 1 || got[0].Fork != "fork-0" {
		t.Errorf("asked %+v, want it in fork-0", got)
	}
}

func TestSplitQuestionPlace(t *testing.T) {
	// In the split view the first row of a two-line replacement holds -d1 and +a1, which
	// have -d2 between them in the diff. A question about that row quotes those two, is kept
	// with the lines from -d1 to +a1, and finds its place, with those two only, after a
	// reopen.
	lines := parseDiff("x", "-d1", "-d2", "+a1", "+a2", "y")
	for i := 1; i <= 4; i++ {
		lines[i].Edit = claude1
	}
	files := []review.File{{Path: "f.go", Status: 'M', Lines: lines}}
	dir := t.TempDir()
	f := &fakeAsk{answer: answer}
	typeKeys(askScreen(files, Saved{Dir: dir}, f), "s3jVa<enter>")
	if got := f.asked(); len(got) != 1 || !strings.Contains(got[0].Prompt, "\nf.go:2. Lines marked - were deleted.\n-d1\n>a1\n"+whyLines) {
		t.Fatalf("asked %+v, want -d1 and +a1 quoted", got)
	}
	saved := Load(dir)
	want := []state.Line{{Kind: "-", Text: "d1"}, {Kind: "-", Text: "d2"}, {Kind: "+", Text: "a1"}}
	if qs := saved.Log.Questions; len(qs) != 1 || !slices.Equal(qs[0].Lines, want) || !slices.Equal(qs[0].Selected, []int{0, 2}) {
		t.Fatalf("question records %+v, want the lines from -d1 to +a1 with the first and the third selected", qs)
	}
	m := askScreen(files, saved, f)
	var with []int // the lines with the question, in m.lines: x, -d1, -d2, +a1, +a2, y
	for k := range m.lines {
		if len(m.asked[k]) > 0 {
			with = append(with, k)
		}
	}
	if len(m.files[0].earlier) != 0 || !slices.Equal(with, []int{1, 3}) {
		t.Errorf("after the reopen: %d earlier questions, the question with lines %v; want it with lines 1 and 3", len(m.files[0].earlier), with)
	}
}

func TestStaleFork(t *testing.T) {
	// A fork made before the session's newest edit does not know of it, so a new one is made.
	dir := t.TempDir()
	if err := state.NewLog(state.LogPath(dir)).Append(state.Fork{Agent: "claude", Session: claudeSession, Fork: "fork-0", Base: claude1.Time}); err != nil {
		t.Fatal(err)
	}
	f := &fakeAsk{answer: answer}
	m := askScreen(testFiles(), Load(dir), f)
	typeKeys(m, "4ja<enter>")
	if got := f.asked(); len(got) != 1 || got[0].Fork != "" {
		t.Errorf("asked %+v, want a new fork", got)
	}
}

func TestAskQuit(t *testing.T) {
	// Quitting stops the questions in flight and waits for them, but not for long. A stopped
	// question stays in the log without an answer, and a question that had not gone yet
	// does not go.
	dir := t.TempDir()
	started := make(chan struct{}, 2)
	ask := func(ctx context.Context, _ agent.Question) (agent.Answer, error) {
		started <- struct{}{}
		<-ctx.Done()
		return agent.Answer{}, ctx.Err()
	}
	m := newModel(askFiles(), nil, Saved{Dir: dir}, Asker{Ask: ask})
	m.tick = noTick
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	cmd := typeAndKeep(m, "4ja<enter>")
	later := typeAndKeep(m, "<esc>31Ga<enter>") // to the Codex session
	done := make(chan tea.Msg)
	go func() { done <- cmd() }()
	<-started
	stop := time.Now()
	m.flights.stop(stopWait)
	if took := time.Since(stop); took > time.Second {
		t.Errorf("stopping took %v", took)
	}
	if msg := <-done; msg != nil {
		t.Errorf("a stopped question brought %#v", msg)
	}
	if msg := later(); msg != nil || len(started) != 0 {
		t.Errorf("a question after the stop brought %#v and was asked %t", msg, len(started) != 0)
	}
	records, _, err := state.NewLog(state.LogPath(dir)).Read()
	if err != nil || len(records.Questions) != 1 || len(records.Answers) != 0 {
		t.Errorf("log: %+v, %v; want the first question without an answer", records, err)
	}

	// A question that does not stop is waited for only so long.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	m = newModel(testFiles(), nil, Saved{}, Asker{Ask: func(context.Context, agent.Question) (agent.Answer, error) {
		started <- struct{}{}
		<-release
		return answer, nil
	}})

	m.tick = noTick
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	cmd = typeAndKeep(m, "4ja<enter>")
	go cmd()
	<-started
	stop = time.Now()
	m.flights.stop(100 * time.Millisecond)
	if took := time.Since(stop); took > time.Second {
		t.Errorf("stopping a question that does not stop took %v", took)
	}
}

// firstRune returns the first character of s.
func firstRune(s string) string {
	r, _ := utf8.DecodeRuneInString(s)
	return string(r)
}

// sameColor reports whether a and b are the same color.
func sameColor(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
