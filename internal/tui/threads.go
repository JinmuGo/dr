package tui

import (
	"cmp"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// threadsTitle is the title of the threads panel, and threadLines how many lines each of
// its threads takes.
const (
	threadsTitle = "Threads"
	threadLines  = 2
)

// threadsPanel is the threads panel, which t shows in place of the file tree: whether it
// shows, whether its keys have the focus, and the thread they have selected.
type threadsPanel struct {
	shown, focus bool
	sel          string // the id of the selected thread, "" for the first
}

// The ranks of threads, the order the panel lists them in: what needs you first, as
// herdr's agent list puts the agents waiting for you first.
const (
	threadNew     = iota // an answer, or why none came, that the origin box has not shown
	threadFix            // a fix y can apply, or why no fix came
	threadWaiting        // a question or a fix request in flight
	threadDraft          // a comment :submit sends
	threadRead           // answered and read, or a fix that keeps the lines
)

// thread is a row of the threads panel: the questions about the same lines, or a change
// comment.
type thread struct {
	id   string      // "q" and the ID of its first question, or "c" and the ID of the comment
	qs   []*question // oldest first; nil for a comment
	c    *comment
	rank int
	time time.Time // when it last changed, newest first within a rank
}

// newest returns the newest question of t, a thread of questions.
func (t *thread) newest() *question {
	return t.qs[len(t.qs)-1]
}

// threadList returns the threads of the review, in the order the panel lists them: the
// questions of this run and earlier ones, grouped by where the origin box shows them, and
// the comments, those with no place in the diff among the read ones.
func (m *model) threadList() []*thread {
	var out []*thread
	byPlace := make(map[string]*thread)
	for _, q := range m.questions {
		place := "p" + q.Path
		switch {
		case q.file >= 0 && q.lines == nil:
			place = "h" + strconv.Itoa(q.file)
		case q.file >= 0:
			place = "l" + strconv.Itoa(q.lines[len(q.lines)-1])
		}
		t := byPlace[place]
		if t == nil {
			t = &thread{id: "q" + q.ID}
			byPlace[place] = t
			out = append(out, t)
		}
		t.qs = append(t.qs, q)
	}
	for _, t := range out {
		q := t.newest()
		t.time, t.rank = q.Time, threadRead
		switch {
		case q.pending:
			t.rank = threadWaiting
		case q.unread:
			t.rank = threadNew
		}
		if q.answer != nil {
			t.time = q.answer.Time
		}
	}
	for _, c := range slices.Concat(m.comments, m.away) {
		t := &thread{id: "c" + c.ID, c: c, rank: threadDraft, time: c.Time}
		switch {
		case !c.placed(): // an earlier comment whose lines changed, or whose file left the diff
			t.rank = threadRead
		case c.request != "":
			t.rank, t.time = threadWaiting, c.asked
		case c.fix != nil && c.fix.Keep:
			t.rank, t.time = threadRead, c.fix.Time
		case c.fix != nil:
			t.rank, t.time = threadFix, c.fix.Time
		}
		out = append(out, t)
	}
	slices.SortStableFunc(out, func(a, b *thread) int {
		return cmp.Or(a.rank-b.rank, b.time.Compare(a.time))
	})
	return out
}

// openThreads shows the threads panel in place of the file tree, unfolding the tree, and
// gives it the keys, with the thread of the cursor row selected. When the screen is too
// narrow for the tree beside the diff, it says so.
func (m *model) openThreads() {
	folded := m.folded
	m.folded = false
	if list, _ := m.layout(); list == 0 {
		m.folded = folded
		m.warn("Widen the screen to show the threads")
		return
	}
	m.threads.shown, m.threads.focus = true, true
	m.threads.sel = m.cursorThread(m.threadList())
}

// cursorThread returns the id of the thread of the cursor row among threads: the comment on
// it, or the questions about its lines, or about its file when it is a header. It returns ""
// for none.
func (m *model) cursorThread(threads []*thread) string {
	c := m.commentOn(m.cursor)
	qs := m.questionsAt(m.cursor)
	if r := m.rows[m.cursor]; r.kind == fileRow {
		qs = m.files[r.file].earlier
	}
	for _, t := range threads {
		if t.c != nil && t.c == c || t.qs != nil && slices.Contains(qs, t.newest()) {
			return t.id
		}
	}
	return ""
}

// threadKey handles a key while the threads panel has the keys: j and k move the selection,
// Enter goes to the selected thread, Esc gives the keys back to the diff, t shows the file
// tree again, and ⌘B folds the panel. It returns the command that saves the viewed files when going to a
// thread unfolds a viewed file.
func (m *model) threadKey(k string) tea.Cmd {
	threads := m.threadList()
	at := max(slices.IndexFunc(threads, func(t *thread) bool { return t.id == m.threads.sel }), 0)
	switch k {
	case keyLineDown, keyDown:
		at++
	case keyLineUp, keyUp:
		at--
	case keyEnter:
		if at < len(threads) {
			return m.goThread(threads[at])
		}
	case keyEsc:
		m.threads.focus = false
	case keyThreads:
		m.threads.shown, m.threads.focus = false, false
	case keyFoldTree:
		m.foldTree()
	case keyHelp:
		m.help = true
	case keyQuit:
		return tea.Quit
	}
	if len(threads) > 0 {
		m.threads.sel = threads[max(min(at, len(threads)-1), 0)].id
	}
	return nil
}

// goThread gives the keys back to the diff and goes to t: the origin box of its questions
// opens where it shows the newest with its answer, or the cursor goes to its comment, under
// which the box of the comment and its fix shows. A thread whose file left the diff has
// nowhere to go, which it says.
func (m *model) goThread(t *thread) tea.Cmd {
	m.threads.focus, m.threads.sel = false, t.id
	switch c := t.c; {
	case c != nil && c.placed():
		return m.readFix(c)
	case c != nil && c.file >= 0:
		return m.readHeader(c.file)
	case c != nil:
		m.warn("Its file left the diff")
		return nil
	}
	q := t.newest()
	if q.file < 0 {
		m.warn("Its file left the diff")
		return nil
	}
	return m.read(q)
}

// readHeader goes to the header of file i and opens its origin box at the end, where the
// questions and comments that have no place in its diff show. A file viewed since unfolds.
// It returns the command that saves the viewed files then.
func (m *model) readHeader(i int) tea.Cmd {
	var cmd tea.Cmd
	if m.files[i].viewed {
		cmd = m.toggleViewed(i)
	}
	m.selection.on = false
	m.jumpTo(m.headRow(i))
	m.originBox.open = true
	m.originBox.top = m.blockHeight() // scroll keeps it in the text
	return cmd
}

// markRead marks the questions whose answers the open origin box shows as read: those about
// the lines of the cursor row whose answer is in view, or every question about its file on a
// header, which lists them all.
func (m *model) markRead() {
	if !m.originBox.open {
		return
	}
	if r := m.rows[m.cursor]; r.kind == fileRow {
		for _, q := range m.files[r.file].earlier {
			q.unread = false
		}
		return
	}
	for _, q := range m.questionsAt(m.cursor) {
		if m.answerShown(q) {
			q.unread = false
		}
	}
}

// threadsTop returns the first of threads that a panel of body lines shows: the selected one
// stays in the middle while they are longer than that.
func threadsTop(threads []*thread, sel string, body int) int {
	n := max(body/threadLines, 1)
	at := slices.IndexFunc(threads, func(t *thread) bool { return t.id == sel })
	return max(min(at-n/2, len(threads)-n), 0)
}

// threadAt returns the thread the threads panel, h lines high, shows on line y, or nil.
func (m *model) threadAt(y, h int) *thread {
	threads := m.threadList()
	sel := m.selectedThread(threads)
	i := threadsTop(threads, sel, m.treeBody(h)) + (y-treeHead)/threadLines
	if y < treeHead || i >= len(threads) {
		return nil
	}
	return threads[i]
}

// selectedThread returns the id of the thread the panel marks: the selected one while it
// has the keys, and the thread of the cursor row otherwise.
func (m *model) selectedThread(threads []*thread) string {
	if m.threads.focus {
		return cmp.Or(m.threads.sel, func() string {
			if len(threads) > 0 {
				return threads[0].id
			}
			return ""
		}())
	}
	return m.cursorThread(threads)
}

// threadsList draws the threads panel, w cells wide and h rows high: the title with how many
// threads have news, a rule, then two lines a thread, the marked one on the screen.
func (m *model) threadsList(w, h int) []string {
	t := &m.theme
	on := func(fg color.Color) lipgloss.Style { return t.paint(fg, t.panel) }
	blank := on(nil).Render(strings.Repeat(" ", w))
	threads := m.threadList()
	news := 0
	for _, th := range threads {
		if th.rank == threadNew || th.rank == threadFix {
			news++
		}
	}
	count := []seg{{text: strconv.Itoa(len(threads)), st: on(t.fg)}, {text: " ", st: on(nil)}}
	if news > 0 {
		count = []seg{{text: strconv.Itoa(news) + " new", st: on(t.ask).Bold(true)}, {text: " ", st: on(nil)}}
	}
	titleColor := t.strong
	if m.threads.focus {
		titleColor = t.accent
	}
	title := spread([]seg{{text: " ", st: on(nil)}, {text: threadsTitle, st: on(titleColor).Bold(true)}}, count, w, on(nil))
	rule := newLine(w)
	rule.add(" ", on(nil))
	rule.add(strings.Repeat("─", max(w-2, 0)), on(t.faint))
	lines := make([]string, 0, h)
	lines = append(lines, title, rule.fill(on(nil)), blank)
	body := m.treeBody(h)
	if len(threads) == 0 {
		for _, s := range []seg{{text: "   No questions or comments", st: on(t.muted)}, {text: "   a asks, c comments", st: on(t.subtle)}} {
			l := newLine(w)
			l.add(s.text, s.st)
			lines = append(lines, l.fill(on(nil)))
		}
	}
	sel := m.selectedThread(threads)
	for i := threadsTop(threads, sel, body); i < len(threads) && len(lines)+threadLines <= h; i++ {
		lines = append(lines, m.threadLines(threads[i], w, threads[i].id == sel)...)
	}
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// threadLines draws thread th in two lines w cells wide, as herdr lists its agents: a mark
// of its state and where it is, with its state at the right edge, then the agent it went to
// in the color of the session and the text of its newest question or of the comment. The
// marked thread stands out with a bar and a background, as the file under the cursor does
// in the file tree.
func (m *model) threadLines(th *thread, w int, marked bool) []string {
	t := &m.theme
	bg := t.panel
	bar := " "
	if marked {
		bg, bar = t.surface2, "▌"
	}
	style := func(fg color.Color) lipgloss.Style { return t.paint(fg, bg).Reverse(marked && t.plain) }
	mark, markColor, state, stateColor := m.threadState(th)
	var agent, session, where, text string
	if th.c != nil {
		c := th.c
		agent, session, text = c.Agent, c.Session, c.Text
		where = review.VisibleLine(c.Path)
		if c.placed() {
			from, to := m.commentRange(c)
			where = m.commentWhere(c.file, from, to)
		}
	} else {
		q := th.newest()
		agent, session, text = q.Agent, q.Session, q.Text
		where = review.VisibleLine(q.Path)
		if q.lines != nil {
			where = m.whereAsked(q)
		}
		if len(th.qs) > 1 {
			text = "(" + strconv.Itoa(len(th.qs)) + ") " + text
		}
	}
	name := style(t.fg)
	if marked {
		name = style(t.strong).Bold(true)
	}
	right := []seg{{text: " ", st: style(nil)}}
	if state != "" {
		right = []seg{{text: state, st: style(stateColor)}, {text: " ", st: style(nil)}}
	}
	first := spread(
		[]seg{{text: bar, st: style(t.accent)}, {text: mark + " ", st: style(markColor)}, {text: keepEnd(where, w), st: name, cut: true}},
		right, w, style(nil))
	second := newLine(w)
	second.add(bar, style(t.accent))
	second.add("  ", style(nil))
	second.add(agent, style(m.whoColor(agent, session)))
	second.add(" · ", style(t.subtle))
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	second.add(ansi.Truncate(review.VisibleLine(line), max(second.free-1, 0), "…"), style(t.muted))
	return []string{first, second.fill(style(nil))}
}

// threadState returns the mark of th's state and its color, and the word at the right edge
// of its first line and its color: ◉ for news, ● while it is in flight with how long it has
// waited, ✗ when no answer or fix came, ✎ for a comment :submit sends and ○ when it is read.
func (m *model) threadState(th *thread) (mark string, markColor color.Color, state string, stateColor color.Color) {
	t := &m.theme
	failed := th.c != nil && th.c.fix != nil && th.c.fix.Error != "" ||
		th.qs != nil && th.newest().answer != nil && th.newest().answer.Error != ""
	switch {
	case th.rank == threadWaiting:
		s := max(int(m.now().Sub(th.time)/time.Second), 0)
		return "●", t.pending, string(spinner[s%len(spinner)]) + " " + strconv.Itoa(s) + "s", t.ask
	case failed && th.rank != threadRead:
		what := "no answer"
		if th.c != nil {
			what = "no fix"
		}
		return "✗", t.del, what, t.del
	case th.rank == threadNew:
		return "◉", t.ask, "answer", t.ask
	case th.rank == threadFix:
		return "◉", t.add, "fix", t.add
	case th.rank == threadDraft:
		return commentMark, t.pending, "draft", t.subtle
	case failed:
		return "✗", t.subtle, "", nil
	case th.c != nil && !th.c.placed():
		return "○", t.subtle, "earlier", t.subtle
	case th.c != nil:
		return "○", t.subtle, "kept", t.subtle
	}
	return "○", t.subtle, "", nil
}
