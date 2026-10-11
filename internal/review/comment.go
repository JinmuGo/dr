package review

import (
	"strconv"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
)

// MaxCommentLines is how many lines of the current file a comment covers at most.
const MaxCommentLines = 200

// Commenting returns who a change comment on lines goes to: the selected lines or the line
// under the cursor, which may hold unchanged lines as context. When none can take it, it
// returns why, in words for the person who pressed c.
//
// A comment goes to the session a would ask as the author, or as the parent for lines a
// Claude Code subagent wrote, and never to a session that did not write the lines. So the
// changed lines must all have an origin, from one session. Its range is the lines of the
// current file from the first of lines to the last, at most MaxCommentLines; deleted lines
// in it are context. Nothing is sent, so a session that is answering a question can take
// a comment.
func (a Asking) Commenting(lines []origin.Line) (Target, string) {
	var newest *agent.Edit // of the changed lines
	unknown := false
	first, last := 0, 0 // the range in the current file
	for _, l := range lines {
		if l.New > 0 {
			if first == 0 {
				first = l.New
			}
			last = l.New
		}
		switch e := l.Edit; {
		case l.Old > 0 && l.New > 0:
			// an unchanged line, commented along as context
		case e == nil:
			unknown = true
		case newest != nil && (e.Agent != newest.Agent || e.Session != newest.Session):
			return Target{}, "The selection mixes sessions. Select lines from one session"
		case newest == nil || e.Time.After(newest.Time):
			newest = e
		}
	}
	switch {
	case unknown:
		return Target{}, "Lines without an origin cannot take a comment. Fix them with e"
	case newest == nil:
		return Target{}, "The selection has no changed lines"
	case first == 0:
		return Target{}, "Deleted lines alone cannot take a comment"
	case last-first+1 > MaxCommentLines:
		return Target{}, "A comment covers at most " + strconv.Itoa(MaxCommentLines) + " lines"
	}
	var t Target
	switch err := agent.CanFork(newest); {
	case err == nil:
		t = a.session(Author, newest)
	case newest.Parent != "":
		t = a.parent(newest)
	default:
		return Target{}, "dr cannot send a comment to " + Who(newest.Agent, newest.Session, a.Short) + ": " + Visible(err.Error())
	}
	t.Edit = newest
	return t, ""
}
