package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Log is log.jsonl in the state directory: the questions asked with a, their answers and the
// forks and new sessions dr made to ask them, the comments left with c, the fix requests
// :submit sent and what came back, and the fixes dropped, applied and undone, one JSON record
// a line in the order they happened. dr appends to it as it goes and reads it from the start
// when it opens.
type Log struct {
	path string
	mu   sync.Mutex // one append at a time, as commands that append run at the same time
}

// NewLog returns the log in the file at path.
func NewLog(path string) *Log {
	return &Log{path: path}
}

// Record is a line of the log: a Fork, a Question, an Answer, a Comment, a Request, a Fix,
// a Dropped, an Applied or an Undone.
type Record interface {
	kind() string
}

// Fork is a fork dr made of a session to ask it questions, or a new session dr started to
// ask an agent about lines. Questions to the session go on in its fork until the session
// has newer edits, and questions for a new session of the agent go on in the new session.
type Fork struct {
	Time    time.Time `json:"time"`
	Agent   string    `json:"agent"`
	Session string    `json:"session"` // the session the fork was made of, "" for a new session
	Fork    string    `json:"fork"`    // the fork, or the new session
	// Base is the time of the newest edit in the review that made the fork that the session
	// knew of: one of its own, or of a subagent it started. The fork does not know of the
	// session's edits after it. A new session knows of no edit, and its base is zero.
	Base time.Time `json:"base"`
}

// Question is a question asked with a about lines of a file.
type Question struct {
	Time    time.Time `json:"time"`
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`   // the agent asked
	Session string    `json:"session"` // the session asked, "" for a new session
	// Relation is how the session asked relates to the lines: "author" when it wrote them,
	// "parent" when it started the subagent that wrote them, "editor" when no record says who
	// wrote them and it edited the file, and "new" for a new session. A record without it
	// asked the author.
	Relation string `json:"relation"`
	Path     string `json:"path"` // of the file, as review.File has it
	// Lines are the diff lines from the first line asked about to the last, the lines
	// between them too, so they follow each other in the diff.
	Lines []Line `json:"lines"`
	// Selected holds the indexes in Lines of the lines asked about when they are not all of
	// them, as when a selection in the split view leaves out lines between its first and its
	// last. A record without it asked about every line.
	Selected []int `json:"selected,omitempty"`
	// Before and After are up to two lines of the file's diff right before and after
	// Lines, so the question finds its place again when the diff changes.
	Before []Line `json:"before"`
	After  []Line `json:"after"`
	Text   string `json:"text"`            // the question as sent, without what dr adds to every question
	Model  string `json:"model,omitempty"` // the model picked with ^ to ask it, "" for the session's own
}

// Asked returns the indexes in q.Lines of the lines asked about.
func (q Question) Asked() []int {
	if len(q.Selected) > 0 {
		return q.Selected
	}
	asked := make([]int, len(q.Lines))
	for i := range asked {
		asked[i] = i
	}
	return asked
}

// Line is a line of a diff.
type Line struct {
	Kind string `json:"kind"` // " " unchanged, "+" added or "-" deleted
	Text string `json:"text"`
}

// Answer is the answer to a question, or why there is none.
type Answer struct {
	Time     time.Time `json:"time"`
	Question string    `json:"question"` // the ID of the question
	Fork     string    `json:"fork"`     // the fork that answered
	Model    string    `json:"model"`    // the model that answered, "" when not known
	TookMS   int64     `json:"took_ms"`
	Text     string    `json:"text"`
	Error    string    `json:"error"` // why there is no answer, "" when there is one
}

func (Fork) kind() string     { return "fork" }
func (Question) kind() string { return "question" }
func (Answer) kind() string   { return "answer" }

// Append writes r at the end of the log as a line of its own, making the state directory
// when it is missing.
func (l *Log) Append(r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	// Each record marshals to an object; its type goes first.
	line := fmt.Appendf(nil, `{"type":%q,%s`, r.kind(), data[1:])
	line = append(line, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(line)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Records are the records of a log, read back.
type Records struct {
	Forks     []Fork
	Questions []Question
	Answers   map[string]Answer // by the ID of their question
	Comments  []Comment         // in the order they were written, a comment's newest record last
	Requests  []Request         // the fix requests :submit sent, in the order it sent them
	Fixes     []Fix             // what came back for the comments of requests, in the order it came
	Dropped   []Dropped         // the fixes dropped, in the order they were dropped
	Applied   []Applied         // the fixes dr applied, in the order it applied them
	Undone    []Undone          // the fixes :undo reverted, in the order it reverted them
}

// Read returns the records of the log. A missing file has none. A line that is not a
// record is left out and counted in bad; a record of a type this dr does not know is left
// out without a count, as a newer dr may write it.
func (l *Log) Read() (records Records, bad int, err error) {
	records.Answers = make(map[string]Answer)
	data, err := os.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return records, 0, nil
	}
	if err != nil {
		return records, 0, err
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &head) != nil {
			bad++
			continue
		}
		ok := true
		switch head.Type {
		case "fork":
			var f Fork
			ok = json.Unmarshal(line, &f) == nil && f.Agent != "" && f.Fork != ""
			if ok {
				records.Forks = append(records.Forks, f)
			}
		case "question":
			var q Question
			ok = json.Unmarshal(line, &q) == nil && q.ID != "" && (q.Session != "" || q.Relation == "new") && q.Path != "" && len(q.Lines) > 0 &&
				!slices.ContainsFunc(q.Selected, func(i int) bool { return i < 0 || i >= len(q.Lines) })
			if ok {
				records.Questions = append(records.Questions, q)
			}
		case "answer":
			var a Answer
			ok = json.Unmarshal(line, &a) == nil && a.Question != ""
			if ok {
				records.Answers[a.Question] = a
			}
		case "comment":
			var c Comment
			ok = json.Unmarshal(line, &c) == nil && c.valid()
			if ok {
				records.Comments = append(records.Comments, c)
			}
		case "request":
			var r Request
			ok = json.Unmarshal(line, &r) == nil && r.valid()
			if ok {
				records.Requests = append(records.Requests, r)
			}
		case "fix":
			var f Fix
			ok = json.Unmarshal(line, &f) == nil && f.valid()
			if ok {
				records.Fixes = append(records.Fixes, f)
			}
		case "dropped":
			var d Dropped
			ok = json.Unmarshal(line, &d) == nil && d.Fix != ""
			if ok {
				records.Dropped = append(records.Dropped, d)
			}
		case "applied":
			var a Applied
			ok = json.Unmarshal(line, &a) == nil && a.valid()
			if ok {
				records.Applied = append(records.Applied, a)
			}
		case "undone":
			var u Undone
			ok = json.Unmarshal(line, &u) == nil && u.Applied != ""
			if ok {
				records.Undone = append(records.Undone, u)
			}
		}
		if !ok {
			bad++
		}
	}
	return records, bad, nil
}

// Fork returns the fork to go on asking the session of agent in: the newest fork made of
// it, unless newest, the time of the newest edit in this review the session knows of, is
// after the fork's base. The session's newer work is not in such a fork, so it returns ""
// then, and when no fork was made, to make a new one. For session "", a new session of the
// agent, it returns the newest new session, which knows no edit to be newer than.
func (r Records) Fork(agent, session string, newest time.Time) string {
	for _, f := range slices.Backward(r.Forks) {
		if f.Agent == agent && f.Session == session {
			if session != "" && newest.After(f.Base) {
				return ""
			}
			return f.Fork
		}
	}
	return ""
}

// Place finds the lines of a question or a comment again in lines, the diff lines of its
// file now, and returns the index of the first of anchored. It looks for, in this order,
// before, anchored and after; anchored and after; before and anchored; and anchored alone,
// following each other, and takes the first of these found exactly once. So a record keeps
// its place when a line next to it changes, as when a fix of a comment on the next line is
// applied. Empty anchored lines are only looked for with all of before and after. When no
// run is found exactly once, the code changed too much to tell where they belong, and ok
// is false.
func Place(lines, before, anchored, after []Line) (at int, ok bool) {
	if at, ok := placeRun(lines, slices.Concat(before, anchored, after), len(before)); ok || len(anchored) == 0 {
		return at, ok
	}
	if at, ok := placeRun(lines, slices.Concat(anchored, after), 0); ok {
		return at, true
	}
	if at, ok := placeRun(lines, slices.Concat(before, anchored), len(before)); ok {
		return at, true
	}
	return placeRun(lines, anchored, 0)
}

// placeRun returns the index of run in lines plus offset when run is there exactly once.
func placeRun(lines, run []Line, offset int) (at int, ok bool) {
	found := 0
	for i := 0; i+len(run) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(run)], run) {
			at, found = i+offset, found+1
		}
	}
	return at, found == 1
}
