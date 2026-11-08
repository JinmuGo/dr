package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
)

// approve decides the review as approved and quits. It is :approve, which only dr --review
// offers.
func (m *model) approve() tea.Cmd {
	m.verdict = review.VerdictApprove
	return tea.Quit
}

// approveDoes says what :approve does now.
func (m *model) approveDoes() string {
	if n := len(m.reported()); n > 0 {
		return "Approve, with " + countOf(n, "comment") + ", and quit"
	}
	return "Approve the changes and quit"
}

// request decides the review as needing changes and quits. It needs a comment to say what
// to change. It is :request, which only dr --review offers.
func (m *model) request() tea.Cmd {
	if len(m.reported()) == 0 {
		m.warn("Leave a comment with c first: a change request says what to change")
		return nil
	}
	m.verdict = review.VerdictRequest
	return tea.Quit
}

// requestDoes says what :request does now.
func (m *model) requestDoes() string {
	n := len(m.reported())
	if n == 0 {
		return "Needs a comment first"
	}
	return "Request changes with " + countOf(n, "comment") + " and quit"
}

// reported returns the comments the report carries: those on lines of the diff now, in the
// order they were made, the ones a fix dr applied resolved after them.
func (m *model) reported() []review.ReportComment {
	var out []review.ReportComment
	add := func(c *comment, fixed bool) {
		if !c.placed() {
			return
		}
		from, to := m.commentRange(c)
		r := review.ReportComment{
			Path: c.Path, First: from, Last: to, Text: c.Text,
			Agent: c.Agent, Session: c.Session, Fixed: fixed,
		}
		for _, k := range c.picked() {
			l := m.lines[k]
			kind := " "
			switch {
			case l.Old == 0:
				kind = "+"
			case l.New == 0:
				kind = "-"
			}
			r.Lines = append(r.Lines, kind+l.Text)
		}
		out = append(out, r)
	}
	for _, c := range m.comments {
		add(c, false)
	}
	for _, c := range m.resolved {
		add(c, true)
	}
	return out
}

// Outcome is what the screen decided when it quit, as Run returns it for dr --review.
type Outcome struct {
	Verdict  review.Verdict
	Comments []review.ReportComment
}

// outcome returns what the screen decided: the verdict, none when it quit with q, and the
// comments the report carries.
func (m *model) outcome() Outcome {
	v := m.verdict
	if v == "" {
		v = review.VerdictNone
	}
	return Outcome{Verdict: v, Comments: m.reported()}
}
