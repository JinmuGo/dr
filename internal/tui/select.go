package tui

import (
	"slices"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
)

// startSelection starts a selection of lines at the cursor, as vim's V does. Moves then
// extend it, but not out of the file it started in. A header is not a line, so the
// selection starts at the first line after it; a file that shows no lines has nothing to
// select.
func (m *model) startSelection() {
	first, last := m.lineRows(m.rows[m.cursor].file)
	if first < 0 {
		m.warn("This file has no lines to select")
		return
	}
	m.cursor = max(m.cursor, first)
	for m.rows[m.cursor].kind != lineRow {
		m.cursor++ // past a hunk header, which always has lines after it
	}
	m.selecting, m.anchor, m.selectFirst, m.selectLast = true, m.cursor, first, last
}

// lineRows returns the first and the last row of file f on the screen that shows lines, or
// -1 and -1 when none does.
func (m *model) lineRows(f int) (first, last int) {
	first, last = -1, -1
	for i := m.headRow(f) + 1; i < len(m.rows) && m.rows[i].kind != fileRow; i++ {
		if m.rows[i].kind == lineRow {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	return first, last
}

// selectedRows returns the rows of the selection that show lines, from the top. Headers
// between them are not part of it.
func (m *model) selectedRows() []int {
	var rows []int
	for i := min(m.anchor, m.cursor); i <= max(m.anchor, m.cursor); i++ {
		if m.rows[i].kind == lineRow {
			rows = append(rows, i)
		}
	}
	return rows
}

// selectedLines returns the lines of model.lines on the rows of the selection, from the top,
// each once: a line on both sides of a row of the split view is one line.
func (m *model) selectedLines() []int {
	var lines []int
	for _, i := range m.selectedRows() {
		for _, k := range []int{m.rows[i].left, m.rows[i].right} {
			if k >= 0 && !slices.Contains(lines, k) {
				lines = append(lines, k)
			}
		}
	}
	return lines
}

// sessionsOf returns the sessions that wrote the changed lines among lines, lines of
// model.lines, by their tags in the order they come, with the edit of the first changed line
// that has an origin; and whether a changed line among them has none.
func (m *model) sessionsOf(lines []int) (who []string, first *agent.Edit, unknown bool) {
	for _, k := range lines {
		l := m.lines[k]
		switch {
		case l.Old != 0 && l.New != 0:
			continue // unchanged
		case l.Edit == nil:
			unknown = true
			continue
		}
		if first == nil {
			first = l.Edit
		}
		if w := review.Visible(review.Who(l.Edit.Agent, l.Edit.Session, m.short)); !slices.Contains(who, w) {
			who = append(who, w)
		}
	}
	return who, first, unknown
}

// mixed reports whether the changed lines of the selection mix sessions, so that a and c
// refuse it.
func (m *model) mixed() bool {
	who, _, _ := m.sessionsOf(m.selectedLines())
	return len(who) > 1
}

// isSelected reports whether row i is part of the selection.
func (m *model) isSelected(i int) bool {
	return m.selecting && m.rows[i].kind == lineRow && i >= min(m.anchor, m.cursor) && i <= max(m.anchor, m.cursor)
}
