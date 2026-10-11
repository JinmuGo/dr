package tui

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/diff"
	"github.com/JinmuGo/dr/internal/fix"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"
)

// What the screen says of the lock on the worktree when another dr holds it: in the band
// over the diff, and when y or :undo refuse to write a file.
const (
	lockedBand   = "Another dr has this worktree open. Read, ask and comment here; apply fixes there."
	lockedRefuse = "Another dr has this worktree open. Apply fixes there"
)

// lockBand returns what the band over the diff says while this dr does not hold the lock
// on the worktree, which err tells why it could not take.
func lockBand(err error) string {
	if errors.Is(err, state.ErrLocked) {
		return lockedBand
	}
	return lockFailed(err)
}

// lockRefuse returns why y and :undo write no file without the lock on the worktree, which
// err tells why it could not take.
func lockRefuse(err error) string {
	if errors.Is(err, state.ErrLocked) {
		return lockedRefuse
	}
	return lockFailed(err)
}

// lockFailed says that the lock on the worktree could not be taken, for err, a reason other
// than another dr holding it.
func lockFailed(err error) string {
	return "Could not lock the worktree: " + review.Visible(err.Error()) + "; fixes cannot be applied"
}

// request is a fix request in flight: the pending comments :submit sent to a session.
type request struct {
	state.Request
	who  session
	base time.Time      // the time of the newest edit in the review that the session knows of
	ask  agent.Question // as sent, with the fork to go on in and the prompt
	sent []sentComment  // in the order they are numbered
}

// sentComment is a comment as a fix request sent it.
type sentComment struct {
	c    *comment
	fix  fix.Comment
	hash string // sha256 hex of the whole file as sent
}

// fixedMsg brings the answer to the fix request with ID request, or why there is none.
type fixedMsg struct {
	request string
	answer  agent.Answer
	err     error
}

// submit sends the pending comments, one fix request to each session they go to, as
// pendingComments groups them, and returns the commands that ask. A session takes one
// request at a time, so of a session's groups, which picked different models, the group of
// its first comment goes, or the next when none of that group's comments can go, and the
// others are held back until the next :submit. A session answering a question or another
// request is skipped, and a comment that no longer goes to its session, or whose lines are
// not at its range in the file any more, is not sent. A notice tells how many requests went
// to whom, and what was left out and why.
func (m *model) submit() tea.Cmd {
	groups := m.pendingComments()
	if len(groups) == 0 {
		m.warn("No comments to submit")
		return nil
	}
	var cmds []tea.Cmd
	var left, to []string
	sent := 0                         // the comments sent
	asked := make(map[session]bool)   // the sessions a request went to
	skipped := make(map[session]bool) // the sessions skipped as busy
	for _, g := range groups {
		who := session{g.agent, g.session}
		name := review.Visible(review.Who(g.agent, g.session, m.short))
		switch {
		case asked[who]:
			left = append(left, "Held back "+countOf(len(g.comments), "comment")+" for "+name+" with "+withModel(g.model)+
				"; submit again when its fixes come")
		case skipped[who]:
		case m.busy[who]:
			left = append(left, "Skipped "+name+", still answering")
			skipped[who] = true
		default:
			r, why := m.newRequest(who, g.model, g.comments)
			left = append(left, why...)
			if r != nil {
				cmds = append(cmds, m.requestCmd(r))
				asked[who] = true
				to = append(to, name)
				sent += len(r.sent)
			}
		}
	}
	t := &m.theme
	n := &notice{tone: toneOK}
	switch {
	case len(cmds) > 0:
		n.text = []chunk{
			{text: "Sent " + countOf(len(cmds), "fix request"), color: t.fg},
			{text: " to " + strings.Join(to, ", ") + " (" + countOf(sent, "comment") + ")", color: t.muted},
		}
		n.warnings, n.hint = left, m.quiet("The fixes come under the comments.")
	default:
		n.tone, n.text, n.warnings = toneWarn, []chunk{{text: left[0], color: t.fg}}, left[1:]
	}
	m.notice = n
	return tea.Batch(cmds...)
}

// withModel names the model a group of comments picked with ^, as the notice of :submit
// tells it: "^sonnet", or "its own model" for none.
func withModel(pick string) string {
	if pick == "" {
		return "its own model"
	}
	return "^" + review.Visible(pick)
}

// newRequest makes the fix request for comments, the pending comments that go to who with
// pick, the model they picked with ^, and writes it to the log. It returns nil when none of
// them can go, and says why each that cannot go does not. The comments are numbered from 1
// in the order of the diff, and the request goes the way asking does: to the session's fork, with
// what the target of the comments' lines, the newest of them, says of the session. A picked
// model goes in place of the session's, with the CLI's default reasoning effort. Each
// comment it sends waits for its fix.
func (m *model) newRequest(who session, pick string, comments []pendingComment) (*request, []string) {
	comments = slices.Clone(comments)
	slices.SortStableFunc(comments, func(a, b pendingComment) int {
		return cmp.Or(a.comment.file-b.comment.file, a.first-b.first)
	})
	var left []string
	var sent []sentComment
	var target review.Target
	for _, p := range comments {
		t, s, why := m.check(who, p)
		if why != "" {
			left = append(left, review.Visible(p.path)+":"+lineRange(p.first, p.last)+" not sent: "+why)
			continue
		}
		s.fix.N = len(sent) + 1
		sent = append(sent, s)
		if target.Edit == nil || t.Edit.Time.After(target.Edit.Time) {
			target = t
		}
	}
	if len(sent) == 0 {
		return nil, left
	}
	now := m.now()
	r := &request{
		Request: state.Request{Time: now, ID: newID(), Agent: who.agent, Session: who.id, Relation: string(target.Relation), Model: pick},
		who:     who, base: target.Base, sent: sent, ask: target.Ask,
	}
	if pick != "" {
		r.ask.Model, r.ask.Effort = pick, ""
	}
	asked := make([]fix.Comment, len(sent))
	for i, s := range sent {
		r.Comments = append(r.Comments, s.c.ID)
		asked[i] = s.fix
	}
	r.ask.Fork, r.ask.Prompt = m.fork(who, target.Base), fix.Prompt(asked, m.earlierFixes(who))
	if err := write(m.log, r.Request); err != nil {
		left = append(left, "Could not save the request: "+review.Visible(err.Error()))
	}
	m.records.Requests = append(m.records.Requests, r.Request)
	for _, s := range sent {
		s.c.request, s.c.asked, s.c.fix = r.ID, now, nil
	}
	m.busy[who] = true
	m.requests[r.ID] = r
	return r, left
}

// check returns who p, a pending comment for who, goes to now, and p as a fix request sends
// it, with the lines of its range and the sha256 of its file as fix.Read reads them. Or it
// returns why p cannot go: its lines no longer go to who, or the file cannot be read, or
// the lines at its range are not those of the diff any more.
func (m *model) check(who session, p pendingComment) (review.Target, sentComment, string) {
	var covered []origin.Line
	for _, k := range m.commentedLines(p.comment.file, p.comment.picked()) {
		covered = append(covered, m.lines[k])
	}
	t, why := m.whom.Commenting(covered)
	if why != "" {
		return t, sentComment{}, why
	}
	if t.Ask.Agent != who.agent || t.Ask.Session != who.id {
		return t, sentComment{}, "its lines now come from " + review.Visible(review.Who(t.Ask.Agent, t.Ask.Session, m.short))
	}
	data, hash, err := fix.Read(m.asker.Root, p.path)
	if err != nil {
		return t, sentComment{}, review.Visible(err.Error())
	}
	file := diff.Lines(string(data))
	if p.last > len(file) || !slices.Equal(file[p.first-1:p.last], p.current) {
		return t, sentComment{}, "its lines changed on disk"
	}
	return t, sentComment{c: p.comment, hash: hash, fix: fix.Comment{
		Path: p.path, First: p.first, Lines: slices.Clone(file[p.first-1 : p.last]), Deleted: m.deletedContext(p), Text: p.text,
	}}, ""
}

// deletedContext returns the deleted lines selected for p, which a fix request sends as
// context, each before the line of p's range that follows it in the diff.
func (m *model) deletedContext(p pendingComment) []fix.Deleted {
	c, n := p.comment, p.last-p.first+1
	end := m.files[c.file].end
	var deleted []fix.Deleted
	for _, k := range c.picked() {
		if m.lines[k].New > 0 {
			continue
		}
		before := n
		for j := k + 1; j < end; j++ {
			if next := m.lines[j].New; next > 0 {
				before = min(max(next-p.first, 0), n)
				break
			}
		}
		deleted = append(deleted, fix.Deleted{Before: before, Text: m.lines[k].Text})
	}
	return deleted
}

// earlierFixes returns the fixes who sent for earlier requests that the person applied or
// dropped, for the next request to tell it.
func (m *model) earlierFixes(who session) []fix.Earlier {
	var earlier []fix.Earlier
	for _, t := range m.records.Taken(who.agent, who.id) {
		earlier = append(earlier, fix.Earlier{Path: t.Path, First: t.First, Last: t.First + len(t.Lines) - 1, Text: t.Text, Applied: t.Applied})
	}
	return earlier
}

// requestCmd returns the command that sends r and brings back the answer. When the screen
// quits first, the command stops, and r stays in the log without fixes.
func (m *model) requestCmd(r *request) tea.Cmd {
	ask, flights, a, id := m.asker.Ask, m.flights, r.ask, r.ID
	return func() tea.Msg {
		ctx, ok := flights.start(id)
		if !ok {
			return nil // the screen quit before the request went
		}
		defer flights.end(id)
		answer, err := ask(ctx, a)
		if ctx.Err() != nil {
			return nil
		}
		return fixedMsg{request: id, answer: answer, err: err}
	}
}

// fixed takes the answer to a fix request and writes a Fix record for each comment it sent:
// the new lines and the note fix.Parse reads for it, or a note that keeps the lines as they
// are, or why no fix came, the same for every comment when the session did not answer or
// its answer holds no fixes. A comment without a fix is pending again. The fork that
// answered goes on for the session's next questions and requests, as after asking. When the
// cursor is not on a comment of the request, a notice tells of the fixes until the next
// key, and Enter then goes to them. When the log cannot save them, an error notice says so,
// and which fixes came when the cursor is not on them.
func (m *model) fixed(msg fixedMsg) {
	r := m.requests[msg.request]
	if r == nil {
		return
	}
	delete(m.requests, msg.request)
	delete(m.busy, r.who)
	asked := make([]fix.Comment, len(r.sent))
	for i, s := range r.sent {
		asked[i] = s.fix
	}
	var results []fix.Result
	why := "" // why no comment got a fix
	if msg.err != nil {
		why = msg.err.Error()
	} else if parsed, err := fix.Parse(msg.answer.Text, asked); err != nil {
		why = err.Error()
	} else {
		results = parsed
	}
	var logErr error
	if msg.err == nil && msg.answer.Fork != "" {
		m.forks[r.who] = runFork{id: msg.answer.Fork, base: r.base}
		if msg.answer.Fork != r.ask.Fork {
			logErr = write(m.log, state.Fork{Time: m.now(), Agent: r.Agent, Session: r.Session, Fork: msg.answer.Fork, Base: r.base})
		}
	}
	fixes := 0
	for i, s := range r.sent {
		f := state.Fix{
			Time: m.now(), ID: newID(), Request: r.ID, Comment: s.c.ID, Text: s.fix.Text,
			Agent: r.ask.Agent, Session: r.ask.Session, Log: r.ask.Log, Cwd: r.ask.Cwd, Effort: r.ask.Effort,
			Path: s.fix.Path, First: s.fix.First, Lines: s.fix.Lines, Hash: s.hash,
		}
		if msg.err == nil {
			f.Fork, f.Model, f.TookMS = msg.answer.Fork, msg.answer.Model, msg.answer.Took.Milliseconds()
		}
		switch {
		case why != "":
			f.Error = why
		case results[i].Problem != "":
			f.Error = results[i].Problem
		case results[i].Fixed:
			f.New, f.Note = results[i].Lines, results[i].Note
			fixes++
		default:
			f.Keep, f.Note = true, results[i].Note
			fixes++
		}
		logErr = errors.Join(logErr, write(m.log, f))
		m.records.Fixes = append(m.records.Fixes, f)
		s.c.request, s.c.fix = "", &f
	}
	var n *notice
	if on := m.commentOn(m.cursor); on == nil || !slices.ContainsFunc(r.sent, func(s sentComment) bool { return s.c == on }) {
		n = m.fixesNotice(r, fixes) // else the cursor is on a comment of the request, whose box shows what came
	}
	if logErr != nil {
		failed := &notice{
			tone: toneErr,
			text: []chunk{{text: "Could not save the fixes: ", color: m.theme.fg}, {text: review.Visible(logErr.Error()), color: m.theme.del}},
			hint: m.quiet("The fixes show now; saving them failed."),
		}
		if n != nil {
			failed.fixes = n.fixes
			failed.hint = append(slices.Clone(n.text), m.quiet(": shown now; saving them failed.")...)
			if n.fixes != nil {
				failed.hint = append(append(failed.hint, chunk{text: keyGap}), m.readHint()...)
			}
		}
		n = failed
	}
	if n != nil {
		m.notice = n
	}
}

// fixesNotice returns the notice that fixes, how many, came for the comments of r, or that
// none did, from whom and where. When a comment of r has a place in the diff, Enter goes to
// it.
func (m *model) fixesNotice(r *request, fixes int) *notice {
	t := &m.theme
	first := r.sent[0].fix
	what := "Fix from "
	if len(r.sent) > 1 {
		what = "Fixes from "
	}
	n := &notice{tone: toneOK, hint: m.keys([2]string{keyEsc, "dismiss"})}
	if fixes == 0 {
		n.tone, what = toneWarn, "No "+strings.ToLower(what[:1])+what[1:]
	}
	n.text = []chunk{
		{text: what, color: t.fg},
		{text: review.Visible(review.Who(r.Agent, r.Session, m.short)), color: m.whoColor(r.Agent, r.Session), bold: true},
		{text: " on ", color: t.fg},
		{text: review.Visible(first.Path) + ":" + lineRange(first.First, first.First+len(first.Lines)-1), color: t.strong},
	}
	if more := len(r.sent) - 1; more > 0 {
		n.text = append(n.text, chunk{text: " and " + strconv.Itoa(more) + " more", color: t.fg})
	}
	if i := slices.IndexFunc(r.sent, func(s sentComment) bool { return s.c.placed() && slices.Contains(m.comments, s.c) }); i >= 0 {
		n.fixes, n.hint = r.sent[i].c, m.readHint()
	}
	return n
}

// readFix goes to c, the comment a notice told of the fixes for, so that its box shows under
// the cursor row. A file viewed since unfolds. It returns the command that saves the viewed
// files then.
func (m *model) readFix(c *comment) tea.Cmd {
	if !c.placed() {
		return nil
	}
	var cmd tea.Cmd
	if m.files[c.file].viewed {
		cmd = m.toggleViewed(c.file)
	}
	m.selecting = false
	m.jumpTo(m.lineRow(c.under))
	return cmd
}

// commentOn returns the comment row i is a line of, the one with a fix under it when two
// are, and nil when none is.
func (m *model) commentOn(i int) *comment {
	if i < 0 || i >= len(m.rows) || m.rows[i].kind != lineRow {
		return nil
	}
	r := m.rows[i]
	var on *comment
	for _, c := range m.comments {
		if c.placed() && (r.left >= 0 && m.covers(c, r.left) || r.right >= 0 && m.covers(c, r.right)) {
			if c.fix != nil {
				return c
			}
			on = cmp.Or(on, c)
		}
	}
	return on
}

// apply applies the fix under the comment the cursor row is a line of, with fix.Apply, from
// the file as the request sent it through the fixes dr applied to it since. It writes the
// Applied record, resolves the comment, which is then neither drawn nor pending, and returns
// the command that rebuilds the diff, where the new lines have the fix as their origin. A
// fix that keeps the lines, or a reason no fix came, has nothing to apply. Without the lock
// on the worktree, it refuses. A notice says what it did, or why it could not.
func (m *model) apply() tea.Cmd {
	c := m.commentOn(m.cursor)
	if c == nil || c.fix == nil {
		m.warn("No fix on this line")
		return nil
	}
	f := c.fix
	switch {
	case f.Error != "":
		m.warn("Nothing to apply: no fix came for this comment")
		return nil
	case f.Keep:
		m.warn("Nothing to apply: the session left the lines as they are")
		return nil
	case m.lockErr != nil:
		m.warn(lockRefuse(m.lockErr))
		return nil
	}
	change, err := fix.Apply(m.asker.Root, fix.Comment{Path: f.Path, First: f.First, Lines: f.Lines}, f.Hash, f.New, m.since(f))
	if err != nil {
		m.notice = m.failed("Didn't apply: ", err)
		return nil
	}
	a := state.Applied{
		Time: m.now(), ID: newID(), Comment: f.Comment, Text: f.Text, Note: f.Note,
		Agent: f.Agent, Session: f.Session, Log: f.Log, Cwd: f.Cwd, Fork: f.Fork, Model: f.Model, Effort: f.Effort,
		Path: change.Path, First: change.First, Old: change.Old, New: change.New, Before: change.Before, After: change.After,
	}
	m.records.Applied = append(m.records.Applied, a)
	m.comments = slices.DeleteFunc(m.comments, func(o *comment) bool { return o == c })
	c.fix = nil
	m.resolved = append(m.resolved, c)
	m.listWidth = m.treeWidth()
	done := m.did("Applied the fix for ", review.Visible(a.Path)+":"+lineSpan(a.First, len(a.Old)), []chunk{
		{text: ":undo", color: m.theme.strong, bold: true}, {text: " reverts it", color: m.theme.muted},
	})
	return m.reloadCmd(done, write(m.log, a))
}

// since returns the changes dr applied to the file of f after f's request and that are
// still in place, oldest first, which fix.Apply goes through from the file as sent.
func (m *model) since(f *state.Fix) []fix.Change {
	asked := f.Time
	if r, ok := m.records.Request(f.Request); ok {
		asked = r.Time
	}
	var since []fix.Change
	for _, a := range m.records.InPlace() {
		if a.Path == f.Path && a.Time.After(asked) {
			since = append(since, change(a))
		}
	}
	return since
}

// change returns what a, a fix dr applied, changed.
func change(a state.Applied) fix.Change {
	return fix.Change{Path: a.Path, First: a.First, Old: a.Old, New: a.New, Before: a.Before, After: a.After}
}

// drop drops the fix under the comment the cursor row is a line of. The comment stays,
// pending again, to be edited and submitted again, as a notice says.
func (m *model) drop() {
	c := m.commentOn(m.cursor)
	if c == nil || c.fix == nil {
		m.warn("No fix on this line")
		return
	}
	from, to := m.commentRange(c)
	if m.dropFix(c) {
		m.notice = m.did("Dropped the fix for ", m.commentWhere(c.file, from, to), m.quiet("The comment is pending again."))
	}
}

// dropFix takes the fix under c away and writes a Dropped record for it. It reports whether
// the record was saved; a notice says why not.
func (m *model) dropFix(c *comment) bool {
	d := state.Dropped{Time: m.now(), Fix: c.fix.ID}
	m.records.Dropped = append(m.records.Dropped, d)
	c.fix = nil
	if err := write(m.log, d); err != nil {
		m.notice = &notice{
			tone: toneErr,
			text: []chunk{{text: "Could not save the drop: ", color: m.theme.fg}, {text: review.Visible(err.Error()), color: m.theme.del}},
			hint: m.quiet("The fix is dropped on screen; saving it failed, so the next dr shows it again."),
		}
		return false
	}
	return true
}

// undo reverts the newest fix dr applied that no :undo reverted, in this run or an earlier
// one, with fix.Undo, writes the Undone record and returns the command that rebuilds the
// diff. The comment the fix resolved comes back, pending, on the lines the undo restored.
// Without the lock on the worktree, it refuses. A notice says what it did, or why it could
// not.
func (m *model) undo() tea.Cmd {
	if m.lockErr != nil {
		m.warn(lockRefuse(m.lockErr))
		return nil
	}
	inPlace := m.records.InPlace()
	if len(inPlace) == 0 {
		m.warn("No fix to undo")
		return nil
	}
	a := inPlace[len(inPlace)-1]
	if err := fix.Undo(m.asker.Root, change(a)); err != nil {
		m.notice = m.failed("Didn't revert: ", err)
		return nil
	}
	u := state.Undone{Time: m.now(), Applied: a.ID}
	m.records.Undone = append(m.records.Undone, u)
	var hint []chunk
	if i := slices.IndexFunc(m.resolved, func(c *comment) bool { return c.ID == a.Comment }); i >= 0 {
		m.away = append(m.away, m.resolved[i])
		m.resolved = slices.Delete(m.resolved, i, i+1)
		m.placeComments()
		m.listWidth = m.treeWidth()
		hint = m.quiet("The comment is pending again.")
	}
	done := m.did("Reverted the fix for ", review.Visible(a.Path)+":"+lineSpan(a.First, len(a.Old)), hint)
	return m.reloadCmd(done, write(m.log, u))
}

// did returns the notice of something done: what, then where in the strong color, and hint
// under them.
func (m *model) did(what, where string, hint []chunk) *notice {
	return &notice{tone: toneOK, text: []chunk{{text: what, color: m.theme.fg}, {text: where, color: m.theme.strong}}, hint: hint}
}

// failed returns the notice of a fix that y or :undo could not write, for err: what, then
// err in the error color, and that no file changed.
func (m *model) failed(what string, err error) *notice {
	return &notice{
		tone: toneErr, text: []chunk{{text: what, color: m.theme.fg}, {text: review.Visible(err.Error()), color: m.theme.del}},
		hint: m.quiet("Nothing was written."),
	}
}

// lineSpan returns n lines from first as lineRange writes them, or first alone when n is 0.
func lineSpan(first, n int) string {
	return lineRange(first, first+max(n, 1)-1)
}

// reloadCmd returns the command that reads the changed files again, for reloaded to rebuild
// the diff with and then show done, with logErr, why its record was not saved, among its
// warnings. With no way to read them, the diff stays, and done shows now.
func (m *model) reloadCmd(done *notice, logErr error) tea.Cmd {
	var warning string
	if logErr != nil {
		warning = "could not save the record: " + review.Visible(logErr.Error())
	}
	if m.reload == nil {
		done.warn(warning)
		m.notice = done
		return nil
	}
	return m.readAgain(reloadedMsg{done: done, warning: warning})
}

// reopenFixes sets the comments of earlier runs as the log left them: those a fix dr applied
// resolved go aside, for :undo to bring back, and the others get the fix that shows under
// them, as state.Records.Shown finds it.
func (m *model) reopenFixes() {
	var open []*comment
	for _, c := range m.comments {
		if m.records.Resolved(c.ID) {
			m.resolved = append(m.resolved, c)
			continue
		}
		if f, ok := m.records.Shown(c.Comment); ok {
			c.fix = &f
		}
		open = append(open, c)
	}
	m.comments = open
}

// fixedRows tells what came for c in its card, wrapped to width, under the comment the way an
// answer comes under a question, after an empty row. A fix has a header, ◆ fix from who sent
// it, and at the right edge the model that sent it and how long it took, as answers have;
// then the change as a small diff, the comment's lines as sent deleted and the new lines
// added, and the session's note. A fix that keeps the lines has who sent it over the note,
// and when no fix came, who was asked over why. It is nil when nothing came.
func (m *model) fixedRows(c *comment, width int) []cardRow {
	f := c.fix
	if f == nil {
		return nil
	}
	t := &m.theme
	col := m.whoColor(f.Agent, f.Session)
	who := review.Visible(review.Who(f.Agent, f.Session, m.short))
	from := cardRow{left: []chunk{{text: "● ", color: col}, {text: who, color: col, bold: true}}}
	switch {
	case f.Error != "":
		return slices.Concat([]cardRow{{}, from}, wrapped(review.Visible(f.Error), chunk{color: t.del}, width))
	case f.Keep:
		from.right = []chunk{m.fixedBy(f)}
		rows := []cardRow{{}, from}
		if note := strings.TrimSpace(f.Note); note != "" {
			rows = append(rows, wrapped(review.Visible(note), chunk{color: t.fg}, width)...)
		}
		return rows
	}
	head := cardRow{
		left: []chunk{
			{text: "◆ ", color: t.fix}, {text: "fix", color: t.fix, bold: true}, {text: "  from ", color: t.subtle}, {text: who, color: col},
		},
		right: []chunk{m.fixedBy(f)},
	}
	rows := append([]cardRow{{}, head}, m.miniDiff(f, width)...)
	if note := strings.TrimSpace(f.Note); note != "" {
		rows = append(rows, m.noted(review.Visible(note), chunk{color: t.muted, italic: true}, width)...)
	}
	return rows
}

// miniDiff returns the change f makes as the rows of a small diff, width cells wide: the
// comment's lines as sent, deleted, then the new lines, added, syntax colored by the lexer
// for f's file and with the words that differ marked, a deleted line against the added line
// at its place. A line too long for a row goes on in the rows under it, so no change is cut
// off.
func (m *model) miniDiff(f *state.Fix, width int) []cardRow {
	trim := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = strings.TrimSuffix(l, "\r")
		}
		return out
	}
	old, cur := fixPieces(f.Path, trim(f.Lines)), fixPieces(f.Path, trim(f.New))
	for i := range min(len(old), len(cur)) {
		old[i], cur[i] = markPair(old[i], cur[i])
	}
	code := max(width-diffSignWidth, 1)
	var rows []cardRow
	add := func(sign byte, lines [][]piece) {
		for _, p := range lines {
			for k, part := range splitPieces(p, code) {
				rows = append(rows, cardRow{sign: sign, code: part, more: k > 0})
			}
		}
	}
	add('-', old)
	add('+', cur)
	return rows
}

// splitPieces returns pieces cut into rows of at most width cells each, a wide character
// not split, each piece keeping its token type and its mark. A line without text is one
// empty row.
func splitPieces(pieces []piece, width int) [][]piece {
	var rows [][]piece
	var row []piece
	used := 0
	for _, p := range pieces {
		for p.text != "" {
			cut := ansi.Truncate(p.text, width-used, "")
			if cut == "" && used == 0 { // a character wider than the row
				_, size := utf8.DecodeRuneInString(p.text)
				cut = p.text[:size]
			}
			if cut != "" {
				row = append(row, piece{text: cut, kind: p.kind, changed: p.changed})
				used += ansi.StringWidth(cut)
				p.text = p.text[len(cut):]
			}
			if p.text != "" {
				rows, row, used = append(rows, row), nil, 0
			}
		}
	}
	return append(rows, row)
}

// fixPieces returns lines, lines of the file at path, as the pieces of each, syntax colored
// by the lexer for the file name when there is one.
func fixPieces(path string, lines []string) [][]piece {
	var tokens [][]chroma.Token
	if lexer := lexers.Match(path); lexer != nil && len(lines) > 0 {
		tokens = tokenize(chroma.Coalesce(lexer), strings.Join(lines, "\n")+"\n")
	}
	pieces := make([][]piece, len(lines))
	for i, l := range lines {
		pieces[i] = toPieces(lineTokens(tokens, i+1), l)
	}
	return pieces
}

// fixedBy returns the note on f at the right edge of its header, as answeredBy does for an
// answer: against the model of the session's newest edit, or as pickedNote writes it when
// f's request went with a model picked with ^.
func (m *model) fixedBy(f *state.Fix) chunk {
	took := time.Duration(f.TookMS) * time.Millisecond
	if r, ok := m.records.Request(f.Request); ok && r.Model != "" {
		return m.pickedNote(f.Model, r.Model, took)
	}
	own := ""
	if e := m.newest[session{f.Agent, f.Session}]; e != nil {
		own = modelName(e.Model)
	}
	return m.modelNote(modelName(f.Model), own, took)
}
