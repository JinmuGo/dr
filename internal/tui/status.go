package tui

import (
	"fmt"
	"image/color"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/x/ansi"
)

// minKeyRowHeight is how many rows the screen needs to show the key row under the status
// line. A shorter screen leaves it out, for the diff.
const minKeyRowHeight = 12

// minMessage is how many cells of a message the status line keeps for it before the right
// side takes the rest.
const minMessage = 24

// spinner is the frames of the spinner on the status line while questions are out, one a
// second, as the tick comes.
var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// messageTone is how the message on the status line reads.
type messageTone int

const (
	toneWarn messageTone = iota // a warning, such as why a key did nothing
	toneOK                      // news, such as an answer that came
	toneErr                     // an error, such as a file that could not be saved
)

// warn shows text in the status line as a warning, until the next key.
func (m *model) warn(text string) {
	m.message, m.tone = text, toneWarn
}

// tell shows news in the status line, until the next key.
func (m *model) tell(text string) {
	m.message, m.tone = text, toneOK
}

// fail shows an error in the status line, until the next key.
func (m *model) fail(text string) {
	m.message, m.tone = text, toneErr
}

// mode returns the name of what the keys do now, which the pill of the status line shows
// and whose keys the key row lists: HELP, SEARCH or COMMAND while their input is open, ASK
// or COMMENT while the input box of a or c is, ORIGIN while the origin box is, VISUAL while
// lines are selected, and NORMAL otherwise.
func (m *model) mode() string {
	switch {
	case m.help:
		return "HELP"
	case m.searching:
		return "SEARCH"
	case m.commanding:
		return "COMMAND"
	case m.asking != nil:
		return "ASK"
	case m.drafting != nil:
		return "COMMENT"
	case m.reading:
		return "ORIGIN"
	case m.selecting:
		return "VISUAL"
	}
	return "NORMAL"
}

// pill returns what the status line starts with: the mode in its pill, a chip after it while
// the model list of the input box is open, and the gap after them.
func (m *model) pill() []seg {
	t := &m.theme
	mode := m.mode()
	var bg color.Color
	fg := t.base
	switch mode {
	case "NORMAL":
		fg, bg = t.fg, t.line
	case "VISUAL":
		bg = t.accent
	case "ORIGIN":
		bg = t.fg
	case "ASK":
		bg = t.ask
	case "COMMENT":
		bg = t.pending
	case "SEARCH", "COMMAND":
		bg = t.strong
	case "HELP":
		bg = t.muted
	}
	segs := []seg{{text: " " + mode + " ", st: t.pill(fg, bg)}}
	if m.typing() && m.entry.picker != nil {
		segs = append(segs, seg{text: " ^ model ", st: t.chip(t.fg, t.ask)})
	}
	return append(segs, seg{text: "  ", st: t.paint(nil, t.panel)})
}

// pillWidth returns how many cells the status line takes before what is typed in the search
// or the : input.
func (m *model) pillWidth() int {
	return segsWidth(m.pill())
}

// status draws the status line under the diff. It starts with the mode pill. Then comes the
// open search or : input, or a message with an icon of its tone, or the size of the
// selection with its lines and the session that wrote them, or else where the cursor is:
// the file and line, and the origin of a changed line with the first line of its request,
// which is cut first, the directory next. noChanges takes its place when no changed file is
// left. The right shows how long questions have been out, how many fixes are ready and
// comments pending, how far the open origin box is read when its text does not fit, that
// the diff is too narrow for the split view s asked for, the file number, the row number and
// the note on the session logs dr could not fully read. The pill and the start of the left
// come first: what of the right does not fit beside them is left out whole, the note first,
// then the row number, the file number, the pending comments and the fixes, the questions
// last. An open input and the cursor after it are kept whole.
func (m *model) status() string {
	t := &m.theme
	on := func(fg color.Color) lipgloss.Style { return t.paint(fg, t.panel) }
	left := m.pill()
	typed := m.inputText()
	switch {
	case typed != "":
		left = append(left, seg{text: typed, st: on(t.strong)})
	case m.message != "":
		icon, c := "!", t.pending
		switch m.tone {
		case toneOK:
			icon, c = "✓", t.add
		case toneErr:
			icon, c = "✗", t.del
		}
		left = append(left, seg{text: icon + " ", st: on(c).Bold(true)}, seg{text: m.message, st: on(t.fg), cut: true})
	case len(m.rows) == 0:
		left = append(left, seg{text: noChanges, st: on(t.fg)})
	case m.selecting:
		left = append(left, m.selectionSegs(on)...)
	default:
		left = append(left, m.cursorSegs(on)...)
	}
	keep := 2 // a gap before the right
	for _, s := range left {
		if !s.cut {
			keep += segsWidth([]seg{s})
		}
	}
	if m.message != "" && typed == "" {
		keep += min(ansi.StringWidth(m.message), minMessage)
	}
	if typed != "" {
		keep++ // the cursor after it
	}
	var problems, position, narrow, asking, fixes, pending, rowAt, fileAt []seg
	if m.problems != "" {
		problems = []seg{{text: m.problems, st: on(t.pending)}}
	}
	if d, ok := m.waited(); ok {
		frame := spinner[int(d/time.Second)%len(spinner)]
		asking = []seg{{text: fmt.Sprintf("%c asking %ds", frame, int(d/time.Second)), st: on(t.ask)}}
	}
	if n := m.fixesReady(); n > 0 {
		fixes = []seg{{text: "◆ " + strconv.Itoa(n) + " fix ready", st: on(t.fix)}}
	}
	if n := m.pendingCount(); n > 0 {
		pending = []seg{{text: commentMark + " " + strconv.Itoa(n) + " pending", st: on(t.pending)}}
	}
	if len(m.rows) > 0 {
		if m.reading {
			if n, rows := len(m.originLines()), m.boxRows(); n > rows {
				position = []seg{{text: fmt.Sprintf("origin %d/%d", min(m.readTop+rows, n), n), st: on(t.muted)}}
			}
		}
		if m.splitOn && !m.splitShown {
			narrow = []seg{{text: "too narrow for split", st: on(t.pending)}}
		}
		rowAt = []seg{{text: fmt.Sprintf("row %d/%d", m.cursor+1, len(m.rows)), st: on(t.muted)}}
		fileAt = []seg{{text: fmt.Sprintf("file %d/%d", m.rows[m.cursor].file+1, len(m.files)), st: on(t.subtle)}}
	}
	right := fitGroups([]group{
		{segs: problems, rank: 8, keep: keep},
		{segs: position, rank: 4, keep: keep},
		{segs: narrow, rank: 5, keep: keep},
		{segs: asking, rank: 1, keep: keep},
		{segs: fixes, rank: 2, keep: keep},
		{segs: pending, rank: 3, keep: keep},
		{segs: rowAt, rank: 7, keep: keep},
		{segs: fileAt, rank: 6, keep: keep},
	}, m.width-1, seg{text: "   ", st: on(nil)}) // a margin at the right edge
	if right != nil {
		right = append(right, seg{text: " ", st: on(nil)})
	}
	return spread(left, right, m.width, on(nil))
}

// cursorSegs returns where the cursor is, as the status line shows it in the style on gives
// each color: the directory and the name of its file, the line, and for a changed line who
// wrote it and the first line of the request, or that it is unknown.
func (m *model) cursorSegs(on func(color.Color) lipgloss.Style) []seg {
	t := &m.theme
	r := m.rows[m.cursor]
	dir, name := path.Split(m.files[r.file].path)
	segs := []seg{{text: dir, st: on(t.muted), cut: true}, {text: name, st: on(t.strong)}}
	if r.kind != lineRow {
		return segs
	}
	l := m.lines[r.main()]
	n := l.New
	if n == 0 {
		n = l.Old
	}
	segs = append(segs, seg{text: ":" + strconv.Itoa(n), st: on(t.muted)})
	if !r.isChange() {
		return segs
	}
	gap := seg{text: "    ", st: on(nil)}
	if l.Edit == nil {
		return append(segs, gap, seg{text: "unknown", st: on(t.subtle)})
	}
	segs = append(segs, gap, seg{text: review.Tag(l.Edit, m.short), st: on(t.accent).Bold(true)})
	if request, _, _ := strings.Cut(strings.TrimSpace(l.Edit.Prompt), "\n"); request != "" {
		segs = append(segs, seg{text: "  " + review.Visible(request), st: on(t.subtle), cut: true})
	}
	return segs
}

// selectionSegs returns the selection as the status line shows it in the style on gives
// each color: how many lines it has, where they are, and the session that wrote its changed
// lines when one did.
func (m *model) selectionSegs(on func(color.Color) lipgloss.Style) []seg {
	t := &m.theme
	rows := m.selectedRows()
	var lines []int
	for _, i := range rows {
		for _, k := range []int{m.rows[i].left, m.rows[i].right} {
			if k >= 0 && !slices.Contains(lines, k) {
				lines = append(lines, k)
			}
		}
	}
	segs := []seg{{text: countOf(len(rows), "line"), st: on(t.strong)}}
	if len(lines) > 0 {
		segs = append(segs, seg{text: " · " + m.lineLabel(lines), st: on(t.muted)})
	}
	if who := m.sessionOf(lines); who != "" {
		segs = append(segs, seg{text: "   ", st: on(nil)}, seg{text: who, st: on(t.accent).Bold(true)})
	}
	return segs
}

// sessionOf returns the agent and session that wrote the changed lines among lines, lines of
// model.lines, when one session wrote all of them, and "" otherwise.
func (m *model) sessionOf(lines []int) string {
	who := ""
	for _, k := range lines {
		l := m.lines[k]
		if l.Old != 0 && l.New != 0 {
			continue // unchanged
		}
		if l.Edit == nil {
			return ""
		}
		w := review.Visible(review.Who(l.Edit.Agent, l.Edit.Session, m.short))
		if who != "" && w != who {
			return ""
		}
		who = w
	}
	return who
}

// waited returns how long the oldest question in flight has waited for its answer, and
// whether a question is in flight.
func (m *model) waited() (time.Duration, bool) {
	var oldest time.Time
	for _, q := range m.questions {
		if q.pending && (oldest.IsZero() || q.Time.Before(oldest)) {
			oldest = q.Time
		}
	}
	if oldest.IsZero() {
		return 0, false
	}
	return m.now().Sub(oldest), true
}

// fixesReady returns how many comments have a fix under them that y can apply.
func (m *model) fixesReady() int {
	n := 0
	for _, c := range m.comments {
		if c.placed() && c.fix != nil && c.fix.Error == "" && !c.fix.Keep {
			n++
		}
	}
	return n
}

// pendingCount returns how many comments :submit sends.
func (m *model) pendingCount() int {
	n := 0
	for _, c := range m.comments {
		if c.pending() {
			n++
		}
	}
	return n
}

// keycap is a key and what it does, as the key row shows it.
type keycap struct {
	key, does string
}

// keycaps returns the keys of the current mode for the key row, the most used first.
func (m *model) keycaps() []keycap {
	switch m.mode() {
	case "HELP":
		return []keycap{{"any key", "close"}}
	case "SEARCH": // n and N type into the query here
		return []keycap{{"⏎", "search"}, {"esc", "cancel"}}
	case "COMMAND":
		return []keycap{{"⏎", "run"}, {"esc", "cancel"}}
	case "ASK", "COMMENT":
		if m.entry.picker != nil {
			return []keycap{{"↑ ↓", "move"}, {"⏎ tab", "pick"}, {"esc", "close the list"}}
		}
		verb, newline := "save", "⌥⏎"
		if m.asking != nil {
			verb = "send"
		}
		if m.shiftEnter {
			newline = "⇧⏎"
		}
		return []keycap{{"⏎", verb}, {newline, "new line"}, {"^", "pick a model"}, {"esc", "cancel"}}
	case "ORIGIN":
		// a asks from the box where its title offers it: on a changed line, or a line with
		// questions, which a follows up.
		keys := []keycap{{"j k", "scroll"}}
		switch {
		case m.hasQuestions(m.cursor):
			keys = append(keys, keycap{"a", "ask a follow-up"})
		case m.rows[m.cursor].isChange():
			keys = append(keys, keycap{"a", "ask"})
		}
		return append(keys, keycap{"⏎ esc", "close"})
	case "VISUAL":
		n := countOf(len(m.selectedRows()), "line")
		return []keycap{{"a", "ask about " + n}, {"c", "comment on " + n}, {"j k", "extend"}, {"V esc", "stop selecting"}}
	}
	keys := []keycap{
		{"a", "ask"}, {"c", "comment"}, {"e", "edit"}, {"⏎", "origin"}, {"v", "viewed"},
		{"V", "select"}, {"s", "split"}, {"/", "search"}, {":", "command"}, {"?", "help"},
	}
	if c := m.commentOn(m.cursor); c != nil && c.fix != nil {
		keys = append([]keycap{{"y", "apply"}, {"x", "drop"}}, keys...)
	}
	return keys
}

// keyRow draws the key row, the last row of the screen: the keys of the current mode, each
// on a keycap with what it does, as many as fit, and at the right edge in NORMAL and VISUAL
// how to quit, or how to send the pending comments when there are some.
func (m *model) keyRow() string {
	t := &m.theme
	keyStyle := t.paint(t.strong, t.surface2).Bold(true).Reverse(t.plain)
	label := t.paint(t.muted, nil)
	add := func(segs []seg, k keycap) []seg {
		return append(segs, seg{text: " " + k.key + " ", st: keyStyle}, seg{text: " " + k.does, st: label})
	}
	var right []seg
	if mode := m.mode(); mode == "NORMAL" || mode == "VISUAL" {
		end := keycap{"q", "quit"}
		if n := m.pendingCount(); n > 0 {
			end = keycap{":submit", "send " + strconv.Itoa(n)}
		}
		right = append(add(nil, end), seg{text: " "})
	}
	room := m.width
	if right != nil {
		room -= segsWidth(right) + 2 // as spread leaves the left: a gap before the right
	}
	left := []seg{{text: " "}}
	for _, k := range m.keycaps() {
		next := add(nil, k)
		if segsWidth(left)+segsWidth(next) > room {
			break
		}
		left = append(append(left, next...), seg{text: "   "})
	}
	return spread(left, right, m.width, lipgloss.NewStyle())
}
