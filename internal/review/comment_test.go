package review_test

import (
	"testing"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// span returns n lines that e added, the lines first to first+n-1 of the current file.
func span(e *agent.Edit, first, n int) []origin.Line {
	lines := make([]origin.Line, n)
	for i := range lines {
		lines[i] = origin.Line{Text: "x", New: first + i, Edit: e}
	}
	return lines
}

// around returns the lines 1 to n of the current file: the first and the last added by e,
// and the ones between them unchanged.
func around(e *agent.Edit, n int) []origin.Line {
	lines := span(e, 1, n)
	for i := 1; i < n-1; i++ {
		lines[i].Old, lines[i].Edit = i+1, nil
	}
	return lines
}

func TestCommenting(t *testing.T) {
	author := review.Target{
		Relation: review.Author, Ask: agent.Question{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5"},
		Base: c1.Time, Edit: c1,
	}
	for _, tc := range []struct {
		name   string
		lines  []origin.Line
		review []origin.Line // of every file
		want   review.Target
		why    string
	}{
		{
			// Deleted and unchanged lines go along as context.
			name:   "the author",
			lines:  []origin.Line{unchanged, deleted(c1), added(c1)},
			review: []origin.Line{deleted(c1), added(c1)},
			want:   author,
		},
		{
			name:   "the parent of a Claude subagent",
			lines:  []origin.Line{added(a1)},
			review: []origin.Line{added(c1), added(a1)},
			want: review.Target{
				Relation: review.Parent, Ask: agent.Question{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5"},
				Base: a1.Time, Edit: a1,
			},
		},
		{
			name:   "an author dr cannot fork that has no parent",
			lines:  []origin.Line{added(oNoLog)},
			review: []origin.Line{added(oNoLog)},
			why:    "dr cannot send a comment to omp o2: the session's log is not known",
		},
		{
			name:   "lines without an origin",
			lines:  []origin.Line{added(c1), added(nil)},
			review: []origin.Line{added(c1), added(nil)},
			why:    "Lines without an origin cannot take a comment. Fix them with e",
		},
		{
			name:   "two sessions",
			lines:  []origin.Line{added(c1), unchanged, added(x1)},
			review: []origin.Line{added(c1), added(x1)},
			why:    "The selection mixes sessions. Select lines from one session",
		},
		{
			name:   "unchanged lines",
			lines:  []origin.Line{unchanged, unchanged},
			review: []origin.Line{added(c1)},
			why:    "The selection has no changed lines",
		},
		{
			name:   "deleted lines only",
			lines:  []origin.Line{deleted(c1), deleted(c1)},
			review: []origin.Line{deleted(c1)},
			why:    "Deleted lines alone cannot take a comment",
		},
		{
			name:   "200 lines",
			lines:  span(c1, 7, 200),
			review: []origin.Line{added(c1)},
			want:   author,
		},
		{
			// The range runs from the first line of the current file to the last, so the
			// unchanged lines between them count too.
			name:   "201 lines",
			lines:  around(c1, 201),
			review: []origin.Line{added(c1)},
			why:    "A comment covers at most 200 lines",
		},
	} {
		// A session answering a question can take a comment, as nothing is sent.
		a := asking(tc.review, []string{"claude", "codex", "omp"}, [2]string{"claude", "c1"})
		got, why := a.Commenting(tc.lines)
		if got != tc.want || why != tc.why {
			t.Errorf("%s:\n got %+v, %q\nwant %+v, %q", tc.name, got, why, tc.want, tc.why)
		}
	}
}
