package tui

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// fakeModels stands for agent.Models: it gives the models of each agent in models, or fails
// with err, and keeps the agents it was asked for.
type fakeModels struct {
	mu     sync.Mutex
	models map[string][]agent.Model
	err    error
	calls  []string
}

func (f *fakeModels) list(_ context.Context, name string) ([]agent.Model, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return f.models[name], f.err
}

// asked returns the agents f was asked for.
func (f *fakeModels) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// agentModels are what the fake Asker.Models lists: Claude's aliases and two full IDs, one
// of them the session's own model in testFiles, and Codex's models.
func agentModels() *fakeModels {
	return &fakeModels{models: map[string][]agent.Model{
		"claude": {
			{ID: "opus", Name: "Latest Opus"},
			{ID: "sonnet", Name: "Latest Sonnet"},
			{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5"},
			{ID: "claude-opus-5-5", Name: "Claude Opus 5.5"},
			{ID: "haiku", Name: "Latest Haiku"},
		},
		"codex": {{ID: "gpt-6-luna", Name: "GPT-6-Luna"}, {ID: "gpt-6.1-sol", Name: "GPT-6.1-Sol"}},
	}}
}

// pickFiles returns testFiles and f.go, rows 27 to 33. Two other Claude sessions wrote the
// lines on rows 30 and 31 with other models, Sonnet the newer, and a Codex session, the
// newest, the line on row 32 with its model and a reasoning effort.
func pickFiles() []review.File {
	f := parseDiff("x", "+haiku line", "+sonnet line", "+codex line", "y")
	f[1].Edit = &agent.Edit{Agent: "claude", Session: "4a1c0000-5555", Turn: 1, Time: start.Add(4 * time.Minute), Cwd: "/work", Model: "claude-haiku-5-5"}
	f[2].Edit = &agent.Edit{Agent: "claude", Session: "50e70000-6666", Turn: 1, Time: start.Add(5 * time.Minute), Cwd: "/work", Model: "claude-sonnet-5-5"}
	f[3].Edit = &agent.Edit{
		Agent: "codex", Session: "c0dex000-3333", Turn: 1, Time: start.Add(6 * time.Minute), Cwd: "/work", Model: "gpt-6.1-sol", Effort: "medium",
	}
	return append(testFiles(), review.File{Path: "f.go", Status: 'M', Lines: f})
}

// pickScreen returns the screen on files in a 125×24 terminal that asks through f and lists
// models through models, with what it kept in the state directory dir, at the time start.
// It does not tick.
func pickScreen(files []review.File, dir string, f *fakeAsk, models *fakeModels) *model {
	m := New(files, nil, Load(dir), Asker{Ask: f.ask, Installed: f.isInstalled, Models: models.list, Root: root}).(*model)
	m.tick, m.now = noTick, func() time.Time { return start }
	m.Update(tea.WindowSizeMsg{Width: 125, Height: 24})
	return m
}

// listRows returns the rows of the open model list of m, in order: a model's ID, after ❯ for
// the highlighted one, the row that takes the query as typed as Use "query" as typed, and a
// note as it reads. It returns nil while the list is closed.
func listRows(m *model) []string {
	if m.entry.picker == nil {
		return nil
	}
	rows := m.pickRows()
	at := m.entry.picker.index(rows)
	var out []string
	for k, r := range rows {
		s := r.id
		switch {
		case r.note != "":
			s = r.note
		case r.typed:
			s = `Use "` + r.id + `" as typed`
		}
		if k == at {
			s = "❯" + s
		}
		out = append(out, s)
	}
	return out
}

// shownList returns the lines of the model list on the screen of m without colors, each from
// the input box's left edge and without the spaces at its end, nil while no list shows.
func shownList(t *testing.T, m *model) []string {
	t.Helper()
	lines := strings.Split(screen(t, m), "\n")
	bottom := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "┗") })
	if bottom < 0 || m.entry.picker == nil {
		return nil
	}
	left := ansi.StringWidth(lines[bottom][:strings.Index(lines[bottom], "┗")])
	_, w := m.layout()
	var rows []string
	for _, l := range lines[bottom+1 : min(bottom+1+m.pickerHeight(w), len(lines))] {
		rows = append(rows, strings.TrimRight(ansi.Cut(l, left, m.width), " "))
	}
	return rows
}

func TestPickerOpens(t *testing.T) {
	// ^ opens the model list at the start of the text, after a space and after a line break,
	// in the input box of a and of c, and not in the middle of a word, where it is text.
	for _, tc := range []struct {
		keys string
		open bool
	}{
		{keys: "4ja^", open: true},
		{keys: "4jawhy ^", open: true},
		{keys: "4jawhy<alt+enter>^", open: true},
		{keys: "4jc^", open: true},
		{keys: "4jawhy^"},
		{keys: "4jaa^b"},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		typeKeys(m, tc.keys)
		if open := m.entry.picker != nil; open != tc.open || open != (len(shownList(t, m)) > 0) {
			t.Errorf("%q: list open %t with the screen\n%s\nwant open %t", tc.keys, open, screen(t, m), tc.open)
		}
	}
}

func TestPickerOrder(t *testing.T) {
	// The list holds the session's own model first, marked, then the models other Claude
	// sessions of the review used, the newest edit first, then Claude's own list, each model
	// once: a model of Claude's list already listed keeps its place and lends it its name.
	// Codex's model is not Claude's. The list is asked for once in the run.
	models := agentModels()
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	typeKeys(m, "4ja^")
	want := []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "opus", "sonnet", "haiku"}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	shown := []string{
		"❯ ● claude-opus-5-5    Claude Opus 5.5 · session",
		"  ● claude-sonnet-5-5  Claude Sonnet 5.5",
		"  ● claude-haiku-5-5",
		"  ● opus               Latest Opus",
		"  ● sonnet             Latest Sonnet",
		"  ● haiku              Latest Haiku",
	}
	if got := shownList(t, m); !slices.Equal(got, shown) {
		t.Errorf("the list\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(shown, "\n"))
	}
	typeKeys(m, "<esc><esc>27Gc^")
	if m.drafting == nil || m.entry.picker == nil {
		t.Fatal("c on c/new.go and ^: want the list open")
	}
	if got := models.asked(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("Models asked for %q, want claude once", got)
	}
}

func TestPickerLoading(t *testing.T) {
	// While the agent's list is coming, a dim row says so under the session's and the
	// review's models, which can be picked. When it fails, a dim row says why, and it is not
	// asked for again in the run.
	models := agentModels()
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	cmd := typeAndKeep(m, "4ja^")
	want := []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "Loading claude models…"}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("while loading: rows %q, want %q", got, want)
	}
	if got := shownList(t, m); len(got) != 4 || got[3] != "    Loading claude models…" {
		t.Errorf("while loading: the list\n%s", strings.Join(got, "\n"))
	}
	typeKeys(m, "<down><down><down>")
	if got := listRows(m); got[2] != "❯claude-haiku-5-5" {
		t.Errorf("Down past the last model: rows %q, want claude-haiku-5-5 highlighted", got)
	}
	send(m, cmd())
	if got := listRows(m); len(got) != 6 || got[2] != "❯claude-haiku-5-5" {
		t.Errorf("after the list came: rows %q, want the highlight kept", got)
	}

	models = agentModels()
	models.err = errors.New("claude exited: not logged in")
	m = pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	typeKeys(m, "4ja^")
	want = []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "Could not list claude models: claude exited: not logged in"}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("on an error: rows %q, want %q", got, want)
	}
	typeKeys(m, "<down><enter><esc>a^")
	if got := models.asked(); m.entry.pick != "" || !slices.Equal(got, []string{"claude"}) {
		t.Errorf("Models asked for %q, pick %q; want claude once and no pick in the new box", got, m.entry.pick)
	}
}

func TestPickerFilter(t *testing.T) {
	// The query filters the list, ignoring case: the models whose ID or name holds it, then
	// those whose ID holds its characters in order. A query that is not the ID of a model
	// listed can be taken as typed, on the last row.
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"sonnet", []string{"❯claude-sonnet-5-5", "sonnet"}},
		{"SON", []string{"❯claude-sonnet-5-5", "sonnet", `Use "SON" as typed`}},
		{"latest", []string{"❯opus", "sonnet", "haiku", `Use "latest" as typed`}},
		{"cs5", []string{"❯claude-opus-5-5", "claude-sonnet-5-5", `Use "cs5" as typed`}},
		{"5-5", []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", `Use "5-5" as typed`}},
		{"hku", []string{"❯claude-haiku-5-5", "haiku", `Use "hku" as typed`}},
		{"my-model", []string{`❯Use "my-model" as typed`}},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		typeKeys(m, "4ja^"+tc.query)
		if got := listRows(m); !slices.Equal(got, tc.want) {
			t.Errorf("%q: rows %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestPickerHighlight(t *testing.T) {
	// The highlight stays on its model while the query changes and the model still matches,
	// and else goes to the first row.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4ja^")
	for _, step := range []struct {
		keys, want string
	}{
		{"<down><down><down><down><down>", "haiku"},
		{"a", "haiku"},
		{"i", "haiku"},
		{"x", `Use "aix" as typed`},
		{"<bs>", "claude-haiku-5-5"},
	} {
		typeKeys(m, step.keys)
		rows := listRows(m)
		if !slices.Contains(rows, "❯"+step.want) {
			t.Errorf("after %q: rows %q, want %s highlighted", step.keys, rows, step.want)
		}
	}
}

func TestPickerKeys(t *testing.T) {
	// Up, Down, ctrl+p and ctrl+n move the highlight, which stays on the first and the last
	// row. Typing filters and Backspace edits the query. Enter and Tab pick. Esc closes the
	// list and leaves the token as typed. Deleting the ^, a key that moves the cursor, a space
	// or a new line closes it, and ctrl+c cancels the whole box.
	for _, tc := range []struct {
		name, keys string
		rows       []string // nil for the list closed
		text, pick string
		closed     bool // the box closed
	}{
		{name: "Down and Up", keys: "^<down><down><up>", rows: []string{"claude-opus-5-5", "❯claude-sonnet-5-5"}, text: "^"},
		{name: "ctrl+n and ctrl+p", keys: "^<ctrl+n><ctrl+n><ctrl+n><ctrl+p>", rows: []string{"claude-opus-5-5", "claude-sonnet-5-5", "❯claude-haiku-5-5"}, text: "^"},
		{name: "Up on the first row", keys: "^<up>", rows: []string{"❯claude-opus-5-5"}, text: "^"},
		{name: "Down on the last row", keys: "^hku<down><down><down>", rows: []string{"claude-haiku-5-5", "haiku", `❯Use "hku" as typed`}, text: "^hku"},
		{name: "typing and Backspace", keys: "^sonx<bs>", rows: []string{"❯claude-sonnet-5-5", "sonnet"}, text: "^son"},
		{name: "Enter", keys: "why ^son<down><enter>", text: "why ", pick: "sonnet"},
		{name: "Tab", keys: "why ^son<tab>", text: "why ", pick: "claude-sonnet-5-5"},
		{name: "Esc", keys: "why ^son<esc>", text: "why ^son"},
		{name: "Backspace on the ^", keys: "why ^<bs>", text: "why "},
		{name: "Left", keys: "why ^so<left>", text: "why ^so"},
		{name: "Right", keys: "why ^so<right>", text: "why ^so"},
		{name: "Home", keys: "why ^so<home>", text: "why ^so"},
		{name: "End", keys: "why ^so<end>", text: "why ^so"},
		{name: "a space", keys: "why ^so ", text: "why ^so "},
		{name: "a new line", keys: "why ^so<alt+enter>", text: "why ^so\n"},
		{name: "ctrl+c", keys: "why ^so<ctrl+c>", closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
			typeKeys(m, "4ja"+tc.keys)
			if tc.closed {
				if m.typing() || shownList(t, m) != nil {
					t.Errorf("box open %t, want the box and the list closed", m.typing())
				}
				return
			}
			rows := listRows(m)
			if len(rows) > len(tc.rows) {
				rows = rows[:len(tc.rows)]
			}
			if open := m.entry.picker != nil; !m.typing() || open != (tc.rows != nil) || !slices.Equal(rows, tc.rows) ||
				m.entry.text != tc.text || m.entry.pick != tc.pick {
				t.Errorf("box open %t, list open %t with rows %q, text %q, pick %q; want rows %q, text %q, pick %q",
					m.typing(), open, rows, m.entry.text, m.entry.pick, tc.rows, tc.text, tc.pick)
			}
		})
	}
}

func TestPickerPick(t *testing.T) {
	// Picking takes the token out of the text and names the model in the box's title, after
	// the lines. The query as typed can be picked too. Picking the session's own model clears
	// the pick.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	for _, step := range []struct {
		keys, text, title string
	}{
		{"4jawhy ^son<down><enter>", "why ", "Ask claude c1a0 #1 · L2 · ^sonnet (Enter:send Alt-Enter:newline ^:model Esc:cancel)"},
		{"^mine<enter>", "why ", "Ask claude c1a0 #1 · L2 · ^mine (Enter:send Alt-Enter:newline ^:model Esc:cancel)"},
		{"^<enter>", "why ", "Ask claude c1a0 #1 · L2 (Enter:send Alt-Enter:newline ^:model Esc:cancel)"},
	} {
		typeKeys(m, step.keys)
		if title := shownTitle(entryBox(t, m)); m.entry.picker != nil || m.entry.text != step.text || title != step.title {
			t.Errorf("after %q: list open %t, text %q, title %q; want the list closed, %q and %q",
				step.keys, m.entry.picker != nil, m.entry.text, title, step.text, step.title)
		}
	}
}

func TestPickerAsk(t *testing.T) {
	// a asks with the model picked in place of the session's, and with the CLI's default
	// reasoning effort, not the session's. The log keeps the model picked. The answer's
	// header marks it with ^, quietly, with the model that answered or, when the CLI did not
	// tell, the one picked, in this run and the next. The next question asks with the session's
	// model again.
	dir := t.TempDir()
	f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Text: "[chose] a", Took: 7 * time.Second}}
	m := pickScreen(pickFiles(), dir, f, agentModels())
	typeKeys(m, "33Gawhy? ^luna<enter><enter>")
	want := agent.Question{Agent: "codex", Session: "c0dex000-3333", Cwd: "/work", Model: "gpt-6-luna", Effort: ""}
	if got := f.asked(); len(got) != 1 || got[0].Agent != want.Agent || got[0].Session != want.Session || got[0].Model != want.Model || got[0].Effort != want.Effort {
		t.Fatalf("asked %+v, want %+v", got, want)
	}
	if qs := records(t, dir).Questions; len(qs) != 1 || qs[0].Model != "gpt-6-luna" || qs[0].Text != "why?" {
		t.Errorf("question records %+v, want one with the model picked", qs)
	}
	note := func(m *model) []chunk { // of the newest answer in the open origin card
		var last []chunk
		for _, r := range m.originLines() {
			if r.right != nil {
				last = r.right
			}
		}
		return last
	}
	if got, want := note(m), []chunk{{text: "^gpt-6-luna · 7s", color: m.theme.subtle}}; !slices.Equal(got, want) {
		t.Errorf("the answer's note %+v, want %+v", got, want)
	}
	f.answer.Model = "gpt-6-luna-2026-09"
	typeKeys(m, "<esc>a^gpt-6-luna<enter><enter>")
	if got, want := note(m), []chunk{{text: "^gpt-6-luna-2026-09 · 7s", color: m.theme.subtle}}; !slices.Equal(got, want) {
		t.Errorf("the second answer's note %+v, want %+v", got, want)
	}
	m = pickScreen(pickFiles(), dir, f, agentModels())
	typeKeys(m, "33G<enter>")
	if text := boxText(m); !strings.Contains(text, "^gpt-6-luna · 7s") || !strings.Contains(text, "^gpt-6-luna-2026-09 · 7s") {
		t.Errorf("the next run's thread:\n%s\nwant both notes", text)
	}
	typeKeys(m, "<esc>a<enter>")
	if got := f.asked(); len(got) != 3 || got[2].Model != "gpt-6.1-sol" || got[2].Effort != "medium" {
		t.Errorf("the next question: asked %+v, want the session's model and effort", got[2:])
	}
}

func TestPickerComment(t *testing.T) {
	// A comment keeps the model it picked: in the log, in its box's header, in the next run
	// and in the input box that edits it. Saving the edit unchanged writes nothing; changing
	// only the pick writes a record, and picking the session's own model clears it.
	dir := t.TempDir()
	m := pickScreen(pickFiles(), dir, &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4jcfix ^son<enter><enter>")
	if records := commentRecords(t, dir); len(records) != 1 || records[0].Text != "fix" || records[0].Model != "claude-sonnet-5-5" {
		t.Fatalf("records %+v, want the comment with its model", records)
	}
	const header = "✎ you → ● claude c1a0 · a.go:2 · ^claude-sonnet-5-5   pending"
	if box := boxOf(m, m.comments[0]); !strings.HasPrefix(box, header+"  c edit\n") {
		t.Errorf("the comment's box:\n%s\nwant it headed %q", box, header)
	}
	if s := screen(t, m); !strings.Contains(s, "✎ you → ● claude c1a0 · a.go:2 · ^claude-sonnet-5-5    pending ") {
		t.Errorf("the screen\n%s\nhas no header with the model", s)
	}
	m = pickScreen(pickFiles(), dir, &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4jc")
	if title := shownTitle(entryBox(t, m)); len(m.comments) != 1 || m.comments[0].Model != "claude-sonnet-5-5" ||
		!strings.HasPrefix(title, "Edit comment for claude c1a0 · L2 · ^claude-sonnet-5-5") {
		t.Fatalf("the next run: comments %+v, the box titled %q", m.comments, title)
	}
	typeKeys(m, "<enter>")
	if records := commentRecords(t, dir); len(records) != 1 {
		t.Errorf("saved unchanged: %d records, want 1", len(records))
	}
	typeKeys(m, "c ^<enter><enter>") // the ^ after a space, as the text ends with a word
	if records := commentRecords(t, dir); len(records) != 2 || records[1].Text != "fix" || records[1].Model != "" || m.comments[0].Model != "" {
		t.Errorf("the pick cleared: records %+v, want a second one without the model", records)
	}
}

func TestPickerSubmit(t *testing.T) {
	// Comments for one session that picked different models go as one request per model.
	// :submit sends the group of the session's first comment and holds the other back,
	// saying so, until the next :submit. A request with a picked model goes with it and the
	// CLI's default reasoning effort, not the session's, and the log keeps it; the note on
	// its fix marks it with ^ and does not warn. Editing a comment's pick drops its fix.
	for _, tc := range []struct {
		name, keys string
		held       string
		models     [2]string // of the first request and the second
		efforts    [2]string
		picked     string // the comment that picked a model
		edit       string // the keys that edit its pick
	}{
		{
			name: "the first comment picked", keys: "4jcone ^sonnet<enter><enter>Gcthree<enter>",
			held:   "Sent 1 fix request. Held back 1 comment for claude c1a0 with its own model; submit again when its fixes come",
			models: [2]string{"sonnet", "claude-opus-5-5"}, efforts: [2]string{"", "high"}, picked: "one", edit: "gg4jc ^<enter><enter>",
		},
		{
			name: "the second comment picked", keys: "4jcone<enter>Gcthree ^sonnet<enter><enter>",
			held:   "Sent 1 fix request. Held back 1 comment for claude c1a0 with ^sonnet; submit again when its fixes come",
			models: [2]string{"claude-opus-5-5", "sonnet"}, efforts: [2]string{"high", ""}, picked: "three", edit: "Gc ^<enter><enter>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, dir := testFiles(), t.TempDir()
			for _, f := range files { // the Claude session asked with a reasoning effort
				for i, l := range f.Lines {
					if e := l.Edit; e != nil && e.Agent == "claude" {
						high := *e
						high.Effort = "high"
						f.Lines[i].Edit = &high
					}
				}
			}
			f := &fakeAsk{answer: agent.Answer{Fork: "fork-1", Model: "claude-sonnet-5-5", Took: 7 * time.Second,
				Text: `{"fixes":[{"comment":1,"new_lines":["Changed two"],"note":"renamed"}]}`}}
			m := New(files, nil, Load(dir), Asker{Ask: f.ask, Installed: f.isInstalled, Models: agentModels().list, Root: writeTree(t, files)}).(*model)
			m.tick, m.now = noTick, fixClock()
			m.Update(tea.WindowSizeMsg{Width: 125, Height: 40})
			typeAll(m, tc.keys)
			cmd := typeAndKeep(m, "gg:submit<enter>")
			if m.message != tc.held {
				t.Errorf("message %q, want %q", m.message, tc.held)
			}
			run(m, cmd)
			typeAll(m, ":submit<enter>")
			asked := f.asked()
			if len(asked) != 2 || [2]string{asked[0].Model, asked[1].Model} != tc.models || [2]string{asked[0].Effort, asked[1].Effort} != tc.efforts {
				t.Fatalf("asked %+v, want the models %q with the efforts %q", asked, tc.models, tc.efforts)
			}
			picked := commentOf(m, tc.picked)
			if requests := records(t, dir).Requests; len(requests) != 2 || !slices.ContainsFunc(requests, func(r state.Request) bool {
				return r.Model == "sonnet" && slices.Equal(r.Comments, []string{picked.ID})
			}) {
				t.Errorf("requests %+v, want the picked comment's with sonnet", requests)
			}
			if box := boxOf(m, picked); !strings.Contains(box, "◆ fix  from claude c1a0  ^claude-sonnet-5-5 · 7s") {
				t.Errorf("box of %q:\n%s\nwant the fix's note marked with ^", tc.picked, box)
			}
			other := commentOf(m, "one")
			if other == picked {
				other = commentOf(m, "three")
			}
			if box := boxOf(m, other); !strings.Contains(box, "◆ fix  from claude c1a0  claude-sonnet-5-5, not claude-opus-5-5 · 7s") {
				t.Errorf("box of %q:\n%s\nwant the fix's note warning of another model", other.Text, box)
			}
			typeAll(m, tc.edit)
			if picked.fix != nil || picked.Model != "" || len(records(t, dir).Dropped) != 1 {
				t.Errorf("after its pick was cleared: %q has model %q, fix %+v, dropped %+v; want its fix dropped",
					picked.Text, picked.Model, picked.fix, records(t, dir).Dropped)
			}
		})
	}
}

func TestPickerLook(t *testing.T) {
	// The highlighted row is in the accent color and bold, the others have the ID in the text
	// color and the name dim. Without colors, as with NO_COLOR, no color reaches the
	// terminal, and ❯ and bold mark the highlighted row.
	for _, tc := range []struct {
		name string
		th   theme
	}{
		{"dark", newTheme(nil, true, false)},
		{"light", newTheme(nil, false, false)},
		{"no colors", newTheme(nil, true, true)},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		m.theme = tc.th
		typeKeys(m, "4ja^")
		th := &m.theme
		highlight, text, dim := th.paint(th.accent, th.surface).Bold(true), th.paint(th.fg, th.surface), th.paint(th.muted, th.surface)
		raw := m.View().Content
		for name, want := range map[string]string{
			"the highlight's mark": highlight.Render("❯ "),
			"the highlight's ID":   highlight.Render("claude-opus-5-5  "),
			"another ID":           text.Render("claude-haiku-5-5"),
			"another name":         dim.Render("  Claude Sonnet 5.5"),
		} {
			if !strings.Contains(raw, want) {
				t.Errorf("%s: the screen has no %s %q", tc.name, name, want)
			}
		}
		if !th.plain {
			continue
		}
		var out bytes.Buffer
		w := &colorprofile.Writer{Forward: &out, Profile: colorprofile.ASCII}
		if _, err := w.WriteString(raw); err != nil {
			t.Fatal(err)
		}
		if s := out.String(); strings.Contains(s, "38;") || strings.Contains(s, "48;") || !strings.Contains(s, "\x1b[1m❯ ") {
			t.Errorf("%s: as the terminal gets it\n%q\nwant no colors and a bold ❯", tc.name, s)
		}
	}
}

func TestPickerWidths(t *testing.T) {
	// A narrow list leaves out the names first, then cuts the IDs, never through a wide
	// character: screen checks every line is as wide as the screen.
	models := agentModels()
	models.models["claude"] = append(models.models["claude"], agent.Model{ID: "클로드-모델", Name: "한글 이름"})
	for _, tc := range []struct {
		width int
		want  []string // the first rows
	}{
		{125, []string{"❯ ● claude-opus-5-5    Claude Opus 5.5 · session", "  ● claude-sonnet-5-5  Claude Sonnet 5.5"}},
		{41, []string{"❯ ● claude-opus-5-5", "  ● claude-sonnet-5-5"}},
		{20, []string{"❯ ● claude-opu", "  ● claude-son"}},
		{9, []string{"❯ ● cl", "  ● cl"}},
	} {
		for _, th := range []theme{newTheme(nil, true, false), newTheme(nil, true, true)} {
			m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
			m.theme = th
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
			typeKeys(m, "4ja^")
			got := shownList(t, m)
			if len(got) != 7 || !slices.Equal(got[:2], tc.want) {
				t.Errorf("width %d: the list\n%s\nwant 7 rows starting with\n%s", tc.width, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		}
	}
}

func TestPickerScroll(t *testing.T) {
	// The list shows 8 rows at most and scrolls to keep the highlighted row in view.
	models := agentModels()
	for i := range 6 {
		models.models["claude"] = append(models.models["claude"], agent.Model{ID: "model-" + string(rune('a'+i))})
	}
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	typeKeys(m, "4ja^")
	for _, step := range []struct {
		keys        string
		first, last string
	}{
		{"", "❯ ● claude-opus-5-5", "  ● model-b"},
		{strings.Repeat("<down>", 8), "  ● claude-sonnet-5-5", "❯ ● model-c"},
		{strings.Repeat("<down>", 3), "  ● sonnet", "❯ ● model-f"},
		{strings.Repeat("<up>", 10), "❯ ● claude-sonnet-5-5", "  ● model-c"},
	} {
		typeKeys(m, step.keys)
		got := shownList(t, m)
		if len(got) != 8 || !strings.HasPrefix(got[0], step.first) || !strings.HasPrefix(got[7], step.last) {
			t.Errorf("after %q: the list\n%s\nwant 8 rows from %q to %q", step.keys, strings.Join(got, "\n"), step.first, step.last)
		}
	}
}

func TestPickerOnScreen(t *testing.T) {
	// The screen scrolls so the row, the box and the list show together. On a short screen
	// the list shows fewer rows, and the row, the box and the file header on top keep their
	// place. The list is only drawn: the line after it is the next row, and the rows are
	// counted without it.
	for _, tc := range []struct {
		height, rows int
	}{
		{24, 6},
		{12, 5}, // with the key row
		{8, 2},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		m.Update(tea.WindowSizeMsg{Width: 125, Height: tc.height})
		typeKeys(m, "31Ga^")
		lines := strings.Split(screen(t, m), "\n")
		y := cursorLine(m)
		list := shownList(t, m)
		if y < 0 || !strings.Contains(lines[y], "haiku line") || !strings.Contains(lines[y+1], "┏━ Ask") || !strings.Contains(lines[y+3], "┗━") ||
			len(list) != tc.rows || !strings.Contains(lines[y+4], "❯ ● claude-haiku-5-5") || y+3+tc.rows >= m.diffHeight() {
			t.Errorf("height %d: want the row, the box and %d rows of the list:\n%s", tc.height, tc.rows, strings.Join(lines, "\n"))
			continue
		}
		if after := y + 4 + tc.rows; after < m.diffHeight() && (m.rowAt(after) != 31 || m.rowAt(after-1) != -1) {
			t.Errorf("height %d: rows at lines %d and %d: %d and %d, want -1 for the list and then row 31",
				tc.height, after-1, after, m.rowAt(after-1), m.rowAt(after))
		}
		if status := statusLine(t, m); !strings.Contains(status, "row 31/34") {
			t.Errorf("height %d: status line %q, want row 31/34", tc.height, status)
		}
	}
}
