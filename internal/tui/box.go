package tui

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/x/ansi"
)

// tone is how a paragraph of the origin reads.
type tone int

const (
	body      tone = iota // text
	heading               // the quiet title of a part
	strong                // what a part is about, such as the tag of a request
	quiet                 // something to know in passing
	warn                  // a changed line no session log explains
	trouble               // a sentence on something that went wrong
	removal               // the line under the cursor was deleted
	leftSide              // the title of the deleted line of a split row
	rightSide             // the title of the added line of a split row
	agreed                // the badge of an answer whose reason was agreed with the user
	chosen                // the badge of an answer whose reason the session asked chose
	guessed               // the badge of an answer that is a guess
	unsent                // the badge of a comment not sent yet
	fixOld                // a line of a comment's range that a fix replaces
	fixNew                // a line a fix writes
)

// bar is the bar at the left edge of the lines of a question or an answer in a thread.
type bar int

const (
	noBar     bar = iota // not in a thread
	askBar               // a question
	answerBar            // an answer
	dimBar               // an earlier question or comment, or an answer, which has no place in the diff now
	changeBar            // a change comment
)

// paragraph is a piece of the origin and how it reads. The origin box wraps it to its
// width, after the bar of its thread when it is part of one. A header of a thread is a line
// of its own: its text, a badge after it, and a note at the right edge, which is left out
// when the line has no room for it.
type paragraph struct {
	text  string // made visible
	tone  tone
	bar   bar
	head  bool // a header of a thread
	badge part // after the text of a header
	note  part // at the right edge of a header
	rule  bool // a rule across the box, in place of text
}

// part is a piece of a header of a thread and how it reads.
type part struct {
	text string // made visible
	tone tone
}

// originText returns the questions asked about the row under the cursor, as a thread, and
// the origin of the row under a rule. A row of the split view whose deleted and added lines
// read differently tells both, under Deleted line and Added line.
func (m *model) originText() []paragraph {
	r := m.rows[m.cursor]
	if r.kind != lineRow {
		return m.fileText(m.files[r.file])
	}
	var paragraphs []paragraph
	if qs := m.questionsAt(m.cursor); len(qs) > 0 {
		paragraphs = append(m.threadText(qs), paragraph{rule: true})
	}
	if r.isChange() && r.left >= 0 && r.right >= 0 && !sameOrigin(m.lines[r.left].Edit, m.lines[r.right].Edit) {
		paragraphs = append(paragraphs, paragraph{text: "Deleted line", tone: leftSide})
		paragraphs = append(paragraphs, m.lineText(m.lines[r.left])...)
		paragraphs = append(paragraphs, paragraph{}, paragraph{text: "Added line", tone: rightSide})
		return append(paragraphs, m.lineText(m.lines[r.right])...)
	}
	return append(paragraphs, m.lineText(m.lines[r.main()])...)
}

// sameOrigin reports whether lines from edits a and b have the same origin as the origin
// box tells it: the same request or fix and the same note, or no origin.
func sameOrigin(a, b *agent.Edit) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Agent == b.Agent && a.Session == b.Session && a.Turn == b.Turn && a.Note == b.Note && a.Fix == b.Fix
}

// lineText tells the origin of l: the edit or the fix that wrote or deleted it, or that no
// session log explains a changed line, or that the line did not change.
func (m *model) lineText(l origin.Line) []paragraph {
	switch {
	case l.Edit != nil && l.Edit.Fix != nil:
		return m.fixText(l)
	case l.Edit != nil:
		return m.editText(l)
	case l.Old == 0 || l.New == 0:
		return []paragraph{{text: "Unknown", tone: warn}, {text: review.NoOrigin}}
	}
	return []paragraph{{text: "Unchanged line", tone: quiet}}
}

// editText tells who wrote or deleted l: the agent and the session of the request, the
// whole request, what the agent said just before the edit, and what it said last for the
// request.
func (m *model) editText(l origin.Line) []paragraph {
	e := l.Edit
	paragraphs := []paragraph{
		{text: "Session", tone: heading},
		{text: review.Tag(e, m.short), tone: strong},
		{text: review.Visible(e.Session), tone: quiet},
	}
	if e.Parent != "" {
		paragraphs = append(paragraphs, paragraph{text: "subagent of " + m.short[e.Parent], tone: quiet})
	}
	if l.New == 0 {
		paragraphs = append(paragraphs, paragraph{text: "This edit deleted the line", tone: removal})
	}
	paragraphs = append(paragraphs, section("Request", e.Prompt)...)
	paragraphs = append(paragraphs, section("Note", e.Note)...)
	return append(paragraphs, section("Reply", e.Reply)...)
}

// section returns a part of the origin, text under the heading title after a blank line, or
// nothing when text is blank.
func section(title, text string) []paragraph {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return []paragraph{{}, {text: title, tone: heading}, {text: review.Visible(text)}}
}

// fileText lists the requests that changed f, each with the first line of the request, or
// of the comment for a fix dr applied, and the questions about f and the comments on it that
// have no place in its diff now.
func (m *model) fileText(f file) []paragraph {
	paragraphs := []paragraph{{text: "Requests that changed this file", tone: heading}}
	for _, e := range f.requests {
		first, _, _ := strings.Cut(strings.TrimSpace(requestText(e)), "\n")
		paragraphs = append(paragraphs, paragraph{text: review.Tag(e, m.short), tone: strong}, paragraph{text: review.Visible(first)})
	}
	switch {
	case f.unknown:
		paragraphs = append(paragraphs, paragraph{}, paragraph{text: "Unknown", tone: warn}, paragraph{text: review.NoOrigin})
	case f.noDiff != "":
		paragraphs = append(paragraphs, paragraph{text: f.noDiff}, paragraph{text: "No origin lookup for this file", tone: quiet})
	case len(f.requests) == 0:
		paragraphs = append(paragraphs, paragraph{text: "No changed lines", tone: quiet})
	}
	paragraphs = append(paragraphs, m.earlierText(f.earlier)...)
	return append(paragraphs, m.earlierComments(f.key)...)
}

// threadText tells qs, the questions about a row, oldest first, the way GitHub draws a
// review thread: under Questions and their count, each question with its answer, a blank
// line apart.
func (m *model) threadText(qs []*question) []paragraph {
	paragraphs := []paragraph{{text: "Questions · " + strconv.Itoa(len(qs)), tone: heading}}
	for i, q := range qs {
		if i > 0 {
			paragraphs = append(paragraphs, paragraph{})
		}
		paragraphs = append(paragraphs, m.exchange(q)...)
	}
	return paragraphs
}

// exchange tells q and its answer, each after its bar. The question has a header with You
// and how long ago it was asked, when the log knows, over its text. A blank line under it,
// the answer has a header with who was asked and the badge of the answer's tag, or how long
// it has been coming, or that none came, and at the right edge the model that answered and
// how long it took. Under it come how the session asked relates to the lines when it did
// not write them, and the answer without its tag, or why none came.
func (m *model) exchange(q *question) []paragraph {
	you := paragraph{text: "You", tone: strong, bar: askBar, head: true}
	if !q.Time.IsZero() {
		you.note = part{text: ago(m.now().Sub(q.Time)), tone: quiet}
	}
	who := paragraph{text: m.askedWho(q), tone: strong, bar: answerBar, head: true}
	var answer paragraph
	switch a := q.answer; {
	case q.pending:
		waited := max(m.now().Sub(q.Time), 0) / time.Second
		who.badge = part{text: "asking… " + strconv.Itoa(int(waited)) + "s", tone: quiet}
	case a == nil:
		who.badge = part{text: "no answer", tone: trouble}
		answer = paragraph{text: "dr closed before it came", tone: trouble}
	case a.Error != "":
		who.badge = part{text: "no answer", tone: trouble}
		answer = paragraph{text: review.Visible(a.Error), tone: trouble}
	default:
		kind, text := answerKind(a.Text)
		who.badge, who.note = badge(kind), answeredBy(q, a)
		answer = paragraph{text: review.Visible(text)}
	}
	paragraphs := []paragraph{you, {text: review.Visible(q.Text), bar: askBar}, {bar: askBar}, who}
	if r := relationText(review.Relation(q.Relation)); r != "" {
		paragraphs = append(paragraphs, paragraph{text: r, tone: quiet, bar: answerBar})
	}
	if answer.text != "" {
		answer.bar = answerBar
		paragraphs = append(paragraphs, answer)
	}
	return paragraphs
}

// earlierText tells qs, questions about a file that have no place in its diff now, under
// Earlier questions and their count: each with the lines it asked about quoted, then as in
// a thread, all of it dim.
func (m *model) earlierText(qs []*question) []paragraph {
	if len(qs) == 0 {
		return nil
	}
	paragraphs := []paragraph{{}, {text: "Earlier questions · " + strconv.Itoa(len(qs)), tone: heading}}
	for i, q := range qs {
		if i > 0 {
			paragraphs = append(paragraphs, paragraph{})
		}
		for _, k := range q.Asked() {
			l := q.Lines[k]
			mark := "> "
			if l.Kind == "-" {
				mark = "- "
			}
			paragraphs = append(paragraphs, paragraph{text: mark + display(l.Text), tone: quiet})
		}
		for _, p := range m.exchange(q) {
			p.bar, p.tone, p.badge.tone, p.note.tone = dimBar, quiet, quiet, quiet
			paragraphs = append(paragraphs, p)
		}
	}
	return paragraphs
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

// badge returns the badge of an answer of kind k, as answerKind reads it from its tag, and
// none for none.
func badge(k string) part {
	switch k {
	case decided:
		return part{text: "● decided", tone: agreed}
	case chose:
		return part{text: "● chose", tone: chosen}
	case guess:
		return part{text: "● guess", tone: guessed}
	}
	return part{}
}

// answeredBy returns the note on answer a to q: the model that answered, without its
// provider, and how long it took, as modelNote writes it, or as pickedNote writes it when q
// picked its model with ^. A new session has no model of its own to compare with.
func answeredBy(q *question, a *state.Answer) part {
	took := time.Duration(a.TookMS) * time.Millisecond
	if q.Model != "" {
		return pickedNote(a.Model, q.Model, took)
	}
	own := modelName(q.own)
	if review.Relation(q.Relation) == review.NewSession {
		own = ""
	}
	return modelNote(modelName(a.Model), own, took)
}

// modelNote returns "model · took", in the warning color with ", not own" after the model
// when the session's own model is known and model is unknown or another. Without its own
// model, dr cannot compare, so the note stays quiet.
func modelNote(model, own string, took time.Duration) part {
	tail := " · " + took.Round(time.Second).String()
	if own != "" && model != own {
		return part{text: cmp.Or(model, "unknown") + ", not " + own + tail, tone: trouble}
	}
	return part{text: cmp.Or(model, "unknown") + tail, tone: quiet}
}

// pickedNote returns "^model · took" for an answer or a fix asked with picked, a model picked
// with ^: model, the one that answered, or picked when the CLI did not tell, both without
// their provider. It is always quiet: the person chose the model, and an alias such as
// "sonnet" cannot be compared with the full name the CLI tells.
func pickedNote(model, picked string, took time.Duration) part {
	return part{text: "^" + cmp.Or(modelName(model), modelName(picked)) + " · " + took.Round(time.Second).String(), tone: quiet}
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

// boxLeft returns the cell of a diff w cells wide where the origin box starts: the code
// column of the cursor row, on the left side in the split view, so the box hangs under the
// code across the whole diff, but no further right than a third of the diff.
func (m *model) boxLeft(w int) int {
	return min(m.codeColumn(m.files[m.rows[m.cursor].file]), w/3)
}

// boxRows returns how many lines of its text the origin box shows at most: the diff height
// less the cursor row, the boxes of its comments, one more row and the box's two borders.
func (m *model) boxRows() int {
	return max(m.diffHeight()-4-m.commentHeight(m.cursor), 1)
}

// boxHeight returns how many rows of the screen the origin box takes, its borders included,
// for a text of n lines.
func (m *model) boxHeight(n int) int {
	return min(n, m.boxRows()) + 2
}

// originLines returns the lines of the origin box's text in the diff as the screen lays it
// out.
func (m *model) originLines() []string {
	_, w := m.layout()
	return m.boxText(w)
}

// boxWidth returns how wide the origin box lays out its text in a diff w cells wide: inside
// "│ " and a margin, up to maxReadWidth.
func (m *model) boxWidth(w int) int {
	return max(min(w-m.boxLeft(w)-3, maxReadWidth), 1)
}

// boxText returns the origin of the row under the cursor as the origin box shows it in a
// diff w cells wide, each paragraph laid out by paragraphLines.
func (m *model) boxText(w int) []string {
	width := m.boxWidth(w)
	var lines []string
	for _, p := range m.originText() {
		lines = append(lines, m.paragraphLines(p, width)...)
	}
	return lines
}

// paragraphLines returns p as the origin box shows it, width cells wide and styled: a rule
// across the box, a header of a thread on a line of its own, or the text wrapped after the
// bar of its thread when it is part of one.
func (m *model) paragraphLines(p paragraph, width int) []string {
	t := &m.theme
	switch {
	case p.rule:
		return []string{t.paint(t.subtle, t.band).Render(strings.Repeat("─", width))}
	case p.head:
		return []string{m.headLine(p, width)}
	}
	var prefix string
	if p.bar != noBar {
		prefix = m.barStyle(p.bar).Render("▍") + t.paint(nil, t.band).Render(" ")
		width = max(width-2, 1)
	}
	style := m.toneStyle(p.tone)
	lines := strings.Split(lipgloss.NewStyle().Width(width).Render(p.text), "\n")
	for i, s := range lines {
		lines[i] = prefix + style.Render(s)
	}
	return lines
}

// headLine draws p, a header of a thread, width cells wide: its bar and its text, cut to fit
// without splitting a wide character, then its badge and its note at the right edge, each
// only when it fits whole two cells after what comes before. Line breaks and tabs in any of
// them draw as spaces, so the header stays one row.
func (m *model) headLine(p paragraph, width int) string {
	t := &m.theme
	base := t.paint(nil, t.band)
	l := newLine(width)
	l.add("▍", m.barStyle(p.bar))
	l.add(" ", base)
	l.add(oneLine.Replace(p.text), m.toneStyle(p.tone))
	if b := oneLine.Replace(p.badge.text); b != "" && ansi.StringWidth(b)+2 <= l.free {
		l.add("  ", base)
		l.add(b, m.toneStyle(p.badge.tone))
	}
	note := oneLine.Replace(p.note.text)
	if n := ansi.StringWidth(note); n > 0 && n+2 <= l.free {
		l.add(strings.Repeat(" ", l.free-n), base)
		l.add(note, m.toneStyle(p.note.tone))
	}
	return l.fill(base)
}

// oneLine turns line breaks and tabs into spaces for text drawn on a single row.
var oneLine = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")

// questionTop returns the first line of the origin box's text to show so that the box shows
// q, a question about the cursor row, and its answer: the top when the thread fits down to
// the end of the answer, or else the line that brings that end to the bottom of the box,
// but no further down than q's header. It returns 0 when q is not about the cursor row.
func (m *model) questionTop(q *question) int {
	qs := m.questionsAt(m.cursor)
	k := slices.Index(qs, q)
	if k < 0 {
		return 0
	}
	_, w := m.layout()
	width := m.boxWidth(w)
	count := func(paragraphs []paragraph) int {
		n := 0
		for _, p := range paragraphs {
			n += len(m.paragraphLines(p, width))
		}
		return n
	}
	start := count(m.threadText(qs[:k])) + min(k, 1) // the questions before q, and the blank line after them
	end := start + count(m.exchange(q))
	return min(start, max(end-m.boxRows(), 0))
}

// box draws the open origin box, w cells wide: a top border with the tag of the request
// behind the cursor row and where the row is, the part of the text the box shows from
// readTop, and a bottom border. Like tuicr's line comments it hangs from the code column to
// the right edge and has no right border, so a wide character never pushes a border out of
// line.
func (m *model) box(w int) []string {
	t := &m.theme
	x := m.boxLeft(w)
	text := m.boxText(w)
	text = text[m.readTop:min(m.readTop+m.boxRows(), len(text))]
	pad := strings.Repeat(" ", x)
	base, border := t.paint(nil, t.band), t.paint(t.subtle, t.band)
	lines := make([]string, 0, len(text)+2)
	lines = append(lines, pad+m.boxTop(w-x))
	for _, s := range text {
		l := newLine(w - x)
		l.add("│ ", border)
		l.add(s, lipgloss.NewStyle())
		lines = append(lines, pad+l.fill(base))
	}
	return append(lines, pad+border.Render("╰"+strings.Repeat("─", max(w-x-1, 0))))
}

// boxTop draws the top border of the origin box, w cells wide: the tag of the request behind
// a changed cursor row, where the row is, and the keys that ask from the box and close it
// when there is room for them, or else the one that closes it. a shows when the box has
// lines to ask about, the changed line of its row or those its newest question asked about.
// A row of the split view goes by its main line, as the status line does.
func (m *model) boxTop(w int) string {
	t := &m.theme
	border := t.paint(t.subtle, t.band)
	r := m.rows[m.cursor]
	var title []span
	if r.isChange() {
		tag := span{text: "unknown", color: t.warning, bold: true}
		if e := m.lines[r.main()].Edit; e != nil {
			tag = span{text: review.Tag(e, m.short), color: t.accent, bold: true}
		}
		title = append(title, tag, span{text: "  "})
	}
	title = append(title, span{text: m.where(), color: t.muted})
	l := newLine(w)
	l.add("╭─ ", border)
	for _, s := range title {
		l.add(s.text, t.paint(s.color, t.band).Bold(s.bold))
	}
	l.add(" ", border)
	esc := []span{{text: " "}, {text: "Esc", color: t.accent, bold: true}, {text: " close ", color: t.muted}}
	hints := [][]span{esc}
	if r.isChange() || m.hasQuestions(m.cursor) {
		ask := []span{{text: " "}, {text: "a", color: t.accent, bold: true}, {text: " ask · ", color: t.muted}}
		hints = [][]span{append(ask, esc[1:]...), esc}
	}
	var hint []span
	rule := l.free
	for _, h := range hints {
		if n := l.free - spansWidth(h) - 1; n >= 2 { // a rule before the hint and a cell of it after
			hint, rule = h, n
			break
		}
	}
	l.add(strings.Repeat("─", rule), border)
	for _, s := range hint {
		l.add(s.text, t.paint(s.color, t.band).Bold(s.bold))
	}
	l.add(strings.Repeat("─", l.free), border)
	return l.fill(border)
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

// toneStyle returns the style of a paragraph of tone k in the origin box.
func (m *model) toneStyle(k tone) lipgloss.Style {
	t := &m.theme
	switch k {
	case heading:
		return t.paint(t.muted, t.band).Bold(true)
	case strong:
		return t.paint(t.text, t.band).Bold(true)
	case quiet:
		return t.paint(t.muted, t.band)
	case warn:
		return t.paint(t.warning, t.band).Bold(true)
	case trouble, guessed:
		return t.paint(t.warning, t.band)
	case removal:
		return t.paint(t.deleted, t.band)
	case leftSide:
		return t.paint(t.deleted, t.band).Bold(true)
	case rightSide:
		return t.paint(t.added, t.band).Bold(true)
	case agreed:
		return t.paint(t.added, t.band)
	case chosen:
		return t.paint(t.accent, t.band)
	case unsent:
		return t.paint(t.warning, t.band).Bold(true)
	case fixOld:
		return t.paint(t.text, t.deleteLine)
	case fixNew:
		return t.paint(t.text, t.addLine)
	}
	return t.paint(t.text, t.band)
}

// barStyle returns the style of bar b of a thread: a question's in the accent color, an
// answer's in the color of added lines, a comment's in the warning color, and an earlier
// one's dim.
func (m *model) barStyle(b bar) lipgloss.Style {
	t := &m.theme
	switch b {
	case askBar:
		return t.paint(t.accent, t.band)
	case answerBar:
		return t.paint(t.added, t.band)
	case changeBar:
		return t.paint(t.warning, t.band)
	}
	return t.paint(t.muted, t.band)
}

// spansWidth returns the width of spans in cells.
func spansWidth(spans []span) int {
	w := 0
	for _, s := range spans {
		w += ansi.StringWidth(s.text)
	}
	return w
}
