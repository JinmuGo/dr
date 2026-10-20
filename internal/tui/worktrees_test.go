package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/charmbracelet/x/ansi"
)

func TestChooser(t *testing.T) {
	worktrees := []git.Worktree{
		{Dir: "/home/dev/dr", Branch: "main", Head: "1111111aaa"},
		{Dir: "/home/dev/wt/fix-save", Branch: "fix/save", Head: "2222222bbb"},
		{Dir: "/tmp/feature", Branch: "feat/picker", Head: "3333333ccc"},
		{Dir: "/home/dev/wt/old", Head: "4444444ddd"},
	}
	for _, tc := range []struct {
		name string
		keys string
		want int // the index picked, -1 for none
		quit bool
	}{
		{"enter opens the current worktree, first", "<enter>", 2, true},
		{"down moves to the next in git's order", "<down><enter>", 0, true},
		{"the highlight stays on the last", "<down><down><down><down><down><enter>", 3, true},
		{"the highlight stays on the first", "<up><ctrl+p><enter>", 2, true},
		{"typing matches as fzf does", "fxsv<enter>", 1, true},
		{"the directory matches too", "wtold<enter>", 3, true},
		{"a detached HEAD matches by its commit", "4444<enter>", 3, true},
		{"backspace widens the match again", "fxsvz<bs><enter>", 1, true},
		{"ctrl+u clears the query", "fxsv<ctrl+u><enter>", 2, true},
		{"enter with no match does nothing", "zzzz<enter>", -1, false},
		{"esc cancels", "<down><esc>", -1, true},
	} {
		c := newChooser(worktrees, 2, "/home/dev", "")
		c.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
		quit := false
		for _, k := range keyPresses(tc.keys) {
			if _, cmd := c.Update(k); cmd != nil {
				quit = true
				break
			}
		}
		if c.picked != tc.want || quit != tc.quit {
			t.Errorf("%s: picked %d, quit %v; want %d, %v", tc.name, c.picked, quit, tc.want, tc.quit)
		}
	}
}

func TestChooserLines(t *testing.T) {
	// The current worktree comes first with its tag, directories under home show with ~,
	// and a short screen scrolls to keep the highlight on it.
	worktrees := []git.Worktree{
		{Dir: "/home/dev/dr", Branch: "main"},
		{Dir: "/home/dev/wt/a", Branch: "a"},
		{Dir: "/home/dev/wt/b", Branch: "b"},
		{Dir: "/srv/c", Branch: "c"},
	}
	c := newChooser(worktrees, 1, "/home/dev", "")
	c.Update(tea.WindowSizeMsg{Width: 50, Height: 6})
	for _, k := range keyPresses("<down><down><down>") {
		c.Update(k)
	}
	lines := c.lines()
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6", len(lines))
	}
	var plain []string
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 50 {
			t.Errorf("line %q is %d cells wide, more than 50", ansi.Strip(l), w)
		}
		plain = append(plain, ansi.Strip(l))
	}
	for i, want := range map[int]string{1: " > ", 2: "4/4", 3: "b     ~/wt/b", 4: "▌  c     /srv/c"} {
		if !strings.Contains(plain[i], want) {
			t.Errorf("line %d is %q, want it to contain %q", i, plain[i], want)
		}
	}
	c = newChooser(worktrees, 1, "/home/dev", "")
	c.Update(tea.WindowSizeMsg{Width: 50, Height: 8})
	if got := ansi.Strip(c.lines()[3]); !strings.HasPrefix(got, "▌  a ") || !strings.HasSuffix(got, "current  ") {
		t.Errorf("first row is %q, want the current worktree, highlighted and tagged", got)
	}
}
