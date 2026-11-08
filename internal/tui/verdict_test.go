package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
)

// quits reports whether cmd is tea.Quit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestReviewCommands(t *testing.T) {
	// Without --review, :approve and :request are not dr commands. With it, :request needs
	// a comment and quits with the request and the comments, each with the range in the
	// current file and the diff lines it is on; :approve quits with the approval. q quits
	// with no decision.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	if names := commandNames(m.commandRows()); !slices.Equal(names, []string{"submit", "undo"}) {
		t.Errorf("commands without --review: %q, want submit and undo", names)
	}
	if cmd := typeAndKeep(m, ":approve<enter>"); quits(cmd) || m.tone != toneErr {
		t.Errorf(":approve without --review: quit %t, tone %v; want an error and no quit", quits(cmd), m.tone)
	}

	m = askScreen(testFiles(), Saved{Review: true}, &fakeAsk{answer: answer})
	if names := commandNames(m.commandRows()); !slices.Equal(names, []string{"submit", "undo", "approve", "request"}) {
		t.Errorf("commands with --review: %q, want submit, undo, approve and request", names)
	}
	if cmd := typeAndKeep(m, ":request<enter>"); quits(cmd) || m.tone != toneWarn {
		t.Errorf(":request without a comment: quit %t, tone %v; want a warning and no quit", quits(cmd), m.tone)
	}
	if got := m.outcome(); got.Verdict != review.VerdictNone || len(got.Comments) != 0 {
		t.Errorf("outcome before a decision: %+v; want none and no comments", got)
	}
	typeKeys(m, "4jcuse errors.Is<enter>")
	cmd := typeAndKeep(m, ":request<enter>")
	if !quits(cmd) {
		t.Fatal(":request with a comment did not quit")
	}
	got := m.outcome()
	want := Outcome{Verdict: review.VerdictRequest, Comments: []review.ReportComment{{
		Path: "a.go", First: 2, Last: 2, Lines: []string{"+Changed 2"}, Text: "use errors.Is",
		Agent: claude1.Agent, Session: claude1.Session,
	}}}
	if got.Verdict != want.Verdict || len(got.Comments) != 1 || !sameComment(got.Comments[0], want.Comments[0]) {
		t.Errorf(":request outcome:\n%+v\nwant\n%+v", got, want)
	}

	m = askScreen(testFiles(), Saved{Review: true}, &fakeAsk{answer: answer})
	if cmd := typeAndKeep(m, ":approve<enter>"); !quits(cmd) || m.outcome().Verdict != review.VerdictApprove {
		t.Errorf(":approve: quit %t, verdict %q; want quit and approve", quits(cmd), m.outcome().Verdict)
	}
}

// commandNames returns the names of rows.
func commandNames(rows []command) []string {
	var names []string
	for _, c := range rows {
		names = append(names, c.name)
	}
	return names
}

// sameComment reports whether a and b are the same report comment.
func sameComment(a, b review.ReportComment) bool {
	return a.Path == b.Path && a.First == b.First && a.Last == b.Last && a.Text == b.Text &&
		a.Agent == b.Agent && a.Session == b.Session && a.Fixed == b.Fixed && slices.Equal(a.Lines, b.Lines)
}
