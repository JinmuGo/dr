package tui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// commentMark comes before the count of a file's comments in the file tree and in the
// file's header.
const commentMark = "✎"

// comment is a change comment left with c, in this run or an earlier one, which is not
// removed and which no fix dr applied resolved.
type comment struct {
	state.Comment
	n    int // its place among the comments, in the order they were made
	file int // in model.files
	// first and last are its lines in model.lines, from the first selected to the last, and
	// under is the last line of the current file among the selected ones, under whose row
	// its box shows. All three are -1 when it has no place in the diff now.
	first, last, under int
	request            string     // the fix request in flight for it, "" when none is
	asked              time.Time  // when that request went
	fix                *state.Fix // the fix that shows under it, or why none came; nil for none
}

// placed reports whether c has a place in the diff now. Only then is it drawn under its
// lines and counted.
func (c *comment) placed() bool {
	return c.first >= 0
}

// pending reports whether :submit sends c: it has a place in the diff, and neither waits
// for a fix nor has one under it, other than why none came.
func (c *comment) pending() bool {
	return c.placed() && c.request == "" && (c.fix == nil || c.fix.Error != "")
}

// picked returns the lines in model.lines that were selected for c, which has a place.
func (c *comment) picked() []int {
	picked := c.Picked()
	lines := make([]int, len(picked))
	for i, k := range picked {
		lines[i] = c.first + k
	}
	return lines
}

// commentInput is what the input box of c comments on, and who the comment goes to.
type commentInput struct {
	editing  *comment      // the comment edited, nil for a new one
	target   review.Target // who a new comment goes to
	file     int
	lines    []int  // the lines selected in model.lines, in the order of the diff
	row      int    // the row the input box is under: that of the last selected line of the current file
	who      string // who the comment goes to, as the box's title names them
	from, to int    // the comment's range in the current file
}

// pendingComment is a pending comment as a fix request needs it.
type pendingComment struct {
	comment     *comment
	id          string
	relation    review.Relation // how the session it goes to relates to its lines
	path        string          // of the file, as review.File has it
	first, last int             // the range in the current file, from 1
	current     []string        // the lines of the range in the current file, those between hunks too
	diff        []origin.Line   // the diff lines from the first selected to the last, deleted ones as context
	text        string
}

// commentGroup is the pending comments that go to one session with one model, as one fix
// request carries them.
type commentGroup struct {
	agent, session string
	model          string // picked with ^, "" for the session's own
	comments       []pendingComment
}

// startComment opens the input box of c for the lines of the selection or of the cursor
// row, under the last of them in the current file, or says why they cannot take a comment.
// On a line of a comment, or with a selection of exactly a comment's lines, it edits that
// comment: the box takes the place of the comment's box, holding its text with the cursor at
// the end, and the model it picked.
func (m *model) startComment() {
	in, why := m.commentTarget()
	if in == nil {
		m.warn(why)
		return
	}
	m.drafting, m.entry = in, textBox{}
	if in.editing != nil {
		m.entry = newTextBox(in.editing.Text)
		m.entry.pick = in.editing.Model
	}
}

// commentTarget returns what the input box of c comments on, and who the comment goes to,
// or why there is none. review.Asking.Commenting decides who a new comment goes to. Without
// a selection the cursor row must be a changed line or a line of a comment. A selection that
// overlaps a comment without having exactly its range cannot take one, and a comment that
// waits for its fix cannot be edited.
func (m *model) commentTarget() (*commentInput, string) {
	rows := []int{m.cursor}
	switch {
	case m.selecting:
		rows = m.selectedRows()
	case m.rows[m.cursor].kind != lineRow:
		return nil, "Move to a changed line to comment"
	}
	in := &commentInput{file: m.rows[rows[0]].file, lines: m.rowLines(rows)}
	from, to := m.currentRange(in.lines)
	for _, c := range m.comments {
		if !c.placed() || c.file != in.file {
			continue
		}
		at, end := m.commentRange(c)
		if !slices.ContainsFunc(in.lines, func(k int) bool { return m.covers(c, k) }) && (from == 0 || from > end || to < at) {
			continue
		}
		switch {
		case m.selecting && (from != at || to != end):
			return nil, "The selection overlaps a comment. Edit it with c on its lines"
		case c.request != "":
			return nil, "Wait for the fix to this comment"
		}
		in.editing, in.row = c, m.lineRow(c.under)
		in.who, in.from, in.to = m.commentWho(c.Agent, c.Session, review.Relation(c.Relation)), at, end
		return in, ""
	}
	if !m.selecting && !m.rows[m.cursor].isChange() {
		return nil, "Move to a changed line to comment"
	}
	var lines []origin.Line
	for _, k := range m.commentedLines(in.file, in.lines) {
		lines = append(lines, m.lines[k])
	}
	var why string
	if in.target, why = m.whom.Commenting(lines); why != "" {
		return nil, why
	}
	for _, k := range in.lines {
		if m.lines[k].New > 0 {
			in.row = m.lineRow(k) // Commenting took no deleted lines alone, so one is of the current file
		}
	}
	in.who, in.from, in.to = m.commentWho(in.target.Ask.Agent, in.target.Ask.Session, in.target.Relation), from, to
	return in, ""
}

// rowLines returns the lines of rows in model.lines, in the order of the diff, each once.
func (m *model) rowLines(rows []int) []int {
	var lines []int
	for _, i := range rows {
		for _, k := range [2]int{m.rows[i].left, m.rows[i].right} {
			if k >= 0 {
				lines = append(lines, k)
			}
		}
	}
	slices.Sort(lines)
	return slices.Compact(lines)
}

// commentedLines returns the lines of model.lines that a comment on lines, selected lines of
// file, covers, in the order of the diff: lines, and the changed lines of the current file
// in their range, which a selection of the split view can leave out. Who the comment goes
// to must have written them all.
func (m *model) commentedLines(file int, lines []int) []int {
	from, to := m.currentRange(lines)
	covered := slices.Clone(lines)
	f := m.files[file]
	for k := f.first; k < f.end; k++ {
		if l := m.lines[k]; l.Old == 0 && l.New >= from && l.New <= to {
			covered = append(covered, k)
		}
	}
	slices.Sort(covered)
	return slices.Compact(covered)
}

// commentWho names who a comment goes to, session of agent as relation, as the title of the
// input box of c does.
func (m *model) commentWho(agent, session string, relation review.Relation) string {
	who := review.Visible(review.Who(agent, session, m.short))
	if relation == review.Parent {
		who += ", which started the subagent"
	}
	return who
}

// commentWhere returns the path of file and the range from to to of its current file.
func (m *model) commentWhere(file, from, to int) string {
	return m.files[file].path + ":" + lineRange(from, to)
}

// currentRange returns the range in the current file of lines, lines of model.lines in the
// order of the diff: the first and the last line number of the current file among them, 0
// and 0 when none is of the current file.
func (m *model) currentRange(lines []int) (from, to int) {
	for _, k := range lines {
		if n := m.lines[k].New; n > 0 {
			if from == 0 {
				from = n
			}
			to = n
		}
	}
	return from, to
}

// commentRange returns the range of c, which has a place, in the current file: that of its
// selected lines. Lines its diff lines hold that were not selected, as in the split view,
// are not in it.
func (m *model) commentRange(c *comment) (from, to int) {
	return m.currentRange(c.picked())
}

// covers reports whether line k of model.lines is a line of c, which has a place: one of
// its selected lines, or a line of the current file in its range.
func (m *model) covers(c *comment, k int) bool {
	f := m.files[c.file]
	if k < f.first || k >= f.end {
		return false
	}
	from, to := m.commentRange(c)
	n := m.lines[k].New
	return n > 0 && n >= from && n <= to || slices.Contains(c.picked(), k)
}

// saveComment saves what is typed in the input box of c, as typed but for the space around
// it, with the model picked with ^, and ends the selection: a new comment, or the new text
// or model of the comment edited, which goes when nothing is typed. A new comment with
// nothing typed is not made. Each change is appended to the log. An edit drops the fix under
// the comment, which answered its old text or came from its old model.
func (m *model) saveComment() {
	in := m.drafting
	text, pick := strings.TrimSpace(m.entry.text), m.entry.pick
	m.drafting, m.entry = nil, textBox{}
	c := in.editing
	switch {
	case c != nil && c.Text == text && c.Model == pick:
		return
	case c != nil:
		if c.fix != nil {
			m.dropFix(c)
		}
		c.Time, c.Text, c.Model = m.now(), text, pick
		if text == "" {
			m.comments = slices.DeleteFunc(m.comments, func(o *comment) bool { return o == c })
		}
	case text == "":
		return
	default:
		c = m.newComment(in, text, pick)
		m.comments = append(m.comments, c)
	}
	m.selecting = false
	m.listWidth = m.treeWidth() // for the count by the file's name
	if err := write(m.log, c.Comment); err != nil {
		m.fail("could not save the comment: " + review.Visible(err.Error()))
	}
}

// newComment returns the comment with text and pick, the model picked with ^, that in makes.
// Like a question, it keeps the diff lines from the first selected to the last, which of them
// are selected when a selection of the split view left some out, and two lines of context
// before and after.
func (m *model) newComment(in *commentInput, text, pick string) *comment {
	f := m.files[in.file]
	first, last := in.lines[0], in.lines[len(in.lines)-1]
	var selected []int
	if len(in.lines) < last+1-first {
		for _, k := range in.lines {
			selected = append(selected, k-first)
		}
	}
	c := &comment{
		Comment: state.Comment{
			Time: m.now(), ID: newID(), Agent: in.target.Ask.Agent, Session: in.target.Ask.Session,
			Relation: string(in.target.Relation), Path: f.key,
			Lines:    diffLines(m.lines[first : last+1]),
			Selected: selected,
			Before:   diffLines(m.lines[max(first-2, f.first):first]),
			After:    diffLines(m.lines[last+1 : min(last+3, f.end)]),
			Text:     text,
			Model:    pick,
		},
		n:    m.nextComment(),
		file: in.file,
	}
	m.setPlace(c, first)
	return c
}

// setPlace puts c on its diff lines from line first of model.lines on, its box under the
// row of the last line of the current file among its selected lines. It has no place when
// none is.
func (m *model) setPlace(c *comment, first int) {
	c.first, c.last, c.under = first, first+len(c.Lines)-1, -1
	for _, k := range c.picked() {
		if m.lines[k].New > 0 {
			c.under = k
		}
	}
	if c.under < 0 {
		c.first, c.last = -1, -1
	}
}

// nextComment returns the place of a new comment in the order the comments were made: after
// every comment, on a file of the diff or not, or resolved by a fix.
func (m *model) nextComment() int {
	n := 0
	for _, c := range slices.Concat(m.comments, m.away, m.resolved) {
		n = max(n, c.n+1)
	}
	return n
}

// loggedComments returns the comments of earlier runs in records, each as its newest record
// has it, without the removed ones. placeComments puts them in the diff.
func loggedComments(records state.Records) []*comment {
	current := records.CurrentComments()
	cs := make([]*comment, len(current))
	for i, r := range current {
		cs[i] = &comment{Comment: r, n: i, file: -1, first: -1, last: -1, under: -1}
	}
	return cs
}

// placeComments puts the comments of this run and earlier ones in the diff, when the screen
// opens and when it rebuilds the diff. A comment goes on its lines when state.Place finds
// them in its file's diff now, and else it has no place and shows dim in the origin box of
// its file's header, no longer pending. A comment on a file that is not in the diff is left
// out of model.comments and kept in model.away, for a rebuild that brings the file back.
func (m *model) placeComments() {
	files := make(map[string]int, len(m.files))
	for i, f := range m.files {
		files[f.key] = i
	}
	all := slices.Concat(m.comments, m.away)
	slices.SortFunc(all, func(a, b *comment) int { return a.n - b.n })
	m.comments, m.away = nil, nil
	diffs := make(map[int][]state.Line) // the lines of each file, as the log keeps them
	for _, c := range all {
		c.file, c.first, c.last, c.under = -1, -1, -1, -1
		i, ok := files[c.Path]
		if !ok {
			m.away = append(m.away, c)
			continue
		}
		f := m.files[i]
		if diffs[i] == nil {
			diffs[i] = diffLines(m.lines[f.first:f.end])
		}
		c.file = i
		if at, ok := state.Place(diffs[i], c.Before, c.Lines, c.After); ok {
			m.setPlace(c, f.first+at)
		}
		m.comments = append(m.comments, c)
	}
}

// commentCount returns how many comments the file at key, as review gives its path, has in
// its diff: those with a place, pending or not.
func (m *model) commentCount(key string) int {
	n := 0
	for _, c := range m.comments {
		if c.placed() && c.Path == key {
			n++
		}
	}
	return n
}

// commentSpans returns the count of the comments of the file at key after
// commentMark, nil when it has none.
func (m *model) commentSpans(key string) []span {
	n := m.commentCount(key)
	if n == 0 {
		return nil
	}
	return []span{{text: commentMark + strconv.Itoa(n), color: m.theme.pending}}
}

// commentsAt returns the comments whose boxes show under row i, oldest first, and
// none when there is no row i, as on the screen that says no changed file is left. The
// comment the input box of c edits is left out, as the input box takes its place.
func (m *model) commentsAt(i int) []*comment {
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	r := m.rows[i]
	if r.kind != lineRow || r.right < 0 {
		return nil
	}
	var cs []*comment
	for _, c := range m.comments {
		if c.under == r.right && (m.drafting == nil || m.drafting.editing != c) {
			cs = append(cs, c)
		}
	}
	return cs
}

// commentHeight returns how many lines of the screen the boxes of the comments under row i
// take.
func (m *model) commentHeight(i int) int {
	if len(m.comments) == 0 {
		return 0
	}
	_, w := m.layout()
	return len(m.commentLines(i, w))
}

// commentLines draws the boxes of the comments under row i, in a diff w cells wide, the way
// GitHub shows a pending review comment under its lines, and nil when it has none. They are
// only drawn, and moving through the rows does not see them. Each box hangs from the code
// column of the row's file, as the origin box does, and has a top border with where its
// lines are, a thread header with who the comment goes to and its state, the comment
// wrapped to the box, and the fix that came for it under that.
func (m *model) commentLines(i, w int) []string {
	cs := m.commentsAt(i)
	if len(cs) == 0 {
		return nil
	}
	t := &m.theme
	x := min(m.codeColumn(m.files[m.rows[i].file]), w/3)
	width := max(min(w-x-3, maxReadWidth), 1)
	pad := strings.Repeat(" ", x)
	base, border := t.paint(nil, t.surface), t.paint(t.subtle, t.surface)
	var lines []string
	for _, c := range cs {
		top := newLine(w - x)
		top.add("╭─ ", border)
		from, to := m.commentRange(c)
		top.add(m.commentWhere(c.file, from, to), t.paint(t.muted, t.surface))
		top.add(" ", border)
		top.add(strings.Repeat("─", top.free), border)
		lines = append(lines, pad+top.fill(border))
		for _, p := range slices.Concat(m.commentText(c), m.fixOverlay(c)) {
			for _, s := range m.paragraphLines(p, width) {
				l := newLine(w - x)
				l.add("│ ", border)
				l.add(s, lipgloss.NewStyle())
				lines = append(lines, pad+l.fill(base))
			}
		}
		lines = append(lines, pad+border.Render("╰"+strings.Repeat("─", max(w-x-1, 0))))
	}
	return lines
}

// commentText tells c as its box shows it: a header with who it goes to, the model it picked
// with ^ when it picked one, and a badge of its state, how that session relates to the lines
// when it did not write them, and the text. The badge says pending, or how long its fix
// request has waited, or nothing once a fix came, which shows under it.
func (m *model) commentText(c *comment) []paragraph {
	who := paragraph{
		text: "To " + review.Visible(review.Who(c.Agent, c.Session, m.short)), tone: strong, bar: changeBar, head: true,
	}
	if c.Model != "" {
		who.text += " · ^" + review.Visible(c.Model)
	}
	switch {
	case c.request != "":
		waited := max(m.now().Sub(c.asked), 0) / time.Second
		who.badge = part{text: "asking… " + strconv.Itoa(int(waited)) + "s", tone: quiet}
	case c.fix == nil || c.fix.Error != "":
		who.badge = part{text: "pending", tone: unsent}
	}
	paragraphs := []paragraph{who}
	if r := relationText(review.Relation(c.Relation)); r != "" {
		paragraphs = append(paragraphs, paragraph{text: r, tone: quiet, bar: changeBar})
	}
	return append(paragraphs, paragraph{text: review.Visible(c.Text), bar: changeBar})
}

// earlierComments tells the comments on the file at key that have no place in its diff
// now, under Earlier comments and their count: each with the lines it was left on quoted,
// then as in its box, all of it dim. They are not pending.
func (m *model) earlierComments(key string) []paragraph {
	var cs []*comment
	for _, c := range m.comments {
		if !c.placed() && c.Path == key {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return nil
	}
	paragraphs := []paragraph{{}, {text: "Earlier comments · " + strconv.Itoa(len(cs)), tone: heading}}
	for i, c := range cs {
		if i > 0 {
			paragraphs = append(paragraphs, paragraph{})
		}
		for _, k := range c.Picked() {
			l := c.Lines[k]
			mark := "> "
			if l.Kind == "-" {
				mark = "- "
			}
			paragraphs = append(paragraphs, paragraph{text: mark + display(l.Text), tone: quiet})
		}
		for _, p := range m.commentText(c) {
			p.bar, p.tone, p.badge = dimBar, quiet, part{text: "no place in the diff", tone: quiet}
			paragraphs = append(paragraphs, p)
		}
	}
	return paragraphs
}

// pendingComments returns the pending comments grouped by the session they go to and the
// model they picked with ^, the groups in the order of their first comment and the comments
// of a group in the order they were made. Each has its range, and the lines of the range in
// the current file, read from the file's whole diff, as the lines between two hunks are not
// in model.lines.
func (m *model) pendingComments() []commentGroup {
	var groups []commentGroup
	for _, c := range m.comments {
		if !c.pending() {
			continue
		}
		from, to := m.commentRange(c)
		p := pendingComment{
			comment: c, id: c.ID, relation: review.Relation(c.Relation), path: c.Path, first: from, last: to,
			diff: slices.Clone(m.lines[c.first : c.last+1]), text: c.Text,
		}
		for _, l := range m.files[c.file].diff {
			if l.New >= from && l.New <= to {
				p.current = append(p.current, l.Text)
			}
		}
		g := slices.IndexFunc(groups, func(g commentGroup) bool {
			return g.agent == c.Agent && g.session == c.Session && g.model == c.Model
		})
		if g < 0 {
			g = len(groups)
			groups = append(groups, commentGroup{agent: c.Agent, session: c.Session, model: c.Model})
		}
		groups[g].comments = append(groups[g].comments, p)
	}
	return groups
}
