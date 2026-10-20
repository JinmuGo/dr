package tui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// chooserChrome is the lines of the worktree chooser that are not rows: the title, the
// prompt, the count and the key row.
const chooserChrome = 4

// Choose asks on a screen of its own which of worktrees dr opens, and returns its index, or
// -1 when the person cancels. current is the index of the worktree dr was started in, -1 for
// none; it comes first and is highlighted. Directories under home show with ~, and themeName
// is the theme of the diff screen.
func Choose(worktrees []git.Worktree, current int, home, themeName string) (int, error) {
	c := newChooser(worktrees, current, home, themeName)
	if _, err := tea.NewProgram(c).Run(); err != nil {
		return -1, err
	}
	return c.picked, nil
}

// chooser is the screen that asks which worktree dr opens, drawn the way fzf
// --layout=reverse draws: a title, a prompt with the query, a line that counts the matches,
// and the worktrees that match with the characters that matched lit, the best match first.
type chooser struct {
	all       []chooseRow // every worktree, the current one first
	shown     []chooseRow // those of all that match the query, as rankModels orders them
	query     string
	at, top   int // the highlighted row of shown and the first row on the screen
	picked    int // the index of the worktree picked, -1 until one is
	width     int
	height    int
	themeName string
	dark      bool
	theme     theme
	slab      *util.Slab
}

// chooseRow is a worktree in the chooser.
type chooseRow struct {
	index int    // which of the worktrees given to Choose it is
	text  string // what the query matches and the row shows: the branch, padded, and the directory
	tag   string // "current" for the worktree dr was started in
	lit   []int  // the runes of text the query matched, in order
}

// newChooser returns the chooser for Choose, its arguments as Choose takes them.
func newChooser(worktrees []git.Worktree, current int, home, themeName string) *chooser {
	initAlgo()
	names := make([]string, len(worktrees))
	width := 0
	for i, w := range worktrees {
		names[i] = review.VisibleLine(w.Branch)
		if w.Branch == "" {
			names[i] = "(detached " + w.Head[:min(len(w.Head), 7)] + ")"
		}
		width = max(width, ansi.StringWidth(names[i]))
	}
	c := &chooser{picked: -1, themeName: themeName, dark: true, theme: themeFor(themeName, nil, true, false),
		slab: util.MakeSlab(16*1024, 2048)}
	for i, w := range worktrees {
		dir := w.Dir
		if rel, err := filepath.Rel(home, dir); home != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			dir = filepath.Join("~", rel)
		}
		r := chooseRow{index: i, text: names[i] + strings.Repeat(" ", width-ansi.StringWidth(names[i])+2) + review.VisibleLine(dir)}
		if i == current {
			r.tag = "current"
			c.all = slices.Insert(c.all, 0, r)
		} else {
			c.all = append(c.all, r)
		}
	}
	c.setQuery("")
	return c
}

// setQuery sets the query and highlights the best match.
func (c *chooser) setQuery(query string) {
	c.query, c.at, c.top = query, 0, 0
	c.shown = c.shown[:0]
	if query == "" {
		c.shown = append(c.shown, c.all...)
		return
	}
	pattern := []rune(strings.ToLower(query))
	type hit struct {
		row   chooseRow
		score int
	}
	var hits []hit
	for _, r := range c.all {
		chars := util.ToChars([]byte(r.text))
		res, pos := algo.FuzzyMatchV2(false, true, true, &chars, pattern, true, c.slab)
		if res.Start < 0 {
			continue
		}
		r.lit = slices.Sorted(slices.Values(*pos))
		hits = append(hits, hit{r, res.Score})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.score - a.score })
	for _, h := range hits {
		c.shown = append(c.shown, h.row)
	}
}

// Init asks the terminal for its background color, to pick the dark or the light colors.
func (c *chooser) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

// Update handles a resize, what the terminal tells of its colors, a paste and a key. Typing
// edits the query, Backspace deletes its last character and ctrl+u clears it, as in fzf.
// Up, Down, ctrl+p, ctrl+n, ctrl+k and ctrl+j move the highlight, Enter opens the
// highlighted worktree, and Esc and ctrl+c quit without one.
func (c *chooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width, c.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		c.dark = msg.IsDark()
		c.theme = themeFor(c.themeName, msg.Color, c.dark, c.theme.plain)
	case tea.ColorProfileMsg:
		c.theme = themeFor(c.themeName, c.theme.bg, c.dark, msg.Profile <= colorprofile.ASCII)
	case tea.PasteMsg:
		c.typeQuery(msg.Content)
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case keyUp, keyCtrlP, "ctrl+k":
			c.at = max(c.at-1, 0)
		case keyDown, keyCtrlN, keyCtrlJ:
			c.at = max(min(c.at+1, len(c.shown)-1), 0)
		case keyEnter:
			if len(c.shown) > 0 {
				c.picked = c.shown[c.at].index
				return c, tea.Quit
			}
		case keyEsc, keyCtrlC:
			return c, tea.Quit
		case keyCtrlU:
			c.setQuery("")
		case keyBackspace:
			_, n := utf8.DecodeLastRuneInString(c.query)
			c.setQuery(c.query[:len(c.query)-n])
		default:
			c.typeQuery(msg.Text)
		}
	}
	n := max(c.height-chooserChrome, 1)
	c.top = min(c.top, c.at)
	c.top = max(c.top, c.at-n+1)
	return c, nil
}

// typeQuery adds text, typed or pasted, to the query, on one line.
func (c *chooser) typeQuery(text string) {
	if text = strings.ReplaceAll(clean(text), "\n", " "); text != "" {
		c.setQuery(c.query + text)
	}
}

// View draws the chooser on the alternate screen, with the terminal's cursor after the query.
func (c *chooser) View() tea.View {
	v := tea.NewView(strings.Join(c.lines(), "\n"))
	v.AltScreen = true
	if c.height > 1 {
		v.Cursor = tea.NewCursor(min(3+ansi.StringWidth(review.VisibleLine(c.query)), max(c.width-1, 0)), 1)
	}
	return v
}

// lines returns the lines of the chooser, c.width cells wide: the title, the prompt > with
// the query, the count of the worktrees that match and of all, the rows, and the keys at the
// bottom. A row shows the branch, or the commit of a detached HEAD, and the directory, with
// the characters the query matched in the ask color and bold and "current" at the right;
// the highlighted row is on the selection band with ▌.
func (c *chooser) lines() []string {
	if c.width <= 0 || c.height <= 0 {
		return nil
	}
	t := &c.theme
	base := t.paint(nil, nil)
	title := newLine(c.width)
	title.add(" Open dr in which worktree?", t.paint(t.muted, nil))
	prompt := newLine(c.width)
	prompt.add(" ", base)
	prompt.add(">", t.paint(t.ask, nil).Bold(true))
	prompt.add(" "+keepEnd(review.VisibleLine(c.query), max(c.width-4, 0)), t.paint(t.strong, nil))
	count := newLine(c.width)
	count.add("   "+strconv.Itoa(len(c.shown))+"/"+strconv.Itoa(len(c.all))+" ", t.paint(t.subtle, nil))
	count.add(strings.Repeat("─", max(count.free-1, 0)), t.paint(t.line, nil))
	lines := []string{title.fill(base), prompt.fill(base), count.fill(base)}
	n := max(c.height-chooserChrome, 0)
	for k := c.top; k < min(c.top+n, len(c.shown)); k++ {
		lines = append(lines, c.row(c.shown[k], k == c.at))
	}
	for len(lines) < c.height-1 {
		lines = append(lines, "")
	}
	keys := newLine(c.width)
	keys.add(" ↑↓ move  ⏎ open  esc cancel", t.paint(t.subtle, nil))
	lines = append(lines, keys.fill(base))
	return lines[:min(len(lines), c.height)]
}

// row draws r, highlighted or not, c.width cells wide.
func (c *chooser) row(r chooseRow, highlighted bool) string {
	t := &c.theme
	bg, on := t.surface2, t.onBand
	if !highlighted {
		bg, on = nil, t.paint
	}
	base := on(nil, bg)
	head := newLine(min(3, c.width))
	if highlighted {
		head.add("▌", on(t.ask, bg))
	}
	tagWidth := 0
	if r.tag != "" && c.width >= len(r.tag)+20 {
		tagWidth = len(r.tag) + 3
	}
	tail := newLine(tagWidth)
	tail.add(" "+r.tag, on(t.subtle, bg))
	text := t.fg
	if highlighted {
		text = t.strong
	}
	l := newLine(c.width - min(3, c.width) - tagWidth)
	lit := r.lit
	k := 0 // the rune of text at the loop
	for _, ch := range r.text {
		st := on(text, bg)
		if len(lit) > 0 && lit[0] == k {
			st, lit = on(t.ask, bg).Bold(true), lit[1:]
		}
		l.add(string(ch), st)
		k++
	}
	return head.fill(base) + l.fill(base) + tail.fill(base)
}
