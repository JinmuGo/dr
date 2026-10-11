package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/review"
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

// treeHead is how many lines the head of the file tree takes: its title, the progress line
// under it and a blank line.
const treeHead = 3

// treeWidth returns the width the file tree needs to show every row whole: a file with its
// status, the count of its comments and, when it is viewed or changed since, a mark at the
// right edge; and a session of the legend with its agent, id and count of lines.
func (m *model) treeWidth() int {
	w := ansi.StringWidth(treeTitle) + 2 + ansi.StringWidth(treeCount(len(m.files), len(m.files))+treeViewed) + 2
	for _, r := range m.tree {
		if r.file < 0 {
			w = max(w, 4+2*r.depth+ansi.StringWidth(r.name)+1)
		} else {
			w = max(w, 4+2*r.depth+ansi.StringWidth(r.name)+3+m.countWidth(r.file))
		}
	}
	for _, r := range m.legend {
		w = max(w, 1+2+ansi.StringWidth(agentColumn(r.agent))+ansi.StringWidth(m.short[r.id])+2+len(strconv.Itoa(r.lines))+1)
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

// treeTitle is the title of the file tree, and treeViewed comes after the count of viewed
// files at its right.
const treeTitle, treeViewed = "Files", " viewed"

// treeCount returns how many of files are viewed, as GitHub counts them.
func treeCount(viewed, files int) string {
	return fmt.Sprintf("%d/%d", viewed, files)
}

// treeTop returns the first row of the file tree when body lines show its rows: the file
// under the cursor stays in the middle while the tree is longer than that.
func (m *model) treeTop(body int) int {
	current := m.rows[m.cursor].file
	at := slices.IndexFunc(m.tree, func(r treeRow) bool { return r.file == current })
	return max(min(at-body/2, len(m.tree)-body), 0)
}

// treeBody returns how many lines of a file tree h lines high show its rows: those between
// its head and the legend that legendLines draws.
func (m *model) treeBody(h int) int {
	return max(h-treeHead-m.legendHeight(h), 0)
}

// fileTree draws the file tree, w cells wide and h rows high: the title with how many files
// are viewed, a progress line of that share under it, then the rows, with the file under the
// cursor marked and on the screen, and the legend of the sessions at the bottom.
func (m *model) fileTree(w, h int) []string {
	t := &m.theme
	viewed := 0
	for _, f := range m.files {
		if f.viewed {
			viewed++
		}
	}
	on := func(fg color.Color) lipgloss.Style { return t.paint(fg, t.panel) }
	blank := on(nil).Render(strings.Repeat(" ", w))
	title := spread(
		[]seg{{text: " ", st: on(nil)}, {text: treeTitle, st: on(t.strong).Bold(true)}},
		[]seg{{text: treeCount(viewed, len(m.files)), st: on(t.fg)}, {text: treeViewed, st: on(t.muted)}, {text: " ", st: on(nil)}},
		w, on(nil))
	bar := max(w-2, 0)
	done := 0
	if len(m.files) > 0 {
		done = (bar*viewed*2 + len(m.files)) / (2 * len(m.files)) // rounded
	}
	progress := newLine(w)
	progress.add(" ", on(nil))
	progress.add(strings.Repeat("━", done), on(t.accent))
	progress.add(strings.Repeat("─", bar-done), on(t.faint))
	lines := make([]string, 0, h)
	lines = append(lines, title, progress.fill(on(nil)), blank)
	current := m.rows[m.cursor].file
	body := m.treeBody(h)
	top := m.treeTop(body)
	for y := range body {
		if i := top + y; i < len(m.tree) {
			lines = append(lines, m.treeLine(m.tree[i], w, m.tree[i].file == current))
		} else {
			lines = append(lines, blank)
		}
	}
	lines = append(lines, m.legendLines(w, h)...)
	return lines[:h]
}

// legendItem is an entry of the legend of the file tree: a session, with the line under it
// that names the session that started it when it is a subagent, or the changed lines
// without an origin.
type legendItem []string

// legendHeight returns how many lines of a file tree h lines high the legend takes: all of
// it when it fits in half the tree, leaving a line for a file under the head, and else as
// many as that leaves it.
func (m *model) legendHeight(h int) int {
	n := 0
	if len(m.legend) > 0 || m.unknown > 0 {
		n = 1 // the title
		for _, r := range m.legend {
			n++
			if r.parent != "" {
				n++
			}
		}
		if m.unknown > 0 {
			n++
		}
	}
	room := min(h/2, h-treeHead-1)
	if n > room {
		n = room
		if n < 2 { // the title and +N more
			n = 0
		}
	}
	return n
}

// legendLines draws the legend of the sessions at the bottom of a file tree w cells wide and
// h lines high, legendHeight(h) lines: a title, then each session with changed lines in the
// order of their colors, a dot in its color, its agent and short id, its requests behind
// changed lines and fix when dr applied a fix for a comment to it, and at the right edge how
// many changed lines it wrote, with the session that started it under it when it is a
// subagent; last, how many changed lines have no origin. What does not fit is left out and
// counted as +N more.
func (m *model) legendLines(w, h int) []string {
	n := m.legendHeight(h)
	if n == 0 {
		return nil
	}
	t := &m.theme
	on := func(fg color.Color) lipgloss.Style { return t.paint(fg, t.panel) }
	margin := seg{text: " ", st: on(nil)}
	row := func(left []seg, count int) string {
		return spread(append([]seg{margin}, left...), []seg{{text: strconv.Itoa(count), st: on(t.subtle)}, margin}, w, on(nil))
	}
	var items []legendItem
	for _, r := range m.legend {
		var asked []string
		for _, turn := range r.turns {
			asked = append(asked, "#"+strconv.Itoa(turn))
		}
		if r.fix {
			asked = append(asked, "fix")
		}
		item := legendItem{row([]seg{
			{text: "● ", st: on(m.sessionColor(r.session))},
			{text: agentColumn(review.Visible(r.agent)), st: on(t.fg)},
			{text: m.short[r.id], st: on(t.muted)},
			{text: "  " + strings.Join(asked, " "), st: on(t.subtle), cut: true},
		}, r.lines)}
		if r.parent != "" {
			parent := newLine(w)
			parent.add("   ", on(nil))
			parent.add(ansi.Truncate("↳ subagent of "+review.Visible(r.agent)+" "+m.short[r.parent], max(w-4, 0), "…"), on(t.subtle))
			item = append(item, parent.fill(on(nil)))
		}
		items = append(items, item)
	}
	if m.unknown > 0 {
		items = append(items, legendItem{row([]seg{{text: "┆ ", st: on(t.subtle)}, {text: "unknown", st: on(t.subtle)}}, m.unknown)})
	}
	title := newLine(w)
	title.add(" ", on(nil))
	title.add("Sessions", on(t.strong).Bold(true))
	lines := []string{title.fill(on(nil))}
	shown := 0
	for k, item := range items {
		need := len(lines) + len(item)
		if k < len(items)-1 {
			need++ // a line for +N more, should a later item not fit
		}
		if need > n {
			break
		}
		lines = append(lines, item...)
		shown++
	}
	if shown < len(items) {
		more := newLine(w)
		more.add(" +"+strconv.Itoa(len(items)-shown)+" more", on(t.subtle))
		lines = append(lines, more.fill(on(nil)))
	}
	blank := on(nil).Render(strings.Repeat(" ", w)) // above the title, for the lines an item left out did not take
	return append(slices.Repeat([]string{blank}, n-len(lines)), lines...)
}

// agentColumn returns the name of an agent as the legend shows it, padded to line up the
// ids after it.
func agentColumn(name string) string {
	return fmt.Sprintf("%-6s ", name)
}

// treeLine draws a row of the file tree, w cells wide, as GitHub's file tree does: a
// directory with a fold mark, a file with an icon of its status, + added, − deleted or ±
// modified, the count of its comments after its name, and a viewed file quiet with a check
// at the right edge, or a dot there when it changed since it was viewed. The file under the
// cursor stands out with a bar and a background.
func (m *model) treeLine(r treeRow, w int, current bool) string {
	t := &m.theme
	bg := t.panel
	bar := " "
	if current {
		bg, bar = t.surface2, "▌"
	}
	style := func(fg color.Color) lipgloss.Style { return t.paint(fg, bg).Reverse(current && t.plain) }
	l := newLine(w)
	l.add(bar, style(t.accent))
	l.add(" "+strings.Repeat("  ", r.depth), style(nil))
	if r.file < 0 {
		l.add("▾ ", style(t.subtle))
		l.add(ansi.Truncate(r.name, max(l.free-1, 0), "…"), style(t.muted))
		return l.fill(style(nil))
	}
	f := m.files[r.file]
	icon, iconColor := "±", t.muted
	switch f.status {
	case 'A':
		icon, iconColor = "+", t.add
	case 'D':
		icon, iconColor = "−", t.del
	}
	l.add(icon+" ", style(iconColor))
	name := style(t.fg)
	switch {
	case current:
		name = style(t.strong).Bold(true)
	case f.viewed:
		name = style(t.subtle)
	}
	l.add(ansi.Truncate(r.name, max(l.free-3-m.countWidth(r.file), 0), "…"), name)
	for _, s := range m.commentSpans(f.key) {
		l.add(" "+s.text, style(s.color))
	}
	mark, markColor := "", color.Color(nil)
	switch {
	case f.changed:
		mark, markColor = "●", t.pending
	case f.viewed:
		mark, markColor = "✓", t.add
	}
	if mark != "" {
		l.add(strings.Repeat(" ", max(l.free-2, 0)), style(nil))
		l.add(mark, style(markColor))
	}
	return l.fill(style(nil))
}
