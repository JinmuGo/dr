// Package fix turns change comments into a fix request for the session that wrote their
// lines, reads the fixes out of the session's answer, and applies a fix to its file or
// reverts it. dr applies a fix by line number only: it never searches for text, and it
// refuses when the file is not exactly what it knows the file to be.
package fix

// Comment is a change comment as a fix request carries it.
type Comment struct {
	N     int      // its number in the request, from 1
	Path  string   // slash-separated, relative to the worktree root
	First int      // the line of the current file where the range starts, from 1
	Lines []string // the current file's lines of the range as sent, with any "\r"
	// Deleted are the deleted lines inside the range, sent as context only, in order.
	Deleted []Deleted
	Text    string // how to change the lines
}

// Deleted is a deleted line a comment's range shows between its current lines.
type Deleted struct {
	Before int // the index in Comment.Lines of the line it comes before, len(Lines) for after the last
	Text   string
}

// Change is a fix dr applied: Old at line First of the file at Path became New. It has the
// fields of state.Applied that say what changed.
type Change struct {
	Path   string   // slash-separated, relative to the worktree root
	First  int      // the line where Old started and New starts, from 1
	Old    []string // the lines the fix replaced, as they were in the file
	New    []string // the lines it wrote, as they are in the file
	Before string   // sha256 hex of the whole file before the fix
	After  string   // sha256 hex of the whole file after it
}
