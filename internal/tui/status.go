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

// messageTone is how a message on the status line, or a notice, reads.
type messageTone int

const (
	toneWarn   messageTone = iota // a warning, such as why a key did nothing
	toneOK                        // something done, such as a fix applied
	toneErr                       // an error, such as a file that could not be saved
	toneAnswer                    // an answer that came
)

// icon returns the icon of tone and its color: ! for a warning, ✓ for something done, ✗
// for an error and ● for an answer.
func (t *theme) icon(tone messageTone) (string, color.Color) {
	switch tone {
	case toneOK:
		return "✓", t.add
	case toneErr:
		return "✗", t.del
	case toneAnswer:
		return "●", t.ask
	}
	return "!", t.pending
}

// warn shows text in the status line as a warning, until the next key.
func (m *model) warn(text string) {
	m.message, m.tone = text, toneWarn
}

// fail shows an error in the status line, until the next key.
func (m *model) fail(text string) {
	m.message, m.tone = text, toneErr
}

// mode is what the keys do now. The pill of the status line shows its name, and the key row
// lists its keys.
type mode int

const (
	modeNormal  mode = iota // none of the others
	modeVisual              // lines are selected
	modeOrigin              // the origin box is open
	modeAsk                 // the input box of a is open
	modeComment             // the input box of c is open
	modeSearch              // the search input is open
	modeCommand             // the : input is open
	modeHelp                // the key help is open
)

// modeNames are the names of the modes, as the pill shows them.
var modeNames = [...]string{
	modeNormal:  "NORMAL",
	modeVisual:  "VISUAL",
	modeOrigin:  "ORIGIN",
	modeAsk:     "ASK",
	modeComment: "COMMENT",
	modeSearch:  "SEARCH",
	modeCommand: "COMMAND",
	modeHelp:    "HELP",
}

// String returns the name of md, as the pill shows it.
func (md mode) String() string {
	return modeNames[md]
}

// mode returns what the keys do now: HELP, SEARCH or COMMAND while their input is open, ASK
// or COMMENT while the input box of a or c is, ORIGIN while the origin box is, VISUAL while
// lines are selected, and NORMAL otherwise.
func (m *model) mode() mode {
	switch {
	case m.help:
		return modeHelp
	case m.search.open:
		return modeSearch
	case m.cmdline.open:
		return modeCommand
	case m.asking != nil:
		return modeAsk
	case m.drafting != nil:
		return modeComment
	case m.originBox.open:
		return modeOrigin
	case m.selection.on:
		return modeVisual
	}
	return modeNormal
}

// pill returns what the status line starts with: the mode in its pill, a chip after it while
// the model list of the input box is open, and the gap after them.
func (m *model) pill() []seg {
	t := &m.theme
	mode := m.mode()
	var bg color.Color
	fg := t.base
	switch mode {
	case modeNormal:
		fg, bg = t.fg, t.line
	case modeVisual:
		bg = t.accent
	case modeOrigin:
		bg = t.fg
	case modeAsk:
		bg = t.ask
	case modeComment:
		bg = t.pending
	case modeSearch, modeCommand:
		bg = t.strong
	case modeHelp:
		bg = t.muted
	}
	segs := []seg{{text: " " + mode.String() + " ", st: t.pill(fg, bg)}}
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
// open search or : input, or a message with an icon of its tone, or the selection: its size
// with its lines and the session that wrote them, or a warning when its changed lines mix
// sessions; or else where the cursor is: the file and line, and the origin of a changed line
// with the first line of its request. The right shows which of the matches of the search
// query the current one is while a query is set, a read-only chip when this dr does not hold
// the lock on the worktree, how long questions have been out, how many fixes are ready and
// comments pending, how far the open origin box is read when its text does not fit, that
// the diff is too narrow for the split view s asked for, the file number and the row number.
// As room runs out, the first line of the request is cut first, the directory of the file
// next, then what of the right does not fit beside the rest of the left is left out whole,
// the row number first, then the file number, the pending comments and the fixes, the
// search count, the questions and the chip last; once the row number is left out, so are
// the request and the directory. The pill, the file name, the origin's tag, an open input
// and the cursor after it are kept whole.
func (m *model) status() string {
	t := &m.theme
	on := func(fg color.Color) lipgloss.Style { return t.paint(fg, t.panel) }
	left := m.pill()
	typed := m.inputText()
	where := false // the left shows where the cursor is
	switch {
	case typed != "":
		left = append(left, seg{text: typed, st: on(t.strong)})
	case m.message != "":
		icon, c := t.icon(m.tone)
		left = append(left, seg{text: icon + " ", st: on(c).Bold(true)}, seg{text: m.message, st: on(t.fg), cut: true})
	case len(m.rows) == 0: // the empty screen says why
	case m.selection.on:
		left = append(left, m.selectionSegs(on)...)
	default:
		left, where = append(left, m.cursorSegs(on)...), true
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
	var found, readOnly, position, narrow, asking, fixes, pending, rowAt, fileAt []seg
	if m.lockErr != nil {
		readOnly = []seg{{text: " read-only ", st: t.chip(t.pending, t.pending)}}
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
		if m.searchQuery() != "" {
			h, at := m.found(), 0
			if j := h.current(m.cursor); j >= 0 {
				at = h.before[j] + 1
			}
			found = []seg{{text: fmt.Sprintf("%d of %d", at, h.total), st: on(t.pending).Bold(true)}}
		}
		if m.originBox.open {
			if n, rows := m.blockHeight(), m.boxRows(); n > rows {
				position = []seg{{text: fmt.Sprintf("origin %d/%d", min(m.originBox.top+rows, n), n), st: on(t.muted)}}
			}
		}
		if m.splitOn && !m.splitShown {
			narrow = []seg{{text: "too narrow for split", st: on(t.pending)}}
		}
		rowAt = []seg{{text: fmt.Sprintf("row %d/%d", m.cursor+1, len(m.rows)), st: on(t.muted)}}
		fileAt = []seg{{text: fmt.Sprintf("file %d/%d", m.rows[m.cursor].file+1, len(m.files)), st: on(t.subtle)}}
	}
	right := fitGroups([]group{
		{segs: found, rank: 2, keep: keep},
		{segs: readOnly, rank: 1, keep: keep},
		{segs: position, rank: 5, keep: keep},
		{segs: narrow, rank: 6, keep: keep},
		{segs: asking, rank: 1, keep: keep},
		{segs: fixes, rank: 3, keep: keep},
		{segs: pending, rank: 4, keep: keep},
		{segs: rowAt, rank: 8, keep: keep},
		{segs: fileAt, rank: 7, keep: keep},
	}, m.width-1, seg{text: "   ", st: on(nil)}) // a margin at the right edge
	if where && rowAt != nil && !slices.ContainsFunc(right, func(s seg) bool { return s.text == rowAt[0].text }) {
		left = slices.DeleteFunc(left, func(s seg) bool { return s.cut }) // the request and the directory go before the positions
	}
	if right != nil {
		right = append(right, seg{text: " ", st: on(nil)})
	}
	return spread(left, right, m.width, on(nil))
}

// cursorSegs returns where the cursor is, as the status line shows it in the style on gives
// each color: the directory and the name of its file, the line, and for a changed line its
// origin in the origin's color, with what it asked for in a line.
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
	dot, tag, about, c := m.originOf(l.Edit)
	segs = append(segs, gap, seg{text: dot + " ", st: on(c)}, seg{text: tag, st: on(c).Bold(true)})
	if about != "" {
		segs = append(segs, seg{text: "  " + about, st: on(t.subtle), cut: true})
	}
	return segs
}

// selectionSegs returns the selection as the status line shows it in the style on gives
// each color: how many lines it has, where they are, and the dot and tag of the session
// that wrote its changed lines when one did; or, when its changed lines mix sessions, a
// warning that a and c need lines from one session, before either is pressed.
func (m *model) selectionSegs(on func(color.Color) lipgloss.Style) []seg {
	t := &m.theme
	lines := m.selectedLines()
	who, edit, unknown := m.sessionsOf(lines)
	if len(who) > 1 {
		icon, c := t.icon(toneWarn)
		return []seg{{text: icon + " ", st: on(c).Bold(true)}, {text: mixedSessions(who), st: on(c)}}
	}
	segs := []seg{{text: countOf(len(m.selectedRows()), "line"), st: on(t.strong)}}
	if len(lines) > 0 {
		segs = append(segs, seg{text: " · " + m.lineLabel(lines), st: on(t.muted)})
	}
	if len(who) == 1 && !unknown {
		dot, _, _, c := m.originOf(edit)
		segs = append(segs, seg{text: "   ", st: on(nil)}, seg{text: dot + " ", st: on(c)}, seg{text: who[0], st: on(c).Bold(true)})
	}
	return segs
}

// mixedSessions returns the warning that a selection whose changed lines who, the tags of
// two sessions or more, wrote cannot be asked about or take a comment.
func mixedSessions(who []string) string {
	return "The selection mixes sessions (" + strings.Join(who, ", ") + "). a and c need lines from one session."
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

// keyRow draws the key row, the last row of the screen: the keys of the current mode, each
// on a keycap with what it does, as many as fit, and at the right edge in NORMAL and VISUAL
// how to quit, or how to send the pending comments when there are some. A key that would
// only say why it does nothing is dimmed. The empty screen lists its few keys, quitting
// among them.
func (m *model) keyRow() string {
	t := &m.theme
	keyStyle := t.paint(t.strong, t.surface2).Bold(true).Reverse(t.plain)
	label := t.paint(t.muted, nil)
	mixed := m.mode() == modeVisual && m.mixed() // a and c only say why they do nothing
	add := func(segs []seg, k keycap) []seg {
		if mixed && (k.key == keyAsk || k.key == keyComment) {
			return append(segs, seg{text: " " + k.key + " ", st: t.paint(t.subtle, t.panel).Reverse(t.plain)}, seg{text: " " + k.does, st: t.paint(t.faint, nil)})
		}
		return append(segs, seg{text: " " + k.key + " ", st: keyStyle}, seg{text: " " + k.does, st: label})
	}
	var right []seg
	if mode := m.mode(); len(m.rows) > 0 && (mode == modeNormal || mode == modeVisual) {
		end := keycap{keyQuit, "quit"}
		if n := m.pendingCount(); n > 0 {
			end = keycap{keyCommand + "submit", "send " + strconv.Itoa(n)}
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

// inputPrompt returns what the status line shows before the open input, "" when none is
// open.
func (m *model) inputPrompt() string {
	switch {
	case m.search.open:
		return "/"
	case m.cmdline.open:
		return ":"
	}
	return ""
}

// typed returns what is typed in the open search or : input, "" when none is open.
func (m *model) typed() string {
	switch {
	case m.search.open:
		return m.search.input
	case m.cmdline.open:
		return m.cmdline.input
	}
	return ""
}

// inputText returns what the status line shows of the open search or : input after the
// mode pill: its prompt and what is typed. When they do not fit on the line with the cursor
// after them, their start is cut, so what is typed and the cursor stay on the screen. It
// returns "" when no input is open.
func (m *model) inputText() string {
	prompt := m.inputPrompt()
	if prompt == "" {
		return ""
	}
	return keepEnd(prompt+review.Visible(m.typed()), m.width-m.pillWidth()-1) // the cursor after it
}
