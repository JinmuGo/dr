package fix

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Result is what an answer to a fix request holds for one comment.
type Result struct {
	Fixed bool     // whether the answer has new lines for the comment
	Lines []string // the new lines when Fixed, none to delete the comment's lines
	Note  string   // what the session said with the fix, or why it left the lines as they are
	// Problem is why the answer has no usable fix for the comment, "" when it has one or
	// leaves the lines as they are with a note.
	Problem string
}

// The problems Parse finds with the fix for one comment.
const (
	problemMissing   = "the answer has no fix for this comment"
	problemDuplicate = "the answer has more than one fix for this comment"
	problemShape     = "the answer's fix for this comment is not in the asked form"
	problemNoReason  = "the answer leaves the lines as they are and gives no reason"
)

// cutRunes is how much of an answer an error from Parse shows.
const cutRunes = 80

// entry is a fix in an answer to a fix request.
type entry struct {
	Comment  int       `json:"comment"`
	NewLines *[]string `json:"new_lines"` // nil when left out, to leave the lines as they are
	Note     string    `json:"note"`
}

// Parse reads the fixes out of text, a session's answer to the fix request for comments.
// The JSON object may stand alone, sit in a ```json fence or have prose around it.
// results[i] is for comments[i]. A string in new_lines that holds "\n" is split into lines.
//
// A comment the answer has no fix for, or more than one, gets a Problem of its own, and the
// fixes for numbers no comment has are left out; the other comments keep their fixes. An
// answer without a JSON object holding "fixes" is an error.
func Parse(text string, comments []Comment) ([]Result, error) {
	fixes, err := find(text)
	if err != nil {
		return nil, err
	}
	index := make(map[int]int, len(comments))
	for i, c := range comments {
		index[c.N] = i
	}
	results := make([]Result, len(comments))
	count := make([]int, len(comments))
	for _, raw := range fixes {
		var e entry
		shaped := json.Unmarshal(raw, &e) == nil
		if !shaped {
			var n struct {
				Comment int `json:"comment"`
			}
			_ = json.Unmarshal(raw, &n) // a fix without a number is for no comment
			e = entry{Comment: n.Comment}
		}
		i, ok := index[e.Comment]
		if !ok {
			continue
		}
		count[i]++
		switch {
		case !shaped:
			results[i] = Result{Problem: problemShape}
		case e.NewLines == nil && e.Note == "":
			results[i] = Result{Problem: problemNoReason}
		case e.NewLines == nil:
			results[i] = Result{Note: e.Note}
		default:
			results[i] = Result{Fixed: true, Lines: split(*e.NewLines), Note: e.Note}
		}
	}
	for i, n := range count {
		switch {
		case n == 0:
			results[i] = Result{Problem: problemMissing}
		case n > 1:
			results[i] = Result{Problem: problemDuplicate}
		}
	}
	return results, nil
}

// find returns the fixes of the first JSON object in text that has a "fixes" list.
func find(text string) ([]json.RawMessage, error) {
	object := false
	for i := strings.IndexByte(text, '{'); i >= 0; {
		var v map[string]json.RawMessage
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&v) == nil {
			object = true
			var fixes []json.RawMessage
			if raw, ok := v["fixes"]; ok && json.Unmarshal(raw, &fixes) == nil && fixes != nil {
				return fixes, nil
			}
		}
		next := strings.IndexByte(text[i+1:], '{')
		if next < 0 {
			break
		}
		i += 1 + next
	}
	if object {
		return nil, fmt.Errorf(`no "fixes" list in the answer's JSON: %q`, cut(text))
	}
	return nil, fmt.Errorf("no JSON object in the answer: %q", cut(text))
}

// split returns lines with each one that holds "\n" split into lines, and with the "\r"
// that ends a line left out. A "\n" that ends a string ends its last line and starts no
// other.
func split(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		for _, part := range strings.Split(strings.TrimSuffix(l, "\n"), "\n") {
			out = append(out, strings.TrimSuffix(part, "\r"))
		}
	}
	return out
}

// cut returns s on one line, cut to its first cutRunes characters.
func cut(s string) string {
	s = oneLine(s)
	if utf8.RuneCountInString(s) <= cutRunes {
		return s
	}
	return string([]rune(s)[:cutRunes]) + "…"
}
