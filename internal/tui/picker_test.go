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
	"github.com/junegunn/fzf/src/util"
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

// listRows returns the rows of the open model finder of m, in order: a model's ID, after ❯
// for the highlighted one, the row that takes the query as typed as Use "query" as typed,
// and a note as it reads. It returns nil while the finder is closed.
func listRows(m *model) []string {
	if m.entry.picker == nil {
		return nil
	}
	rows, _ := m.pickRows()
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

// shownFinder returns the lines of the model finder on the screen of m without colors, each
// as wide as the finder, nil while it is closed.
func shownFinder(t *testing.T, m *model) []string {
	t.Helper()
	if !m.typing() || m.entry.picker == nil {
		return nil
	}
	lines := strings.Split(screen(t, m), "\n")
	_, w := m.layout()
	x, y, width, height := m.finderPlace(w, m.diffHeight())
	left := m.width - w + x
	var out []string
	for _, l := range lines[y : y+height] {
		out = append(out, ansi.Cut(l, left, left+width))
	}
	return out
}

// finderRows returns the rows of the model finder on the screen of m, under its prompt and
// count, without the spaces at their ends and the preview.
func finderRows(t *testing.T, m *model) []string {
	t.Helper()
	f := shownFinder(t, m)
	var rows []string
	for _, l := range f[3 : len(f)-1] {
		mid := finderSplit(ansi.StringWidth(l))
		row := strings.TrimRight(ansi.Cut(l, 1, mid), " ")
		if row != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

// finderPreview returns the preview of the model finder on the screen of m, its lines
// without the spaces around them.
func finderPreview(t *testing.T, m *model) []string {
	t.Helper()
	f := shownFinder(t, m)
	var lines []string
	for _, l := range f[1 : len(f)-1] {
		w := ansi.StringWidth(l)
		lines = append(lines, strings.TrimSpace(ansi.Cut(l, finderSplit(w)+1, w-1)))
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func TestPickerOpens(t *testing.T) {
	// ^ at the start of a word opens the model finder and types nothing: at the start of the
	// text, after a space and after a line break, in the input box of a and of c. In a word
	// it types itself.
	for _, tc := range []struct {
		keys, text string
		open       bool
	}{
		{keys: "4ja^", open: true},
		{keys: "4jawhy ^", text: "why ", open: true},
		{keys: "4jawhy<alt+enter>^", text: "why\n", open: true},
		{keys: "4jc^", open: true},
		{keys: "4jawhy^", text: "why^"},
		{keys: "4jaa^b", text: "a^b"},
		{keys: "4jaab<left>^", text: "a^b"},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		typeKeys(m, tc.keys)
		if open := m.entry.picker != nil; open != tc.open || open != (shownFinder(t, m) != nil) || m.entry.text != tc.text {
			t.Errorf("%q: finder open %t, text %q; want open %t and %q", tc.keys, open, m.entry.text, tc.open, tc.text)
		}
	}
}

func TestPickerCaret(t *testing.T) {
	// ^ on an empty query types a ^ into the box and closes the finder, the way to write a
	// word that starts with ^. On a query it is part of the query.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4jawhy ^^foo")
	if m.entry.picker != nil || m.entry.text != "why ^foo" || m.entry.pick != "" {
		t.Errorf("^ on an empty query: finder open %t, text %q, pick %q; want it closed and %q", m.entry.picker != nil, m.entry.text, m.entry.pick, "why ^foo")
	}
	typeKeys(m, " ^a^")
	if p := m.entry.picker; p == nil || p.query != "a^" || m.entry.text != "why ^foo " {
		t.Errorf("^ on a query: finder %+v, text %q; want the query a^", p, m.entry.text)
	}
}

func TestPickerOrder(t *testing.T) {
	// The finder lists the session's own model first, tagged session, then the models other
	// Claude sessions of the review used, the newest edit first, tagged review, then Claude's
	// own list, each model once: a model of Claude's list already listed keeps its place.
	// Codex's model is not Claude's. The list is asked for once in the run.
	models := agentModels()
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	run(m, typeAndKeep(m, "4ja^"))
	want := []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "opus", "sonnet", "haiku"}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("rows %q, want %q", got, want)
	}
	rows := finderRows(t, m)
	if len(rows) != 6 || !strings.HasPrefix(rows[0], "▌  claude-opus-5-5") || !strings.HasSuffix(rows[0], " session") ||
		!strings.HasSuffix(rows[1], " review") || !strings.HasSuffix(rows[2], " review") || rows[3] != "   opus" {
		t.Errorf("the finder's rows\n%s", strings.Join(rows, "\n"))
	}
	if f := shownFinder(t, m); !strings.HasPrefix(f[0], "╭─ model · claude ─") || !strings.Contains(f[0], "┬─ preview ─") ||
		!strings.HasPrefix(f[1], "│ >  ") || !strings.HasPrefix(f[2], "│   6/6 ───") {
		t.Errorf("the finder\n%s\nwant its label, the prompt and the count 6/6", strings.Join(f, "\n"))
	}
	typeKeys(m, "<esc><esc><esc>27Gc^")
	if m.drafting == nil || m.entry.picker == nil {
		t.Fatal("c on c/new.go and ^: want the finder open")
	}
	if got := models.asked(); !slices.Equal(got, []string{"claude"}) {
		t.Errorf("Models asked for %q, want claude once", got)
	}
}

func TestPickerRank(t *testing.T) {
	// The query ranks the models by fzf's score, the best first, and lights the characters
	// it matched. gpt6m matches openai/gpt-6-mini closer than openai-codex/gpt-6-astra-mini.
	// The count says how many of the models listed match.
	models := agentModels()
	models.models["omp"] = []agent.Model{
		{ID: "anthropic/claude-opus-5-5", Name: "Claude Opus 5.5"},
		{ID: "openai-codex/gpt-6-astra", Name: "GPT-6 Astra"},
		{ID: "openai-codex/gpt-6-astra-mini", Name: "GPT-6 Astra mini"},
		{ID: "openai/gpt-6", Name: "GPT-6"},
		{ID: "openai/gpt-6-mini", Name: "GPT-6 mini"},
		{ID: "google/gemini-3-pro", Name: "Gemini 3 Pro"},
	}
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	run(m, typeAndKeep(m, "12ja^gpt6m"))
	rows, total := m.pickRows()
	if len(rows) != 3 || rows[0].id != "openai/gpt-6-mini" || rows[1].id != "openai-codex/gpt-6-astra-mini" || !rows[2].typed || total != 6 {
		t.Fatalf("rows %+v of %d, want the two minis, gpt-6-mini first, and the query as typed", rows, total)
	}
	if !slices.Equal(rows[0].lit, []int{7, 8, 9, 11, 13}) || !slices.Equal(rows[1].lit, []int{13, 14, 15, 17, 25}) {
		t.Errorf("lit %v and %v, want g, p, t, 6 and m of each", rows[0].lit, rows[1].lit)
	}
	if f := shownFinder(t, m); !strings.HasPrefix(f[1], "│ > gpt6m ") || !strings.HasPrefix(f[2], "│   2/6 ───") {
		t.Errorf("the finder\n%s\nwant the query and 2/6", strings.Join(f, "\n"))
	}
	th := &m.theme
	lit := th.onBand(th.ask, th.surface2).Bold(true)
	if raw := m.View().Content; !strings.Contains(raw, lit.Render("g")) || !strings.Contains(raw, th.paint(th.ask, th.surface).Bold(true).Render("m")) ||
		!strings.Contains(raw, th.onBand(th.subtle, th.surface2).Render("o")) {
		t.Error("the screen does not light the characters that matched, nor quiet the provider")
	}

	slab := util.MakeSlab(16*1024, 2048)
	initAlgo()
	all := []pickRow{{id: "openai-codex/gpt-6-astra-mini"}, {id: "openai/gpt-6-mini"}}
	if got := rankModels(all, "", slab); len(got) != 2 || got[0].id != all[0].id || got[1].id != all[1].id {
		t.Errorf("an empty query: %+v, want the rows in their order", got)
	}
	tied := []pickRow{{id: "b-sonnet"}, {id: "a-sonnet"}}
	if got := rankModels(tied, "sonnet", slab); got[0].id != "b-sonnet" || got[1].id != "a-sonnet" {
		t.Errorf("a tie: %+v, want the rows in their order", got)
	}
}

func TestPickerLoading(t *testing.T) {
	// While the agent's list is coming, a spinner turns before the count and a row says so
	// under the session's and the review's models, which can be picked. When it fails, a row
	// says why, another that a model can be typed, and it is not asked for again in the run.
	models := agentModels()
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	cmd := typeAndKeep(m, "4ja^")
	want := []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "Loading claude models…"}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("while loading: rows %q, want %q", got, want)
	}
	if f := shownFinder(t, m); !strings.HasPrefix(f[2], "│   "+string(spinner[0])+" 3/3 ───") || finderRows(t, m)[3] != "   Loading claude models…" {
		t.Errorf("while loading: the finder\n%s", strings.Join(shownFinder(t, m), "\n"))
	}
	typeKeys(m, "<down><down><down>")
	if got := listRows(m); got[2] != "❯claude-haiku-5-5" {
		t.Errorf("Down past the last model: rows %q, want claude-haiku-5-5 highlighted", got)
	}
	send(m, cmd())
	if got := listRows(m); len(got) != 6 || got[2] != "❯claude-haiku-5-5" || strings.Contains(shownFinder(t, m)[2], string(spinner[0])) {
		t.Errorf("after the list came: rows %q, want the highlight kept and no spinner", got)
	}
	// A query typed while the list comes highlights it as typed; the list's match takes the
	// highlight when it comes.
	m = pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	cmd = typeAndKeep(m, "4ja^")
	typeKeys(m, "ltt")
	models.models["claude"] = append(models.models["claude"], agent.Model{ID: "latest"})
	if got := listRows(m); !slices.Equal(got, []string{"Loading claude models…", `❯Use "ltt" as typed`}) {
		t.Errorf("while loading: rows %q, want the query as typed highlighted", got)
	}
	send(m, cmd())
	if got := listRows(m); len(got) == 0 || got[0] != "❯latest" {
		t.Errorf("after the list came: rows %q, want latest highlighted", got)
	}

	models = agentModels()
	models.err = errors.New("claude exited: not logged in")
	m = pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	typeKeys(m, "4ja^")
	want = []string{"❯claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude models failed: claude exited: not logged in", "Type a model name to use it as typed."}
	if got := listRows(m); !slices.Equal(got, want) {
		t.Errorf("on an error: rows %q, want %q", got, want)
	}
	if rows := finderRows(t, m); !strings.HasPrefix(rows[3], "   ✗ claude models failed: claude exited") || rows[4] != "   Type a model name to use it as typed." {
		t.Errorf("on an error: the rows\n%s", strings.Join(rows, "\n"))
	}
	th := &m.theme
	if raw := m.View().Content; !strings.Contains(raw, th.paint(th.del, th.surface).Bold(true).Render("✗ ")) {
		t.Error("the error is not in the color of errors")
	}
	typeKeys(m, "<down><enter><esc>a^")
	if got := models.asked(); m.entry.pick != "" || !slices.Equal(got, []string{"claude"}) {
		t.Errorf("Models asked for %q, pick %q; want claude once and no pick in the new box", got, m.entry.pick)
	}
}

func TestPickerFilter(t *testing.T) {
	// The query keeps the models whose ID holds its characters in order, ignoring case, by
	// fzf's score. A query that is not the ID of a model listed can be taken as typed, on the
	// last row.
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"sonnet", []string{"❯sonnet", "claude-sonnet-5-5"}},
		{"SON", []string{"❯sonnet", "claude-sonnet-5-5", `Use "SON" as typed`}},
		{"cs5", []string{"❯claude-sonnet-5-5", "claude-opus-5-5", `Use "cs5" as typed`}},
		{"hku", []string{"❯haiku", "claude-haiku-5-5", `Use "hku" as typed`}},
		{"my-model", []string{`❯Use "my-model" as typed`}},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		run(m, typeAndKeep(m, "4ja^"))
		typeKeys(m, tc.query)
		if got := listRows(m); !slices.Equal(got, tc.want) {
			t.Errorf("%q: rows %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestPickerHighlight(t *testing.T) {
	// Each change of the query puts the highlight on the best match, the first row, as fzf
	// does; the rows below it are a key away.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	run(m, typeAndKeep(m, "4ja^"))
	for _, step := range []struct {
		keys, want string
	}{
		{"<down><down><down><down><down>", "haiku"},
		{"a", "claude-opus-5-5"},
		{"i", "claude-haiku-5-5"},
		{"<down>", "haiku"},
		{"x", `Use "aix" as typed`},
		{"<bs>", "claude-haiku-5-5"},
		{"<ctrl+u>", "claude-opus-5-5"},
	} {
		typeKeys(m, step.keys)
		if rows := listRows(m); !slices.Contains(rows, "❯"+step.want) {
			t.Errorf("after %q: rows %q, want %s highlighted", step.keys, rows, step.want)
		}
	}
}

func TestPickerKeys(t *testing.T) {
	// Up, Down, ctrl+p and ctrl+n move the highlight, which stays on the first and the last
	// row. Typing edits the query, Backspace deletes its last character and ctrl+u clears it.
	// Enter and Tab pick, Esc closes the finder and keeps the pick, and ctrl+c closes the
	// box. The text of the box does not change.
	for _, tc := range []struct {
		name, keys string
		rows       []string // nil for the finder closed
		query      string
		pick       string
		closed     bool // the box closed
	}{
		{name: "Down and Up", keys: "^<down><down><up>", rows: []string{"claude-opus-5-5", "❯claude-sonnet-5-5"}},
		{name: "ctrl+n and ctrl+p", keys: "^<ctrl+n><ctrl+n><ctrl+n><ctrl+p>", rows: []string{"claude-opus-5-5", "claude-sonnet-5-5", "❯claude-haiku-5-5"}},
		{name: "Up on the first row", keys: "^<up>", rows: []string{"❯claude-opus-5-5"}},
		{name: "Down on the last row", keys: "^hku<down><down><down>", rows: []string{"haiku", "claude-haiku-5-5", `❯Use "hku" as typed`}, query: "hku"},
		{name: "typing and Backspace", keys: "^sonx<bs>", rows: []string{"❯sonnet", "claude-sonnet-5-5"}, query: "son"},
		{name: "ctrl+u", keys: "^son<ctrl+u>", rows: []string{"❯claude-opus-5-5", "claude-sonnet-5-5"}},
		{name: "Backspace on an empty query", keys: "^<bs>", rows: []string{"❯claude-opus-5-5"}},
		{name: "Enter", keys: "^son<down><enter>", pick: "claude-sonnet-5-5"},
		{name: "Tab", keys: "^son<tab>", pick: "sonnet"},
		{name: "Esc", keys: "^son<tab>^opus<esc>", pick: "sonnet"},
		{name: "ctrl+c", keys: "^so<ctrl+c>", closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
			run(m, typeAndKeep(m, "4jawhy "))
			typeKeys(m, tc.keys)
			if tc.closed {
				if m.typing() || shownFinder(t, m) != nil {
					t.Errorf("box open %t, want the box and the finder closed", m.typing())
				}
				return
			}
			rows := listRows(m)
			if len(rows) > len(tc.rows) {
				rows = rows[:len(tc.rows)]
			}
			p := m.entry.picker
			if open := p != nil; !m.typing() || open != (tc.rows != nil) || !slices.Equal(rows, tc.rows) || (open && p.query != tc.query) ||
				m.entry.text != "why " || m.entry.pick != tc.pick {
				t.Errorf("box open %t, finder open %t with rows %q, text %q, pick %q; want rows %q, query %q, pick %q",
					m.typing(), open, rows, m.entry.text, m.entry.pick, tc.rows, tc.query, tc.pick)
			}
		})
	}
}

func TestPickerPaste(t *testing.T) {
	// A paste while the finder is open goes to its query, on one line.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4ja^")
	send(m, tea.PasteMsg{Content: "son\nnet"})
	if p := m.entry.picker; p == nil || p.query != "son net" || m.entry.text != "" {
		t.Errorf("finder %+v, text %q; want the query son net", p, m.entry.text)
	}
}

func TestPickerChip(t *testing.T) {
	// The top border of the box tells the model it goes with: the session's own, quiet; while
	// the finder is open, that a model is being picked, in the ask color and with no cursor in
	// the box; then the model picked for this question or comment alone, in the color of
	// comments. Picking the session's own model clears the pick, and a query can be picked as
	// typed. Without the session's model, the agent's default.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	th := &m.theme
	for _, step := range []struct {
		keys, chip string
		tone       any
	}{
		{"4jawhy ", "^ claude-opus-5-5 · session", th.muted},
		{"^", "^ picking a model…", th.ask},
		{"son<down><enter>", "^ claude-sonnet-5-5 · this question", th.pending},
		{"^mine<enter>", "^ mine · this question", th.pending},
		{"^<enter>", "^ claude-opus-5-5 · session", th.muted},
		{"<esc>c^sonnet<enter>", "^ sonnet · this comment", th.pending},
	} {
		typeKeys(m, step.keys)
		box := entryBox(t, m)
		chip, tone := m.modelChip()
		if got := shownModel(box); got != step.chip || tone != step.tone {
			t.Errorf("after %q: the box says %q in %v, want %q in %v", step.keys, got, tone, step.chip, step.tone)
		}
		if !strings.Contains(m.View().Content, th.paint(tone, th.surface).Render(" "+chip+" ")) {
			t.Errorf("after %q: the screen has no chip in its color", step.keys)
		}
		x, y, _ := m.entryCursor()
		c := m.View().Cursor
		if picking, inBox := m.entry.picker != nil, c != nil && c.X == x && c.Y == y; picking == inBox {
			t.Errorf("after %q: finder open %t, the cursor %+v; want it in the box only while the finder is closed", step.keys, picking, c)
		}
		if _, w := m.layout(); m.entry.picker != nil && strings.Contains(strings.Join(m.entryLines(w), ""), th.onBand(th.surface, th.ask).Render(" ")) {
			t.Errorf("after %q: the box shows a cursor while the finder is open", step.keys)
		}
	}
	typeKeys(m, "<esc>a")
	m.asking.Ask.Model = ""
	if chip, _ := m.modelChip(); chip != "^ claude's default model" {
		t.Errorf("without the session's model: %q", chip)
	}
}

func TestPickerPreview(t *testing.T) {
	// The preview tells of the highlighted row: its name and ID, and what picking it does.
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
	run(m, typeAndKeep(m, "4ja^"))
	for _, step := range []struct {
		keys string
		want []string
	}{
		{"", []string{"Claude Opus 5.5", "claude-opus-5-5", "", "The session's own model. Picking it asks the way a always does."}},
		{"<down>", []string{"Claude Sonnet 5.5", "claude-sonnet-5-5", "", "used in this review by claude 6666", "",
			"For this question only. The next one goes back to claude-opus-5-5.", "", "The session's reasoning effort is left out."}},
		{"<down><down>", []string{"Latest Opus", "opus", "", "For this question only. The next one goes back to claude-opus-5-5.", "",
			"The session's reasoning effort is left out."}},
		{"llama-5", []string{"llama-5", "not in the list of claude models", "",
			"dr passes it to claude as typed. If claude has no such model, its error comes back in place of an answer."}},
	} {
		typeKeys(m, step.keys)
		got := strings.Join(finderPreview(t, m), " ")
		if want := strings.Join(step.want, " "); strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(want), " ") {
			t.Errorf("after %q: the preview\n%s\nwant\n%s", step.keys, strings.Join(finderPreview(t, m), "\n"), strings.Join(step.want, "\n"))
		}
		sonnet := session{"claude", "50e70000-6666"}
		if th := &m.theme; step.keys == "<down>" && !strings.Contains(m.View().Content, th.paint(m.sessionColor(sonnet), th.surface).Render("claude 6666")) {
			t.Errorf("the preview does not show claude 6666 in its session's color")
		}
	}
	typeKeys(m, "<esc><esc>c^<down><down><down>")
	if got := strings.Join(strings.Fields(strings.Join(finderPreview(t, m), " ")), " "); !strings.Contains(got,
		"For this comment only. :submit asks for its fix with this model; the other comments keep theirs.") {
		t.Errorf("the preview in the box of c: %q", got)
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
	typeAll(m, "33Gawhy? ^luna<enter><enter>")
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
	typeAll(m, "<esc>a^gpt-6-luna<enter><enter>")
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
	typeAll(m, "4jcfix ^son<tab><enter>")
	if records := commentRecords(t, dir); len(records) != 1 || records[0].Text != "fix" || records[0].Model != "sonnet" {
		t.Fatalf("records %+v, want the comment with its model", records)
	}
	const header = "✎ you → ● claude c1a0 · a.go:2 · ^sonnet   pending"
	if box := boxOf(m, m.comments[0]); !strings.HasPrefix(box, header+"  c edit\n") {
		t.Errorf("the comment's box:\n%s\nwant it headed %q", box, header)
	}

	m = pickScreen(pickFiles(), dir, &fakeAsk{answer: answer}, agentModels())
	typeKeys(m, "4jc")
	if box := entryBox(t, m); len(m.comments) != 1 || m.comments[0].Model != "sonnet" ||
		shownTitle(box) != "EDIT → claude c1a0 · L2" || shownModel(box) != "^ sonnet · this comment" {
		t.Fatalf("the next run: comments %+v, the box\n%s", m.comments, strings.Join(box, "\n"))
	}
	typeKeys(m, "<enter>")
	if records := commentRecords(t, dir); len(records) != 1 {
		t.Errorf("saved unchanged: %d records, want 1", len(records))
	}
	typeAll(m, "c ^<enter><enter>") // the ^ after a space, as the text ends with a word
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
			held:   "✓ Sent 1 fix request to claude c1a0 (1 comment) ! Held back 1 comment for claude c1a0 with its own model; submit again when its fixes come The fixes come under the comments.",
			models: [2]string{"sonnet", "claude-opus-5-5"}, efforts: [2]string{"", "high"}, picked: "one", edit: "gg4jc ^<enter><enter>",
		},
		{
			name: "the second comment picked", keys: "4jcone<enter>Gcthree ^sonnet<enter><enter>",
			held:   "✓ Sent 1 fix request to claude c1a0 (1 comment) ! Held back 1 comment for claude c1a0 with ^sonnet; submit again when its fixes come The fixes come under the comments.",
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
			if got := noticeText(t, m); got != tc.held {
				t.Errorf("notice %q, want %q", got, tc.held)
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
	// The finder floats in a border of floating boxes. The highlighted row is on the
	// selection band with ▌ in the ask color, the prompt > in the ask color. Without colors,
	// as with NO_COLOR, no color reaches the terminal, and the highlighted row is reversed.
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
		run(m, typeAndKeep(m, "4ja^"))
		th := &m.theme
		raw := m.View().Content
		for name, want := range map[string]string{
			"the border":       th.paint(th.line2, th.surface).Render("╭─"),
			"the prompt":       th.paint(th.ask, th.surface).Bold(true).Render(">"),
			"the highlight":    th.onBand(th.ask, th.surface2).Render("▌"),
			"the highlight ID": th.onBand(th.strong, th.surface2).Render("c"),
			"another ID":       th.paint(th.fg, th.surface).Render("s"),
			"a tag":            th.paint(th.subtle, th.surface).Render(" review"),
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
		if s := out.String(); strings.Contains(s, "38;") || strings.Contains(s, "48;") || !strings.Contains(s, "\x1b[7m▌") {
			t.Errorf("%s: as the terminal gets it\n%q\nwant no colors and a reversed highlight", tc.name, s)
		}
	}
}

func TestPickerWidths(t *testing.T) {
	// The finder is as wide as the room right of the box allows, 100 cells at most, and
	// narrows with the screen; its rows and preview are cut, never through a wide character:
	// screen checks every line is as wide as the screen.
	models := agentModels()
	models.models["claude"] = append(models.models["claude"], agent.Model{ID: "클로드-모델", Name: "한글 이름"})
	for _, tc := range []struct {
		width, finder int
	}{
		{200, 100},
		{125, 91}, // the room right of the box less a cell
		{60, 46},
		{40, 26},
		{30, 30}, // too narrow beside the box: the whole diff
	} {
		for _, th := range []theme{newTheme(nil, true, false), newTheme(nil, true, true)} {
			m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
			m.theme = th
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 24})
			run(m, typeAndKeep(m, "4ja^"))
			typeKeys(m, "<down><down><down><down><down><down><down>")
			_, w := m.layout()
			if _, _, width, _ := m.finderPlace(w, m.diffHeight()); width != tc.finder || len(shownFinder(t, m)) == 0 {
				t.Errorf("width %d: the finder is %d wide, want %d", tc.width, width, tc.finder)
			}
		}
	}
}

func TestPickerScroll(t *testing.T) {
	// The finder shows 8 rows and scrolls to keep the highlighted row in view.
	models := agentModels()
	for i := range 6 {
		models.models["claude"] = append(models.models["claude"], agent.Model{ID: "model-" + string(rune('a'+i))})
	}
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	run(m, typeAndKeep(m, "4ja^"))
	for _, step := range []struct {
		keys        string
		first, last string
	}{
		{"", "▌  claude-opus-5-5", "   model-b"},
		{strings.Repeat("<down>", 8), "   claude-sonnet-5-5", "▌  model-c"},
		{strings.Repeat("<down>", 3), "   sonnet", "▌  model-f"},
		{strings.Repeat("<up>", 10), "▌  claude-sonnet-5-5", "   model-c"},
	} {
		typeKeys(m, step.keys)
		got := finderRows(t, m)
		if len(got) != 8 || !strings.HasPrefix(got[0], step.first) || !strings.HasPrefix(got[7], step.last) {
			t.Errorf("after %q: the rows\n%s\nwant 8 rows from %q to %q", step.keys, strings.Join(got, "\n"), step.first, step.last)
		}
	}
}

func TestPickerPlace(t *testing.T) {
	// The finder floats over the diff under the box, 12 lines high, without moving the rows:
	// the line after the box is still the next row. Where 12 lines do not fit under the box
	// it goes above it, and where they fit on neither side it takes the larger, never over
	// the status line and the key row.
	for _, tc := range []struct {
		name, keys string
		height     int
		above      bool
		lines      int
	}{
		{"under the box", "4ja^", 24, false, 12},
		{"above the box", "33Ga^", 24, true, 12},
		{"shorter, under", "4ja^", 16, false, 6},
		{"shorter, above", "33Ga^", 16, true, 11},
	} {
		m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
		m.Update(tea.WindowSizeMsg{Width: 125, Height: tc.height})
		run(m, typeAndKeep(m, tc.keys))
		_, w := m.layout()
		_, y, _, height := m.finderPlace(w, m.diffHeight())
		boxTop := m.lineOf(m.entryRow()) + 1
		boxEnd := boxTop + m.entryHeight(w)
		f := shownFinder(t, m)
		lines := strings.Split(screen(t, m), "\n")
		if height != tc.lines || len(f) != tc.lines || !strings.HasPrefix(f[0], "╭─ model") || !strings.HasPrefix(f[len(f)-1], "╰─") ||
			y+height > m.diffHeight() || tc.above != (y+height <= boxTop) || !tc.above && y != boxEnd || tc.above && y+height != boxTop {
			t.Errorf("%s: the finder at line %d, %d high, the box from %d to %d:\n%s", tc.name, y, height, boxTop, boxEnd, strings.Join(lines, "\n"))
		}
		if !strings.HasPrefix(statusLine(t, m), " ASK  ^ model ") || !strings.HasPrefix(keyLine(t, m), "  ↑ ↓ ^N ^P  move") {
			t.Errorf("%s: the status line %q and key row %q are not in view", tc.name, statusLine(t, m), keyLine(t, m))
		}
		if !tc.above && boxEnd+height < m.diffHeight() && m.rowAt(boxEnd) != m.entryRow()+1 {
			t.Errorf("%s: row at line %d is %d, want the row after the box's", tc.name, boxEnd, m.rowAt(boxEnd))
		}
		if c := m.View().Cursor; c == nil || c.Y != y+1 {
			t.Errorf("%s: the cursor %+v, want it on the finder's prompt", tc.name, c)
		}
	}
}

func TestPickerLateList(t *testing.T) {
	// A list that comes for another agent than the open finder's is kept, and leaves the
	// finder's highlight where it is: on the query as typed, here, though a model matches it.
	models := agentModels()
	m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, models)
	claude := typeAndKeep(m, "4ja^")
	typeKeys(m, "<esc><esc>")
	run(m, typeAndKeep(m, "33Ga^"))
	typeKeys(m, "luna<down>")
	if got := listRows(m); !slices.Equal(got, []string{"gpt-6-luna", `❯Use "luna" as typed`}) {
		t.Fatalf("the finder of codex: rows %q, want the query as typed highlighted", got)
	}
	send(m, claude())
	if got := listRows(m); !slices.Equal(got, []string{"gpt-6-luna", `❯Use "luna" as typed`}) || m.models["claude"].loading {
		t.Errorf("after Claude's list came: rows %q, Claude's list loading %t; want the highlight kept and the list kept",
			got, m.models["claude"].loading)
	}
	typeKeys(m, "<enter>")
	if m.entry.pick != "luna" {
		t.Errorf("pick %q, want luna as typed", m.entry.pick)
	}
}

func TestPickerShortScreen(t *testing.T) {
	// On a screen too short for the finder beside the box, it still shows the highlighted
	// row, moving with it, and leaves the status line and the key row in view.
	for height := 5; height <= 14; height++ {
		for _, keys := range []string{"4ja^", "33Ga^"} {
			m := pickScreen(pickFiles(), t.TempDir(), &fakeAsk{answer: answer}, agentModels())
			m.Update(tea.WindowSizeMsg{Width: 125, Height: height})
			run(m, typeAndKeep(m, keys))
			for _, move := range []string{"", "<down>", "<down>"} {
				typeKeys(m, move)
				rows, _ := m.pickRows()
				want := "▌  " + rows[m.entry.picker.index(rows)].id
				_, w := m.layout()
				_, y, _, h := m.finderPlace(w, m.diffHeight())
				if f := shownFinder(t, m); y < 0 || y+h > m.diffHeight() || !slices.ContainsFunc(f, func(l string) bool { return strings.Contains(l, want) }) {
					t.Errorf("height %d, %q then %q: the finder at line %d, %d high\n%s\nwant %q in it", height, keys, move, y, h, strings.Join(f, "\n"), want)
				}
				if !strings.HasPrefix(statusLine(t, m), " ASK  ^ model ") || height >= minKeyRowHeight && !strings.HasPrefix(keyLine(t, m), "  ↑ ↓ ^N ^P  move") {
					t.Errorf("height %d, %q then %q: the status line %q or the key row is covered", height, keys, move, statusLine(t, m))
				}
			}
		}
	}
}
