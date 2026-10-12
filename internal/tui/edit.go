package tui

import (
	"cmp"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// editedMsg tells that what e or ga ran in the terminal, the editor or an agent, ended, with
// the error it ended with, and what the status line says before that error: "the editor
// failed", or "claude exited".
type editedMsg struct {
	err    error
	failed string
}

// noSession is what ga says about lines that no session it can fork wrote or edited.
const noSession = "No session wrote or edited these lines; nothing to open"

// reloadedMsg brings the changed files read again after the editor or an agent ga opened
// ended, or a fix was applied or undone, or why they could not be read. done is the notice of
// what the fix did, nil for none, and warning what went wrong besides: why the fix's record
// was not saved, or how the editor or the agent failed. reload numbers the reading, as
// model.reloads counted it when it was asked for.
type reloadedMsg struct {
	files    []review.File
	problems []agent.Problem // the session logs dr could not fully read
	err      error
	done     *notice
	warning  string
	reload   int
}

// spot is where the cursor is, said in a way that outlives a rebuild of the diff: the file
// and its header, or the line of the current file e opens the editor at, and for a deleted
// line its line in base.
type spot struct {
	key    string // the path as review gives it
	header bool   // the file's header
	line   int    // the line of the current file
	old    int    // the line in base of the deleted line the cursor is on, 0 for none
}

// openEditor opens the user's editor, $VISUAL, else $EDITOR, else vi, at the line of the
// cursor row in its file, as editLine says, and returns the command that runs it. The
// editor gets the terminal until it closes, and then the diff is rebuilt. A deleted file and
// a file with no line diff have nothing to edit, and the status line says so.
func (m *model) openEditor() tea.Cmd {
	f := m.files[m.rows[m.cursor].file]
	switch {
	case f.status == 'D':
		m.warn("Nothing to edit: " + f.path + " is deleted")
		return nil
	case f.noDiff == review.Binary:
		m.warn("Nothing to edit: " + f.path + " is a binary file")
		return nil
	case f.noDiff != "":
		m.warn("Nothing to edit: " + f.path + " is a directory or a submodule")
		return nil
	}
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	path := filepath.Join(m.asker.Root, filepath.FromSlash(f.key))
	return m.handOver(editorCommand(editor, m.editLine(m.cursor), path), "the editor failed")
}

// openSession opens a fork of the session of the lines a would ask about, as target finds
// them, in the session's agent, and returns the command that runs it. A session answering a
// question does not stop it, as the fork is a new one. The agent gets the terminal until it
// exits, and then the diff is rebuilt as after the editor, so the lines the fork changed
// show it as their origin. The origin box closes and a selection ends. Lines that no
// session wrote or edited have no session to open, and the status line says so, or why
// there is no one to ask about them, or why the agent cannot open the session.
func (m *model) openSession() tea.Cmd {
	whom := m.whom
	whom.Busy = func(string, string) bool { return false }
	t, why := m.target(whom)
	switch {
	case t == nil:
		m.warn(why)
		return nil
	case t.Relation == review.NewSession:
		m.warn(noSession)
		return nil
	}
	cmd, err := m.open(t.Ask)
	if err != nil {
		m.fail(review.Visible(err.Error()))
		return nil
	}
	m.originBox.open, m.selection.on = false, false
	return m.handOver(cmd, t.Ask.Agent+" exited")
}

// handOver returns the command that gives the terminal to c until it exits, and marks the
// terminal handed over until edited gets the editedMsg c ends with; failed is what the
// status line says before the error c ends with. Bubble Tea reads keys again before it
// sends that message, so a ctrl+c that comes between, meant for c, must not quit dr.
func (m *model) handOver(c *exec.Cmd, failed string) tea.Cmd {
	m.handedOver = true
	return m.execute(c, func(err error) tea.Msg {
		return editedMsg{err: err, failed: failed}
	})
}

// editorCommand returns the command that opens editor at line of the file at path. It runs
// editor through sh, the way git does, so an editor with arguments, such as "code --wait",
// works.
func editorCommand(editor string, line int, path string) *exec.Cmd {
	return exec.Command("sh", "-c", editor+` "$@"`, editor, "+"+strconv.Itoa(line), path)
}

// editLine returns the line of the current file that row i stands for: the line on the
// right, and for a deleted line the next current line of the diff, or the one before at
// the end of the file. A file header stands for the file's first changed line, a hunk
// header for its first line, and a file with no changed line for line 1.
func (m *model) editLine(i int) int {
	r := m.rows[i]
	diff := m.files[r.file].diff
	switch {
	case r.kind == hunkRow:
		return m.editLine(i + 1) // a hunk header always has lines after it
	case r.kind == lineRow && r.right >= 0:
		return m.lines[r.right].New
	case r.kind == lineRow:
		old := m.lines[r.left].Old
		return position(diff, slices.IndexFunc(diff, func(l origin.Line) bool { return l.New == 0 && l.Old == old }))
	}
	return position(diff, slices.IndexFunc(diff, func(l origin.Line) bool { return l.Old == 0 || l.New == 0 }))
}

// position returns the line of the current file where diff[at] is: its own line, or the next
// current line after it, or the one before it when none comes after. It returns 1 when at
// is -1 or diff has no current line.
func position(diff []origin.Line, at int) int {
	if at < 0 {
		return 1
	}
	for _, l := range diff[at:] {
		if l.New > 0 {
			return l.New
		}
	}
	for _, l := range slices.Backward(diff[:at]) {
		if l.New > 0 {
			return l.New
		}
	}
	return 1
}

// edited handles the editor or the agent ga opened ending: it returns the command that reads
// the changed files again, for reloaded to rebuild the diff with. Without a way to read
// them, the diff stays, and only the error it ended with shows. Ending on an interrupt, as
// ctrl+c leaves a CLI, is no error and shows nothing.
func (m *model) edited(msg editedMsg) tea.Cmd {
	m.handedOver, m.handedBack = false, m.now()
	var failure string
	if msg.err != nil && !interrupted(msg.err) {
		failure = msg.failed + ": " + review.Visible(msg.err.Error())
	}
	if m.reload == nil {
		if failure != "" {
			m.fail(failure)
		}
		return nil
	}
	return m.readAgain(reloadedMsg{warning: failure})
}

// interruptedStatus is the exit status many CLIs end with when interrupted: 128 + SIGINT.
const interruptedStatus = 130

// interrupted tells whether err is a process ending on an interrupt: killed by SIGINT, or
// exiting with status 130.
func interrupted(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == interruptedStatus || killedByInterrupt(exitErr.ProcessState)
}

// readAgain returns the command that reads the changed files again with m.reload and brings
// them in msg, numbered as the newest reading, so that reloaded drops what an older one
// brings.
func (m *model) readAgain(msg reloadedMsg) tea.Cmd {
	m.reloads++
	msg.reload = m.reloads
	reload := m.reload
	return func() tea.Msg {
		msg.files, msg.problems, msg.err = reload()
		return msg
	}
}

// reloaded rebuilds the diff from the files read again, and shows that no changed file is
// left when none is, as after the last change was undone. When the files could not be read,
// the diff stays as it was. The notice of what the fix did shows that and the warning among
// its warnings; without a fix, the status line tells of them. What a reading older than the
// newest asked for brings is dropped, so a slow one cannot show an older diff over a newer
// one.
func (m *model) reloaded(msg reloadedMsg) {
	if msg.reload != m.reloads {
		return
	}
	var problems []string
	if msg.err != nil {
		problems = append(problems, "could not read the diff again: "+review.VisibleLine(msg.err.Error()))
	} else {
		m.rebuild(msg.files, msg.problems)
	}
	if msg.warning != "" {
		problems = append(problems, msg.warning)
	}
	switch {
	case msg.done != nil:
		for _, p := range problems {
			msg.done.warn(p)
		}
		m.notice = msg.done
	case len(problems) > 0:
		m.warn(strings.Join(problems, "; "))
	}
}

// rebuild shows files, and problems, the session logs dr could not fully read, in place of
// the diff. The cursor stays on its file and its line, or the nearest that is left, as
// restore says, the same lines from the top of the screen. The view, the file tree, the
// search and the viewed marks stay; the questions and the comments go where they are now.
// The origin box, a selection, the input box of a or c with what is typed in it and the :
// input close, and the status line and the notice clear. Without files the screen says
// there are none, and the search input, a count and a key waiting for the rest close too,
// since only q, ? and : do anything there.
func (m *model) rebuild(files []review.File, problems []agent.Problem) {
	at, offset := m.spot(), m.cursor-m.top
	if m.sticky() {
		offset++ // the header that stays on top takes a line
	}
	if m.typing() || m.cmdline.open {
		m.asking, m.drafting, m.entry, m.cmdline.open, m.cmdline.input = nil, nil, textBox{}, false, ""
	}
	m.originBox.open, m.selection.on, m.enteredAt = false, false, -1 // the split view has other rows now
	m.message, m.tone, m.notice = "", toneWarn, nil
	m.unreadable = unreadableAgents(problems)
	m.setFiles(files)
	if len(m.rows) == 0 {
		m.cursor, m.top = 0, 0
		m.search.open, m.search.input, m.count, m.pending = false, "", 0, ""
		return
	}
	m.cursor = m.restore(at)
	m.top = m.cursor - offset // scroll keeps it in the diff
	if m.top >= 0 && m.top < m.cursor && m.sticky() {
		m.top++
	}
}

// spot returns where the cursor is, as restore finds it again; nowhere when no changed file
// is left, which restore takes for the first file.
func (m *model) spot() spot {
	if len(m.rows) == 0 {
		return spot{header: true}
	}
	r := m.rows[m.cursor]
	s := spot{key: m.files[r.file].key}
	if r.kind == fileRow || r.kind == noteRow {
		s.header = true
		return s
	}
	s.line = m.editLine(m.cursor)
	if r.kind == lineRow && r.right < 0 {
		s.old = m.lines[r.left].Old
	}
	return s
}

// restore returns the row of s in the diff: the header of its file, or the row of its
// deleted line or its current line. When that line is not in the diff, it returns the row of
// the file whose line is nearest, or its header when it has no line rows. When the file
// left the diff, it returns the header of the next file in path order, or of the last file.
func (m *model) restore(s spot) int {
	i := slices.IndexFunc(m.files, func(f file) bool { return f.key == s.key })
	if i < 0 {
		i = slices.IndexFunc(m.files, func(f file) bool { return f.key > s.key })
		if i < 0 {
			i = len(m.files) - 1
		}
		return m.headRow(i)
	}
	head := m.headRow(i)
	if s.header {
		return head
	}
	best, distance := head, -1
	for k := head + 1; k < len(m.rows) && m.rows[k].file == i; k++ {
		r := m.rows[k]
		if r.kind != lineRow {
			continue
		}
		if s.old > 0 && r.left >= 0 && m.lines[r.left].New == 0 && m.lines[r.left].Old == s.old ||
			s.old == 0 && r.right >= 0 && m.lines[r.right].New == s.line {
			return k
		}
		d := m.editLine(k) - s.line
		if d < 0 {
			d = -d
		}
		if distance < 0 || d < distance {
			best, distance = k, d
		}
	}
	return best
}
