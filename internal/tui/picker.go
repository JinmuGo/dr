package tui

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// maxPickRows is how many rows the model list under the input box shows at most. A longer
// list scrolls to keep the highlighted row in view.
const maxPickRows = 8

// picker is the model list open under the input box while ^ picks a model for what is
// typed, as OMP's ^ does. The token, the ^ and what is typed after it up to the cursor,
// filters it.
type picker struct {
	at     int      // the byte of the box's text where the token's ^ is
	agent  string   // whose models it lists: the agent of the session the box sends to
	own    string   // the model of that session, "" when not known
	review []string // the other models of agent the sessions in the review used, newest edit first
	id     string   // the model of the highlighted row, or the query of the row that takes it as typed
	typed  bool     // the highlighted row takes the query as typed
	top    int      // the first row it shows when it has more than fit
}

// pickRow is a row of the model list: a model, the query as typed, or a dim note on the
// agent's own list.
type pickRow struct {
	id, name string // the model as the CLI takes it and as the agent names it
	session  bool   // the model of the session the box sends to
	typed    bool   // the query as typed, for a model the list does not hold
	note     string // a note, which cannot be picked, in place of a model
}

// modelList is what Asker.Models gave for an agent in this run, or that it has not yet.
type modelList struct {
	loading bool
	models  []agent.Model
	err     string // why there is no list, "" when there is one
}

// modelsMsg brings the models of agent that Asker.Models listed, or why it could not.
type modelsMsg struct {
	agent  string
	models []agent.Model
	err    error
}

// pickKey handles key k while the model list is open, before the input box does. Up, Down,
// ctrl+p and ctrl+n move the highlight, Enter and Tab pick, and Esc closes the list and
// leaves the token as typed. A key that moves the cursor in the text or starts a new line
// closes the list and then does what it does in the box. It reports whether it took k.
func (m *model) pickKey(k string) bool {
	switch k {
	case "up", "ctrl+p":
		m.moveHighlight(-1)
	case "down", "ctrl+n":
		m.moveHighlight(1)
	case "enter", "tab":
		m.pick()
	case "esc":
		m.entry.picker = nil
	case "left", "right", "home", "end", "ctrl+a", "ctrl+e", "alt+left", "ctrl+left", "alt+b", "alt+right", "ctrl+right",
		"alt+f", "shift+enter", "alt+enter", "ctrl+j":
		m.entry.picker = nil
		return false
	default:
		return false
	}
	return true
}

// opensPicker returns where the ^ right before the cursor is, and whether it opens the model
// list: it starts the text or comes after white space, a line break too.
func (b *textBox) opensPicker() (at int, ok bool) {
	at = b.cursor - 1
	if at < 0 || b.text[at] != '^' {
		return 0, false
	}
	return at, at == 0 || b.space(b.before(at))
}

// openPicker opens the model list for the ^ at byte at of the box's text, the highlight on
// its first row. It returns the command that lists the models of the agent the box sends
// to, the first time the list opens for that agent in this run.
func (m *model) openPicker(at int) tea.Cmd {
	name, own := m.boxSession()
	m.entry.picker = &picker{at: at, agent: name, own: own, review: m.reviewModels(name, own)}
	cmd := m.listModels(name)
	m.settle()
	return cmd
}

// keepPicker closes the model list when its token is gone: its ^ was deleted, or the
// cursor is no longer after it with no white space between. Otherwise the highlight stays
// on its model, as settle keeps it.
func (m *model) keepPicker() {
	b := &m.entry
	p := b.picker
	if p.at >= len(b.text) || b.text[p.at] != '^' || b.cursor <= p.at || strings.ContainsFunc(b.text[p.at+1:b.cursor], unicode.IsSpace) {
		b.picker = nil
		return
	}
	m.settle()
}

// boxSession returns the agent of the session the open input box sends to, the target's
// for a and the comment's for c, and the model of that session, "" when not known: the one
// a asks with or a new comment goes with, or that of the session's newest edit for a
// comment edited.
func (m *model) boxSession() (name, own string) {
	if t := m.asking; t != nil {
		return t.Ask.Agent, t.Ask.Model
	}
	in := m.drafting
	c := in.editing
	if c == nil {
		return in.target.Ask.Agent, in.target.Ask.Model
	}
	if e := m.newest[session{c.Agent, c.Session}]; e != nil {
		own = e.Model
	}
	return c.Agent, own
}

// reviewModels returns the models of agent name, other than own, that sessions made the
// edits in the review with, the model of the newest edit first. A fix dr applied does not
// count.
func (m *model) reviewModels(name, own string) []string {
	newest := make(map[string]time.Time) // the time of the newest edit of each model
	for _, f := range m.files {
		for _, l := range f.diff {
			e := l.Edit
			if e == nil || e.Fix != nil || e.Agent != name || e.Model == "" || e.Model == own {
				continue
			}
			if t, ok := newest[e.Model]; !ok || e.Time.After(t) {
				newest[e.Model] = e.Time
			}
		}
	}
	models := slices.Collect(maps.Keys(newest))
	slices.SortFunc(models, func(a, b string) int { return cmp.Or(newest[b].Compare(newest[a]), strings.Compare(a, b)) })
	return models
}

// listModels returns the command that lists the models of agent name with Asker.Models the
// first time it is called for name in this run, and nil after that or without
// Asker.Models: the list it gives, or why there is none, is kept for the run. Quitting
// stops it, as it stops a question.
func (m *model) listModels(name string) tea.Cmd {
	if m.asker.Models == nil || m.models[name] != nil {
		return nil
	}
	m.models[name] = &modelList{loading: true}
	list, flights, id := m.asker.Models, m.flights, "models "+name
	return func() tea.Msg {
		ctx, ok := flights.start(id)
		if !ok {
			return nil // the screen quit before the list was asked for
		}
		defer flights.end(id)
		models, err := list(ctx, name)
		if ctx.Err() != nil {
			return nil
		}
		return modelsMsg{agent: name, models: models, err: err}
	}
}

// listed keeps the models that came for an agent, or why none did, for the run, and keeps
// the highlight of the open model list on its model.
func (m *model) listed(msg modelsMsg) {
	l := &modelList{models: msg.models}
	if msg.err != nil {
		l = &modelList{err: msg.err.Error()}
	}
	m.models[msg.agent] = l
	if m.entry.picker != nil {
		m.settle()
	}
}

// pickRows returns the rows of the open model list: the model of the session the box sends
// to, marked, then the other models of its agent the review used, then the agent's own list
// from Asker.Models, each model once, of them those that match the query as matchModels
// orders them. A dim note tells that the agent's list is still coming, or why it is not
// there. Last comes the query as typed, when it is not the ID of a model listed.
func (m *model) pickRows() []pickRow {
	p := m.entry.picker
	list := m.models[p.agent]
	names := make(map[string]string) // by ID
	var listed []agent.Model
	if list != nil {
		listed = list.models
		for _, md := range listed {
			names[md.ID] = cmp.Or(names[md.ID], md.Name)
		}
	}
	var all []pickRow
	seen := make(map[string]bool)
	add := func(r pickRow) {
		if r.id != "" && !seen[r.id] {
			seen[r.id] = true
			all = append(all, r)
		}
	}
	add(pickRow{id: p.own, name: names[p.own], session: true})
	for _, id := range p.review {
		add(pickRow{id: id, name: names[id]})
	}
	for _, md := range listed {
		add(pickRow{id: md.ID, name: md.Name})
	}
	query := m.entry.text[p.at+1 : m.entry.cursor]
	rows := matchModels(all, query)
	switch {
	case list == nil:
	case list.loading:
		rows = append(rows, pickRow{note: "Loading " + review.Visible(p.agent) + " models…"})
	case list.err != "":
		rows = append(rows, pickRow{note: "Could not list " + review.Visible(p.agent) + " models: " + review.Visible(list.err)})
	}
	if query != "" && !seen[query] {
		rows = append(rows, pickRow{id: query, typed: true})
	}
	return rows
}

// matchModels returns the rows that match query, ignoring case: first those whose ID or name
// holds it, then those whose ID holds its characters in order with others between, each in
// the order of rows. An empty query matches every row.
func matchModels(rows []pickRow, query string) []pickRow {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var near, far []pickRow
	for _, r := range rows {
		id := strings.ToLower(r.id)
		switch {
		case strings.Contains(id, q) || strings.Contains(strings.ToLower(r.name), q):
			near = append(near, r)
		case inOrder(id, q):
			far = append(far, r)
		}
	}
	return append(near, far...)
}

// inOrder reports whether s holds the characters of sub in their order, with or without
// others between them.
func inOrder(s, sub string) bool {
	for _, r := range sub {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+utf8.RuneLen(r):]
	}
	return true
}

// index returns which of rows is highlighted, -1 for none.
func (p *picker) index(rows []pickRow) int {
	return slices.IndexFunc(rows, func(r pickRow) bool { return r.note == "" && r.id == p.id && r.typed == p.typed })
}

// settle keeps the highlight of the open model list on its model while the list holds it,
// as the query or the agent's own list changes, and else puts it on the first row that can
// be picked.
func (m *model) settle() {
	p := m.entry.picker
	rows := m.pickRows()
	i := p.index(rows)
	if i < 0 {
		i = slices.IndexFunc(rows, func(r pickRow) bool { return r.note == "" })
	}
	p.id, p.typed = "", false
	if i >= 0 {
		p.id, p.typed = rows[i].id, rows[i].typed
	}
}

// moveHighlight moves the highlight of the open model list to the next row that can be
// picked in the direction dir, 1 down or -1 up. It stays on the first and the last.
func (m *model) moveHighlight(dir int) {
	p := m.entry.picker
	rows := m.pickRows()
	for k := p.index(rows) + dir; k >= 0 && k < len(rows); k += dir {
		if rows[k].note == "" {
			p.id, p.typed = rows[k].id, rows[k].typed
			return
		}
	}
}

// pick takes the highlighted model as the box's and closes the list. The token goes from the
// text, and picking the session's own model clears the pick, as asking with it is no pick.
// With nothing to pick, it closes the list and leaves the token as typed.
func (m *model) pick() {
	b := &m.entry
	p := b.picker
	rows := m.pickRows()
	b.picker = nil
	i := p.index(rows)
	if i < 0 {
		return
	}
	b.remove(p.at, b.cursor)
	b.pick = rows[i].id
	if b.pick == p.own {
		b.pick = ""
	}
}

// pickRoom returns how many of n rows the model list shows in a diff w cells wide: at most
// maxPickRows, and on a short screen only as many as fit under the input box and its row,
// which keep their place.
func (m *model) pickRoom(n, w int) int {
	return max(min(n, maxPickRows, m.diffHeight()-1-m.entryHeight(w)), 0)
}

// pickerHeight returns how many lines of the screen the open model list takes in a diff w
// cells wide, a line a row it shows, 0 when it is closed.
func (m *model) pickerHeight(w int) int {
	if m.entry.picker == nil {
		return 0
	}
	return m.pickRoom(len(m.pickRows()), w)
}

// followPicker scrolls the open model list in a diff w cells wide as little as it takes to
// show the highlighted row, and leaves no empty rows at its bottom while there are rows
// above to show.
func (m *model) followPicker(w int) {
	p := m.entry.picker
	if p == nil {
		return
	}
	rows := m.pickRows()
	n := m.pickRoom(len(rows), w)
	if i := p.index(rows); i >= 0 {
		p.top = max(min(p.top, i), i-n+1)
	}
	p.top = max(min(p.top, len(rows)-n), 0)
}

// pickerLines draws the open model list in a diff w cells wide, right under the input box
// from its left edge to the right edge of the diff, the way OMP lists models under its
// input: the highlighted row "❯ ● id  name" in the accent color and bold, the others
// "  ● id  name" with the id in the text color and the name dim, the ids padded to one
// column. A row too narrow for its name leaves the name out, and then cuts the id, never
// through a wide character. Without colors, ❯ and bold mark the highlighted row. Notes are
// dim, under the ids. It returns nil while the list is closed.
func (m *model) pickerLines(w int) []string {
	p := m.entry.picker
	if p == nil {
		return nil
	}
	t := &m.theme
	rows := m.pickRows()
	n := m.pickRoom(len(rows), w)
	top := max(min(p.top, len(rows)-n), 0)
	at := p.index(rows)
	x := m.entryLeft(w)
	pad := strings.Repeat(" ", x)
	idWidth := 0
	for _, r := range rows {
		if r.note == "" && !r.typed {
			idWidth = max(idWidth, ansi.StringWidth(review.Visible(r.id)))
		}
	}
	base, dim, text := t.paint(nil, t.band), t.paint(t.muted, t.band), t.paint(t.text, t.band)
	lines := make([]string, 0, n)
	for k := top; k < top+n; k++ {
		r := rows[k]
		l := newLine(w - x)
		if r.note != "" {
			l.add("    ", base)
			l.add(r.note, dim)
			lines = append(lines, pad+l.fill(base))
			continue
		}
		mark, bullet, id, name := "  ", dim, text, dim
		if k == at {
			mark, bullet = "❯ ", t.paint(t.accent, t.band).Bold(true)
			id, name = bullet, bullet
		}
		l.add(mark, id)
		l.add("● ", bullet)
		shown := review.Visible(r.id)
		switch about := pickName(r); {
		case r.typed:
			l.add(`Use "`+shown+`" as typed`, id)
		case about != "" && idWidth+2+ansi.StringWidth(about) <= l.free:
			l.add(shown+strings.Repeat(" ", idWidth-ansi.StringWidth(shown)), id)
			l.add("  "+about, name)
		default:
			l.add(shown, id)
		}
		lines = append(lines, pad+l.fill(base))
	}
	return lines
}

// pickName returns what the model list tells of r after its ID: the name the agent gives
// it, made visible, and for the session's own model that it is the session's.
func pickName(r pickRow) string {
	name := review.Visible(r.name)
	switch {
	case !r.session:
		return name
	case name == "":
		return "session"
	}
	return name + " · session"
}
