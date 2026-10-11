package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// treeRow is a row of the file tree: a directory, or one of model.files.
type treeRow struct {
	name  string // the file name, or the directory path below the directory row above it
	depth int
	file  int // index in model.files, -1 for a directory
}

// tree lays out files the way GitHub's file tree does, under their directories, in the
// order of the diff. A directory that holds nothing but one directory shares a row with it.
func tree(files []file) []treeRow {
	children := make(map[string]map[string]bool) // names in each directory
	for _, f := range files {
		parts := strings.Split(f.path, "/")
		for k := range parts {
			dir := strings.Join(parts[:k], "/")
			if children[dir] == nil {
				children[dir] = make(map[string]bool)
			}
			children[dir][parts[k]] = true
		}
	}
	var rows []treeRow
	depths := map[string]int{"": 0} // depth of the rows in each directory that has a row
	var last []string               // directories of the file before
	for i, f := range files {
		parts := strings.Split(f.path, "/")
		dirs := parts[:len(parts)-1]
		k := 0
		for k < len(dirs) && k < len(last) && dirs[k] == last[k] {
			k++
		}
		for k < len(dirs) {
			parent, start := strings.Join(dirs[:k], "/"), k
			for k+1 < len(dirs) && len(children[strings.Join(dirs[:k+1], "/")]) == 1 {
				k++
			}
			rows = append(rows, treeRow{name: strings.Join(dirs[start:k+1], "/"), depth: depths[parent], file: -1})
			depths[strings.Join(dirs[:k+1], "/")] = depths[parent] + 1
			k++
		}
		rows = append(rows, treeRow{name: parts[len(parts)-1], depth: depths[strings.Join(dirs, "/")], file: i})
		last = dirs
	}
	return rows
}

// treeWidth returns the width the file tree needs to show every row whole: a file with its
// status, the count of its comments and, when it is viewed, a check at the right edge.
func (m *model) treeWidth() int {
	w := ansi.StringWidth(treeTitle(len(m.files), len(m.files))) + 2
	for _, r := range m.tree {
		if r.file < 0 {
			w = max(w, 4+2*r.depth+ansi.StringWidth(r.name)+1)
		} else {
			w = max(w, 4+2*r.depth+ansi.StringWidth(r.name)+3+m.countWidth(r.file))
		}
	}
	return w
}

// countWidth returns the width of the count of file i's comments in the file tree, with the
// space before it, and 0 when it has none.
func (m *model) countWidth(i int) int {
	if spans := m.commentSpans(m.files[i].key); spans != nil {
		return 1 + spansWidth(spans)
	}
	return 0
}

// treeTitle returns the title of the file tree, how many of files are viewed as GitHub
// counts them.
func treeTitle(viewed, files int) string {
	return fmt.Sprintf("%d / %d files viewed", viewed, files)
}

// treeTop returns the first row of the file tree on a screen h lines high: the file under
// the cursor stays in the middle while the tree is longer than the screen.
func (m *model) treeTop(h int) int {
	current := m.rows[m.cursor].file
	at := slices.IndexFunc(m.tree, func(r treeRow) bool { return r.file == current })
	body := h - 1
	return max(min(at-body/2, len(m.tree)-body), 0)
}

// fileTree draws the file tree, w cells wide and h rows high: a title band, then the rows,
// with the file under the cursor marked and on the screen.
func (m *model) fileTree(w, h int) []string {
	viewed := 0
	for _, f := range m.files {
		if f.viewed {
			viewed++
		}
	}
	lines := make([]string, 0, h)
	lines = append(lines, m.band([]span{{text: treeTitle(viewed, len(m.files)), color: m.theme.text, bold: true}}, nil, w))
	current := m.rows[m.cursor].file
	top := m.treeTop(h)
	for y := range h - 1 {
		if i := top + y; i < len(m.tree) {
			lines = append(lines, m.treeLine(m.tree[i], w, m.tree[i].file == current))
		} else {
			lines = append(lines, strings.Repeat(" ", w))
		}
	}
	return lines
}

// treeLine draws a row of the file tree, w cells wide, as GitHub's file tree does: a
// directory with a fold mark, a file with an icon of its status, + added, − deleted or ±
// modified, the count of its comments after its name, and a viewed file quiet with
// a check at the right edge. The file under the cursor stands out with a bar and a
// background.
func (m *model) treeLine(r treeRow, w int, current bool) string {
	t := &m.theme
	var bg color.Color
	bar := " "
	if current {
		bg, bar = t.cursor, "▌"
	}
	style := func(fg color.Color) lipgloss.Style { return t.paint(fg, bg).Reverse(current && t.plain) }
	l := newLine(w)
	l.add(bar, style(t.accent))
	l.add(" "+strings.Repeat("  ", r.depth), style(nil))
	if r.file < 0 {
		l.add("▾ ", style(t.subtle))
		l.add(ansi.Truncate(r.name, max(l.free-1, 0), "…"), style(t.text))
		return l.fill(style(nil))
	}
	f := m.files[r.file]
	icon, iconColor := "±", t.muted
	switch f.status {
	case 'A':
		icon, iconColor = "+", t.added
	case 'D':
		icon, iconColor = "−", t.deleted
	}
	l.add(icon+" ", style(iconColor))
	name := t.text
	if f.viewed {
		name = t.muted
	}
	l.add(ansi.Truncate(r.name, max(l.free-3-m.countWidth(r.file), 0), "…"), style(name).Bold(current))
	for _, s := range m.commentSpans(f.key) {
		l.add(" "+s.text, style(s.color))
	}
	if f.viewed {
		l.add(strings.Repeat(" ", max(l.free-2, 0)), style(nil))
		l.add("✓", style(t.accent))
	}
	return l.fill(style(nil))
}
