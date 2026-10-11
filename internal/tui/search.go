package tui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JinmuGo/dr/internal/review"
)

// search is the state of the search.
type search struct {
	open  bool       // the search input is open
	input string     // what is typed in it
	query string     // the last search
	hits  searchHits // where query is in the rows
}

// searchHits is where the search query is in the rows on the screen: the rows whose text
// holds it, as searchText gives the text, and how many matches come before each of them.
type searchHits struct {
	query  string // the query they are for
	ok     bool   // they are for the rows on the screen; place and relayout clear it
	rows   []int  // the rows that hold the query, from the top
	before []int  // how many matches the rows before each of rows hold
	total  int    // how many matches all the rows hold
}

// searchQuery returns what the search looks for now: what is typed while the search input
// is open, and the last query otherwise. Esc clears the last query.
func (m *model) searchQuery() string {
	if m.search.open {
		return m.search.input
	}
	return m.search.query
}

// found returns where the search query is in the rows, counted again when the query or the
// rows changed since the last count.
func (m *model) found() *searchHits {
	q := m.searchQuery()
	if m.search.hits.ok && m.search.hits.query == q {
		return &m.search.hits
	}
	h := searchHits{query: q, ok: true}
	if q != "" {
		fold := foldCase(q)
		for i := range m.rows {
			if n := len(matchesIn(m.searchText(i), q, fold)); n > 0 {
				h.rows = append(h.rows, i)
				h.before = append(h.before, h.total)
				h.total += n
			}
		}
	}
	m.search.hits = h
	return &m.search.hits
}

// current returns the index in h.rows of the row of the current match, the first row at or
// after row cursor that holds the query, going on from the top; -1 when no row holds it.
// The current match is the first in its row.
func (h *searchHits) current(cursor int) int {
	if len(h.rows) == 0 {
		return -1
	}
	j, _ := slices.BinarySearch(h.rows, cursor)
	if j == len(h.rows) {
		j = 0
	}
	return j
}

// currentRow returns the row of the current match, -1 for none.
func (m *model) currentRow() int {
	h := m.found()
	if j := h.current(m.cursor); j >= 0 {
		return h.rows[j]
	}
	return -1
}

// findHere moves the cursor to the current match of the last query, the one the search
// input lit while it was typed, or says the query is not found.
func (m *model) findHere() {
	if i := m.currentRow(); i >= 0 {
		m.jumpTo(i)
		return
	}
	m.notFound()
}

// find moves the cursor count times to the next row in the direction dir (1 or -1) whose
// text holds the last query, going on from the other end.
func (m *model) find(count, dir int) {
	if m.search.query == "" {
		return
	}
	h := m.found()
	if len(h.rows) == 0 {
		m.notFound()
		return
	}
	n := max(count, 1)
	j, at := slices.BinarySearch(h.rows, m.cursor)
	switch {
	case dir > 0 && at:
		j += n
	case dir > 0:
		j += n - 1 // h.rows[j] is already after the cursor
	default:
		j -= n
	}
	m.jumpTo(h.rows[(j%len(h.rows)+len(h.rows))%len(h.rows)])
}

// notFound says that no row holds the last query.
func (m *model) notFound() {
	m.fail("Not found: " + review.Visible(m.search.query))
}

// marks returns where the search query is in text, which the screen shows: the byte offsets
// of each match, as matchesIn gives them.
func (m *model) marks(text string) [][2]int {
	q := m.searchQuery()
	return matchesIn(text, q, foldCase(q))
}

// sideMarks returns where the search query is in pieces, the code of a side of a row, and
// which of those matches is the current one: the first when current is set, -1 otherwise.
func (m *model) sideMarks(pieces []piece, current bool) ([][2]int, int) {
	if m.searchQuery() == "" {
		return nil, -1
	}
	marks := m.marks(join(pieces))
	if current && marks != nil {
		return marks, 0
	}
	return marks, -1
}

// markSegs returns segs with the parts of their text in marks, byte offsets of the text of
// all of them, split off in the style of a search match, and marks[current] in that of the
// current match. It returns segs itself when marks is empty.
func (m *model) markSegs(segs []seg, marks [][2]int, current int) []seg {
	if len(marks) == 0 {
		return segs
	}
	out := make([]seg, 0, len(segs)+2*len(marks))
	at := 0 // where the text of s starts
	for _, s := range segs {
		end, pos := at+len(s.text), at
		for k, mark := range marks {
			from, to := max(mark[0], pos), min(mark[1], end)
			if from >= to {
				continue
			}
			if from > pos {
				out = append(out, seg{text: s.text[pos-at : from-at], st: s.st, cut: s.cut})
			}
			out = append(out, seg{text: s.text[from-at : to-at], st: m.theme.match(s.st, k == current), cut: s.cut})
			pos = to
		}
		if pos < end {
			out = append(out, seg{text: s.text[pos-at:], st: s.st, cut: s.cut})
		}
		at = end
	}
	return out
}

// foldCase reports whether the search for query ignores case: when it has no capitals.
func foldCase(query string) bool {
	return query == strings.ToLower(query)
}

// matchesIn returns where query is in text: the byte offsets of the start and the end of
// each match, from the left and not overlapping. fold ignores case; query is then lower
// case.
func matchesIn(text, query string, fold bool) [][2]int {
	if query == "" {
		return nil
	}
	var out [][2]int
	for at := 0; at < len(text); {
		if !fold {
			i := strings.Index(text[at:], query)
			if i < 0 {
				break
			}
			out = append(out, [2]int{at + i, at + i + len(query)})
			at += i + len(query)
			continue
		}
		if n, ok := foldedPrefix(text[at:], query); ok {
			out = append(out, [2]int{at, at + n})
			at += n
			continue
		}
		_, size := utf8.DecodeRuneInString(text[at:])
		at += size
	}
	return out
}

// foldedPrefix reports whether s starts with query, a lower case query, when case is
// ignored, and how many bytes of s that takes.
func foldedPrefix(s, query string) (int, bool) {
	n := 0
	for _, q := range query {
		r, size := utf8.DecodeRuneInString(s[n:])
		if size == 0 || unicode.ToLower(r) != q {
			return 0, false
		}
		n += size
	}
	return n, true
}
