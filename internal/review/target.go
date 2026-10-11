package review

import (
	"cmp"
	"strings"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
)

// Relation is how the session a asks relates to the lines it asks about.
type Relation string

// How the session asked can relate to the lines, from the closest.
const (
	Author     Relation = "author" // it wrote them
	Parent     Relation = "parent" // it started the subagent that wrote them, which cannot be asked
	Editor     Relation = "editor" // no record says who wrote them, and it made the newest edit of their file
	NewSession Relation = "new"    // a new session, which knows nothing of them
)

// Target is who a asks about some lines.
type Target struct {
	Relation Relation
	// Ask names the agent and the session asked, and how to ask them: all of the question
	// but the fork to go on in and the prompt. A new session has no Session and runs in the
	// top directory of the worktree.
	Ask agent.Question
	// Base is the time of the newest edit in the review that the session asked knows of: one
	// of its own, or of a subagent it started. It is zero for a new session.
	Base time.Time
	// Edit is the newest edit among the changed lines asked about, nil when they have no
	// origin.
	Edit *agent.Edit
}

// Asking is what Target decides with besides the lines asked about.
type Asking struct {
	Lines     []origin.Line           // the lines of every file in the review
	Root      string                  // the top directory of the worktree, where a new session runs
	Short     map[string]string       // short session ids, as ShortIDs makes them
	Installed func(agent string) bool // whether the CLI of an agent is installed
	// Busy reports whether session of agent, or the new session of agent when session is "",
	// is answering a question. A session takes one question at a time.
	Busy func(agent, session string) bool
}

// Target returns who a asks about lines, the selected lines or the line under the cursor,
// which may hold unchanged lines as context, and file, the lines of their file's diff.
// When there is no one to ask, it returns why, in words for the person who pressed a.
//
// The changed lines must all come from one session, or none of them from any. The session
// that wrote them is asked when its agent can fork it, or else the session that started it
// when it is a subagent. For lines without an origin, the session behind the newest edit of
// the file is asked when its agent can fork it. Otherwise a new session is asked, of the
// installed agent behind the most edits in the review, on a tie the one with the newest
// edit, and when the review has no edits the first installed in agent.Names.
func (a Asking) Target(lines, file []origin.Line) (Target, string) {
	var newest *agent.Edit // of the changed lines
	unknown := false
	for _, l := range lines {
		switch e := l.Edit; {
		case l.Old > 0 && l.New > 0:
			// an unchanged line, asked along as context
		case e == nil:
			unknown = true
		case newest != nil && (e.Agent != newest.Agent || e.Session != newest.Session):
			return Target{}, "The selection mixes sessions. Select lines from one session"
		case newest == nil || e.Time.After(newest.Time):
			newest = e
		}
	}
	var t Target
	switch {
	case newest != nil && unknown:
		return Target{}, "Some selected lines have no origin"
	case newest == nil && !unknown:
		return Target{}, "The selection has no changed lines"
	case newest != nil && agent.CanFork(newest) == nil:
		t = a.session(Author, newest)
	case newest != nil && newest.Parent != "":
		t = a.parent(newest)
	case newest == nil:
		if e := latest(file); e != nil && agent.CanFork(e) == nil {
			t = a.session(Editor, e)
		}
	}
	if t.Relation == "" {
		var why string
		if t, why = a.newSession(); why != "" {
			return Target{}, why
		}
	}
	t.Edit = newest
	if a.Busy(t.Ask.Agent, t.Ask.Session) {
		return Target{}, "Still waiting for " + Who(t.Ask.Agent, t.Ask.Session, a.Short) + " to answer"
	}
	return t, ""
}

// Who names session of agent name the way dr shows it, "claude 1a2b3c4d", or a new
// session, session "", as "omp in a new session". short holds the short session ids from
// ShortIDs.
func Who(name, session string, short map[string]string) string {
	if session == "" {
		return name + " in a new session"
	}
	return name + " " + cmp.Or(short[session], Visible(session))
}

// session returns the target that asks the session that made e, as relation.
func (a Asking) session(relation Relation, e *agent.Edit) Target {
	return Target{
		Relation: relation,
		Ask:      agent.Question{Agent: e.Agent, Session: e.Session, Log: e.Log, Cwd: e.Cwd, Model: e.Model, Effort: e.Effort},
		Base:     a.base(e.Agent, e.Session),
	}
}

// parent returns the target that asks the session that started the subagent that made e.
// The parent is asked as its newest edit in the review shows it, or, when it made none, with
// what the subagent's edit tells of it: its id and its directory.
func (a Asking) parent(e *agent.Edit) Target {
	var own *agent.Edit
	for _, l := range a.Lines {
		if p := l.Edit; p != nil && p.Agent == e.Agent && p.Session == e.Parent && (own == nil || p.Time.After(own.Time)) {
			own = p
		}
	}
	if own != nil {
		return a.session(Parent, own)
	}
	return Target{
		Relation: Parent,
		Ask:      agent.Question{Agent: e.Agent, Session: e.Parent, Cwd: e.Cwd},
		Base:     a.base(e.Agent, e.Parent),
	}
}

// base returns the time of the newest edit in the review that session of agent knows of:
// one of its own, or of a subagent it started. A fix dr applied does not count: the fork
// that sent it knows of it.
func (a Asking) base(name, session string) time.Time {
	var newest time.Time
	for _, l := range a.Lines {
		if e := l.Edit; e != nil && e.Fix == nil && e.Agent == name && (e.Session == session || e.Parent == session) && e.Time.After(newest) {
			newest = e.Time
		}
	}
	return newest
}

// newSession returns the target that asks a new session of the installed agent behind the
// most edits in the review, the one with the newest edit on a tie, and the first installed
// in agent.Names when there are no edits. When no agent is installed it returns why.
func (a Asking) newSession() (Target, string) {
	edits := make(map[string]int)         // by agent
	newest := make(map[string]time.Time)  // by agent
	counted := make(map[*agent.Edit]bool) // the lines of an edit count once
	for _, l := range a.Lines {
		if e := l.Edit; e != nil && !counted[e] {
			counted[e] = true
			edits[e.Agent]++
			if e.Time.After(newest[e.Agent]) {
				newest[e.Agent] = e.Time
			}
		}
	}
	names := agent.Names()
	best := ""
	for _, name := range names {
		switch {
		case !a.Installed(name):
		case best == "", edits[name] > edits[best], edits[name] == edits[best] && newest[name].After(newest[best]):
			best = name
		}
	}
	if best == "" {
		list := names[len(names)-1]
		if len(names) > 1 {
			list = strings.Join(names[:len(names)-1], ", ") + " or " + list
		}
		return Target{}, "No agent CLI is installed to ask (" + list + ")"
	}
	return Target{Relation: NewSession, Ask: agent.Question{Agent: best, Cwd: a.Root}}, ""
}

// latest returns the newest edit among the origins of lines, nil when they have none.
func latest(lines []origin.Line) *agent.Edit {
	var newest *agent.Edit
	for _, l := range lines {
		if e := l.Edit; e != nil && (newest == nil || e.Time.After(newest.Time)) {
			newest = e
		}
	}
	return newest
}
