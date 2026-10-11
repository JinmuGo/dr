// Package tui is the diff screen of dr: the changed files as one diff moved through with vim
// keys, drawn the way GitHub draws a pull request's files, unified or split, with the
// origin of the line under the cursor in the status line and, on Enter, in full in a box
// under the line. a asks an agent about a line, the session that wrote it when it can, and
// the answer comes in that box. It shows only what the review package gives it and asks
// only through the Asker it is given.
package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/charmbracelet/colorprofile"
)

// wheelLines is how many lines a step of the mouse wheel moves, as in tuicr.
const wheelLines = 3

// model is the state of the diff screen.
type model struct {
	files      []file
	lines      []origin.Line     // the lines of the hunks, in the order of the unified view
	labeled    []bool            // which of lines end with the label of their run of changes, as runLabels marks them
	unified    []row             // the rows of the unified view
	split      []row             // the rows of the split view
	oldPieces  [][]piece         // the pieces of the base version of each line once its file is colored
	newPieces  [][]piece         // the pieces of the current version of each line likewise
	tree       []treeRow         // the file tree
	short      map[string]string // short session ids, from review.ShortIDs
	colors     map[session]int   // the color of each session, as placeSessions gives them, kept while the screen is open
	unreadable []string          // the agents whose session logs dr could not fully read
	listWidth  int               // width the file tree needs
	theme      theme

	rows          []row // the rows on the screen, of the unified or the split view
	width, height int
	cursor        int  // row under the cursor
	top           int  // first row on the screen
	splitOn       bool // s asked for the split view
	splitShown    bool // the rows are the split view's: s asked for it and the diff is wide enough
	enteredFrom   int  // the unified row the split view was last entered from
	enteredAt     int  // the split row it was entered at, which holds the line of enteredFrom
	treeSize      int  // width the file tree was dragged to, 0 when it was not
	folded        bool // the file tree was dragged narrow and shows as a handle
	dragging      bool // the line between the file tree and the diff is being dragged

	count   int         // number typed before a key, 0 when none
	pending string      // "g" waiting for the key that completes it
	help    bool        // the key help is open
	reading bool        // the origin box is open under the cursor row
	readTop int         // first line of the origin box's text on the screen
	asking  *target     // the input box of a is open, and what it asks about
	input   string      // what is typed in the search or the : input
	search  search      // the search: whether its input is open, the last query and where it is
	choice  int         // the highlighted row of the command list
	message string      // shown in the status line until the next key
	tone    messageTone // how the message reads: a warning or an error
	notice  *notice     // floats at the top right of the diff until the next key

	drafting   *commentInput // the input box of c is open, and what it comments on
	entry      textBox       // what is typed in the input box of a or c
	shiftEnter bool          // the terminal tells Shift+Enter from Enter, so the input box names it for a new line
	commanding bool          // the : input is open

	selecting               bool // V started a selection of lines
	anchor                  int  // the row it started at; it runs from there to the cursor
	selectFirst, selectLast int  // the first and the last line row of its file, which the cursor stays between

	viewed  state.Viewed // the files marked viewed, of this diff and of others
	saver   *saver       // writes viewed to its file
	version int          // how many times viewed changed

	asker     Asker                 // how a asks agents
	whom      review.Asking         // decides who a asks
	log       *state.Log            // where the questions and their answers are kept, nil for nowhere
	earlier   state.Records         // what the log held when the screen opened
	flights   *flights              // the questions in flight
	questions []*question           // asked in this run and earlier ones, oldest first
	asked     [][]*question         // the questions about each of model.lines
	busy      map[session]bool      // the sessions and new sessions with a question or a fix request in flight
	forks     map[session]runFork   // the fork or new session that answered each last in this run
	comments  []*comment            // left in this run and earlier ones and not removed, on files of the diff, oldest first
	away      []*comment            // likewise, on files not in the diff now
	resolved  []*comment            // likewise, resolved by a fix dr applied that is in place
	models    map[string]*modelList // what Asker.Models gave for each agent in this run, or that it is still at it

	newest   map[session]*agent.Edit // the newest edit of each session in the diff, as newestEdits finds it
	records  state.Records           // what the log holds: what it held when the screen opened, and the fix records of this run
	requests map[string]*request     // the fix requests in flight, by ID
	lockErr  error                   // why this dr does not hold the lock on the worktree, so y and :undo write no file; nil when it does
	base     Base                    // what the diff compares the working tree with
	reloads  int                     // how many times the changed files were asked to be read again; reloaded rebuilds from the last only

	reload  func() ([]review.File, []agent.Problem, error) // reads the changed files again, nil for never
	execute func(*exec.Cmd, tea.ExecCallback) tea.Cmd      // runs the editor or the agent ga opens: tea.ExecProcess, or a test's
	open    func(agent.Question) (*exec.Cmd, error)        // returns the command that opens a fork of a session: agent.Open, or a test's

	// handedOver tells that the editor or the agent ga opened has the terminal: its command
	// was returned and its editedMsg has not come. handedBack is when one last handed the
	// terminal back, zero before either ran. ctrl+c does not quit while handedOver, nor for
	// ctrlCGuard after handedBack: an agent that exits on one ctrl+c, as Codex does, must not
	// let a second press meant for it quit dr, and Bubble Tea reads keys again before it
	// sends the editedMsg.
	handedOver bool
	handedBack time.Time

	now     func() time.Time // returns the time: time.Now, or the clock of a test
	tick    func() tea.Cmd   // returns the command that brings a tickMsg a second later: tick, or a test's
	ticking bool             // a tickMsg is on its way
}

// Saved is what the screen keeps between runs: what it read from the state directory when
// it opened, and the directory, where it saves the viewed files and appends to the log of
// questions. Reload reads the changed files again after e edits one.
type Saved struct {
	Dir     string        // the state directory; "" keeps nothing
	Viewed  state.Viewed  // the files marked viewed, of this diff and of others
	Log     state.Records // what the log holds: the questions, answers, forks, comments and fixes so far
	Problem string        // what could not be read, which the screen shows until the first key
	Lock    error         // why this dr does not hold the lock on the worktree, so fixes cannot be applied here; nil when it does
	// Reload returns the changed files and the session logs dr could not fully read, as New
	// takes them, read again; nil keeps the diff as it is.
	Reload func() ([]review.File, []agent.Problem, error)
	Base   Base // what the diff compares the working tree with
}

// Base is what the diff compares the working tree with, which the screen names when no file
// differs from it.
type Base struct {
	Ref    string // the ref dr was opened with, or the default branch
	Commit string // the base commit
	// MergeBase tells that Commit is the merge-base of HEAD and Ref, not Ref's own commit.
	MergeBase bool
}

// Load reads what the screen keeps in the state directory dir. What cannot be read is left
// out, and Problem says what it was.
func Load(dir string) Saved {
	s := Saved{Dir: dir}
	var problems []string
	var err error
	if s.Viewed, err = state.Load(state.ViewedPath(dir)); err != nil {
		problems = append(problems, "could not read viewed state: "+review.Visible(err.Error()))
	}
	var bad int
	logPath := state.LogPath(dir)
	s.Log, bad, err = state.NewLog(logPath).Read()
	switch {
	case err != nil:
		problems = append(problems, "could not read questions: "+review.Visible(err.Error()))
	case bad > 0:
		problems = append(problems, "skipped "+countOf(bad, "unreadable line")+" of "+filepath.Base(logPath))
	}
	s.Problem = strings.Join(problems, "; ")
	return s
}

// newModel returns the diff screen for files. problems are the session logs dr could not
// fully read, saved what the screen kept from earlier runs, and asker how it asks agents.
func newModel(files []review.File, problems []agent.Problem, saved Saved, asker Asker) *model {
	m := &model{
		unreadable: unreadableAgents(problems),
		theme:      newTheme(nil, true, false),
		message:    saved.Problem,
		viewed:     saved.Viewed,
		saver:      &saver{},
		asker:      asker,
		earlier:    saved.Log,
		records:    saved.Log,
		lockErr:    saved.Lock,
		base:       saved.Base,
		reload:     saved.Reload,
		execute:    tea.ExecProcess,
		open:       agent.Open,
		flights:    &flights{},
		busy:       make(map[session]bool),
		forks:      make(map[session]runFork),
		requests:   make(map[string]*request),
		models:     make(map[string]*modelList),
		now:        time.Now,
		tick:       tick,
	}
	if saved.Dir != "" {
		m.saver.path = state.ViewedPath(saved.Dir)
		m.log = state.NewLog(state.LogPath(saved.Dir))
	}
	if m.viewed == nil {
		m.viewed = state.Viewed{}
	}
	m.questions = earlierQuestions(saved.Log)
	m.comments = loggedComments(saved.Log)
	m.reopenFixes()
	m.setFiles(files)
	return m
}

// setFiles builds everything the screen keeps of files, when it opens and when it rebuilds
// the diff: the lines and rows of both views, which files are viewed, the newest edit of
// each session, the file tree, short session ids, the colors of the sessions, where the
// comments go with the fixes under them, who a asks and where the questions go.
func (m *model) setFiles(files []review.File) {
	m.lines, m.labeled = nil, nil
	m.build(files)
	for i := range m.files {
		f := &m.files[i]
		f.viewed, f.changed = m.viewed.Check(f.key, f.hash)
	}
	m.place()
	m.newest = newestEdits(m.files)
	m.oldPieces = make([][]piece, len(m.lines))
	m.newPieces = make([][]piece, len(m.lines))
	m.tree = tree(m.files)
	var ids []string
	for _, f := range m.files {
		for _, e := range f.requests {
			ids = append(ids, e.Session, e.Parent)
		}
	}
	for _, q := range m.questions {
		ids = append(ids, q.Session) // the sessions asked, which may have no line in this diff
	}
	for _, c := range slices.Concat(m.comments, m.away) {
		ids = append(ids, c.Session) // the sessions comments go to, which may have no line in this diff
	}
	m.short = review.ShortIDs(ids)
	m.placeSessions()
	m.placeComments()
	m.listWidth = m.treeWidth()
	m.whom = review.Asking{
		Lines: m.lines, Root: m.asker.Root, Short: m.short, Installed: m.asker.Installed,
		Busy: func(name, id string) bool { return m.busy[session{name, id}] },
	}
	m.asked = make([][]*question, len(m.lines))
	m.placeQuestions()
}

// Run shows the diff screen for files until the user quits. Then it stops the questions in
// flight, which stay in the log without an answer. A save of the viewed files that was
// still running may not have ended, so Run saves them once more when they changed.
func Run(files []review.File, problems []agent.Problem, saved Saved, asker Asker) error {
	m := newModel(files, problems, saved, asker)
	_, err := tea.NewProgram(m).Run()
	m.flights.stop(stopWait)
	if err != nil {
		return err
	}
	if err := m.saver.save(m.version, m.viewed); err != nil {
		return fmt.Errorf("save viewed state: %w", err)
	}
	return nil
}

// Init starts the screen by asking the terminal for its background color, to pick the dark
// or the light colors.
func (m *model) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// Update handles a resize, a key press, a paste, the mouse, the end of a save of the viewed
// files, an answer or fixes that came, the models an agent lists, the editor closing, the
// files read again after it, a tick, or what the terminal tells of its colors and its
// keyboard. A paste goes into the open input box, and nowhere else. While a question or a
// fix request waits for its answer, a tick comes once a second, so the boxes redraw how long
// it has waited and the spinner on the status line turns; no tick comes at other times. Then
// Update shows the view that fits, scrolls so the cursor stays on the screen and colors the
// files that came on it.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		cmd = m.key(msg)
	case tea.PasteMsg:
		switch {
		case m.typing() && m.entry.picker != nil:
			m.typeQuery(msg.Content)
		case m.typing():
			m.entry.paste(msg.Content)
		}
	case tea.MouseMsg:
		cmd = m.mouse(msg)
	case savedMsg:
		m.saved(msg)
	case answeredMsg:
		m.answered(msg)
	case fixedMsg:
		m.fixed(msg)
	case modelsMsg:
		m.listed(msg)
	case editedMsg:
		cmd = m.edited(msg)
	case reloadedMsg:
		m.reloaded(msg)
	case tickMsg:
		m.ticking = false
	case tea.BackgroundColorMsg:
		m.theme = newTheme(msg.Color, msg.IsDark(), m.theme.plain)
	case tea.ColorProfileMsg:
		m.theme = newTheme(m.theme.bg, m.theme.dark, msg.Profile <= colorprofile.ASCII)
	case tea.KeyboardEnhancementsMsg:
		m.shiftEnter = msg.SupportsKeyDisambiguation()
	}
	if !m.ticking && (len(m.busy) > 0 || m.listLoading()) { // the spinners turn with the seconds
		m.ticking = true
		cmd = tea.Batch(cmd, m.tick())
	}
	if len(m.rows) == 0 {
		return m, cmd
	}
	m.relayout()
	m.scroll()
	m.color()
	return m, cmd
}

// mouse handles the mouse. Dragging the line between the file tree and the diff resizes the
// tree, folds it to a handle at the left edge when dragged narrow, and opens it again when
// the handle is dragged right. A click on a file of the tree or on the Viewed box of a file
// header does what click says. The wheel moves the cursor, or the text of the open origin
// box, wheelLines lines; it extends a selection as a move does. When no changed file is
// left, or while the input box is open, the mouse does nothing. It returns the command that
// saves the viewed files after a click on a Viewed box.
func (m *model) mouse(msg tea.MouseMsg) tea.Cmd {
	if len(m.rows) == 0 || m.typing() {
		m.dragging = false
		return nil
	}
	mouse := msg.Mouse()
	switch msg.(type) {
	case tea.MouseClickMsg:
		m.dragging = mouse.Button == tea.MouseLeft && mouse.Y < m.diffHeight() && m.onDivider(mouse.X)
		if mouse.Button == tea.MouseLeft && !m.dragging && !m.help && !m.reading && !m.search.open {
			return m.click(mouse.X, mouse.Y)
		}
	case tea.MouseMotionMsg:
		if m.dragging {
			m.dragTo(mouse.X)
		}
	case tea.MouseReleaseMsg:
		m.dragging = false
	case tea.MouseWheelMsg:
		n := 0
		switch mouse.Button {
		case tea.MouseWheelDown:
			n = wheelLines
		case tea.MouseWheelUp:
			n = -wheelLines
		}
		switch {
		case m.help:
		case m.reading:
			m.readTop += n // scroll keeps it in the text
		default:
			m.moveTo(m.cursor + n)
		}
	}
	return nil
}

// click handles a click at column x of line y. On a file of the file tree it goes to the
// file's header, and on the Viewed box of a file header it marks the file viewed or not, as
// v does there. Either ends a selection first. A click on a band does nothing. It returns
// the command that saves the viewed files then.
func (m *model) click(x, y int) tea.Cmd {
	h := m.diffHeight()
	if y >= h {
		return nil
	}
	if list, _ := m.layout(); !m.folded && x < list {
		body := m.treeBody(h)
		if i := m.treeTop(body) + y - treeHead; y >= treeHead && y < treeHead+body && i < len(m.tree) && m.tree[i].file >= 0 {
			m.selecting = false
			m.jumpTo(m.headRow(m.tree[i].file))
		}
		return nil
	}
	box := 1 + len(viewedBox) // the check box and its label, before the margin at the right edge
	if y < m.bandLines() {
		return nil
	}
	if i := m.rowAt(y - m.bandLines()); i >= 0 && m.rows[i].kind == fileRow && x >= m.width-1-box && x < m.width-1 {
		m.cursor, m.selecting = i, false
		return m.toggleViewed(m.rows[i].file)
	}
	return nil
}

// onDivider reports whether column x grabs the line between the file tree and the diff, or
// the handle of the folded tree: that column or one beside it.
func (m *model) onDivider(x int) bool {
	at := 0 // the handle
	if !m.folded {
		list, _ := m.layout()
		if list == 0 {
			return false
		}
		at = list
	}
	return x >= at-1 && x <= at+1
}

// dragTo makes the file tree reach column x, as wide as the rules of layout allow. Dragged
// narrower than half its least width, it folds to a handle.
func (m *model) dragTo(x int) {
	most := m.width - 1 - minDiffWidth
	if x < minListWidth/2 || most < minListWidth {
		m.folded = true
		return
	}
	m.folded = false
	m.treeSize = min(max(x, minListWidth), most)
}

// relayout shows the split view when s asked for it and the diff is wide enough for both
// sides, and the unified view otherwise, whether s, a resize or a drag of the file tree
// changed that. A change of view keeps the cursor on the same line, or header, where it was
// on the screen. Leaving the split view from the row it was entered at goes back to the line
// it was entered from, the deleted one of a pair too; from any other row it goes to the line
// on the right, or on the left when the right has none. It ends a selection, whose rows are
// those of the other view, and moves the row the input box is under the same way.
func (m *model) relayout() {
	_, w := m.layout()
	split := m.splitOn && w >= minSplitWidth
	if split == m.splitShown {
		return
	}
	m.splitShown, m.selecting, m.search.hits.ok = split, false, false
	switch {
	case m.asking != nil:
		m.asking.row = m.rows[m.asking.row].twin
	case m.drafting != nil:
		m.drafting.row = m.rows[m.drafting.row].twin
	}
	cursor := m.rows[m.cursor].twin
	switch {
	case split:
		m.enteredFrom, m.enteredAt = m.cursor, cursor
	case m.cursor == m.enteredAt:
		cursor = m.enteredFrom
	}
	m.top = cursor - (m.cursor - m.top) // scroll keeps it in the diff
	m.cursor = cursor
	m.rows = m.unified
	if split {
		m.rows = m.split
	}
}

// diffHeight returns how many lines of the diff fit above the status line and, on a screen
// of minKeyRowHeight rows or more, the key row. It is also the line of the status line.
func (m *model) diffHeight() int {
	h := m.height - 1
	if m.height >= minKeyRowHeight {
		h--
	}
	return max(h, 1)
}

// bandLines returns how many lines of the diff the bands take at its top: a line each, as
// many as leave a line for the rows.
func (m *model) bandLines() int {
	return min(len(m.bands()), m.diffHeight()-1)
}

// rowsHeight returns how many lines of the diff the rows take: those under the bands.
func (m *model) rowsHeight() int {
	return m.diffHeight() - m.bandLines()
}

// scroll moves the screen as little as it takes to show the cursor row, and the open origin
// box under it, and leaves no blank lines at the bottom while there are rows above to show
// instead, as far as the blank line above a file header allows. The box likewise leaves no
// blank lines at its bottom while its text has lines above to show. The open input box
// scrolls its text to show the cursor's row, and the screen then moves to show the whole
// box with its row, the cursor row too when they fit together. The model finder floating
// over the diff scrolls its rows to show the highlighted one.
func (m *model) scroll() {
	h := m.rowsHeight()
	if m.reading {
		m.readTop = max(min(m.readTop, m.blockHeight()-m.boxRows()), 0)
		h = max(h-m.boxHeight(), 1) // the lines that fit beside the card
	}
	m.top = max(min(m.top, m.cursor, m.fitTop(len(m.rows)-1, h)), m.fitTop(m.cursor, h))
	if r := m.entryRow(); r >= 0 {
		_, w := m.layout()
		m.entry.follow(m.entryWidth(w), m.entryRows())
		m.top = max(min(m.top, r), m.fitTop(r, h))
		m.followPicker(w, h)
	}
}

// fitTop returns the first row of a screen h lines high that ends with row last and what
// comes under it: the smallest row whose lines up to there fit. Each row takes a line and
// what comes under it, the boxes of its comments and the input box, and the header of each
// file after the first takes one more for the blank line above it, as GitHub leaves a gap
// between files, unless it is the top row. A top row inside a file takes one more for the
// header that stays on top. When last holds the open input box, only the box counts under
// it: the comments' boxes after it may run past the bottom of the screen.
func (m *model) fitTop(last, h int) int {
	under := m.underHeight(last)
	if last == m.entryRow() {
		_, w := m.layout()
		under = m.entryHeight(w)
	}
	top, lines := last, 1+under
	for top > 0 {
		more := 1 + m.underHeight(top-1) // the row above the top and what comes under it
		if m.rows[top].kind == fileRow {
			more++ // the blank line above the top row, which shows once it is not the top
		}
		head := 0
		if m.rows[top-1].kind != fileRow {
			head = 1 // the header that stays on top above it
		}
		if lines+more+head > h {
			break
		}
		top, lines = top-1, lines+more
	}
	return top
}

// rowAt returns the row drawn on line y of the rows, under the bands, or -1 for a blank line
// or a line of a comment's box or of the input box, as diffLines draws them while the origin
// box is closed. The header that stays on top is that of its file.
func (m *model) rowAt(y int) int {
	line := 0
	if m.sticky() {
		if y == 0 {
			return m.headRow(m.rows[m.top].file)
		}
		line++
	}
	for i := m.top; i < len(m.rows) && line <= y; i++ {
		if i > m.top && m.rows[i].kind == fileRow {
			if line == y {
				return -1 // the blank line above a file header
			}
			line++
		}
		if line == y {
			return i
		}
		line += 1 + m.underHeight(i)
	}
	return -1
}

// unreadableAgents returns the agents whose session logs dr could not fully read, in the
// order of problems. dr's own log is left out: Load tells of it when the screen opens.
func unreadableAgents(problems []agent.Problem) []string {
	var agents []string
	for _, p := range problems {
		if p.Agent != review.FixAgent && !slices.Contains(agents, p.Agent) {
			agents = append(agents, p.Agent)
		}
	}
	return agents
}
