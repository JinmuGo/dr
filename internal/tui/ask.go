package tui

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// Asker is how the screen asks agents about lines.
type Asker struct {
	// Ask sends a question and returns the answer, as agent.Ask does. The screen runs it in
	// a command, and stops it through ctx when it quits.
	Ask func(ctx context.Context, q agent.Question) (agent.Answer, error)
	// Installed reports whether the CLI of an agent is installed, as agent.Installed does.
	Installed func(agent string) bool
	// Models lists the models the CLI of an agent can be asked with, as agent.Models does.
	Models func(ctx context.Context, agent string) ([]agent.Model, error)
	// Root is the top directory of the worktree, where a new session runs.
	Root string
}

// What a asks when nothing is typed: the session that wrote the lines, and a session that
// did not.
const (
	whyAdded        = "Why did you write this line this way?"
	whyDeleted      = "Why did you delete this line?"
	whyLines        = "Why did you write these lines this way?"
	whyAddedOther   = "Why is this line written this way?"
	whyDeletedOther = "Why was this line deleted?"
	whyLinesOther   = "Why are these lines written this way?"
)

// stopWait is how long quitting waits for the questions in flight to stop.
const stopWait = 2 * time.Second

// tickMsg comes a second after tick was called, so that the origin box redraws how long the
// questions in flight have waited.
type tickMsg struct{}

// tick returns the command that brings a tickMsg a second from now.
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// session names a session of an agent; id "" names the new session dr asks the agent in
// about lines that no session it can fork wrote or edited.
type session struct{ agent, id string }

// runFork is a fork or new session that answered a question in this run, and the base of
// that question: the time of the newest edit in the review the session knew of, which is as
// far as the fork knows the session's work.
type runFork struct {
	id   string
	base time.Time
}

// question is a question asked with a, in this run or an earlier one, and its answer.
type question struct {
	state.Question
	n       int           // its place in model.questions, which are oldest first
	file    int           // in model.files
	lines   []int         // the lines it asks about in model.lines, nil when it has no place in the diff now
	tag     string        // who was asked, as the input box of a named them; "" for an earlier run's
	own     string        // the model of the session asked, to compare the one that answered with; "" when not known
	pending bool          // asked in this run, and the answer has not come
	answer  *state.Answer // nil while pending, or when dr closed before the answer came
}

// target is what a asks about, and who, while its input box is open.
type target struct {
	review.Target
	file  int
	lines []int // in model.lines, in the order of the diff
	row   int   // the last row of them, under which the input box and then the origin box open
	// tag names who is asked: for the author, the request behind the changed lines, or only
	// the session when they come from several; otherwise the session, or the agent of a new
	// session.
	tag         string
	label       string // tag, and how the session relates to the lines
	placeholder string // what is asked when nothing is typed
	followUp    bool   // a in the origin box asks after the questions there
	reading     bool   // a closed the origin box under row to open the input box
	readTop     int    // and the first line of its text that it showed
}

// answeredMsg brings the answer to a question, or why there is none, and why it could not
// be written to the log. base is the base the question was asked with, which the fork that
// answered knows the session's work up to.
type answeredMsg struct {
	answer state.Answer
	base   time.Time
	logErr error
}

// startAsk opens the input box for what a asks about, under the last row of its lines, or
// says why there is no one to ask about it. a in the open origin box closes the origin box,
// and Esc in the input box opens it again as it was.
func (m *model) startAsk() {
	t, why := m.target(m.whom)
	if t == nil {
		m.warn(why)
		return
	}
	t.reading, t.readTop = m.originBox.open, m.originBox.top
	m.asking, m.entry, m.originBox.open = t, textBox{}, false
}

// target returns what a asks about, and who: whom.Target decides, which m.whom is for a.
// In the open origin box, a asks about the lines the newest question there asked about, as
// GitHub's Reply answers a thread, or else about the box's changed line. Otherwise it asks
// about the lines of the selection or the changed line under the cursor. Unchanged lines in
// a selection go along as context. When there is no one to ask about them, it returns why.
func (m *model) target(whom review.Asking) (*target, string) {
	t := &target{row: m.cursor}
	switch r := m.rows[m.cursor]; {
	case m.originBox.open && m.hasQuestions(m.cursor):
		qs := m.questionsAt(m.cursor)
		t.lines, t.followUp = slices.Clone(qs[len(qs)-1].lines), true
	case m.selecting:
		rows := m.selectedRows()
		for _, i := range rows {
			for _, k := range [2]int{m.rows[i].left, m.rows[i].right} {
				if k >= 0 {
					t.lines = append(t.lines, k)
				}
			}
		}
		slices.Sort(t.lines) // the order of the diff
		t.lines = slices.Compact(t.lines)
		t.row = rows[len(rows)-1]
	case r.isChange():
		t.lines = []int{r.main()}
	default:
		return nil, "Move to a changed line to ask"
	}
	t.file = m.rows[t.row].file
	lines := make([]origin.Line, len(t.lines))
	// The requests behind the lines: a turn of the session, or a fix dr applied.
	type request struct {
		turn int
		fix  *agent.Fix
	}
	turns := make(map[request]bool)
	for i, k := range t.lines {
		lines[i] = m.lines[k]
		if e := lines[i].Edit; e != nil {
			turns[request{e.Turn, e.Fix}] = true
		}
	}
	var why string
	if t.Target, why = whom.Target(lines, m.files[t.file].diff); why != "" {
		return nil, why
	}
	t.tag = review.Who(t.Ask.Agent, t.Ask.Session, m.short)
	if t.Relation == review.Author && len(turns) == 1 {
		t.tag = review.Tag(t.Edit, m.short)
	}
	t.label = t.tag
	switch t.Relation {
	case review.Parent:
		t.label += ", which started the subagent"
	case review.Editor:
		t.label += ", which edited this file"
	}
	t.placeholder = placeholder(t.Relation == review.Author, lines)
	return t, ""
}

// placeholder returns what a asks about lines when nothing is typed: the session that wrote
// them, author, or else a session that did not.
func placeholder(author bool, lines []origin.Line) string {
	switch {
	case len(lines) > 1 && author:
		return whyLines
	case len(lines) > 1:
		return whyLinesOther
	case lines[0].New == 0 && author:
		return whyDeleted
	case lines[0].New == 0:
		return whyDeletedOther
	case author:
		return whyAdded
	}
	return whyAddedOther
}

// send asks what is typed in the input box of a, as typed but for the space around it, or
// the placeholder when nothing is, and ends the selection. The origin box opens under the
// last line asked about, where the question waits for its answer at the end of the thread.
// A model picked with ^ asks in place of the session's, with the CLI's default reasoning
// effort, as the session's effort belongs to the session's model. It returns the command
// that asks.
func (m *model) send() tea.Cmd {
	t := m.asking
	text := strings.TrimSpace(m.entry.text)
	if text == "" {
		text = t.placeholder
	}
	pick := m.entry.pick
	m.asking, m.entry, m.selecting = nil, textBox{}, false
	f := m.files[t.file]
	who := session{t.Ask.Agent, t.Ask.Session}
	asked := make([]origin.Line, len(t.lines))
	for i, k := range t.lines {
		asked[i] = m.lines[k]
	}
	first, last := t.lines[0], t.lines[len(t.lines)-1]
	// The log keeps the lines from the first to the last, so they follow each other in the
	// diff, and which of them are asked about when a selection of the split view left some
	// out.
	var selected []int
	if len(t.lines) < last+1-first {
		for _, k := range t.lines {
			selected = append(selected, k-first)
		}
	}
	q := &question{
		Question: state.Question{
			Time: m.now(), ID: newID(), Agent: who.agent, Session: who.id, Relation: string(t.Relation), Path: f.key,
			Lines:    diffLines(m.lines[first : last+1]),
			Selected: selected,
			Before:   diffLines(m.lines[max(first-2, f.first):first]),
			After:    diffLines(m.lines[last+1 : min(last+3, f.end)]),
			Text:     text,
			Model:    pick,
		},
		file: t.file, lines: t.lines, tag: t.tag, own: t.Ask.Model, pending: true,
	}
	m.addQuestion(q)
	m.busy[who] = true
	m.cursor, m.originBox.open = t.row, true
	m.originBox.top = m.questionTop(q)
	a := t.Ask
	if pick != "" {
		a.Model, a.Effort = pick, ""
	}
	a.Fork, a.Prompt = m.fork(who, t.Base), review.Prompt(t.Relation, f.key, asked, text)
	return m.askCmd(q.Question, a, t.Base)
}

// fork returns the fork to go on asking who in: the one that answered last in this run,
// unless base, the time of the newest edit in the review the session knows of, is after the
// base of the question it answered, and the fork does not know of the session's newer work.
// Otherwise it returns the one an earlier run left, as state.Records.Fork decides, or "" to
// make a new one.
func (m *model) fork(who session, base time.Time) string {
	if f, ok := m.forks[who]; ok && !base.After(f.base) {
		return f.id
	}
	return m.earlier.Fork(who.agent, who.id, base)
}

// askCmd returns the command that asks a, the question q, and brings back the answer. It
// writes q, the fork or new session it made if it made one, and the answer to the log.
// base is the time of the newest edit in the review that the session asked knows of, the
// last a new fork knows of. When the screen quits first, the command stops and q stays in
// the log without an answer.
func (m *model) askCmd(q state.Question, a agent.Question, base time.Time) tea.Cmd {
	ask, log, flights := m.asker.Ask, m.log, m.flights
	return func() tea.Msg {
		ctx, ok := flights.start(q.ID)
		if !ok {
			return nil // the screen quit before the question went
		}
		defer flights.end(q.ID)
		logErr := write(log, q)
		answer, err := ask(ctx, a)
		if ctx.Err() != nil {
			return nil
		}
		now := time.Now()
		record := state.Answer{
			Time: now, Question: q.ID, Fork: answer.Fork, Model: answer.Model,
			TookMS: answer.Took.Milliseconds(), Text: answer.Text,
		}
		switch {
		case err != nil:
			record = state.Answer{Time: now, Question: q.ID, Error: err.Error()}
		case answer.Fork != "" && answer.Fork != a.Fork:
			fork := state.Fork{Time: now, Agent: a.Agent, Session: a.Session, Fork: answer.Fork, Base: base}
			logErr = errors.Join(logErr, write(log, fork))
		}
		return answeredMsg{answer: record, base: base, logErr: errors.Join(logErr, write(log, record))}
	}
}

// write appends r to log, when the screen keeps one.
func write(log *state.Log, r state.Record) error {
	if log == nil {
		return nil
	}
	return log.Append(r)
}

// answered puts the answer that came in place of its question's waiting, and keeps the fork
// that answered, with the base the question was asked with, for the next questions to the
// session. When the origin box does not show the question, a notice tells of the answer
// until the next key, and Enter then goes to read it.
func (m *model) answered(msg answeredMsg) {
	i := slices.IndexFunc(m.questions, func(q *question) bool { return q.ID == msg.answer.Question })
	if i < 0 {
		return
	}
	q := m.questions[i]
	shown := m.originBox.open && slices.Contains(m.questionsAt(m.cursor), q)
	following := shown && m.originBox.top == m.questionTop(q) // the box shows the question waiting
	q.pending, q.answer = false, &msg.answer
	who := session{q.Agent, q.Session}
	delete(m.busy, who)
	if msg.answer.Error == "" && msg.answer.Fork != "" {
		m.forks[who] = runFork{id: msg.answer.Fork, base: msg.base}
	}
	if following {
		m.originBox.top = m.questionTop(q) // to show the answer that came
	}
	t := &m.theme
	from := []chunk{{text: q.tag, color: m.whoColor(q.Agent, q.Session), bold: true}, {text: " on ", color: t.fg}}
	switch {
	case msg.logErr != nil:
		m.fail("could not save the question: " + review.Visible(msg.logErr.Error()))
	case shown:
	case q.file < 0: // its file left the diff, so the answer has nowhere to show
		m.notice = &notice{
			tone: toneAnswer,
			text: slices.Concat([]chunk{{text: "Answer from ", color: t.fg}}, from, []chunk{{text: review.Visible(q.Path), color: t.strong}}),
			hint: m.quiet("Its file left the diff."),
		}
	case msg.answer.Error != "":
		m.notice = &notice{
			tone: toneErr, question: q, hint: m.readHint(),
			text: slices.Concat([]chunk{{text: "No answer from ", color: t.fg}}, from, []chunk{{text: m.whereAsked(q), color: t.strong}}),
		}
	default:
		m.notice = &notice{
			tone: toneAnswer, question: q, hint: m.readHint(),
			text: slices.Concat([]chunk{{text: "Answer from ", color: t.fg}}, from, []chunk{{text: m.whereAsked(q), color: t.strong}}),
		}
	}
}

// whereAsked returns where the origin box shows q: the path of its file and the number of
// its last line, the current one or else the one in base, or the path alone when q has no
// place in the diff.
func (m *model) whereAsked(q *question) string {
	if q.lines == nil {
		return m.files[q.file].path
	}
	l := m.lines[q.lines[len(q.lines)-1]]
	return m.files[q.file].path + ":" + strconv.Itoa(cmp.Or(l.New, l.Old))
}

// read goes to the question a notice told of and opens the origin box where the box
// shows it with its answer: under its last line, or under its file's header, at the end,
// when it has no place in the diff. A file viewed since unfolds. It returns the command that
// saves the viewed files then.
func (m *model) read(q *question) tea.Cmd {
	var cmd tea.Cmd
	if m.files[q.file].viewed {
		cmd = m.toggleViewed(q.file)
	}
	m.selecting = false
	m.originBox.open = true
	if q.lines == nil {
		m.jumpTo(m.headRow(q.file))
		m.originBox.top = m.blockHeight() // scroll keeps it in the text
		return cmd
	}
	m.jumpTo(m.lineRow(q.lines[len(q.lines)-1]))
	m.originBox.top = m.questionTop(q)
	return cmd
}

// lineRow returns the row of the view on the screen that shows line k of model.lines.
func (m *model) lineRow(k int) int {
	return slices.IndexFunc(m.rows, func(r row) bool { return r.kind == lineRow && (r.left == k || r.right == k) })
}

// addQuestion keeps q, a question asked in this run, with the lines it asks about.
func (m *model) addQuestion(q *question) {
	q.n = len(m.questions)
	m.questions = append(m.questions, q)
	m.attach(q)
}

// attach puts q with the lines it asks about, or with the header of its file when it has
// no place in the diff. A question about a file that is not in the diff is left out.
func (m *model) attach(q *question) {
	switch {
	case q.file < 0:
	case q.lines == nil:
		m.files[q.file].earlier = append(m.files[q.file].earlier, q)
	default:
		for _, k := range q.lines {
			m.asked[k] = append(m.asked[k], q)
		}
	}
}

// questionsAt returns the questions about the lines of row i, oldest first.
func (m *model) questionsAt(i int) []*question {
	r := m.rows[i]
	if r.kind != lineRow {
		return nil
	}
	var qs []*question
	for _, k := range [2]int{r.left, r.right} {
		if k >= 0 {
			qs = append(qs, m.asked[k]...)
		}
	}
	slices.SortFunc(qs, func(a, b *question) int { return a.n - b.n })
	return slices.Compact(qs)
}

// hasQuestions reports whether there are questions about the lines of row i.
func (m *model) hasQuestions(i int) bool {
	r := m.rows[i]
	return r.kind == lineRow && (r.left >= 0 && len(m.asked[r.left]) > 0 || r.right >= 0 && len(m.asked[r.right]) > 0)
}

// earlierQuestions returns the questions of earlier runs in records, with their answers.
// placeQuestions puts them in the diff.
func earlierQuestions(records state.Records) []*question {
	qs := make([]*question, len(records.Questions))
	for i, r := range records.Questions {
		qs[i] = &question{Question: r, n: i, file: -1}
		if a, ok := records.Answers[r.ID]; ok {
			qs[i].answer = &a
		}
	}
	return qs
}

// placeQuestions puts model.questions, of this run and earlier ones, in the diff. A
// question goes with the lines it asked about when state.Place finds them in its file's diff
// now, and else with the header of its file. A question about a file that is not in the
// diff is kept, but has no place.
func (m *model) placeQuestions() {
	files := make(map[string]int, len(m.files))
	for i, f := range m.files {
		files[f.key] = i
	}
	newest := newestEdits(m.files)
	diffs := make(map[int][]state.Line) // the lines of each file, as the log keeps them
	for _, q := range m.questions {
		q.file, q.lines = -1, nil
		i, ok := files[q.Path]
		if !ok {
			continue
		}
		f := m.files[i]
		if diffs[i] == nil {
			diffs[i] = diffLines(m.lines[f.first:f.end])
		}
		q.file = i
		if q.tag == "" { // an earlier run's, which did not keep the model it asked
			q.own = ""
			if e := newest[session{q.Agent, q.Session}]; e != nil {
				q.own = e.Model
			}
		}
		if at, ok := state.Place(diffs[i], q.Before, q.Lines, q.After); ok {
			for _, k := range q.Asked() {
				q.lines = append(q.lines, f.first+at+k)
			}
		}
		m.attach(q)
	}
}

// newestEdits returns the newest edit of each session among the lines of files. A fix dr
// applied does not count, as Asking does not count it either.
func newestEdits(files []file) map[session]*agent.Edit {
	newest := make(map[session]*agent.Edit)
	for _, f := range files {
		for _, l := range f.diff {
			if e := l.Edit; e != nil && e.Fix == nil {
				who := session{e.Agent, e.Session}
				if n := newest[who]; n == nil || e.Time.After(n.Time) {
					newest[who] = e
				}
			}
		}
	}
	return newest
}

// diffLines returns lines as the log keeps them.
func diffLines(lines []origin.Line) []state.Line {
	out := make([]state.Line, len(lines))
	for i, l := range lines {
		out[i] = state.Line{Kind: " ", Text: l.Text}
		switch {
		case l.Old == 0:
			out[i].Kind = "+"
		case l.New == 0:
			out[i].Kind = "-"
		}
	}
	return out
}

// lineRange returns the lines from first to last as "14-16", or "14" for one line.
func lineRange(first, last int) string {
	if first == last {
		return strconv.Itoa(first)
	}
	return strconv.Itoa(first) + "-" + strconv.Itoa(last)
}

// newID returns a new question ID: 16 hex digits.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // it does not fail: it crashes the program instead
	return hex.EncodeToString(b)
}

// countOf returns n and noun, "1 line" or "3 lines".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// flights keeps the questions in flight, so that the screen can stop them when it quits.
type flights struct {
	mu      sync.Mutex
	stopped bool
	cancels map[string]context.CancelFunc // by question ID
	wg      sync.WaitGroup
}

// start returns the context to ask question id in, which stop cancels. It returns false
// when the screen has quit, and the question is not to be asked then.
func (f *flights) start(id string) (context.Context, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	if f.cancels == nil {
		f.cancels = make(map[string]context.CancelFunc)
	}
	f.cancels[id] = cancel
	f.wg.Add(1)
	return ctx, true
}

// end tells that question id, which start started, ended.
func (f *flights) end(id string) {
	f.mu.Lock()
	f.cancels[id]()
	delete(f.cancels, id)
	f.mu.Unlock()
	f.wg.Done()
}

// stop cancels the questions in flight and waits until they end, at most wait. Questions
// that start later are not asked.
func (f *flights) stop(wait time.Duration) {
	f.mu.Lock()
	f.stopped = true
	for _, cancel := range f.cancels {
		cancel()
	}
	f.mu.Unlock()
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(wait):
	}
}
