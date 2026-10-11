package review

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/JinmuGo/dr/internal/origin"
)

// Hunk is a run of changed lines and the unchanged lines around it, as in a unified diff.
type Hunk struct {
	OldStart, OldCount, NewStart, NewCount int // as in "@@ -OldStart,OldCount +NewStart,NewCount @@"
	Lines                                  []origin.Line
}

// Hunks returns the runs of changes in lines, each with up to context unchanged lines
// before and after it. Runs whose context lines touch or overlap make one hunk.
func Hunks(lines []origin.Line, context int) []Hunk {
	var hunks []Hunk
	start, last := 0, -1 // first line of the hunk being built and its last changed line
	for i, l := range lines {
		if l.Old > 0 && l.New > 0 {
			continue
		}
		if last >= 0 && i-last-1 > 2*context {
			hunks = append(hunks, hunk(lines, start, last+context+1))
			last = -1
		}
		if last < 0 {
			start = max(i-context, 0)
		}
		last = i
	}
	if last >= 0 {
		hunks = append(hunks, hunk(lines, start, last+context+1))
	}
	return hunks
}

// hunk returns the hunk of lines[start:end], with end cut to the last line. A side with no
// lines in the hunk starts at the line before it, as unified diffs number it.
func hunk(lines []origin.Line, start, end int) Hunk {
	h := Hunk{Lines: lines[start:min(end, len(lines))]}
	for _, l := range h.Lines {
		if l.Old > 0 {
			if h.OldCount == 0 {
				h.OldStart = l.Old
			}
			h.OldCount++
		}
		if l.New > 0 {
			if h.NewCount == 0 {
				h.NewStart = l.New
			}
			h.NewCount++
		}
	}
	for i := start - 1; h.OldCount == 0 && i >= 0; i-- {
		if lines[i].Old > 0 {
			h.OldStart = lines[i].Old
			break
		}
	}
	for i := start - 1; h.NewCount == 0 && i >= 0; i-- {
		if lines[i].New > 0 {
			h.NewStart = lines[i].New
			break
		}
	}
	return h
}

// Hash returns the hash of the diff f shows with context unchanged lines around each change:
// sha256, in hex, of its status, why it has no line diff, and the kind and the text of each
// line of its hunks. Line numbers are left out, so a diff that only moved, as after a
// rebase on changes elsewhere in the file, keeps its hash.
func (f File) Hash(context int) string {
	h := sha256.New()
	buf := []byte{f.Status, '\n'}
	buf = append(buf, f.NoDiff...)
	h.Write(append(buf, '\n'))
	for _, hk := range Hunks(f.Lines, context) {
		h.Write([]byte("@@\n"))
		for _, l := range hk.Lines {
			kind := byte(' ')
			switch {
			case l.Old == 0:
				kind = '+'
			case l.New == 0:
				kind = '-'
			}
			buf = append(append(append(buf[:0], kind), l.Text...), '\n')
			h.Write(buf)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
