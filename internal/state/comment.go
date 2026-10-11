package state

import (
	"slices"
	"time"
)

// Comment is a change comment left with c on lines of a file, for the session that wrote
// them to fix. Each add, edit and removal of a comment is a record of its own: a later
// record with the same ID replaces the earlier one, and one with no text removes it.
type Comment struct {
	Time    time.Time `json:"time"`
	ID      string    `json:"id"`      // 16 hex digits
	Agent   string    `json:"agent"`   // of the session the comment goes to
	Session string    `json:"session"` // the session the comment goes to
	// Relation is how that session relates to the lines: "author" when it wrote them, and
	// "parent" when it started the subagent that wrote them.
	Relation string `json:"relation"`
	Path     string `json:"path"` // of the file, as review.File has it
	// Lines are the diff lines from the first line selected to the last, the lines between
	// them too, so they follow each other in the diff.
	Lines []Line `json:"lines"`
	// Selected holds the indexes in Lines of the lines selected when they are not all of
	// them, as when a selection in the split view leaves out lines between its first and
	// its last. A record without it selected every line.
	Selected []int `json:"selected,omitempty"`
	// Before and After are up to two lines of the file's diff right before and after Lines,
	// so the comment finds its place again when the diff changes.
	Before []Line `json:"before"`
	After  []Line `json:"after"`
	Text   string `json:"text"`            // what to change; "" removes the comment
	Model  string `json:"model,omitempty"` // the model picked with ^ to fix it, "" for the session's own
}

func (Comment) kind() string { return "comment" }

// valid reports whether c, read from a log, can be a comment: it has an ID, a session to
// go to, a path and lines, and selects only lines it has.
func (c Comment) valid() bool {
	return c.ID != "" && c.Agent != "" && c.Session != "" && c.Path != "" && len(c.Lines) > 0 &&
		!slices.ContainsFunc(c.Selected, func(i int) bool { return i < 0 || i >= len(c.Lines) })
}

// Picked returns the indexes in c.Lines of the lines selected.
func (c Comment) Picked() []int {
	if len(c.Selected) > 0 {
		return c.Selected
	}
	picked := make([]int, len(c.Lines))
	for i := range picked {
		picked[i] = i
	}
	return picked
}

// CurrentComments returns the comments as their newest records have them, in the order the
// comments were first made, without the ones removed.
func (r Records) CurrentComments() []Comment {
	var comments []Comment
	at := make(map[string]int) // the index of each comment in comments
	for _, c := range r.Comments {
		if i, ok := at[c.ID]; ok {
			comments[i] = c
			continue
		}
		at[c.ID] = len(comments)
		comments = append(comments, c)
	}
	return slices.DeleteFunc(comments, func(c Comment) bool { return c.Text == "" })
}
