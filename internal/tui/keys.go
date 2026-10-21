package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// ctrlCGuard is how long ctrl+c does nothing after the editor or the agent ga opened hands
// the terminal back, as it does while they have it. An agent that exits on one ctrl+c, as
// Codex does, must not let a second press meant for it quit dr. q quits at any time.
const ctrlCGuard = time.Second

// Keys that are not a character, as tea.KeyPressMsg.String names them. keyLabel gives the
// glyph the key help and the key row show for them.
const (
	keyEnter      = "enter"
	keyShiftEnter = "shift+enter"
	keyAltEnter   = "alt+enter"
	keyEsc        = "esc"
	keyTab        = "tab"
	keyBackspace  = "backspace"
	keyUp         = "up"
	keyDown       = "down"
	keyLeft       = "left"
	keyRight      = "right"
	keyCtrlC      = "ctrl+c"
	keyCtrlJ      = "ctrl+j"
	keyCtrlN      = "ctrl+n"
	keyCtrlP      = "ctrl+p"
	keyCtrlU      = "ctrl+u"
	keySuperB     = "super+b" // ⌘B, which only a terminal that reports the super key sends
)

// The keys of the diff and the origin box, named for what they do. keyPrefix waits for the
// key after it, and with it makes keyTop and keyOpenSession.
const (
	keyLineDown    = "j"
	keyLineUp      = "k"
	keyHalfDown    = "d"
	keyHalfUp      = "u"
	keyHalfDownAlt = "J"
	keyHalfUpAlt   = "K"
	keyPrefix      = "g"
	keyTop         = "gg"
	keyBottom      = "G"
	keyNextChange  = "}"
	keyPrevChange  = "{"
	keyNextFile    = "l"
	keyPrevFile    = "h"
	keyScrollRight = ">"
	keyScrollLeft  = "<"
	keySearch      = "/"
	keyNextMatch   = "n"
	keyPrevMatch   = "N"
	keyOrigin      = keyEnter
	keySplit       = "s"
	keyViewed      = "v"
	keySelect      = "V"
	keyAsk         = "a"
	keyOpenSession = "ga"
	keyComment     = "c"
	keyEdit        = "e"
	keyThreads     = "t"
	keyFoldTree    = keySuperB
	keyPeek        = "space"
	keyFocus       = "f"
	keyCommand     = ":"
	keyApply       = "y"
	keyDrop        = "x"
	keyHelp        = "?"
	keyQuit        = "q"
	keyPickModel   = "^" // in the input box, at the start of a word
)

// glyphs are the labels of the keys that keyLabel does not show by their name.
var glyphs = map[string]string{
	keyEnter: "⏎", keyShiftEnter: "⇧⏎", keyAltEnter: "⌥⏎", keyUp: "↑", keyDown: "↓",
	keyCtrlC: "^C", keyCtrlJ: "^J", keyCtrlN: "^N", keyCtrlP: "^P", keyCtrlU: "^U",
	keySuperB: "⌘B",
}

// keyLabel returns how the key help and the key row show key k: a glyph for Enter, the
// arrows, the ctrl keys and ⌘B, and its name for the others.
func keyLabel(k string) string {
	if g, ok := glyphs[k]; ok {
		return g
	}
	return k
}

// keyLabels returns the labels of keys, one after another with a space between them, for
// keys that share a line of the key help or a keycap.
func keyLabels(keys ...string) string {
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = keyLabel(k)
	}
	return strings.Join(labels, " ")
}

// key handles a key press and hands it to the handler of what is open: the : input, the
// empty screen, the search input, the input box, the threads panel, the peek view, the
// origin box or the diff. It returns the
// command the key starts: quitting, saving the viewed files, asking a question, opening the
// editor or a session's agent, sending fix requests or reading the diff again after a fix.
// While the input box of a or c is open, every key goes to it, and ctrl+c cancels the box
// there. While the editor or the agent has the terminal, and for ctrlCGuard after it hands
// it back, ctrl+c does nothing. A key takes the message off the status line and the notice
// off the diff. While a notice shows, Esc does only that, whatever is open, so the input and
// what is typed in it stay. Enter on a notice of an answer or of fixes goes to read them,
// unless an input is open, which Enter belongs to; any other key then does its own work.
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	n := m.notice
	m.message, m.tone, m.notice = "", toneWarn, nil
	switch {
	case k == keyCtrlC && !m.typing():
		if m.handedOver || m.now().Sub(m.handedBack) < ctrlCGuard {
			return nil
		}
		return tea.Quit
	case n != nil && k == keyEsc:
		return nil
	case m.help:
		m.help = false // any key closes the help
		return nil
	case m.cmdline.open:
		return m.typeCommand(k, msg.Text)
	case len(m.rows) == 0:
		return m.emptyKey(k)
	case m.search.open:
		m.typeSearch(k, msg.Text)
		return nil
	case m.typing():
		return m.typeBox(msg)
	case n != nil && k == keyEnter && n.question != nil:
		m.peek.open = false // the answer is in the diff
		return m.read(n.question)
	case n != nil && k == keyEnter && n.fixes != nil:
		m.peek.open = false
		return m.readFix(n.fixes)
	case m.threads.focus:
		return m.threadKey(k)
	case m.peek.open:
		return m.peekKey(k)
	case m.originBox.open:
		return m.readKey(k)
	case k == keyEsc:
		m.escKey()
		return nil
	}
	return m.normalKey(k)
}

// emptyKey handles a key when no changed file is left: only q, ? and : do anything, so :undo
// can bring back the change a fix took away.
func (m *model) emptyKey(k string) tea.Cmd {
	switch k {
	case keyQuit:
		return tea.Quit
	case keyHelp:
		m.help = true
	case keyCommand:
		m.openCommand()
	}
	return nil
}

// escKey handles Esc in the diff. It clears a count or a g waiting for its key, the
// selection with them; without them it ends a selection, and without one it clears the
// search query and its marks.
func (m *model) escKey() {
	switch {
	case m.count > 0 || m.pending != "":
		m.count, m.pending, m.selection.on = 0, "", false
	case m.selection.on:
		m.selection.on = false
	default:
		m.search.query = "" // and its marks
	}
}

// normalKey handles a key of the diff, with the count and the g typed before it. Every key
// of the diff is a case of a switch on the key's name, here or in moveKey for the keys that
// count and move, so a new key is a new case.
func (m *model) normalKey(k string) tea.Cmd {
	k = m.pending + k
	count := m.count
	m.count, m.pending = 0, ""
	switch k {
	case keyPrevFile, keyLeft, keyNextFile, keyRight, keySearch, keySplit, keyViewed, keyOrigin, keyPeek:
		m.selection.on = false // these end a selection, then do what they do
	}
	if m.moveKey(k, count) {
		return nil
	}
	switch k {
	case keySearch:
		m.search.open, m.search.input = true, ""
	case keyOrigin:
		m.originBox.open, m.originBox.top = true, 0
		if qs := m.questionsAt(m.cursor); len(qs) > 0 {
			m.originBox.top = m.questionTop(qs[len(qs)-1]) // the newest, which the box opens for
		}
	case keySplit:
		m.splitOn = !m.splitOn // Update shows the view that fits
	case keyViewed:
		return m.toggleViewed(m.rows[m.cursor].file)
	case keyHelp:
		m.help = true
	case keySelect:
		if m.selection.on {
			m.selection.on = false
		} else {
			m.startSelection()
		}
	case keyAsk:
		m.startAsk()
	case keyEdit:
		return m.openEditor()
	case keyOpenSession:
		return m.openSession()
	case keyComment:
		m.startComment()
	case keyCommand:
		m.openCommand()
	case keyThreads:
		m.openThreads()
	case keyFoldTree:
		m.foldTree()
	case keyPeek:
		m.openPeek()
	case keyFocus:
		m.focused = !m.focused // scroll keeps the screen in the file
	case keyApply:
		return m.apply()
	case keyDrop:
		m.drop()
	case keyQuit:
		return tea.Quit
	}
	return nil
}

// moveKey handles k when it is a digit of a count, the g that waits for its key, a key that
// moves the cursor count times, or to row count, or one that scrolls the code sideways count
// times, and reports whether it was one.
func (m *model) moveKey(k string, count int) bool {
	switch k {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		m.count = count*10 + int(k[0]-'0')
	case "0":
		m.count = count * 10 // a count does not start with 0
	case keyPrefix:
		m.count, m.pending = count, k
	case keyLineDown, keyDown:
		m.moveTo(m.cursor + max(count, 1))
	case keyLineUp, keyUp:
		m.moveTo(m.cursor - max(count, 1))
	case keyTop:
		first, last := m.viewRows()
		m.jumpTo(min(first+max(count, 1)-1, last))
	case keyBottom:
		first, last := m.viewRows()
		if count == 0 {
			count = last - first + 1
		}
		m.jumpTo(min(first+count-1, last))
	case keyHalfDown, keyHalfDownAlt:
		m.halfPage(1)
	case keyHalfUp, keyHalfUpAlt:
		m.halfPage(-1)
	case keyNextChange:
		m.jump(count, 1, m.changeStart)
	case keyPrevChange:
		m.jump(count, -1, m.changeStart)
	case keyNextFile, keyRight:
		if m.focused {
			m.otherFile(max(count, 1))
		} else {
			m.jump(count, 1, m.fileStart)
		}
	case keyPrevFile, keyLeft:
		if m.focused {
			m.otherFile(-max(count, 1))
		} else {
			m.jump(count, -1, m.fileStart)
		}
	case keyScrollRight:
		m.scrollCode(max(count, 1) * scrollCells)
	case keyScrollLeft:
		m.scrollCode(-max(count, 1) * scrollCells)
	case keyNextMatch:
		m.find(count, 1)
	case keyPrevMatch:
		m.find(count, -1)
	default:
		return false
	}
	return true
}

// typeSearch handles a key while the search input is open: Enter searches, going to the
// match it lit, or again for the last query when nothing is typed; Esc closes the input;
// and other keys edit it.
func (m *model) typeSearch(k, text string) {
	switch k {
	case keyEnter:
		m.search.open = false
		if m.search.input == "" {
			m.find(1, 1)
			return
		}
		m.search.query = m.search.input
		m.findHere()
	case keyEsc:
		m.search.open = false
	default:
		editInput(&m.search.input, k, text)
	}
}

// editInput edits input, the open search or : input, with key k: Backspace deletes the last
// character, ctrl+u deletes all, and other keys type text, the text of the key, which an
// input method makes whole characters of.
func editInput(input *string, k, text string) {
	switch k {
	case keyBackspace:
		_, size := utf8.DecodeLastRuneInString(*input)
		*input = (*input)[:len(*input)-size]
	case keyCtrlU:
		*input = ""
	default:
		*input += text
	}
}

// readKey handles a key while the origin box is open: j, k, d, u, J, K, gg and G scroll its
// text the way they move through the diff, half the box for d, u, J and K, a asks about the
// box's lines and ga opens their session, and q, Esc and Enter close it. scroll keeps the
// text in its bounds. It returns the command of ga.
func (m *model) readKey(k string) tea.Cmd {
	k = m.pending + k
	m.pending = ""
	half := max(m.boxRows()/2, 1)
	switch k {
	case keyPrefix:
		m.pending = k
	case keyLineDown, keyDown:
		m.originBox.top++
	case keyLineUp, keyUp:
		m.originBox.top--
	case keyHalfDown, keyHalfDownAlt:
		m.originBox.top += half
	case keyHalfUp, keyHalfUpAlt:
		m.originBox.top -= half
	case keyTop:
		m.originBox.top = 0
	case keyBottom:
		m.originBox.top = m.blockHeight()
	case keyAsk:
		m.startAsk()
	case keyOpenSession:
		return m.openSession()
	case keyQuit, keyEsc, keyEnter:
		m.originBox.open = false
	}
	return nil
}

// moveTo puts the cursor on row i, or on the nearest row there is. During a selection the
// nearest row is in the selection's file, between its first and its last line, and
// otherwise in the rows viewRows gives.
func (m *model) moveTo(i int) {
	first, last := m.viewRows()
	if m.selection.on {
		first, last = m.selection.first, m.selection.last
	}
	m.cursor = max(min(i, last), first)
}

// viewRows returns the first and the last row the keys that move go between: the rows the
// peek view shows while it is open, the rows of the cursor's file while the diff is focused
// on it, and all rows otherwise.
func (m *model) viewRows() (first, last int) {
	switch {
	case m.peek.open:
		return m.peekRows()
	case m.focused:
		return m.fileSpan()
	}
	return 0, len(m.rows) - 1
}

// lastRow returns the last row the diff draws: the last of the cursor's file while the diff
// is focused on it, else the last row.
func (m *model) lastRow() int {
	if m.focused {
		_, last := m.fileSpan()
		return last
	}
	return len(m.rows) - 1
}

// jumpTo puts the cursor on row i, or on the nearest row there is, in the selection during
// one. Unlike moveTo it goes to another file while the diff is focused on one, which then
// shows that file. When the row is off the screen, it scrolls the row to the middle of the
// screen, or as near as the ends of the diff allow, the way vim shows where a jump lands.
func (m *model) jumpTo(i int) {
	first, last := 0, len(m.rows)-1
	if m.selection.on {
		first, last = m.selection.first, m.selection.last
	}
	m.cursor = max(min(i, last), first)
	if h := m.rowsHeight(); m.cursor < m.top || m.top < m.fitTop(m.cursor, h) {
		m.top = min(m.fitTop(m.cursor, h/2+1), m.fitTop(m.lastRow(), h))
	}
}

// otherFile puts the cursor on the header of the file n files after the cursor's, before it
// when n is negative, or of the last or the first file when there are fewer.
func (m *model) otherFile(n int) {
	m.jumpTo(m.headRow(min(max(m.rows[m.cursor].file+n, 0), len(m.files)-1)))
}

// halfPage moves the cursor and the screen half a screen down (dir 1) or up (dir -1).
func (m *model) halfPage(dir int) {
	half := max(m.rowsHeight()/2, 1)
	m.top += dir * half
	m.moveTo(m.cursor + dir*half)
}

// jump moves the cursor count times to the next row in the direction dir (1 or -1) for
// which ok holds, inside the rows viewRows gives. It stays at the last such row when there
// are fewer.
func (m *model) jump(count, dir int, ok func(i int) bool) {
	first, last := m.viewRows()
	to := m.cursor
	for range max(count, 1) {
		i := to + dir
		for i >= first && i <= last && !ok(i) {
			i += dir
		}
		if i < first || i > last {
			break
		}
		to = i
	}
	m.jumpTo(to)
}

// helpGroup is a group of keys in the key help: its name and each key with what it does.
type helpGroup struct {
	name string
	keys [][2]string
}

// helpColumns are the groups of the key help, in its two columns, left and right. What a key
// does is short, so that both columns fit on a screen 80 cells wide.
var helpColumns = [2][]helpGroup{
	{
		{"MOVE", [][2]string{
			{keyLabels(keyLineDown, keyLineUp), "line down, up · 5j"},
			{keyLabels(keyHalfDown, keyHalfUp, keyHalfDownAlt, keyHalfUpAlt), "half a page down, up"},
			{keyLabels(keyTop, keyBottom), "top, bottom · 12G"},
			{keyLabels(keyNextChange, keyPrevChange), "next, previous change"},
			{keyLabels(keyNextFile, keyPrevFile, keyScrollRight, keyScrollLeft), "file · 3l; code sideways"},
			{keyLabels(keySearch, keyNextMatch, keyPrevMatch), "search, next, previous"},
		}},
		{"REVIEW", [][2]string{
			{keyLabels(keyOrigin, keyPeek), "origin; peek at a file"}, {keyLabels(keyViewed, keySelect), "viewed, select lines"},
			{keyLabels(keySplit, keyFoldTree), "split, fold the tree"}, {keyLabels(keyThreads, keyFocus), "threads; focus on a file"},
		}},
		{"MOUSE", [][2]string{
			{"drag", "resize the tree"}, {"wheel", "scroll three lines"}, {"click", "a file, or a Viewed box"},
			{"⇧ drag", "select text (⌥ on macOS)"},
		}},
	},
	{
		{"ASK AND FIX", [][2]string{
			{keyAsk, "ask who wrote it"}, {keyOpenSession, "open it in the agent"}, {keyComment, "leave a change comment"},
			{keyCommand + "submit", "send every comment"}, {keyLabels(keyApply, keyDrop), "apply, drop a fix"},
			{keyCommand + "undo", "revert the last fix"}, {keyEdit, "edit in $EDITOR"},
		}},
		{"IN A BOX", [][2]string{
			{keyLabel(keyEnter), "send or save"}, {keyLabel(keyShiftEnter), "new line (⌥⏎, ^J)"},
			{keyPickModel, "pick a model, word start"}, {keyLabels(keyEsc, keyCtrlC), "cancel"},
		}},
		{"OTHER", [][2]string{{keyHelp, "this help"}, {keyEsc, "clear a count or search"}, {keyQuit, "quit"}}},
	},
}

// keycap is a key and what it does, as the key row shows it.
type keycap struct {
	key, does string
}

// keycaps returns the keys of the current mode for the key row, the most used first. On a
// selection that mixes sessions, a and c say only ask and comment, as keyRow dims them.
func (m *model) keycaps() []keycap {
	switch m.mode() {
	case modeHelp:
		return []keycap{{"any key", "close"}}
	case modeSearch: // n and N type into the query here
		return []keycap{{keyLabel(keyEnter), "search"}, {keyEsc, "cancel"}}
	case modeCommand:
		return []keycap{{keyLabels(keyUp, keyDown), "choose"}, {keyTab, "complete"}, {keyLabel(keyEnter), "run"}, {keyEsc, "cancel"}}
	case modeAsk, modeComment:
		if m.entry.picker != nil {
			back := "back to the question"
			if m.drafting != nil {
				back = "back to the comment"
			}
			return []keycap{
				{keyLabels(keyUp, keyDown, keyCtrlN, keyCtrlP), "move"}, {keyLabels(keyEnter, keyTab), "pick"},
				{keyLabel(keyCtrlU), "clear"}, {keyEsc, back},
			}
		}
		verb, newline := "save", keyAltEnter
		if m.asking != nil {
			verb = "send"
		}
		if m.shiftEnter {
			newline = keyShiftEnter
		}
		return []keycap{{keyLabel(keyEnter), verb}, {keyLabel(newline), "new line"}, {keyPickModel, "pick a model"}, {keyEsc, "cancel"}}
	case modePeek:
		return []keycap{
			{keyLabels(keyLineDown, keyLineUp), "move"}, {keyLabels(keyNextFile, keyPrevFile), "next, previous file"},
			{keySplit, "split"}, {keyViewed, "viewed"}, {keyLabels(keyPeek, keyEsc), "close"},
		}
	case modeThreads:
		return []keycap{{keyLabels(keyLineDown, keyLineUp), "move"}, {keyLabel(keyEnter), "go"}, {keyEsc, "back to the diff"}, {keyThreads, "files"}}
	case modeOrigin:
		// a asks from the box where its title offers it: on a changed line, or a line with
		// questions, which a follows up.
		keys := []keycap{{keyLabels(keyLineDown, keyLineUp), "scroll"}}
		switch {
		case m.hasQuestions(m.cursor):
			keys = append(keys, keycap{keyAsk, "ask a follow-up"})
		case m.rows[m.cursor].isChange():
			keys = append(keys, keycap{keyAsk, "ask"})
		}
		return append(keys, keycap{keyLabels(keyEnter, keyEsc), "close"})
	case modeVisual:
		extend, stop := keycap{keyLabels(keyLineDown, keyLineUp), "extend"}, keycap{keyLabels(keySelect, keyEsc), "stop selecting"}
		if m.mixed() {
			return []keycap{{keyAsk, "ask"}, {keyComment, "comment"}, extend, stop}
		}
		n := countOf(len(m.selectedRows()), "line")
		return []keycap{{keyAsk, "ask about " + n}, {keyComment, "comment on " + n}, extend, stop}
	}
	if len(m.rows) == 0 {
		return []keycap{{keyCommand, "command"}, {keyHelp, "help"}, {keyQuit, "quit"}}
	}
	focus := "focus"
	if m.focused {
		focus = "all files"
	}
	keys := []keycap{
		{keyAsk, "ask"}, {keyComment, "comment"}, {keyEdit, "edit"}, {keyLabel(keyOrigin), "origin"}, {keyViewed, "viewed"},
		{keySelect, "select"}, {keySplit, "split"}, {keyPeek, "peek"}, {keyFocus, focus}, {keySearch, "search"}, {keyThreads, "threads"}, {keyCommand, "command"}, {keyHelp, "help"},
	}
	if c := m.commentOn(m.cursor); c != nil && c.fix != nil {
		keys = append([]keycap{{keyApply, "apply"}, {keyDrop, "drop"}}, keys...)
	}
	return keys
}

// readHint returns the hint of a notice that Enter reads: Enter to read it, Esc to dismiss.
func (m *model) readHint() []chunk {
	return m.keys([2]string{keyLabel(keyEnter), "to read"}, [2]string{keyEsc, "dismiss"})
}
