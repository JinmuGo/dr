package tui

import (
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// ctrlCGuard is how long ctrl+c does nothing after the editor or the agent ga opened hands
// the terminal back, as it does while they have it. An agent that exits on one ctrl+c, as
// Codex does, must not let a second press meant for it quit dr. q quits at any time.
const ctrlCGuard = time.Second

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
	case k == "ctrl+c" && !m.typing():
		if m.handedOver || m.now().Sub(m.handedBack) < ctrlCGuard {
			return nil
		}
		return tea.Quit
	case n != nil && k == "esc":
		return nil
	case m.help:
		m.help = false // any key closes the help
		return nil
	case m.commanding:
		return m.typeCommand(k, msg.Text)
	case len(m.rows) == 0:
		switch k {
		case "q":
			return tea.Quit
		case "?":
			m.help = true
		case ":":
			m.openCommand()
		}
		return nil
	case m.searching:
		m.typeSearch(k, msg.Text)
		return nil
	case m.typing():
		return m.typeBox(msg)
	case n != nil && k == "enter" && n.question != nil:
		return m.read(n.question)
	case n != nil && k == "enter" && n.fixes != nil:
		return m.readFix(n.fixes)
	case m.reading:
		return m.readKey(k)
	case k == "esc":
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
	case "h", "left", "l", "right", "/", "s", "v", "enter":
		m.selecting = false // these end a selection, then do what they do
	}
	switch k {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		m.count = count*10 + int(k[0]-'0')
	case "0":
		m.count = count * 10 // a count does not start with 0
	case "g":
		m.count, m.pending = count, k
	case "j", "down":
		m.moveTo(m.cursor + max(count, 1))
	case "k", "up":
		m.moveTo(m.cursor - max(count, 1))
	case "gg":
		m.jumpTo(max(count, 1) - 1)
	case "G":
		if count == 0 {
			count = len(m.rows)
		}
		m.jumpTo(count - 1)
	case "d", "J":
		m.halfPage(1)
	case "u", "K":
		m.halfPage(-1)
	case "}":
		m.jump(count, 1, m.changeStart)
	case "{":
		m.jump(count, -1, m.changeStart)
	case "l", "right":
		m.jump(count, 1, m.fileStart)
	case "h", "left":
		m.jump(count, -1, m.fileStart)
	case "/":
		m.searching, m.input = true, ""
	case "n":
		m.find(count, 1)
	case "N":
		m.find(count, -1)
	case "enter":
		m.reading, m.readTop = true, 0
		if qs := m.questionsAt(m.cursor); len(qs) > 0 {
			m.readTop = m.questionTop(qs[len(qs)-1]) // the newest, which the box opens for
		}
	case "s":
		m.splitOn = !m.splitOn // Update shows the view that fits
	case "v":
		return m.toggleViewed(m.rows[m.cursor].file)
	case "?":
		m.help = true
	case "V":
		if m.selecting {
			m.selecting = false
		} else {
			m.startSelection()
		}
	case "a":
		m.startAsk()
	case "e":
		return m.openEditor()
	case "ga":
		return m.openSession()
	case "c":
		m.startComment()
	case ":":
		m.openCommand()
	case "y":
		return m.apply()
	case "x":
		m.drop()
	case "q":
		return tea.Quit
	}
	return nil
}

// typeSearch handles a key while the search input is open: Enter searches, going to the
// match it lit, or again for the last query when nothing is typed; Esc closes the input;
// and other keys edit it.
func (m *model) typeSearch(k, text string) {
	switch k {
	case "enter":
		m.searching = false
		if m.input == "" {
			m.find(1, 1)
			return
		}
		m.query = m.input
		m.findHere()
	case "esc":
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
	case "backspace":
		_, size := utf8.DecodeLastRuneInString(m.input)
		m.input = m.input[:len(m.input)-size]
	case "ctrl+u":
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
	case "g":
		m.pending = k
	case "j", "down":
		m.readTop++
	case "k", "up":
		m.readTop--
	case "d", "J":
		m.readTop += half
	case "u", "K":
		m.readTop -= half
	case "gg":
		m.readTop = 0
	case "G":
		m.readTop = m.blockHeight()
	case "a":
		m.startAsk()
	case "ga":
		return m.openSession()
	case "q", "esc", "enter":
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
			{"j k", "line down, up · 5j"}, {"d u J K", "half a page down, up"}, {"gg G", "top, bottom · 12G"},
			{"} {", "next, previous change"}, {"l h", "next, previous file · 3l"}, {"/ n N", "search, next, previous"},
		}},
		{"REVIEW", [][2]string{
			{"⏎", "origin; j k scroll it"}, {"v", "mark the file viewed"}, {"V", "select lines"},
			{"s", "split or unified"},
		}},
		{"MOUSE", [][2]string{
			{"drag", "resize the tree"}, {"wheel", "scroll three lines"}, {"click", "a file, or a Viewed box"},
			{"⇧ drag", "select text (⌥ on macOS)"},
		}},
	},
	{
		{"ASK AND FIX", [][2]string{
			{"a", "ask who wrote it"}, {"ga", "open it in the agent"}, {"c", "leave a change comment"},
			{":submit", "send every comment"}, {"y x", "apply, drop a fix"}, {":undo", "revert the last fix"},
			{"e", "edit in $EDITOR"},
		}},
		{"IN A BOX", [][2]string{
			{"⏎", "send or save"}, {"⇧⏎", "new line (⌥⏎, ^J)"}, {"^", "pick a model, word start"},
			{"esc ^C", "cancel"},
		}},
		{"OTHER", [][2]string{{"?", "this help"}, {"esc", "clear a count or search"}, {"q", "quit"}}},
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
		return []keycap{{"⏎", "search"}, {"esc", "cancel"}}
	case modeCommand:
		return []keycap{{"↑ ↓", "choose"}, {"tab", "complete"}, {"⏎", "run"}, {"esc", "cancel"}}
	case modeAsk, modeComment:
		if m.entry.picker != nil {
			back := "back to the question"
			if m.drafting != nil {
				back = "back to the comment"
			}
			return []keycap{{"↑ ↓ ^N ^P", "move"}, {"⏎ tab", "pick"}, {"^U", "clear"}, {"esc", back}}
		}
		verb, newline := "save", "⌥⏎"
		if m.asking != nil {
			verb = "send"
		}
		if m.shiftEnter {
			newline = "⇧⏎"
		}
		return []keycap{{"⏎", verb}, {newline, "new line"}, {"^", "pick a model"}, {"esc", "cancel"}}
	case modeOrigin:
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
	case modeVisual:
		if m.mixed() {
			return []keycap{{"a", "ask"}, {"c", "comment"}, {"j k", "extend"}, {"V esc", "stop selecting"}}
		}
		n := countOf(len(m.selectedRows()), "line")
		return []keycap{{"a", "ask about " + n}, {"c", "comment on " + n}, {"j k", "extend"}, {"V esc", "stop selecting"}}
	}
	if len(m.rows) == 0 {
		return []keycap{{":", "command"}, {"?", "help"}, {"q", "quit"}}
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

// readHint returns the hint of a notice that Enter reads: Enter to read it, Esc to dismiss.
func (m *model) readHint() []chunk {
	return m.keys([2]string{"⏎", "to read"}, [2]string{"esc", "dismiss"})
}
