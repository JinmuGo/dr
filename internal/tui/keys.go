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
}

// keyLabel returns how the key help and the key row show key k: a glyph for Enter, the
// arrows and the ctrl keys, and its name for the others.
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

// key handles a key press. Every key of the diff is a case of one switch on the key's
// name, so a new key is a new case. It returns the command the key starts: quitting, saving
// the viewed files, asking a question, opening the editor or a session's agent, sending fix
// requests or reading the diff again after a fix. While the input box of a or c is open,
// every key goes to it, and ctrl+c cancels the box there. When no changed file is left, only
// q, ?, : and ctrl+c do anything, so :undo can bring back the change a fix took away. While
// the editor or the agent has the terminal, and for ctrlCGuard after it hands it back,
// ctrl+c does nothing. A key takes the message off the status line and the notice off the
// diff. While a notice shows, Esc does only that, whatever is open, so the input and what is
// typed in it stay. Enter on a notice of an answer or of fixes goes to read them, unless an
// input is open, which Enter belongs to; any other key then does its own work. Esc clears a
// count or a g waiting for its key, the selection with them; without them it ends a
// selection, and without one it clears the search query and its marks.
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
	case m.commanding:
		return m.typeCommand(k, msg.Text)
	case len(m.rows) == 0:
		switch k {
		case keyQuit:
			return tea.Quit
		case keyHelp:
			m.help = true
		case keyCommand:
			m.openCommand()
		}
		return nil
	case m.searching:
		m.typeSearch(k, msg.Text)
		return nil
	case m.typing():
		return m.typeBox(msg)
	case n != nil && k == keyEnter && n.question != nil:
		return m.read(n.question)
	case n != nil && k == keyEnter && n.fixes != nil:
		return m.readFix(n.fixes)
	case m.reading:
		return m.readKey(k)
	case k == keyEsc:
		switch {
		case m.count > 0 || m.pending != "":
			m.count, m.pending, m.selecting = 0, "", false
		case m.selecting:
			m.selecting = false
		default:
			m.query = "" // and its marks
		}
		return nil
	}

	k = m.pending + k
	count := m.count
	m.count, m.pending = 0, ""
	switch k {
	case keyPrevFile, keyLeft, keyNextFile, keyRight, keySearch, keySplit, keyViewed, keyOrigin:
		m.selecting = false // these end a selection, then do what they do
	}
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
		m.jumpTo(max(count, 1) - 1)
	case keyBottom:
		if count == 0 {
			count = len(m.rows)
		}
		m.jumpTo(count - 1)
	case keyHalfDown, keyHalfDownAlt:
		m.halfPage(1)
	case keyHalfUp, keyHalfUpAlt:
		m.halfPage(-1)
	case keyNextChange:
		m.jump(count, 1, m.changeStart)
	case keyPrevChange:
		m.jump(count, -1, m.changeStart)
	case keyNextFile, keyRight:
		m.jump(count, 1, m.fileStart)
	case keyPrevFile, keyLeft:
		m.jump(count, -1, m.fileStart)
	case keySearch:
		m.searching, m.input = true, ""
	case keyNextMatch:
		m.find(count, 1)
	case keyPrevMatch:
		m.find(count, -1)
	case keyOrigin:
		m.reading, m.readTop = true, 0
		if qs := m.questionsAt(m.cursor); len(qs) > 0 {
			m.readTop = m.questionTop(qs[len(qs)-1]) // the newest, which the box opens for
		}
	case keySplit:
		m.splitOn = !m.splitOn // Update shows the view that fits
	case keyViewed:
		return m.toggleViewed(m.rows[m.cursor].file)
	case keyHelp:
		m.help = true
	case keySelect:
		if m.selecting {
			m.selecting = false
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
	case keyApply:
		return m.apply()
	case keyDrop:
		m.drop()
	case keyQuit:
		return tea.Quit
	}
	return nil
}

// typeSearch handles a key while the search input is open: Enter searches, going to the
// match it lit, or again for the last query when nothing is typed; Esc closes the input;
// and other keys edit it.
func (m *model) typeSearch(k, text string) {
	switch k {
	case keyEnter:
		m.searching = false
		if m.input == "" {
			m.find(1, 1)
			return
		}
		m.query = m.input
		m.findHere()
	case keyEsc:
		m.searching = false
	default:
		m.edit(k, text)
	}
}

// edit edits the open search or : input with key k: Backspace deletes the last character,
// ctrl+u deletes all, and other keys type text, the text of the key, which an input method
// makes whole characters of.
func (m *model) edit(k, text string) {
	switch k {
	case keyBackspace:
		_, size := utf8.DecodeLastRuneInString(m.input)
		m.input = m.input[:len(m.input)-size]
	case keyCtrlU:
		m.input = ""
	default:
		m.input += text
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
		m.readTop++
	case keyLineUp, keyUp:
		m.readTop--
	case keyHalfDown, keyHalfDownAlt:
		m.readTop += half
	case keyHalfUp, keyHalfUpAlt:
		m.readTop -= half
	case keyTop:
		m.readTop = 0
	case keyBottom:
		m.readTop = m.blockHeight()
	case keyAsk:
		m.startAsk()
	case keyOpenSession:
		return m.openSession()
	case keyQuit, keyEsc, keyEnter:
		m.reading = false
	}
	return nil
}

// moveTo puts the cursor on row i, or on the nearest row there is. During a selection the
// nearest row is in the selection's file, between its first and its last line.
func (m *model) moveTo(i int) {
	first, last := 0, len(m.rows)-1
	if m.selecting {
		first, last = m.selectFirst, m.selectLast
	}
	m.cursor = max(min(i, last), first)
}

// jumpTo puts the cursor on row i like moveTo. When the row is off the screen, it scrolls
// the row to the middle of the screen, or as near as the ends of the diff allow, the way
// vim shows where a jump lands.
func (m *model) jumpTo(i int) {
	m.moveTo(i)
	if h := m.rowsHeight(); m.cursor < m.top || m.top < m.fitTop(m.cursor, h) {
		m.top = min(m.fitTop(m.cursor, h/2+1), m.fitTop(len(m.rows)-1, h))
	}
}

// halfPage moves the cursor and the screen half a screen down (dir 1) or up (dir -1).
func (m *model) halfPage(dir int) {
	half := max(m.rowsHeight()/2, 1)
	m.top += dir * half
	m.moveTo(m.cursor + dir*half)
}

// jump moves the cursor count times to the next row in the direction dir (1 or -1) for
// which ok holds. It stays at the last such row when there are fewer.
func (m *model) jump(count, dir int, ok func(i int) bool) {
	to := m.cursor
	for range max(count, 1) {
		i := to + dir
		for i >= 0 && i < len(m.rows) && !ok(i) {
			i += dir
		}
		if i < 0 || i >= len(m.rows) {
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
			{keyLabels(keyNextFile, keyPrevFile), "next, previous file · 3l"},
			{keyLabels(keySearch, keyNextMatch, keyPrevMatch), "search, next, previous"},
		}},
		{"REVIEW", [][2]string{
			{keyLabel(keyOrigin), "origin; j k scroll it"}, {keyViewed, "mark the file viewed"}, {keySelect, "select lines"},
			{keySplit, "split or unified"},
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
	keys := []keycap{
		{keyAsk, "ask"}, {keyComment, "comment"}, {keyEdit, "edit"}, {keyLabel(keyOrigin), "origin"}, {keyViewed, "viewed"},
		{keySelect, "select"}, {keySplit, "split"}, {keySearch, "search"}, {keyCommand, "command"}, {keyHelp, "help"},
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
