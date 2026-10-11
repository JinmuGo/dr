package fix

import (
	"strconv"
	"strings"
)

// Earlier is a fix from an earlier request to the same session, and what the person did
// with it.
type Earlier struct {
	Path        string // slash-separated, relative to the worktree root
	First, Last int    // the lines of the range it was for, from 1
	Text        string // the comment's text
	Applied     bool   // whether it was applied; false when it was dropped
}

// answerShape is the JSON a session answers a fix request with.
const answerShape = `{"fixes":[{"comment":N,"new_lines":[...],"note":""}]}`

// Prompt returns the fix request for comments, all of them for one session. earlier are
// the fixes the session sent for its earlier requests, so it knows which were taken.
//
// It quotes every line of a comment's range in order, > for current lines and - for
// deleted ones, the way a question quotes lines. Current lines are quoted without their
// "\r", which Apply puts back. The comment's text follows on "Request: ", each further line
// of it that is not blank indented by two spaces, so the numbered comments stay apart. An
// earlier fix's text stays on one line.
func Prompt(comments []Comment, earlier []Earlier) string {
	var b strings.Builder
	b.WriteString("[dr] Fix request. Do not change any files, return only the changed lines as JSON.\n")
	if len(earlier) > 0 {
		b.WriteString("Earlier fixes in this conversation and what the person did with them:\n")
		for _, e := range earlier {
			outcome := "dropped"
			if e.Applied {
				outcome = "applied"
			}
			b.WriteString("* " + e.Path + ":" + lineRange(e.First, e.Last) + ", " + outcome + ": " + oneLine(e.Text) + "\n")
		}
	}
	for _, c := range comments {
		b.WriteString(strconv.Itoa(c.N) + ". " + c.Path + ":" + lineRange(c.First, c.First+len(c.Lines)-1))
		if len(c.Deleted) > 0 {
			b.WriteString(". Lines marked - were deleted.")
		}
		b.WriteString("\n")
		deleted := c.Deleted
		for i, l := range c.Lines {
			for len(deleted) > 0 && deleted[0].Before <= i {
				b.WriteString("-" + strings.TrimSuffix(deleted[0].Text, "\r") + "\n")
				deleted = deleted[1:]
			}
			b.WriteString(">" + strings.TrimSuffix(l, "\r") + "\n")
		}
		for _, d := range deleted {
			b.WriteString("-" + strings.TrimSuffix(d.Text, "\r") + "\n")
		}
		request, more, _ := strings.Cut(c.Text, "\n")
		b.WriteString("Request: " + request + "\n")
		if more != "" {
			for _, l := range strings.Split(more, "\n") {
				if l != "" {
					l = "  " + l
				}
				b.WriteString(l + "\n")
			}
		}
	}
	b.WriteString("Change only the lines of each comment. If something else needs to change, do not change it; write it in note.\n")
	b.WriteString("Answer with this JSON only, with one entry per comment:\n")
	b.WriteString(answerShape + "\n")
	b.WriteString("new_lines is the complete replacement for the comment's lines, with their exact indentation; tabs stay tabs. " +
		"[] deletes the lines. To leave the lines as they are, leave out new_lines and say why in note.")
	return b.String()
}

// lineRange writes the lines first to last as "first-last".
func lineRange(first, last int) string {
	return strconv.Itoa(first) + "-" + strconv.Itoa(last)
}

// oneLine returns s on one line: each run of white space, line breaks too, becomes a space.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
