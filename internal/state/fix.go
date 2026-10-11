package state

import (
	"slices"
	"time"
)

// Request is a fix request :submit sent to a session: the pending comments that go to it,
// numbered from 1 in the order of Comments.
type Request struct {
	Time     time.Time `json:"time"`
	ID       string    `json:"id"`              // 16 hex digits
	Agent    string    `json:"agent"`           // of the session asked
	Session  string    `json:"session"`         // the session asked
	Relation string    `json:"relation"`        // how it relates to the lines: "author" or "parent"
	Comments []string  `json:"comments"`        // the IDs of the comments, in the order they are numbered
	Model    string    `json:"model,omitempty"` // the model the comments picked with ^, "" for the session's own
}

// Fix is what came back from a session for one comment of a request: new lines for the
// comment's range, a note that leaves the lines as they are, or why nothing usable came.
type Fix struct {
	Time    time.Time `json:"time"`
	ID      string    `json:"id"`      // 16 hex digits
	Request string    `json:"request"` // ID of the request
	Comment string    `json:"comment"` // ID of the comment
	Text    string    `json:"text"`    // the comment's text as sent
	Agent   string    `json:"agent"`
	Session string    `json:"session"` // the session asked
	Log     string    `json:"log"`     // that session's log
	Cwd     string    `json:"cwd"`     // where that session ran
	Fork    string    `json:"fork"`    // the fork that answered, "" when none did
	Model   string    `json:"model"`   // the model that answered, "" when not known
	Effort  string    `json:"effort"`  // the reasoning effort it was asked with, "" when not known
	TookMS  int64     `json:"took_ms"` // how long the session took to answer
	Path    string    `json:"path"`    // slash-separated, relative to the worktree root
	First   int       `json:"first"`   // the line of the current file where the comment's range starts, from 1
	Lines   []string  `json:"lines"`   // the comment's lines as sent, with any "\r"
	Hash    string    `json:"hash"`    // sha256 hex of the whole file as sent
	New     []string  `json:"new"`     // the lines to write in place of Lines; none deletes them
	Keep    bool      `json:"keep"`    // the answer leaves the lines as they are, and Note says why
	Note    string    `json:"note"`    // what the session said with the fix
	Error   string    `json:"error"`   // why no fix came, "" when one did
}

// Dropped is a fix x dropped, or that went because its comment was edited.
type Dropped struct {
	Time time.Time `json:"time"`
	Fix  string    `json:"fix"` // ID of the Fix record
}

func (Request) kind() string { return "request" }
func (Fix) kind() string     { return "fix" }
func (Dropped) kind() string { return "dropped" }

// valid reports whether r, read from a log, names a session and the comments sent to it.
func (r Request) valid() bool {
	return r.ID != "" && r.Agent != "" && r.Session != "" && len(r.Comments) > 0
}

// valid reports whether f, read from a log, says which comment of which request it is for,
// the session that was asked and where the comment's lines were.
func (f Fix) valid() bool {
	return f.ID != "" && f.Request != "" && f.Comment != "" && f.Agent != "" && f.Session != "" && f.Path != "" && f.First >= 1
}

// Taken is a fix with new lines that the person applied or dropped.
type Taken struct {
	Fix
	Applied bool // it was applied and is still in place; false when it was dropped or undone
}

// Request returns the request with id, and whether the records have it.
func (r Records) Request(id string) (Request, bool) {
	i := slices.IndexFunc(r.Requests, func(q Request) bool { return q.ID == id })
	if i < 0 {
		return Request{}, false
	}
	return r.Requests[i], true
}

// Resolved reports whether a fix dr applied for the comment with id is in place, so the
// comment is done with.
func (r Records) Resolved(id string) bool {
	return slices.ContainsFunc(r.InPlace(), func(a Applied) bool { return a.Comment == id })
}

// Shown returns the fix that shows under c, the comment as its newest record has it: the
// newest Fix record for it, when that came at or after c's record, which a fix for an
// older text of the comment did not, and was neither dropped nor applied.
func (r Records) Shown(c Comment) (Fix, bool) {
	for _, f := range slices.Backward(r.Fixes) {
		if f.Comment == c.ID {
			if f.Time.Before(c.Time) || r.dropped(f) || r.applied(f) {
				return Fix{}, false
			}
			return f, true
		}
	}
	return Fix{}, false
}

// Taken returns the fixes with new lines that the session of agent sent and that the
// person applied or dropped, in the order they came, for the next request to tell it.
func (r Records) Taken(agent, session string) []Taken {
	inPlace := r.InPlace()
	var taken []Taken
	for _, f := range r.Fixes {
		if f.Agent != agent || f.Session != session || f.Error != "" || f.Keep {
			continue
		}
		switch {
		case r.dropped(f):
			taken = append(taken, Taken{Fix: f})
		case r.applied(f):
			applied := slices.ContainsFunc(inPlace, func(a Applied) bool { return a.Comment == f.Comment && !a.Time.Before(f.Time) })
			taken = append(taken, Taken{Fix: f, Applied: applied})
		}
	}
	return taken
}

// dropped reports whether f was dropped.
func (r Records) dropped(f Fix) bool {
	return slices.ContainsFunc(r.Dropped, func(d Dropped) bool { return d.Fix == f.ID })
}

// applied reports whether f was applied: dr applied a fix for its comment at or after it
// came, which can only be f, as only the fix that shows under a comment is applied.
func (r Records) applied(f Fix) bool {
	return slices.ContainsFunc(r.Applied, func(a Applied) bool { return a.Comment == f.Comment && !a.Time.Before(f.Time) })
}
