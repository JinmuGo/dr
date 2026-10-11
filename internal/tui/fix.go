package tui

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/diff"
	"github.com/JinmuGo/dr/internal/fix"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// What the screen says of the lock on the worktree when another dr holds it: when it opens,
// and when y or :undo refuse to write a file.
const (
	lockedOpen   = "Another dr has this worktree open; fixes can be applied only there"
	lockedRefuse = "Another dr has this worktree open. Apply fixes there"
)

// lockOpen returns what the status line says when the screen opens without the lock on the
// worktree, which err tells why it could not take.
func lockOpen(err error) string {
	if errors.Is(err, state.ErrLocked) {
		return lockedOpen
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

// typeCommand handles a key while the : input is open: Enter runs the command typed, Esc
// closes the input, and other keys edit it. It returns the command the dr command starts.
func (m *model) typeCommand(k, text string) tea.Cmd {
	switch k {
	case "enter":
		command := strings.TrimSpace(m.input)
		m.commanding, m.input = false, ""
		return m.runCommand(command)
	case "esc":
		m.commanding, m.input = false, ""
	default:
		m.edit(k, text)
	}
	return nil
}

// runCommand runs a dr command typed in the : input, submit or undo, and returns the
// command it starts. Nothing typed does nothing.
func (m *model) runCommand(command string) tea.Cmd {
	switch command {
	case "":
		return nil
	case "submit":
		return m.submit()
	case "undo":
		return m.undo()
	}
	m.warn("Not a dr command: " + review.Visible(command))
	return nil
}

// submit sends the pending comments, one fix request to each session they go to, as
// pendingComments groups them, and returns the commands that ask. A session takes one
// request at a time, so of a session's groups, which picked different models, the group of
// its first comment goes, or the next when none of that group's comments can go, and the
// others are held back until the next :submit. A session answering a question or another
// request is skipped, and a comment that no longer goes to its session, or whose lines are
// not at its range in the file any more, is not sent. The status line tells how many
// requests went, and what was left out and why.
func (m *model) submit() tea.Cmd {
	groups := m.pendingComments()
	if len(groups) == 0 {
		m.warn("No comments to submit")
		return nil
	}
	var cmds []tea.Cmd
	var left []string
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
			}
		}
	}
	var told []string
	if len(cmds) > 0 {
		told = append(told, "Sent "+countOf(len(cmds), "fix request"))
	}
	text := strings.Join(append(told, left...), ". ")
	if len(left) > 0 {
		m.warn(text)
	} else {
		m.tell(text)
	}
	return tea.Batch(cmds...)
}

// withModel names the model a group of comments picked with ^, as the status line tells
// it: "^sonnet", or "its own model" for none.
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
// cursor is not on a comment of the request, the status line tells of the fixes until the
// next key, and Enter then goes to them.
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
	on := m.commentOn(m.cursor)
	switch {
	case logErr != nil:
		m.fail("could not save the fixes: " + review.Visible(logErr.Error()))
	case on != nil && slices.ContainsFunc(r.sent, func(s sentComment) bool { return s.c == on }):
		// the cursor is on a line of a comment of the request, whose box shows what came
	default:
		m.tellFixes(r, fixes)
	}
}

// tellFixes tells on the status line that fixes, how many, came for the comments of r, or
// that none did, and where. When a comment of r has a place in the diff, Enter goes to it.
func (m *model) tellFixes(r *request, fixes int) {
	first := r.sent[0].fix
	where := review.Visible(first.Path) + ":" + lineRange(first.First, first.First+len(first.Lines)-1)
	what := "Fix"
	if n := len(r.sent); n > 1 {
		where += " and " + strconv.Itoa(n-1) + " more"
		what = "Fixes"
	}
	text := what + " from " + review.Visible(review.Who(r.Agent, r.Session, m.short)) + " on " + where
	if fixes == 0 {
		text = "No " + strings.ToLower(text[:1]) + text[1:]
	}
	if i := slices.IndexFunc(r.sent, func(s sentComment) bool { return s.c.placed() && slices.Contains(m.comments, s.c) }); i >= 0 {
		text += ". Enter to read"
		m.unreadFix = r.sent[i].c
	}
	if fixes == 0 {
		m.warn(text)
	} else {
		m.tell(text)
	}
}

// readFix goes to c, the comment the status line told of the fixes for, so that its box
// shows under the cursor row. A file viewed since unfolds. It returns the command that
// saves the viewed files then.
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
// on the worktree, it refuses. The status line says what it did, or why not.
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
		m.fail(review.Visible(err.Error()))
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
	done := "Applied the fix for " + review.Visible(a.Path) + ":" + lineSpan(a.First, len(a.Old))
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
// pending again, to be edited and submitted again.
func (m *model) drop() {
	c := m.commentOn(m.cursor)
	if c == nil || c.fix == nil {
		m.warn("No fix on this line")
		return
	}
	from, to := m.commentRange(c)
	if m.dropFix(c) {
		m.tell("Dropped the fix for " + m.commentWhere(c.file, from, to))
	}
}

// dropFix takes the fix under c away and writes a Dropped record for it. It reports whether
// the record was saved; the status line says why not.
func (m *model) dropFix(c *comment) bool {
	d := state.Dropped{Time: m.now(), Fix: c.fix.ID}
	m.records.Dropped = append(m.records.Dropped, d)
	c.fix = nil
	if err := write(m.log, d); err != nil {
		m.fail("could not save the drop: " + review.Visible(err.Error()))
		return false
	}
	return true
}

// undo reverts the newest fix dr applied that no :undo reverted, in this run or an earlier
// one, with fix.Undo, writes the Undone record and returns the command that rebuilds the
// diff. The comment the fix resolved comes back, pending, on the lines the undo restored.
// Without the lock on the worktree, it refuses. The status line says what it did, or why
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
		m.fail(review.Visible(err.Error()))
		return nil
	}
	u := state.Undone{Time: m.now(), Applied: a.ID}
	m.records.Undone = append(m.records.Undone, u)
	if i := slices.IndexFunc(m.resolved, func(c *comment) bool { return c.ID == a.Comment }); i >= 0 {
		m.away = append(m.away, m.resolved[i])
		m.resolved = slices.Delete(m.resolved, i, i+1)
		m.placeComments()
		m.listWidth = m.treeWidth()
	}
	done := "Reverted the fix for " + review.Visible(a.Path) + ":" + lineSpan(a.First, len(a.Old))
	return m.reloadCmd(done, write(m.log, u))
}

// lineSpan returns n lines from first as lineRange writes them, or first alone when n is 0.
func lineSpan(first, n int) string {
	return lineRange(first, first+max(n, 1)-1)
}

// reloadCmd returns the command that reads the changed files again, for reloaded to rebuild
// the diff with and then tell done, or done and logErr, why its record was not saved. With
// no way to read them, the diff stays, and the status line tells it now.
func (m *model) reloadCmd(done string, logErr error) tea.Cmd {
	var warning string
	if logErr != nil {
		warning = "could not save the record: " + review.Visible(logErr.Error())
	}
	if m.reload == nil {
		if warning != "" {
			m.warn(done + "; " + warning)
		} else {
			m.tell(done)
		}
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

// fixOverlay tells the fix under c in its box, under the comment the way an answer comes
// under a question: a header with who sent it, a badge and at the right edge the model that
// sent it and how long it took, as answers have; then the change as a small diff, the
// comment's lines as sent deleted and the new lines added, and the note. A fix that keeps
// the lines has only the note, and when no fix came it says why. It is nil without a fix.
func (m *model) fixOverlay(c *comment) []paragraph {
	f := c.fix
	if f == nil {
		return nil
	}
	who := paragraph{text: review.Visible(review.Who(f.Agent, f.Session, m.short)), tone: strong, bar: answerBar, head: true}
	var body []paragraph
	switch {
	case f.Error != "":
		who.badge = part{text: "no fix", tone: trouble}
		body = append(body, paragraph{text: review.Visible(f.Error), tone: trouble, bar: answerBar})
	case f.Keep:
		who.badge, who.note = part{text: "keeps the lines", tone: quiet}, m.fixedBy(f)
		body = append(body, paragraph{text: review.Visible(f.Note), bar: answerBar})
	default:
		who.badge, who.note = part{text: "fix", tone: agreed}, m.fixedBy(f)
		for _, l := range f.Lines {
			body = append(body, paragraph{text: "-" + display(strings.TrimSuffix(l, "\r")), tone: fixOld, bar: answerBar})
		}
		for _, l := range f.New {
			body = append(body, paragraph{text: "+" + display(strings.TrimSuffix(l, "\r")), tone: fixNew, bar: answerBar})
		}
		if note := strings.TrimSpace(f.Note); note != "" {
			body = append(body, paragraph{text: review.Visible(note), bar: answerBar})
		}
	}
	return append([]paragraph{{bar: changeBar}, who}, body...)
}

// fixedBy returns the note on f at the right edge of its header, as answeredBy does for an
// answer: against the model of the session's newest edit, or as pickedNote writes it when
// f's request went with a model picked with ^.
func (m *model) fixedBy(f *state.Fix) part {
	took := time.Duration(f.TookMS) * time.Millisecond
	if r, ok := m.records.Request(f.Request); ok && r.Model != "" {
		return pickedNote(f.Model, r.Model, took)
	}
	own := ""
	if e := m.newest[session{f.Agent, f.Session}]; e != nil {
		own = modelName(e.Model)
	}
	return modelNote(modelName(f.Model), own, took)
}
