package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
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
// ctrl+c does nothing.
func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	unread, unreadFix := m.unread, m.unreadFix
	m.message, m.news, m.unread, m.unreadFix = "", false, nil, nil
	switch {
	case k == "ctrl+c" && !m.typing():
		if m.handedOver || m.now().Sub(m.handedBack) < ctrlCGuard {
			return nil
		}
		return tea.Quit
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
			m.commanding, m.input = true, ""
		}
		return nil
	case m.searching:
		m.typeSearch(k, msg.Text)
		return nil
	case m.typing():
		return m.typeBox(msg)
	case k == "enter" && unread != nil:
		return m.read(unread) // the status line told of its answer
	case k == "enter" && unreadFix != nil:
		return m.readFix(unreadFix) // the status line told of its fixes
	case m.reading:
		return m.readKey(k)
	case k == "esc":
		m.count, m.pending, m.selecting = 0, "", false
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
		m.commanding, m.input = true, ""
	case "y":
		return m.apply()
	case "x":
		m.drop()
	case "q":
		return tea.Quit
	}
	return nil
}

// typeSearch handles a key while the search input is open: Enter searches, Esc closes the
// input, and other keys edit it.
func (m *model) typeSearch(k, text string) {
	switch k {
	case "enter":
		m.searching = false
		if m.input != "" { // an empty input searches again for the last query
			m.query = m.input
		}
		m.find(1, 1)
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
		m.readTop = len(m.originLines())
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
	if h := m.diffHeight(); m.cursor < m.top || m.top < m.fitTop(m.cursor, h) {
		m.top = min(m.fitTop(m.cursor, h/2+1), m.fitTop(len(m.rows)-1, h))
	}
}

// halfPage moves the cursor and the screen half a screen down (dir 1) or up (dir -1).
func (m *model) halfPage(dir int) {
	half := max(m.diffHeight()/2, 1)
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

// find moves the cursor count times to the next row in the direction dir (1 or -1) whose
// text holds the last query, going on from the other end. A query without capitals
// ignores case.
func (m *model) find(count, dir int) {
	if m.query == "" {
		return
	}
	fold := m.query == strings.ToLower(m.query)
	i := m.cursor
	for range max(count, 1) {
		if i = m.match(i, dir, fold); i < 0 {
			m.warn("Not found: " + review.Visible(m.query))
			return
		}
	}
	m.jumpTo(i)
}

// match returns the first row after from in the direction dir, going around the ends,
// whose text holds the query, or -1 when none does.
func (m *model) match(from, dir int, fold bool) int {
	n := len(m.rows)
	for step := 1; step <= n; step++ {
		i := ((from+dir*step)%n + n) % n
		text := m.searchText(i)
		if fold {
			text = strings.ToLower(text)
		}
		if strings.Contains(text, m.query) {
			return i
		}
	}
	return -1
}
