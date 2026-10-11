package tui

import (
	"cmp"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// askingFor says how long an answer or a fix has been coming, d, after the spinner's frame
// for it, as the status line turns it: "⠼ asking… 4s".
func askingFor(d time.Duration) string {
	s := int(max(d, 0) / time.Second)
	return string(spinner[s%len(spinner)]) + " asking… " + strconv.Itoa(s) + "s"
}

// originCard returns the origin card of the cursor row, w cells wide. Its band and its header
// tell the origin of a changed row, a dot and a tag in its color, and where the row is, or
// for a file header the file and how many requests and sessions changed it. At the right
// edge of the header come the keys that ask about the row, a follow-up when it has
// questions, and close the card, or only the one that closes it when both do not fit. A row
// of the split view goes by its main line, as the status line does.
func (m *model) originCard(w int) card {
	t := &m.theme
	r := m.rows[m.cursor]
	f := m.files[r.file]
	c := card{band: t.subtle}
	var left []chunk
	switch {
	case r.kind != lineRow:
		c.band = t.accent
		left = []chunk{{text: "▾ ", color: t.subtle}, {text: f.path, color: t.strong, bold: true, cut: true}}
		if n := len(f.requests); n > 0 {
			left = append(left, chunk{text: "  " + countOf(n, "request") + " · " + countOf(len(f.sessions), "session"), color: t.muted})
		}
	case r.isChange():
		dot, tag, _, col := m.originOf(m.lines[r.main()].Edit)
		c.band = col
		left = []chunk{{text: dot + " ", color: col}, {text: tag, color: col, bold: true}, {text: "  " + m.where(), color: t.muted, cut: true}}
	default:
		left = []chunk{{text: m.where(), color: t.muted, cut: true}}
	}
	closing := m.keys([2]string{"⏎", "close"})
	options := [][]chunk{closing}
	if r.isChange() || m.hasQuestions(m.cursor) {
		ask := [2]string{"a", "ask"}
		if m.hasQuestions(m.cursor) {
			ask[1] = "follow-up"
		}
		options = [][]chunk{m.keys(ask, [2]string{"⏎", "close"}), closing}
	}
	c.head = cardRow{left: left, right: fitKeys(left, w-3, options...)}
	c.rows = m.originRows(textWidth(w))
	return c
}

// originRows returns the body of the origin card, its text wrapped to width: the questions
// asked about the cursor row, as a thread, over a rule, then the origin of the row. A row of
// the split view whose deleted and added lines have different origins tells both, under
// DELETED LINE and ADDED LINE, each with the tag of its origin. A file header lists the
// requests that changed the file.
func (m *model) originRows(width int) []cardRow {
	r := m.rows[m.cursor]
	if r.kind != lineRow {
		return m.fileRows(m.files[r.file], width)
	}
	var thread []cardRow
	if qs := m.questionsAt(m.cursor); len(qs) > 0 {
		thread = append(m.threadRows(qs, width), cardRow{rule: true})
	}
	if r.isChange() && r.left >= 0 && r.right >= 0 && !sameOrigin(m.lines[r.left].Edit, m.lines[r.right].Edit) {
		return joinGroups(thread,
			m.sideHead("DELETED LINE", m.lines[r.left].Edit), m.lineOrigin(m.lines[r.left], width),
			m.sideHead("ADDED LINE", m.lines[r.right].Edit), m.lineOrigin(m.lines[r.right], width))
	}
	return joinGroups(thread, m.lineOrigin(m.lines[r.main()], width))
}

// sideHead returns the head of a side of a split row in the origin card: its label, title,
// and the dot and the tag of e, the edit that wrote or deleted the line, in its color.
func (m *model) sideHead(title string, e *agent.Edit) []cardRow {
	dot, tag, _, c := m.originOf(e)
	return []cardRow{m.label(title, ""), {left: []chunk{{text: dot + " ", color: c}, {text: tag, color: c, bold: true}}}}
}

// sameOrigin reports whether lines from edits a and b have the same origin as the origin
// card tells it: the same request or fix and the same note, or no origin.
func sameOrigin(a, b *agent.Edit) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Agent == b.Agent && a.Session == b.Session && a.Turn == b.Turn && a.Note == b.Note && a.Fix == b.Fix
}

// lineOrigin tells the origin of l: the edit or the fix that wrote or deleted it, or that no
// session log explains a changed line, or that the line did not change.
func (m *model) lineOrigin(l origin.Line, width int) []cardRow {
	switch {
	case l.Edit != nil && l.Edit.Fix != nil:
		return m.fixRows(l, width)
	case l.Edit != nil:
		return m.editRows(l, width)
	case l.Old == 0 || l.New == 0:
		return m.section("UNKNOWN", "", review.NoOrigin, chunk{color: m.theme.fg}, width)
	}
	return []cardRow{{left: []chunk{{text: "Unchanged line", color: m.theme.muted}}}}
}

// editRows tells who wrote or deleted l: the whole request, what the agent said just before
// the edit, and what it said last for the request, then the session and its model, and the
// session that started it when it is a subagent.
func (m *model) editRows(l origin.Line, width int) []cardRow {
	t := &m.theme
	e := l.Edit
	var deleted []cardRow
	if l.New == 0 {
		deleted = wrapped("This edit deleted the line", chunk{color: t.del}, width)
	}
	return joinGroups(
		deleted,
		m.section("REQUEST", "#"+strconv.Itoa(e.Turn), e.Prompt, chunk{color: t.fg}, width),
		m.section("NOTE", "", e.Note, chunk{color: t.muted, italic: true}, width),
		m.section("REPLY", "", e.Reply, chunk{color: t.fg}, width),
		m.sessionRows(e, ""),
	)
}

// sessionRows returns the quiet last rows of the origin of a line e wrote or deleted: the
// session, the fork when fork is not "", and the model, then the session that started it
// when it is a subagent.
func (m *model) sessionRows(e *agent.Edit, fork string) []cardRow {
	t := &m.theme
	text := "session " + review.Visible(e.Session)
	if fork != "" {
		text += " · fork " + review.Visible(fork)
	}
	if e.Model != "" {
		text += " · " + modelName(e.Model)
	}
	rows := []cardRow{{left: []chunk{{text: text, color: t.subtle, cut: true}}}}
	if e.Parent != "" {
		rows = append(rows, cardRow{left: []chunk{{text: "↳ subagent of " + review.Visible(review.Who(e.Agent, e.Parent, m.short)), color: t.subtle}}})
	}
	return rows
}

// section returns a section of a card: text, made visible and wrapped to width in the color
// and the style of c, under the label title and extra, or nothing when text is blank.
func (m *model) section(title, extra, text string, c chunk, width int) []cardRow {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return append([]cardRow{m.label(title, extra)}, wrapped(review.Visible(text), c, width)...)
}

// fileRows lists the requests that changed f, each with the dot and the tag of its origin in
// its color and the line that sums up the request, or the comment for a fix dr applied, and
// the questions about f and the comments on it that have no place in its diff now.
func (m *model) fileRows(f file, width int) []cardRow {
	t := &m.theme
	tags := 0
	for _, e := range f.requests {
		tags = max(tags, ansi.StringWidth(review.Tag(e, m.short)))
	}
	if f.unknown {
		tags = max(tags, len("unknown"))
	}
	request := func(dot, tag, about string, c color.Color) cardRow {
		tag += strings.Repeat(" ", tags-ansi.StringWidth(tag))
		return cardRow{left: []chunk{{text: dot + " ", color: c}, {text: tag, color: c, bold: true}, {text: "  " + about, color: t.fg, cut: true}}}
	}
	rows := []cardRow{m.label("REQUESTS THAT CHANGED THIS FILE", "")}
	for _, e := range f.requests {
		rows = append(rows, request(m.originOf(e)))
	}
	switch {
	case f.unknown:
		dot, tag, about, c := m.originOf(nil)
		r := request(dot, tag, about, c)
		r.left[2].color = t.muted
		rows = append(rows, r)
	case f.noDiff != "":
		rows = append(rows, wrapped(review.Visible(f.noDiff), chunk{color: t.fg}, width)...)
		rows = append(rows, cardRow{left: []chunk{{text: "No origin lookup for this file", color: t.muted}}})
	case len(f.requests) == 0:
		rows = append(rows, cardRow{left: []chunk{{text: "No changed lines", color: t.muted}}})
	}
	return joinGroups(rows, m.earlierRows(f.earlier, width), m.earlierComments(f.key, width))
}

// threadRows tells qs, the questions about a row, oldest first, under THREAD and their
// count: each question with its answer, an empty row apart.
func (m *model) threadRows(qs []*question, width int) []cardRow {
	rows := []cardRow{m.label("THREAD", strconv.Itoa(len(qs)))}
	for i, q := range qs {
		if i > 0 {
			rows = append(rows, cardRow{})
		}
		rows = append(rows, m.exchange(q, width)...)
	}
	return rows
}

// exchange tells q and its answer, wrapped to width. The question has you and how long ago
// it was asked, when the log knows, over its text. The answer has a header with the dot and
// the name of who was asked in its color, then the badge of the answer's tag as a chip, or
// how long it has been coming, or that none came, and at the right edge the model that
// answered and how long it took. Under it come how the session asked relates to the lines
// when it did not write them, and the answer without its tag, or why none came.
func (m *model) exchange(q *question, width int) []cardRow {
	t := &m.theme
	you := cardRow{left: []chunk{{text: "you", color: t.strong, bold: true}}}
	if !q.Time.IsZero() {
		you.left = append(you.left, chunk{text: "  " + ago(m.now().Sub(q.Time)), color: t.subtle})
	}
	rows := append([]cardRow{you}, wrapped(review.Visible(q.Text), chunk{color: t.fg}, width)...)
	c := m.whoColor(q.Agent, q.Session)
	who := cardRow{left: []chunk{{text: "● ", color: c}, {text: m.askedWho(q), color: c, bold: true}}}
	tell := func(s chunk) { who.left = append(who.left, chunk{text: "  "}, s) }
	var answer []cardRow
	switch a := q.answer; {
	case q.pending:
		tell(chunk{text: askingFor(m.now().Sub(q.Time)), color: t.ask})
	case a == nil:
		tell(chunk{text: "no answer", color: t.muted, chip: true})
		answer = wrapped("dr closed before it came", chunk{color: t.muted}, width)
	case a.Error != "":
		tell(chunk{text: "no answer", color: t.del, chip: true})
		answer = wrapped(review.Visible(a.Error), chunk{color: t.del}, width)
	default:
		kind, text := answerKind(a.Text)
		if b, ok := m.badge(kind); ok {
			tell(b)
		}
		who.right = []chunk{m.answeredBy(q, a)}
		if text = review.Visible(text); text != "" {
			answer = wrapped(text, chunk{color: t.fg}, width)
		}
	}
	rows = append(rows, who)
	if r := relationText(review.Relation(q.Relation)); r != "" {
		rows = append(rows, m.relationRow(r))
	}
	return append(rows, answer...)
}

// relationRow returns the row that tells how a session relates to the lines, r, when it did
// not write them: quiet and slanted after ↳.
func (m *model) relationRow(r string) cardRow {
	return cardRow{left: []chunk{{text: "↳ ", color: m.theme.subtle}, {text: r, color: m.theme.subtle, italic: true}}}
}

// whoColor returns the color of session id of agent, quiet for a new session, which has none.
func (m *model) whoColor(agent, id string) color.Color {
	if id == "" {
		return m.theme.muted
	}
	return m.sessionColor(session{agent, id})
}

// earlierRows tells qs, questions about a file that have no place in its diff now, under
// EARLIER QUESTIONS and their count: each with the lines it asked about quoted, then as in a
// thread, all of it dim.
func (m *model) earlierRows(qs []*question, width int) []cardRow {
	if len(qs) == 0 {
		return nil
	}
	rows := []cardRow{m.label("EARLIER QUESTIONS", strconv.Itoa(len(qs)))}
	for i, q := range qs {
		if i > 0 {
			rows = append(rows, cardRow{})
		}
		rows = append(rows, m.quoted(q.Lines, q.Asked(), width)...)
		rows = append(rows, m.dim(m.exchange(q, width))...)
	}
	return rows
}

// quoted returns the lines of lines at picked as rows, quiet, each after > for a line of the
// current file or - for a deleted one. A line wider than width goes on in the rows under it,
// under its text.
func (m *model) quoted(lines []state.Line, picked []int, width int) []cardRow {
	rows := make([]cardRow, 0, len(picked))
	for _, k := range picked {
		l := lines[k]
		mark := "> "
		if l.Kind == "-" {
			mark = "- "
		}
		for _, part := range splitPieces([]piece{{text: display(l.Text)}}, max(width-len(mark), 1)) {
			rows = append(rows, cardRow{left: []chunk{{text: mark + join(part), color: m.theme.muted}}})
			mark = "  "
		}
	}
	return rows
}

// askedWho names who q asked: the agent and the session, or a new session of the agent.
func (m *model) askedWho(q *question) string {
	if q.Session == "" {
		return review.Visible(q.Agent) + ", new session"
	}
	return review.Visible(review.Who(q.Agent, q.Session, m.short))
}

// relationText tells how a session asked relates to the lines of relation r when it did not
// write them, and "" when it did.
func relationText(r review.Relation) string {
	switch r {
	case review.Parent:
		return "started the subagent that wrote these lines"
	case review.Editor:
		return "edited this file, not these lines"
	case review.NewSession:
		return "new session, without this code's history"
	}
	return ""
}

// badge returns the chip of an answer of kind k, as answerKind reads it from its tag: green
// for decided, blue for chose and amber for a guess. It reports false for no kind.
func (m *model) badge(k string) (chunk, bool) {
	t := &m.theme
	switch k {
	case decided:
		return chunk{text: decided, color: t.add, chip: true}, true
	case chose:
		return chunk{text: chose, color: t.accent, chip: true}, true
	case guess:
		return chunk{text: guess, color: t.pending, chip: true}, true
	}
	return chunk{}, false
}

// answeredBy returns the note on answer a to q: the model that answered, without its
// provider, and how long it took, as modelNote writes it, or as pickedNote writes it when q
// picked its model with ^. A new session has no model of its own to compare with.
func (m *model) answeredBy(q *question, a *state.Answer) chunk {
	took := time.Duration(a.TookMS) * time.Millisecond
	if q.Model != "" {
		return m.pickedNote(a.Model, q.Model, took)
	}
	own := modelName(q.own)
	if review.Relation(q.Relation) == review.NewSession {
		own = ""
	}
	return m.modelNote(modelName(a.Model), own, took)
}

// modelNote returns "model · took", in the warning color with ", not own" after the model
// when the session's own model is known and model is unknown or another. Without its own
// model, dr cannot compare, so the note stays quiet.
func (m *model) modelNote(model, own string, took time.Duration) chunk {
	tail := " · " + took.Round(time.Second).String()
	if own != "" && model != own {
		return chunk{text: cmp.Or(model, "unknown") + ", not " + own + tail, color: m.theme.pending}
	}
	return chunk{text: cmp.Or(model, "unknown") + tail, color: m.theme.subtle}
}

// pickedNote returns "^model · took" for an answer or a fix asked with picked, a model picked
// with ^: model, the one that answered, or picked when the CLI did not tell, both without
// their provider. It is always quiet: the person chose the model, and an alias such as
// "sonnet" cannot be compared with the full name the CLI tells.
func (m *model) pickedNote(model, picked string, took time.Duration) chunk {
	return chunk{text: "^" + cmp.Or(modelName(model), modelName(picked)) + " · " + took.Round(time.Second).String(), color: m.theme.subtle}
}

// modelName returns model, made visible, without the provider before its last slash:
// "claude-opus-5-5" for "anthropic/claude-opus-5-5".
func modelName(model string) string {
	return review.Visible(model[strings.LastIndex(model, "/")+1:])
}

// ago says how long ago something was that happened d ago, the short way GitHub does:
// "just now" for less than a minute, then whole minutes, hours or days, "4m", "3h", "2d".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// boxRows returns how many lines of its body the origin card shows at most: the diff height
// less the cursor row, one more row, and the card's header and last row.
func (m *model) boxRows() int {
	return max(m.rowsHeight()-4, 1)
}

// boxHeight returns how many rows of the screen the open origin card takes, its header and
// its last row included.
func (m *model) boxHeight() int {
	return min(m.blockHeight(), m.boxRows()) + 2
}

// blockHeight returns how many lines the open origin card's body scrolls through: its own
// rows, and the cards of the cursor row's comments after them.
func (m *model) blockHeight() int {
	_, w := m.layout()
	_, width := m.boxLeft(w)
	return len(m.block(m.originCard(width), width))
}

// block returns the lines the body of c, the origin card width cells wide, scrolls through:
// its rows, then, when the cursor row has comments, an empty row and the cards of the
// comments, each whole and one cell narrower, for the scroll bar.
func (m *model) block(c card, width int) []blockLine {
	lines := m.cardBody(c, width)
	cs := m.commentsAt(m.cursor)
	if len(cs) == 0 {
		return lines
	}
	lines = append(lines, m.cardBody(card{band: c.band, rows: []cardRow{{}}}, width)...)
	for _, cm := range cs {
		lines = append(lines, m.fullCard(m.commentCard(cm, width-1), width-1)...)
	}
	return lines
}

// boxLeft returns where the origin card starts in a diff w cells wide, and how wide it is.
func (m *model) boxLeft(w int) (x, width int) {
	x = m.cardLeft(m.files[m.rows[m.cursor].file], w)
	return x, w - x
}

// originLines returns the rows of the origin card's body in the diff as the screen lays it
// out.
func (m *model) originLines() []cardRow {
	_, w := m.layout()
	_, width := m.boxLeft(w)
	return m.originRows(textWidth(width))
}

// questionTop returns the first row of the origin card's body to show so that the card shows
// q, a question about the cursor row, and its answer: the top when the thread fits down to
// the end of the answer, or else the row that brings that end to the bottom of the card,
// but no further down than q's first row. It returns 0 when q is not about the cursor row.
func (m *model) questionTop(q *question) int {
	qs := m.questionsAt(m.cursor)
	k := slices.Index(qs, q)
	if k < 0 {
		return 0
	}
	_, w := m.layout()
	_, width := m.boxLeft(w)
	width = textWidth(width)
	start := 1 + k // the label, and the empty row after each question before q
	for _, p := range qs[:k] {
		start += len(m.exchange(p, width))
	}
	end := start + len(m.exchange(q, width))
	return min(start, max(end-m.boxRows(), 0))
}

// box draws the open origin card under the cursor row of a diff w cells wide: its header, its
// body and the cards of the row's comments, which scroll as one from readTop, and its last
// row.
func (m *model) box(w int) []string {
	x, width := m.boxLeft(w)
	c := m.originCard(width)
	return m.frame(c, x, width, m.block(c, width), m.readTop, m.boxRows())
}

// where returns where the cursor row is: the path of its file, and the line number of the
// main line of a line row, the new one or else the old one.
func (m *model) where() string {
	r := m.rows[m.cursor]
	path := m.files[r.file].path
	if r.kind != lineRow {
		return path
	}
	l := m.lines[r.main()]
	n := l.New
	if n == 0 {
		n = l.Old
	}
	return path + ":" + strconv.Itoa(n)
}

// spansWidth returns the width of spans in cells.
func spansWidth(spans []span) int {
	w := 0
	for _, s := range spans {
		w += ansi.StringWidth(s.text)
	}
	return w
}
