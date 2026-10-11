package state

import "time"

// Applied is a fix dr applied: the lines a session sent back for a comment, written in place
// of the lines of the comment's range. The record says what the file held before and after,
// so :undo can revert the fix and the fixed lines take the fix as their origin.
type Applied struct {
	Time    time.Time `json:"time"`
	ID      string    `json:"id"`      // 16 hex digits
	Comment string    `json:"comment"` // ID of the comment the fix was for
	Text    string    `json:"text"`    // the comment's text
	Note    string    `json:"note"`    // what the session said with the fix, "" for nothing
	Agent   string    `json:"agent"`
	Session string    `json:"session"` // the session the comment went to
	Log     string    `json:"log"`     // that session's log
	Cwd     string    `json:"cwd"`     // where that session ran
	Fork    string    `json:"fork"`    // the fork that sent the fix
	Model   string    `json:"model"`   // the model that sent it, "" when not known
	Effort  string    `json:"effort"`  // the reasoning effort it was asked with, "" when not known
	Path    string    `json:"path"`    // slash-separated, relative to the worktree root
	First   int       `json:"first"`   // the line where Old started and New starts, from 1
	Old     []string  `json:"old"`     // the lines the fix replaced
	New     []string  `json:"new"`     // the lines it wrote
	Before  string    `json:"before"`  // sha256 hex of the whole file before the fix
	After   string    `json:"after"`   // sha256 hex of the whole file after it
}

// Undone is a fix :undo reverted.
type Undone struct {
	Time    time.Time `json:"time"`
	Applied string    `json:"applied"` // ID of the Applied record :undo reverted
}

func (Applied) kind() string { return "applied" }
func (Undone) kind() string  { return "undone" }

// valid reports whether a holds what dr needs to tell the fix's origin and to undo it.
func (a Applied) valid() bool {
	return a.ID != "" && a.Agent != "" && a.Session != "" && a.Path != "" && a.First >= 1
}

// InPlace returns the fixes dr applied that no Undone record reverted, in the order it
// applied them.
func (r Records) InPlace() []Applied {
	undone := make(map[string]bool, len(r.Undone))
	for _, u := range r.Undone {
		undone[u.Applied] = true
	}
	var fixes []Applied
	for _, a := range r.Applied {
		if !undone[a.ID] {
			fixes = append(fixes, a)
		}
	}
	return fixes
}
