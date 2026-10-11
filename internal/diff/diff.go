// Package diff matches the lines of two versions of a file, and the words of two lines.
package diff

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxCells bounds the table that matches two parts of files line by line: 16M cells, 64MB.
// Bigger parts are split at lines that occur once in each.
const maxCells = 1 << 24

// Lines splits text into lines without their newlines.
func Lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// Match finds a common subsequence of a and b, a longest one unless their changed part is
// too big for the table. It returns, for each line of b, the index of the line of a it
// matches, or -1 when the line is new in b.
func Match(a, b []string) []int {
	match := make([]int, len(b))
	for j := range match {
		match[j] = -1
	}
	align(match, a, b, 0)
	return match
}

// Words compares two lines word by word. It returns the byte ranges of a and of b that hold
// the words a common subsequence of their words leaves out, in order and merged where they
// touch. A word is a run of letters, digits and underscores, a run of spaces, or any other
// single character. Spaces left between two changed words count as changed, so a changed
// phrase shows as one part.
func Words(a, b string) (aParts, bParts [][2]int) {
	aWords, bWords := words(a), words(b)
	match := Match(aWords, bWords)
	aKept := make([]bool, len(aWords))
	bKept := make([]bool, len(bWords))
	for j, i := range match {
		if i >= 0 {
			aKept[i], bKept[j] = true, true
		}
	}
	return parts(aWords, aKept), parts(bWords, bKept)
}

// words splits s into words, as Words defines them.
func words(s string) []string {
	var out []string
	for start := 0; start < len(s); {
		r, size := utf8.DecodeRuneInString(s[start:])
		end := start + size
		if class := wordClass(r); class != 0 {
			for end < len(s) {
				r, size := utf8.DecodeRuneInString(s[end:])
				if wordClass(r) != class {
					break
				}
				end += size
			}
		}
		out = append(out, s[start:end])
		start = end
	}
	return out
}

// wordClass returns 1 for a character of a word, 2 for a space and 0 for any other
// character, which is a word by itself.
func wordClass(r rune) int {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	case unicode.IsSpace(r):
		return 2
	}
	return 0
}

// parts returns the byte ranges of the words that are not kept, merged where they touch or
// only spaces lie between them.
func parts(words []string, kept []bool) [][2]int {
	var out [][2]int
	at := 0
	for i, w := range words {
		end := at + len(w)
		gap := kept[i] && i > 0 && i < len(words)-1 && !kept[i-1] && !kept[i+1] && strings.TrimSpace(w) == ""
		if !kept[i] || gap {
			if n := len(out); n > 0 && out[n-1][1] == at {
				out[n-1][1] = end
			} else {
				out = append(out, [2]int{at, end})
			}
		}
		at = end
	}
	return out
}

// align matches a and b into match, which has an entry for each line of b. The index of
// a[0] is offset.
func align(match []int, a, b []string, offset int) {
	// Most edits touch a small part of a file, so match the common start and end first.
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		match[start] = offset + start
		start++
	}
	end := 0
	for end < len(a)-start && end < len(b)-start && a[len(a)-1-end] == b[len(b)-1-end] {
		match[len(b)-1-end] = offset + len(a) - 1 - end
		end++
	}
	a, b, match = a[start:len(a)-end], b[start:len(b)-end], match[start:len(b)-end]
	offset += start
	switch {
	case len(a) == 0 || len(b) == 0:
	case len(a)*len(b) <= maxCells:
		lcs(match, a, b, offset)
	default:
		patience(match, a, b, offset)
	}
}

// lcs matches a and b along a longest common subsequence.
func lcs(match []int, a, b []string, offset int) {
	// table[i*width+j] is the length of a longest common subsequence of a[i:] and b[j:].
	width := len(b) + 1
	table := make([]int32, (len(a)+1)*width)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i*width+j] = table[(i+1)*width+j+1] + 1
			} else {
				table[i*width+j] = max(table[(i+1)*width+j], table[i*width+j+1])
			}
		}
	}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			match[j] = offset + i
			i++
			j++
		case table[(i+1)*width+j] >= table[i*width+j+1]:
			i++
		default:
			j++
		}
	}
}

// patience matches the lines that occur once in a and once in b, as many as keep their
// order, and aligns the parts between them. Without such lines a and b stay unmatched.
func patience(match []int, a, b []string, offset int) {
	type count struct{ a, b, i int } // times a line occurs in a and in b, its index in a
	counts := make(map[string]count, len(a))
	for i, t := range a {
		c := counts[t]
		c.a++
		c.i = i
		counts[t] = c
	}
	for _, t := range b {
		if c, ok := counts[t]; ok {
			c.b++
			counts[t] = c
		}
	}
	type anchor struct{ i, j int } // indexes in a and b
	var anchors []anchor           // in the order of b
	for j, t := range b {
		if c := counts[t]; c.a == 1 && c.b == 1 {
			anchors = append(anchors, anchor{c.i, j})
		}
	}
	if len(anchors) == 0 {
		return
	}

	// Match the longest chain of anchors that is in order in a too. ends[n] is the last
	// anchor of the chain of n+1 anchors that ends earliest in a, and before[x] is the
	// anchor before x in its chain.
	var ends []int
	before := make([]int, len(anchors))
	for x, p := range anchors {
		n := sort.Search(len(ends), func(k int) bool { return anchors[ends[k]].i > p.i })
		before[x] = -1
		if n > 0 {
			before[x] = ends[n-1]
		}
		if n == len(ends) {
			ends = append(ends, x)
		} else {
			ends[n] = x
		}
	}
	i, j := len(a), len(b) // end of the part after the anchor
	for x := ends[len(ends)-1]; x >= 0; x = before[x] {
		p := anchors[x]
		align(match[p.j+1:j], a[p.i+1:i], b[p.j+1:j], offset+p.i+1)
		match[p.j] = offset + p.i
		i, j = p.i, p.j
	}
	align(match[:j], a[:i], b[:j], offset)
}
