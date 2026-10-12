package review

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JinmuGo/dr/internal/agent"
)

// NoOrigin explains a changed line that has no origin.
const NoOrigin = "Not in any session record. A person changed it, or a tool that leaves no record, like a shell command."

// Tag names the request behind e the way dr shows it, "claude c1a0 #2", or "claude c1a0
// fix" for a fix dr applied. short holds the short session ids from ShortIDs.
func Tag(e *agent.Edit, short map[string]string) string {
	if e.Fix != nil {
		return fmt.Sprintf("%s %s fix", e.Agent, short[e.Session])
	}
	return fmt.Sprintf("%s %s #%d", e.Agent, short[e.Session], e.Turn)
}

// ShortIDs returns, made visible, the shortest suffix of each id that tells it apart from
// the others: at least 4 characters, and as long for every id. It takes the end, not the
// start: OMP ids are UUIDv7, which start with a timestamp, so the starts of sessions from
// the same hour are the same.
func ShortIDs(ids []string) map[string]string {
	for n := 4; ; n++ {
		short := make(map[string]string, len(ids))
		taken := make(map[string]bool, len(ids))
		clash := false
		for _, id := range ids {
			if id == "" || short[id] != "" {
				continue
			}
			r := []rune(id)
			s := string(r[max(len(r)-n, 0):])
			clash = clash || taken[s]
			taken[s] = true
			short[id] = s
		}
		if !clash {
			for id, s := range short {
				short[id] = Visible(s)
			}
			return short
		}
	}
}

// Summary returns, made visible, the line of prompt that says what it asks: its first
// non-empty line that is neither a Markdown heading nor ends with a colon, or its first
// non-empty line when every line is one of those. OMP's subagent prompts start with
// "Complete assignment thoroughly:" and then "# Target", neither of which tells the task.
func Summary(prompt string) string {
	first := ""
	for l := range strings.Lines(prompt) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "#") && !strings.HasSuffix(l, ":") {
			return Visible(l)
		}
		if first == "" {
			first = l
		}
	}
	return Visible(first)
}

// Visible returns s with control characters other than tabs and newlines, and the
// characters that reorder text, written as escapes such as \x1b and \u202e. Text from the
// file or a session log then cannot drive the terminal or hide what it shows.
func Visible(s string) string {
	return escape(s, true)
}

// VisibleLine returns s as Visible does, with its tabs and newlines written as \t and \n
// too, as git quotes a path. It is for text drawn on one row whose tabs and line breaks are
// part of what it says: a path, which git lets hold both, or an error that may name one.
// Such text could otherwise break its row and pass for rows of its own. Code keeps its tabs
// through Visible.
func VisibleLine(s string) string {
	return escape(s, false)
}

// escape returns s with its control characters, but tabs and newlines when lines is set,
// and the characters that reorder text written as escapes.
func escape(s string, lines bool) string {
	var b strings.Builder
	start := 0 // start of the text not yet written to b
	for i, r := range s {
		switch {
		case lines && (r == '\t' || r == '\n'):
		case unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r):
			b.WriteString(s[start:i])
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1]) // without the quotes
			start = i + utf8.RuneLen(r)
		}
	}
	if start == 0 {
		return s
	}
	b.WriteString(s[start:])
	return b.String()
}
