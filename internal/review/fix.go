package review

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/diff"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/state"
)

// FixAgent is the name problems with dr's own log go by, as agent.Problem.Agent.
const FixAgent = "dr"

// fixes reads the fixes dr applied in the worktree at root from the log in its state
// directory, and returns those that :undo did not revert and that were applied at or after
// since, as edits in the order they were applied. A log dr cannot fully read is a problem;
// a missing log has none.
func fixes(root string, since time.Time) ([]agent.Edit, []agent.Problem, error) {
	dir, err := git.Path(root, "dr")
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "log.jsonl")
	records, bad, err := state.NewLog(path).Read()
	if err != nil {
		return nil, []agent.Problem{{Agent: FixAgent, Path: path}}, nil
	}
	var problems []agent.Problem
	if bad > 0 {
		problems = []agent.Problem{{Agent: FixAgent, Path: path, Bad: bad}}
	}
	var edits []agent.Edit
	for _, a := range records.InPlace() {
		if a.Time.Before(since) {
			continue
		}
		edits = append(edits, agent.Edit{
			Agent:   a.Agent,
			Session: a.Session,
			Log:     a.Log,
			Time:    a.Time,
			Cwd:     a.Cwd,
			Path:    filepath.Join(root, filepath.FromSlash(a.Path)),
			Model:   a.Model,
			Effort:  a.Effort,
			Apply:   replaceLines(a.First, a.Old, a.New),
			Fix:     &agent.Fix{ID: a.ID, Comment: a.Text, Note: a.Note, Fork: a.Fork},
		})
	}
	return edits, problems, nil
}

// withFixes returns edits, which are in the order they happened, with fixed, the fixes dr
// applied, among them in the order of their times.
func withFixes(edits, fixed []agent.Edit) []agent.Edit {
	if len(fixed) == 0 {
		return edits
	}
	edits = slices.Concat(edits, fixed)
	slices.SortStableFunc(edits, func(a, b agent.Edit) int { return a.Time.Compare(b.Time) })
	return edits
}

// replaceLines is the edit of a fix: it writes the lines newLines in place of oldLines,
// which start at line first, counted from 1. Like dr when it applies the fix, it goes by
// the line number alone and does not fit a file whose lines there are not oldLines, and it
// renders the result the same way: no lines make an empty file, and lines end with a
// newline when before did.
func replaceLines(first int, oldLines, newLines []string) func(string) (string, bool) {
	return func(before string) (string, bool) {
		lines := diff.Lines(before)
		at := first - 1
		if at < 0 || at+len(oldLines) > len(lines) || !slices.Equal(lines[at:at+len(oldLines)], oldLines) {
			return "", false
		}
		result := slices.Concat(lines[:at], newLines, lines[at+len(oldLines):])
		if len(result) == 0 {
			return "", true
		}
		after := strings.Join(result, "\n")
		if strings.HasSuffix(before, "\n") {
			after += "\n"
		}
		return after, true
	}
}
