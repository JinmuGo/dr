package tui

import (
	"cmp"
	"image/color"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

const (
	finderHeight   = 12  // how many lines the model finder takes, its borders included, when they fit
	finderMaxWidth = 100 // how many cells wide the model finder is at most
	finderMinWidth = 24  // how many cells wide it is at least beside the box; narrower, it takes the whole diff
	finderChrome   = 4   // the lines of the finder that are not rows: its borders, the prompt and the count
)

// initAlgo sets fzf's matcher to its default scoring scheme, once.
var initAlgo = sync.OnceFunc(func() { algo.Init("default") })

// picker is the model finder, open over the diff under the input box while ^ picks a model
// for what is typed, drawn the way fzf --layout=reverse --border --preview draws: a prompt
// with the query, a line that counts the matches, the matching models with the characters
// that matched lit, and a preview of the highlighted one. The query is its own; the box's
// text does not change while it is open.
type picker struct {
	query  string
	agent  string             // whose models it lists: the agent of the session the box sends to
	own    string             // the model of that session, "" when not known
	review []string           // the other models of agent the sessions in the review used, newest edit first
	by     map[string]session // the session that made the newest edit with each of review
	id     string             // the model of the highlighted row, or the query of the row that takes it as typed
	typed  bool               // the highlighted row takes the query as typed
	top    int                // the first row it shows when it has more than fit
	slab   *util.Slab         // fzf's scratch memory, reused for every match
}

// pickRow is a row of the model finder: a model, the query as typed, or a note that cannot
// be picked.
type pickRow struct {
	id, name string // the model as the CLI takes it and as the agent names it
	tag      string // "session" for the model of the session the box sends to, "review" for one the review used
	lit      []int  // the runes of id the query matched, in order
	typed    bool   // the query as typed, for a model the list does not hold
	note     string // a note, which cannot be picked, in place of a model
	fail     bool   // the note says why the agent's list is not there
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

// pickKey handles a key while the model finder is open, which takes every key. Typing edits
// the query, Backspace deletes its last character and ctrl+u clears it, and the highlight
// goes to the best match, as in fzf. Up, Down, ctrl+p and ctrl+n move the highlight, Enter
// and Tab pick, and Esc closes the finder without changing the pick. ^ on an empty query
// types a ^ into the box and closes the finder, the way to write a word that starts with ^.
// ctrl+c closes the box, as it does without the finder.
func (m *model) pickKey(msg tea.KeyPressMsg) {
	p := m.entry.picker
	switch k := msg.String(); k {
	case keyUp, keyCtrlP:
		m.moveHighlight(-1)
	case keyDown, keyCtrlN:
		m.moveHighlight(1)
	case keyEnter, keyTab:
		m.pick()
	case keyEsc:
		m.entry.picker = nil
	case keyCtrlC:
		m.closeBox()
	case keyCtrlU:
		m.setQuery("")
	case keyBackspace:
		_, n := utf8.DecodeLastRuneInString(p.query)
		m.setQuery(p.query[:len(p.query)-n])
	default:
		if p.query == "" && msg.Text == keyPickModel {
			m.entry.picker = nil
			_, w := m.layout()
			m.entry.edit(k, msg.Text, m.entryWidth(w))
			return
		}
		m.typeQuery(msg.Text)
	}
}

// typeQuery adds text, typed or pasted, to the query of the open model finder, on one line.
func (m *model) typeQuery(text string) {
	if text = strings.ReplaceAll(clean(text), "\n", " "); text != "" {
		m.setQuery(m.entry.picker.query + text)
	}
}

// setQuery sets the query of the open model finder and puts the highlight on its first row
// that can be picked, the best match.
func (m *model) setQuery(query string) {
	p := m.entry.picker
	p.query, p.id, p.typed, p.top = query, "", false, 0
	m.settle()
}

// opensPicker reports whether ^ typed at the cursor opens the model finder: the cursor is at
// the start of a word, at the start of the text or after white space, a line break too.
func (b *textBox) opensPicker() bool {
	return b.cursor == 0 || b.space(b.before(b.cursor))
}

// openPicker opens the model finder with an empty query, the highlight on its first row. It
// returns the command that lists the models of the agent the box sends to, the first time
// the finder opens for that agent in this run.
func (m *model) openPicker() tea.Cmd {
	initAlgo()
	name, own := m.boxSession()
	models, by := m.reviewModels(name, own)
	m.entry.picker = &picker{agent: name, own: own, review: models, by: by, slab: util.MakeSlab(16*1024, 2048)}
	cmd := m.listModels(name)
	m.settle()
	return cmd
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
// edits in the review with, the model of the newest edit first, and the session that made
// the newest edit with each. A fix dr applied does not count.
func (m *model) reviewModels(name, own string) ([]string, map[string]session) {
	newest := make(map[string]*agent.Edit) // the newest edit of each model
	for _, f := range m.files {
		for _, l := range f.diff {
			e := l.Edit
			if e == nil || e.Fix != nil || e.Agent != name || e.Model == "" || e.Model == own {
				continue
			}
			if n := newest[e.Model]; n == nil || e.Time.After(n.Time) {
				newest[e.Model] = e
			}
		}
	}
	models := slices.Collect(maps.Keys(newest))
	slices.SortFunc(models, func(a, b string) int {
		return cmp.Or(newest[b].Time.Compare(newest[a].Time), strings.Compare(a, b))
	})
	by := make(map[string]session, len(models))
	for _, id := range models {
		by[id] = session{newest[id].Agent, newest[id].Session}
	}
	return models, by
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

// listed keeps the models that came for an agent, or why none did, for the run. When the
// open model finder lists that agent's models, its highlight stays on its model, and a
// highlight on the query as typed goes to the best match, as the list can hold models that
// match it. A list for another agent leaves the open finder as it is.
func (m *model) listed(msg modelsMsg) {
	l := &modelList{models: msg.models}
	if msg.err != nil {
		l = &modelList{err: msg.err.Error()}
	}
	m.models[msg.agent] = l
	if p := m.entry.picker; p != nil && p.agent == msg.agent {
		if p.typed {
			p.id, p.typed = "", false
		}
		m.settle()
	}
}

// listLoading reports whether the open model finder waits for its agent's list.
func (m *model) listLoading() bool {
	if !m.typing() || m.entry.picker == nil {
		return false
	}
	l := m.models[m.entry.picker.agent]
	return l != nil && l.loading
}

// pickRows returns the rows of the open model finder, and how many models it holds before
// the query filters them: the model of the session the box sends to, then the other models
// of its agent the review used, then the agent's own list from Asker.Models, each model
// once, of them those that match the query as rankModels orders them. A note follows while
// the agent's list is still coming, or two when it failed: why, and that a model can be
// typed. Last comes the query as typed, when it is not the ID of a model listed.
func (m *model) pickRows() (rows []pickRow, total int) {
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
	add(pickRow{id: p.own, name: names[p.own], tag: "session"})
	for _, id := range p.review {
		add(pickRow{id: id, name: names[id], tag: "review"})
	}
	for _, md := range listed {
		add(pickRow{id: md.ID, name: md.Name})
	}
	rows = rankModels(all, p.query, p.slab)
	name := review.Visible(p.agent)
	switch {
	case list == nil:
	case list.loading:
		rows = append(rows, pickRow{note: "Loading " + name + " models…"})
	case list.err != "":
		rows = append(rows, pickRow{note: name + " models failed: " + review.Visible(list.err), fail: true},
			pickRow{note: "Type a model name to use it as typed."})
	}
	if p.query != "" && !seen[p.query] {
		rows = append(rows, pickRow{id: p.query, typed: true})
	}
	return rows, len(all)
}

// rankModels returns the rows whose ID matches query as fzf matches it, ignoring case, by
// fzf's score, the best first and rows that score the same in the order of rows, each with
// the characters that matched. An empty query keeps every row in its order. slab is fzf's
// scratch memory.
func rankModels(rows []pickRow, query string, slab *util.Slab) []pickRow {
	if query == "" {
		return rows
	}
	pattern := []rune(strings.ToLower(query))
	type hit struct {
		row   pickRow
		score int
	}
	var hits []hit
	for _, r := range rows {
		chars := util.ToChars([]byte(r.id))
		res, pos := algo.FuzzyMatchV2(false, true, true, &chars, pattern, true, slab)
		if res.Start < 0 {
			continue
		}
		r.lit = slices.Sorted(slices.Values(*pos))
		hits = append(hits, hit{r, res.Score})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	ranked := make([]pickRow, len(hits))
	for i, h := range hits {
		ranked[i] = h.row
	}
	return ranked
}

// index returns which of rows is highlighted, -1 for none.
func (p *picker) index(rows []pickRow) int {
	return slices.IndexFunc(rows, func(r pickRow) bool { return r.note == "" && r.id == p.id && r.typed == p.typed })
}

// settle keeps the highlight of the open model finder on its model while the finder holds
// it, as the agent's own list comes, and else puts it on the first row that can be picked.
func (m *model) settle() {
	p := m.entry.picker
	rows, _ := m.pickRows()
	i := p.index(rows)
	if i < 0 {
		i = slices.IndexFunc(rows, func(r pickRow) bool { return r.note == "" })
	}
	p.id, p.typed = "", false
	if i >= 0 {
		p.id, p.typed = rows[i].id, rows[i].typed
	}
}

// moveHighlight moves the highlight of the open model finder to the next row that can be
// picked in the direction dir, 1 down or -1 up. It stays on the first and the last.
func (m *model) moveHighlight(dir int) {
	p := m.entry.picker
	rows, _ := m.pickRows()
	for k := p.index(rows) + dir; k >= 0 && k < len(rows); k += dir {
		if rows[k].note == "" {
			p.id, p.typed = rows[k].id, rows[k].typed
			return
		}
	}
}

// pick takes the highlighted model as the box's and closes the finder. Picking the session's
// own model clears the pick, as asking with it is no pick. With nothing to pick, it closes
// the finder and leaves the pick as it was.
func (m *model) pick() {
	b := &m.entry
	p := b.picker
	rows, _ := m.pickRows()
	b.picker = nil
	i := p.index(rows)
	if i < 0 {
		return
	}
	b.pick = rows[i].id
	if b.pick == p.own {
		b.pick = ""
	}
}

// finderPlace returns where the open model finder shows in a diff w cells wide and h lines
// high: its left cell and its top line, its width and its height. It floats over the diff
// two cells right of the input box's left edge, as wide as finderMaxWidth or the room to the
// right edge less a cell, and finderHeight lines high under the box. When those lines do not
// fit under the box it goes above it, and when they fit on neither side, it takes the
// larger, as long as that shows a row. Otherwise it floats over the box at the bottom of the
// diff. It never leaves the diff, so the status line and the key row stay in view.
func (m *model) finderPlace(w, h int) (x, y, width, height int) {
	x = m.entryLeft(w) + 2
	width = min(finderMaxWidth, w-x-1)
	if width < finderMinWidth {
		x, width = 0, w
	}
	r := m.entryRow()
	boxTop := 0
	if r >= m.top {
		boxTop = min(m.lineOf(r)+1, h)
	}
	boxEnd := min(boxTop+m.entryHeight(w), h) // the line after the box
	below, above := h-boxEnd, boxTop
	switch {
	case below >= finderHeight:
		return x, boxEnd, width, finderHeight
	case above >= finderHeight:
		return x, boxTop - finderHeight, width, finderHeight
	case below >= above && below > finderChrome:
		return x, boxEnd, width, below
	case above > finderChrome:
		return x, 0, width, above
	}
	height = min(finderHeight, h) // no room beside the box: over it, at the bottom of the diff
	return x, h - height, width, height
}

// finderRowsShown returns how many rows a finder h lines high shows: those under its border,
// prompt and count, or, when it is too short to show one under them, a row a line, alone.
func finderRowsShown(h int) int {
	if h > finderChrome {
		return h - finderChrome
	}
	return h
}

// followPicker scrolls the rows of the open model finder in a diff w cells wide and h lines
// high as little as it takes to show the highlighted row, and leaves no empty rows at its
// bottom while there are rows above to show.
func (m *model) followPicker(w, h int) {
	p := m.entry.picker
	if p == nil {
		return
	}
	rows, _ := m.pickRows()
	_, _, _, height := m.finderPlace(w, h)
	n := finderRowsShown(height)
	if i := p.index(rows); i >= 0 {
		p.top = max(min(p.top, i), i-n+1)
	}
	p.top = max(min(p.top, len(rows)-n), 0)
}

// drawFinder draws the open model finder over lines, the diff w cells wide, where
// finderPlace puts it.
func (m *model) drawFinder(lines []string, w int) {
	x, y, width, height := m.finderPlace(w, len(lines))
	overlayBox(lines, m.finderLines(width, height), x, y, width, w)
}

// finderSplit returns the column of the line between the rows and the preview of a finder w
// cells wide: the rows take 54% of it.
func finderSplit(w int) int {
	return w*54/100 + 1
}

// finderCursor returns the cell of the screen after the query of the open model finder, for
// the terminal's cursor, so that an input method composes its characters there; on a finder
// too short for its prompt, the highlighted row.
func (m *model) finderCursor() (cx, cy int) {
	_, w := m.layout()
	x, y, width, height := m.finderPlace(w, m.rowsHeight())
	x, y = x+m.width-w, y+m.bandLines()
	if height <= finderChrome {
		rows, _ := m.pickRows()
		return x + 3, y + max(m.entry.picker.index(rows)-m.entry.picker.top, 0)
	}
	shown := keepEnd(review.Visible(m.entry.picker.query), finderSplit(width)-6)
	return x + 4 + ansi.StringWidth(shown), y + 1
}

// finderLines draws the open model finder, w cells wide and h lines high, in a rounded
// border in the color of floating boxes, with the label "model · agent" on its top border.
// The rows take the left: the prompt > in the ask color with the query and a block cursor,
// the count of the models that match and of those listed with a rule after it, a spinner
// before it while the agent's list loads, and the rows. A model's ID shows its provider, up
// to a /, quiet, and the characters the query matched in the ask color and bold, with
// "session" or "review" at the right; the highlighted row is on the selection band with ▌,
// reversed without colors. The preview of the highlighted row takes the right. A finder too
// short to show a row under its border, prompt and count shows its rows alone, a line each.
func (m *model) finderLines(w, h int) []string {
	t := &m.theme
	p := m.entry.picker
	rows, total := m.pickRows()
	base := t.paint(nil, t.surface)
	at := p.index(rows)
	n := finderRowsShown(h)
	first := max(min(p.top, len(rows)-n), 0)
	if h <= finderChrome {
		shown := make([]string, h)
		for k := range shown {
			shown[k] = base.Render(strings.Repeat(" ", w))
			if first+k < len(rows) {
				shown[k] = m.finderRow(rows[first+k], first+k == at, w)
			}
		}
		return shown
	}
	mid := finderSplit(w)
	left, right := mid-1, w-mid-2 // the cells inside the border on each side of the line
	border := t.floatBorder()
	matched := 0
	for _, r := range rows {
		if r.note == "" && !r.typed {
			matched++
		}
	}

	top := newLine(mid)
	top.add("╭─", border)
	top.add(ansi.Truncate(" model · "+review.Visible(p.agent)+" ", max(mid-3, 0), "…"), t.paint(t.muted, t.surface))
	top.add(strings.Repeat("─", top.free), border)
	head := newLine(right) // between ┬ and ╮
	head.add("─", border)
	head.add(" preview ", t.paint(t.subtle, t.surface))
	head.add(strings.Repeat("─", head.free), border)
	lines := []string{top.fill(border) + border.Render("┬") + head.fill(border) + border.Render("╮")}

	prompt := newLine(left)
	prompt.add(" ", base)
	prompt.add(">", t.paint(t.ask, t.surface).Bold(true))
	prompt.add(" ", base)
	prompt.add(keepEnd(review.Visible(p.query), left-5), t.paint(t.strong, t.surface))
	prompt.add(" ", t.onBand(t.surface, t.ask))
	count := newLine(left)
	count.add("   ", base)
	if m.listLoading() {
		count.add(string(spinner[m.now().Second()%len(spinner)])+" ", t.paint(t.ask, t.surface))
	}
	count.add(strconv.Itoa(matched)+"/"+strconv.Itoa(total), t.paint(t.subtle, t.surface))
	count.add(" ", base)
	count.add(strings.Repeat("─", max(count.free-1, 0)), border)
	body := []string{prompt.fill(base), count.fill(base)} // the left of each line inside the border
	for k := first; k < min(first+n, len(rows)); k++ {
		body = append(body, m.finderRow(rows[k], k == at, left))
	}
	var preview [][]chunk
	if at >= 0 {
		preview = m.previewLines(rows[at], right-2)
	}
	empty := base.Render(strings.Repeat(" ", left))
	for k := range max(h-2, 0) {
		l := empty
		if k < len(body) {
			l = body[k]
		}
		r := newLine(right)
		r.add(" ", base)
		if k < len(preview) {
			for _, s := range preview[k] {
				r.add(s.text, t.paint(s.color, t.surface).Bold(s.bold))
			}
		}
		lines = append(lines, t.floatSide(l+border.Render("│")+r.fill(base)))
	}
	if h > 1 {
		bottom := newLine(w)
		bottom.add("╰"+strings.Repeat("─", max(mid-1, 0))+"┴", border)
		bottom.add(strings.Repeat("─", max(bottom.free-1, 0))+"╯", border)
		lines = append(lines, bottom.fill(border))
	}
	return lines[:min(len(lines), h)]
}

// finderRow draws row r of the open model finder, w cells wide, highlighted or not: a model
// with its tag at the right, the query as typed, or a note.
func (m *model) finderRow(r pickRow, highlighted bool, w int) string {
	t := &m.theme
	bg, on := t.surface, t.paint
	if highlighted {
		bg, on = t.surface2, t.onBand
	}
	base := on(nil, bg)
	head := newLine(min(3, w))
	if highlighted {
		head.add("▌", on(t.ask, bg))
	}
	tag, tagWidth := "", 0
	if r.tag != "" && w >= len(r.tag)+12 {
		tag, tagWidth = r.tag, len(r.tag)+3 // a gap before it and two cells after it
	}
	tail := newLine(tagWidth)
	tail.add(" "+tag, on(t.subtle, bg))
	l := newLine(w - 3 - tagWidth)
	switch {
	case r.fail:
		l.add("✗ ", on(t.del, bg).Bold(true))
		l.add(r.note, on(t.del, bg))
	case r.note != "":
		l.add(r.note, on(t.subtle, bg))
	case r.typed:
		l.add(`Use "`+review.Visible(r.id)+`" as typed`, on(t.fg, bg))
	default:
		text := t.fg
		if highlighted {
			text = t.strong
		}
		slash := strings.IndexByte(r.id, '/')
		lit := r.lit
		k := 0 // the rune of the ID at byte i
		for i, c := range r.id {
			st := on(text, bg)
			switch {
			case len(lit) > 0 && lit[0] == k:
				st, lit = on(t.ask, bg).Bold(true), lit[1:]
			case i <= slash:
				st = on(t.subtle, bg)
			}
			l.add(review.Visible(string(c)), st)
			k++
		}
	}
	return head.fill(base) + l.fill(base) + tail.fill(base)
}

// previewLines returns the preview of row r of the open model finder, w cells wide: the
// model's name and ID, and what picking it does, which is true of a and of c alike. A model
// the review used tells which session used it.
func (m *model) previewLines(r pickRow, w int) [][]chunk {
	t := &m.theme
	p := m.entry.picker
	agentName := review.Visible(p.agent)
	wrap := func(text string, c color.Color) [][]chunk {
		var lines [][]chunk
		for _, l := range strings.Split(ansi.Wrap(text, max(w, 1), ""), "\n") {
			lines = append(lines, []chunk{{text: l, color: c}})
		}
		return lines
	}
	id := review.Visible(r.id)
	if r.typed {
		return slices.Concat([][]chunk{{{text: id, color: t.strong, bold: true}}, {{text: "not in the list of " + agentName + " models", color: t.muted}}, nil},
			wrap("dr passes it to "+agentName+" as typed. If "+agentName+" has no such model, its error comes back in place of an answer.", t.fg))
	}
	lines := [][]chunk{{{text: cmp.Or(review.Visible(r.name), id), color: t.strong, bold: true}}, {{text: id, color: t.muted}}, nil}
	if r.tag == "session" {
		does := "Picking it asks the way a always does."
		if m.drafting != nil {
			does = "Picking it clears the comment's pick."
		}
		return append(lines, wrap("The session's own model. "+does, t.fg)...)
	}
	if s, ok := p.by[r.id]; ok {
		by := chunk{text: review.Visible(review.Who(s.agent, s.id, m.short)), color: m.sessionColor(s)}
		lines = append(lines, []chunk{{text: "used in this review by ", color: t.subtle}, by}, nil)
	}
	own := "the session's model"
	if p.own != "" {
		own = review.Visible(p.own)
	}
	only := "For this question only. The next one goes back to " + own + "."
	if m.drafting != nil {
		only = "For this comment only. :submit asks for its fix with this model; the other comments keep theirs."
	}
	return slices.Concat(lines, wrap(only, t.fg), [][]chunk{nil}, wrap("The session's reasoning effort is left out.", t.muted))
}
