package agent

import (
	"strconv"
	"strings"

	"github.com/JinmuGo/dr/internal/diff"
)

// hunk is one block of a patch. Each line starts with ' ' for a line that stays, '-' for a
// line that goes and '+' for a line that comes.
type hunk struct {
	at    int // index in the old file of the first line the hunk covers, or where it inserts
	lines []string
}

// patch is an edit given as hunks.
func patch(hunks []hunk) func(string) (string, bool) {
	return func(before string) (string, bool) {
		old := diff.Lines(before)
		var out []string
		next, shift := 0, 0
		for _, h := range hunks {
			var want []string
			for _, line := range h.lines {
				if line[0] != '+' {
					want = append(want, line[1:])
				}
			}
			// The line numbers are those of the file the agent saw. When a change outside
			// the logs moved the lines, the hunk still fits where its lines occur just once.
			at := h.at + shift
			if at < next || !linesAt(old, at, want) {
				at = find(old, next, want)
			}
			if at < 0 {
				return "", false
			}
			shift = at - h.at
			out = append(out, old[next:at]...)
			for _, line := range h.lines {
				switch line[0] {
				case ' ':
					out = append(out, line[1:])
					at++
				case '-':
					at++
				case '+':
					out = append(out, line[1:])
				}
			}
			next = at
		}
		out = append(out, old[next:]...)
		if len(out) == 0 {
			return "", true
		}
		after := strings.Join(out, "\n")
		if before == "" || strings.HasSuffix(before, "\n") {
			after += "\n"
		}
		return after, true
	}
}

func linesAt(old []string, at int, want []string) bool {
	if at < 0 || at+len(want) > len(old) {
		return false
	}
	for i, w := range want {
		if old[at+i] != w {
			return false
		}
	}
	return true
}

// find returns where want occurs in old from index from on, or -1 when it does not occur
// exactly once.
func find(old []string, from int, want []string) int {
	if len(want) == 0 {
		return -1
	}
	found := -1
	for at := from; at+len(want) <= len(old); at++ {
		if linesAt(old, at, want) {
			if found >= 0 {
				return -1
			}
			found = at
		}
	}
	return found
}

// unifiedHunks parses the hunks of a unified diff, the form Codex logs:
//
//	@@ -39,2 +39,3 @@
//	 func (l *Limiter) Allow() bool {
//	+	return l.AllowN(1)
//	 	l.mu.Lock()
func unifiedHunks(d string) ([]hunk, bool) {
	var hunks []hunk
	lines := strings.Split(d, "\n")
	for i := 0; i < len(lines); i++ {
		head, ok := strings.CutPrefix(lines[i], "@@ -")
		if !ok {
			continue
		}
		oldSpan, newSpan, ok := strings.Cut(head, " +")
		if !ok {
			return nil, false
		}
		oldStart, oldCount, ok1 := span(oldSpan)
		_, newCount, ok2 := span(newSpan)
		if !ok1 || !ok2 {
			return nil, false
		}
		h := hunk{at: oldStart - 1}
		if oldCount == 0 {
			h.at = oldStart // a hunk that only adds lines inserts after line oldStart
		}
		for oldCount > 0 || newCount > 0 {
			i++
			if i >= len(lines) {
				return nil, false
			}
			line := lines[i]
			if line == "" {
				line = " " // an empty line that stays
			}
			switch line[0] {
			case ' ':
				oldCount--
				newCount--
			case '-':
				oldCount--
			case '+':
				newCount--
			case '\\':
				continue // "\ No newline at end of file"
			default:
				return nil, false
			}
			if oldCount < 0 || newCount < 0 {
				return nil, false
			}
			h.lines = append(h.lines, line)
		}
		hunks = append(hunks, h)
	}
	return hunks, len(hunks) > 0
}

// span parses the "39,2" or "39" at the start of s.
func span(s string) (start, count int, ok bool) {
	end := 0
	for end < len(s) && (s[end] == ',' || '0' <= s[end] && s[end] <= '9') {
		end++
	}
	first, second, two := strings.Cut(s[:end], ",")
	start, err := strconv.Atoi(first)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if two {
		if count, err = strconv.Atoi(second); err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

// numberedHunks parses the diff OMP logs for an edit when it no longer keeps the whole
// file. " 12|text" and "-12|text" are line 12 of the old file, "+12|text" is line 12 of
// the new file, and an empty line separates hunks.
func numberedHunks(d string) ([]hunk, bool) {
	var hunks []hunk
	open := false // whether the last line belongs to the last hunk
	nextOld := 0  // the old line the last hunk expects next, 0 while it has only added lines
	added := 0    // lines added minus lines removed so far
	for _, line := range strings.Split(d, "\n") {
		if line == "" {
			open = false
			continue
		}
		bar := strings.IndexByte(line, '|')
		if bar < 2 {
			return nil, false
		}
		n, err := strconv.Atoi(strings.TrimSpace(line[1:bar]))
		if err != nil || n < 1 {
			return nil, false
		}
		op := line[0]
		switch op {
		case ' ', '-':
			switch {
			case !open || nextOld != 0 && n != nextOld:
				hunks = append(hunks, hunk{at: n - 1})
			case nextOld == 0:
				hunks[len(hunks)-1].at = n - 1 // the added lines go right before this line
			}
			nextOld = n + 1
			if op == '-' {
				added--
			}
		case '+':
			if !open {
				// Line n of the new file comes after old line n-1-added.
				hunks = append(hunks, hunk{at: n - 1 - added})
				nextOld = 0
			}
			added++
		default:
			return nil, false
		}
		open = true
		h := &hunks[len(hunks)-1]
		h.lines = append(h.lines, string(op)+line[bar+1:])
	}
	return hunks, len(hunks) > 0
}
