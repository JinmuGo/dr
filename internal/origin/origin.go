// Package origin finds the agent edit behind each changed line of a file, the way git blame
// finds the commit: it replays the edits from the base commit on and remembers which edit
// last changed each line.
package origin

import (
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/diff"
)

// Line is one line of a file's diff from the base commit.
type Line struct {
	Text string
	Old  int // line number in the base file, 0 for a line the diff adds
	New  int // line number in the current file, 0 for a line the diff deletes
	// Edit is the edit that wrote the line, or deleted it. It is nil for a line the diff
	// leaves alone, and for a changed line no session log explains.
	Edit *agent.Edit
}

// Repo is the worktree dr runs in.
type Repo struct {
	Root      string   // top directory of this worktree
	Worktrees []string // top directories of every worktree of the repository
	Branch    string   // current branch, "" when HEAD is detached
}

// Edits returns the edits to the file at relativePath, a slash-separated path in the
// worktree, that were made on the current branch.
func (r Repo) Edits(edits []agent.Edit, relativePath string) []*agent.Edit {
	var out []*agent.Edit
	for i := range edits {
		e := &edits[i]
		worktree := r.worktree(e.Path)
		if worktree == "" {
			continue
		}
		if p, err := filepath.Rel(worktree, e.Path); err != nil || filepath.ToSlash(p) != relativePath {
			continue
		}
		// A session logs the branch of its own working directory, which says nothing about
		// a file it edited in another worktree.
		inCwd := r.worktree(e.Cwd) == worktree
		if worktree == r.Root {
			if inCwd && e.Branch != "" && r.Branch != "" && e.Branch != r.Branch {
				continue
			}
		} else if !inCwd || e.Branch == "" || e.Branch != r.Branch {
			// Another worktree counts only when the log shows it was on this branch.
			continue
		}
		out = append(out, e)
	}
	return out
}

// worktree returns the worktree that holds path, the deepest one when worktrees nest.
func (r Repo) worktree(path string) string {
	best := ""
	for _, w := range r.Worktrees {
		if (path == w || strings.HasPrefix(path, w+string(filepath.Separator))) && len(w) > len(best) {
			best = w
		}
	}
	return best
}

type line struct {
	text string
	old  int         // line number in the base file while the line is the base line, else 0
	by   *agent.Edit // the edit that wrote the line
}

// Blame returns the diff from base to current with the edit that made each changed line.
// The diff compares base and current directly, so a change that was later undone does not
// show. The replay of edits on top of base only says who made each line of it.
func Blame(base, current string, edits []*agent.Edit) []Line {
	baseLines := diff.Lines(base)
	state := make([]line, len(baseLines))
	numbers := make(map[string][]int, len(baseLines)) // base line text → its line numbers
	for i, t := range baseLines {
		state[i] = line{text: t, old: i + 1}
		numbers[t] = append(numbers[t], i+1)
	}
	deleted := make(map[int]*agent.Edit) // base line number → the edit that deleted it
	text := base
	for _, e := range edits {
		if e.Before != nil {
			// The edit saw the file as Before. Where the replay differs, the file changed
			// outside the logs, so those lines have no origin.
			state = step(state, *e.Before, nil, numbers, deleted)
			text = *e.Before
		}
		after, ok := e.Apply(text)
		if !ok {
			continue // the edit does not fit; the lines it changed end up without an origin
		}
		state = step(state, after, e, numbers, deleted)
		text = after
	}
	state = step(state, current, nil, numbers, deleted)
	return merge(baseLines, state, deleted)
}

// step changes state into the lines of next. A line that stays keeps its origin, a new
// line comes from by, and a base line that goes is deleted by by. A new line with the text
// of a deleted base line that fits where it is brings that base line back, so the edit
// that deletes it again is its origin. numbers holds the line numbers of each base text.
func step(state []line, next string, by *agent.Edit, numbers map[string][]int, deleted map[int]*agent.Edit) []line {
	lines := diff.Lines(next)
	texts := make([]string, len(state))
	for i, l := range state {
		texts[i] = l.text
	}
	match := diff.Match(texts, lines)
	kept := make([]bool, len(state))
	out := make([]line, len(lines))
	for j, i := range match {
		if i >= 0 {
			out[j] = state[i]
			kept[i] = true
		} else {
			out[j] = line{text: lines[j], by: by}
		}
	}
	for i, l := range state {
		if !kept[i] && l.old > 0 {
			deleted[l.old] = by
		}
	}

	// Base lines keep their order, so every base line between the ones around a new line
	// is deleted. The new line brings back the first of them with its text, after any that
	// new lines before it brought back.
	bound := make([]int, len(out)) // number of the first base line after each line
	after := math.MaxInt
	for j := len(out) - 1; j >= 0; j-- {
		bound[j] = after
		if out[j].old > 0 {
			after = out[j].old
		}
	}
	last := 0 // number of the last base line so far
	for j, i := range match {
		switch l := &out[j]; {
		case l.old > 0:
			last = l.old
		case i < 0:
			same := numbers[l.text]
			if k, _ := slices.BinarySearch(same, last+1); k < len(same) && same[k] < bound[j] {
				l.old, l.by = same[k], nil
				delete(deleted, l.old)
				last = l.old
			}
		}
	}
	return out
}

// merge lays out the diff from base to current like a unified diff: each run of changes
// shows its deleted lines first. A line the replay kept as a base line but the diff
// pairs differently, which can happen among repeated lines, has no origin.
func merge(base []string, current []line, deleted map[int]*agent.Edit) []Line {
	texts := make([]string, len(current))
	for j, l := range current {
		texts[j] = l.text
	}
	match := diff.Match(base, texts)
	var out []Line
	next := 0 // index of the next base line to place
	upto := func(n int) {
		for ; next < n; next++ {
			out = append(out, Line{Text: base[next], Old: next + 1, Edit: deleted[next+1]})
		}
	}
	for j, i := range match {
		if i >= 0 {
			upto(i)
			next = i + 1
			out = append(out, Line{Text: texts[j], Old: i + 1, New: j + 1})
			continue
		}
		if j == 0 || match[j-1] >= 0 {
			n := len(base)
			for _, k := range match[j:] {
				if k >= 0 {
					n = k
					break
				}
			}
			upto(n)
		}
		var by *agent.Edit
		if current[j].old == 0 {
			by = current[j].by
		}
		out = append(out, Line{Text: texts[j], New: j + 1, Edit: by})
	}
	upto(len(base))
	return out
}
