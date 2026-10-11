package tui

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

// isSelected reports whether row i is part of the selection.
func (m *model) isSelected(i int) bool {
	return m.selecting && m.rows[i].kind == lineRow && i >= min(m.anchor, m.cursor) && i <= max(m.anchor, m.cursor)
}
