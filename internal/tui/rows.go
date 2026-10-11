package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// contextLines is how many unchanged lines a hunk shows around its changes.
const contextLines = 3

// kind is what a row of the diff shows.
type kind int

const (
	fileRow kind = iota // header of a file: its path and how many lines it adds and deletes
	hunkRow             // header of a hunk, "@@ -1,5 +1,5 @@"
	lineRow             // a line of the diff, or in the split view a line on each side
	noteRow             // why a file has no line diff, under its header
)

// row is one row of the diff.
type row struct {
	kind kind
	file int    // index of the row's file in model.files
	text string // the header of a hunkRow
	// left and right are the lines of a lineRow in model.lines: left the base version and
	// right the current one, -1 for none. An unchanged line is on both sides. The unified
	// view has one line on each row, so a deleted line has no right and an added line no
	// left; the split view pairs them.
	left, right int
	twin        int // the row of the other view that shows the same header or line
}

// isChange reports whether r is a row of a line the diff adds or deletes.
func (r row) isChange() bool {
	return r.kind == lineRow && r.left != r.right
}

// main returns the line of a lineRow whose origin the screen shows: the line on the right,
// or the deleted line on the left when the right has none.
func (r row) main() int {
	if r.right >= 0 {
		return r.right
	}
	return r.left
}

// file is what the screen keeps of a changed file besides its rows.
type file struct {
	path           string // made visible
	key            string // the path as review gives it, which the viewed files are kept by
	hash           string // review.File.Hash of the diff the screen shows
	noDiff         string // why the file has no line diff, "" when it has one
	status         byte   // review.File.Status: 'A' added, 'D' deleted, 'M' modified
	added, deleted int
	digits         int           // width of its widest line number
	requests       []*agent.Edit // requests behind its changed lines, in the order they were made
	unknown        bool          // some changed line has no origin
	sessions       []session     // the sessions behind its changed lines, in the order of their colors
	diff           []origin.Line // the whole diff, unchanged lines outside the hunks included
	hunks          []hunk        // its hunks
	first, end     int           // its lines in model.lines
	head           int           // row of its header in the unified view
	splitHead      int           // row of its header in the split view
	viewed         bool          // marked viewed, so only its header shows
	changed        bool          // marked viewed before, but its diff changed since
	colored        bool          // its lines are split into pieces in model.oldPieces and newPieces
	earlier        []*question   // questions about it that have no place in its diff now
}

// hunk is a hunk of a file: its header and its lines in model.lines.
type hunk struct {
	header     string // "@@ -1,5 +1,5 @@"
	first, end int
}

// build puts the lines of the hunks of files in model.lines, the lines of each hunk in a
// run, and keeps what the screen shows of each file. place lays out their rows.
func (m *model) build(files []review.File) {
	m.files = make([]file, len(files))
	for i, f := range files {
		info := file{
			path: review.Visible(f.Path), key: f.Path, hash: f.Hash(contextLines), status: f.Status, noDiff: f.NoDiff,
			diff: f.Lines, requests: review.Requests(f.Lines),
		}
		last := 0
		for _, l := range f.Lines {
			last = max(last, l.Old, l.New)
			switch {
			case l.Old > 0 && l.New > 0:
				continue
			case l.Old == 0:
				info.added++
			default:
				info.deleted++
			}
			if l.Edit == nil {
				info.unknown = true
			}
		}
		info.digits = len(strconv.Itoa(last))
		info.first = len(m.lines)
		for _, h := range review.Hunks(f.Lines, contextLines) {
			first := len(m.lines)
			m.lines = append(m.lines, h.Lines...)
			m.labeled = append(m.labeled, runLabels(h.Lines)...)
			info.hunks = append(info.hunks, hunk{
				header: fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount),
				first:  first, end: len(m.lines),
			})
		}
		info.end = len(m.lines)
		m.files[i] = info
	}
}

// originKey tells the origins of changed lines apart: a request of a session, a fix dr
// applied, or none, the zero key.
type originKey struct {
	agent, session string
	turn           int
	fix            *agent.Fix
}

// keyOf returns the origin key of a line that e wrote or deleted, nil for none.
func keyOf(e *agent.Edit) originKey {
	if e == nil {
		return originKey{}
	}
	return originKey{e.Agent, e.Session, e.Turn, e.Fix}
}

// runLabels returns which of lines, the lines of a hunk, end with a label of their origin.
// Changed lines in a row with the same origin are a run, which an unchanged line ends; the
// first line of the first run of each origin in the hunk has the label, and the later runs
// of that origin only its rail.
func runLabels(lines []origin.Line) []bool {
	labeled := make([]bool, len(lines))
	seen := make(map[originKey]bool)
	var last originKey
	inRun := false
	for k, l := range lines {
		if l.Old > 0 && l.New > 0 {
			inRun = false
			continue
		}
		key := keyOf(l.Edit)
		if (!inRun || key != last) && !seen[key] {
			labeled[k], seen[key] = true, true
		}
		last, inRun = key, true
	}
	return labeled
}

// placeSessions gives each session its color and keeps the sessions of each file in the
// order of their colors. A session's color is its place in the order the sessions first
// appear in the diff, the files and their lines in order, then the sessions asked and those
// comments go to. A session keeps its color while the screen is open: when the files are
// read again, a session new to the screen takes the next color, wherever in the diff it is.
func (m *model) placeSessions() {
	if m.colors == nil {
		m.colors = make(map[session]int)
	}
	add := func(s session) {
		if _, ok := m.colors[s]; !ok && s.id != "" {
			m.colors[s] = len(m.colors)
		}
	}
	for i := range m.files {
		f := &m.files[i]
		for _, l := range m.lines[f.first:f.end] {
			e := l.Edit
			if l.Old > 0 && l.New > 0 || e == nil {
				continue
			}
			s := session{e.Agent, e.Session}
			add(s)
			if !slices.Contains(f.sessions, s) {
				f.sessions = append(f.sessions, s)
			}
		}
	}
	for _, q := range m.questions {
		add(session{q.Agent, q.Session})
	}
	for _, c := range slices.Concat(m.comments, m.away) {
		add(session{c.Agent, c.Session})
	}
	for i := range m.files {
		slices.SortFunc(m.files[i].sessions, func(a, b session) int { return m.colors[a] - m.colors[b] })
	}
}

// sessionColor returns the color of session s.
func (m *model) sessionColor(s session) color.Color {
	return m.theme.sessions[m.colors[s]%len(m.theme.sessions)]
}

// originColor returns the color of the origin of a line e wrote or deleted: teal for a fix
// dr applied, quiet for no origin, and otherwise the color of e's session.
func (m *model) originColor(e *agent.Edit) color.Color {
	switch {
	case e == nil:
		return m.theme.subtle
	case e.Fix != nil:
		return m.theme.fix
	}
	return m.sessionColor(session{e.Agent, e.Session})
}

// noRecord is what a label says of changed lines without an origin.
const noRecord = "not in any session record"

// originOf returns how the screen names the origin of a line e wrote or deleted: its dot, ●
// for a request of a session, ◆ for a fix dr applied and ┆ for no origin, its tag, what it
// asked for in a line, and its color.
func (m *model) originOf(e *agent.Edit) (dot, tag, about string, c color.Color) {
	switch {
	case e == nil:
		return "┆", "unknown", noRecord, m.theme.subtle
	case e.Fix != nil:
		dot = "◆"
	default:
		dot = "●"
	}
	return dot, review.Tag(e, m.short), review.Summary(requestText(e)), m.originColor(e)
}

// rail returns the rail of line k of model.lines, the cell between its line numbers and its
// sign: a bar in the color of its origin for a changed line, a dashed one for a changed line
// without an origin, and blank for an unchanged line.
func (m *model) rail(k int) chunk {
	l := m.lines[k]
	switch {
	case l.Old > 0 && l.New > 0:
		return chunk{text: " "}
	case l.Edit == nil:
		return chunk{text: "┆", color: m.theme.subtle}
	}
	return chunk{text: "▎", color: m.originColor(l.Edit)}
}

// place lays out the rows of both views, the unified and the split one: for each file a
// header, then, unless the file is viewed and folds to its header, a note when it has no
// line diff, or its hunks with their lines. The rows on the screen are then those of the
// view shown.
func (m *model) place() {
	m.unified, m.split, m.hits.ok = m.unified[:0], m.split[:0], false
	for i := range m.files {
		f := &m.files[i]
		f.head, f.splitHead = len(m.unified), len(m.split)
		m.addRow(row{kind: fileRow, file: i})
		if f.viewed {
			continue
		}
		if f.noDiff != "" {
			m.addRow(row{kind: noteRow, file: i})
		}
		for _, h := range f.hunks {
			m.addRow(row{kind: hunkRow, file: i, text: h.header})
			m.addLines(i, h.first, h.end)
		}
	}
	m.rows = m.unified
	if m.splitShown {
		m.rows = m.split
	}
}

// addRow adds r, a row that is not a line, to both views.
func (m *model) addRow(r row) {
	u, s := r, r
	u.twin, s.twin = len(m.split), len(m.unified)
	m.unified = append(m.unified, u)
	m.split = append(m.split, s)
}

// addLines adds the lines first to end of model.lines, a hunk of file f, to both views. The
// unified view has a row for each line. The split view has a row for each unchanged line,
// on both sides, and pairs the deleted and the added lines of each run of changes in order,
// as GitHub does; a line without a partner has an empty side.
func (m *model) addLines(f, first, end int) {
	lines := m.lines[first:end]
	unified := len(m.unified) // the unified row of the first line
	for k, l := range lines {
		r := row{kind: lineRow, file: f, left: first + k, right: first + k}
		switch {
		case l.Old == 0:
			r.left = -1
		case l.New == 0:
			r.right = -1
		}
		m.unified = append(m.unified, r)
	}
	for k := 0; k < len(lines); {
		if lines[k].Old > 0 && lines[k].New > 0 {
			m.unified[unified+k].twin = len(m.split)
			m.split = append(m.split, row{kind: lineRow, file: f, left: first + k, right: first + k, twin: unified + k})
			k++
			continue
		}
		var deleted, added []int
		for ; k < len(lines) && (lines[k].Old == 0 || lines[k].New == 0); k++ {
			if lines[k].New == 0 {
				deleted = append(deleted, k)
			} else {
				added = append(added, k)
			}
		}
		for n := range max(len(deleted), len(added)) {
			r := row{kind: lineRow, file: f, left: -1, right: -1}
			if n < len(deleted) {
				r.left, r.twin = first+deleted[n], unified+deleted[n]
				m.unified[r.twin].twin = len(m.split)
			}
			if n < len(added) {
				r.right, r.twin = first+added[n], unified+added[n]
				m.unified[r.twin].twin = len(m.split)
			}
			m.split = append(m.split, r)
		}
	}
}

// headRow returns the row of the header of file i on the screen.
func (m *model) headRow(i int) int {
	if m.splitShown {
		return m.files[i].splitHead
	}
	return m.files[i].head
}

// display returns text the way the screen shows it: made visible, a tab as four spaces.
func display(text string) string {
	return strings.ReplaceAll(review.Visible(text), "\t", "    ")
}

// changeStart reports whether row i is the first of a run of changed lines.
func (m *model) changeStart(i int) bool {
	return m.rows[i].isChange() && (i == 0 || !m.rows[i-1].isChange())
}

// fileStart reports whether row i is the header of a file.
func (m *model) fileStart(i int) bool {
	return m.rows[i].kind == fileRow
}

// searchText returns the text of row i that search looks in: its lines as the screen shows
// them, one on each line, or the path of a file header.
func (m *model) searchText(i int) string {
	switch r := m.rows[i]; r.kind {
	case lineRow:
		text := display(m.lines[r.main()].Text)
		if r.left >= 0 && r.right >= 0 && r.left != r.right {
			text = display(m.lines[r.left].Text) + "\n" + text
		}
		return text
	case fileRow:
		return m.files[r.file].path
	}
	return ""
}
