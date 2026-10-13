package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// blame writes to w the requests behind the changed lines of the file at path and its
// diff from the base commit, in the worktree that holds dir, with the agent edits read
// from the session logs under home. It names the logs it could not fully read on stderr.
func blame(w, stderr io.Writer, dir, home, path string) error {
	repo, err := review.Open(dir, home, "")
	if err != nil {
		return err
	}
	relativePath, err := locate(repo.Root, path)
	if err != nil {
		return err
	}
	f, err := repo.File(relativePath)
	for _, p := range repo.Problems {
		_, _ = fmt.Fprintf(stderr, "dr: some %s records could not be read: %s\n", p.Agent, p.Path)
	}
	if err != nil {
		return err
	}
	if f.NoDiff != "" {
		_, err = fmt.Fprintln(w, f.NoDiff)
		return err
	}
	return show(w, f.Lines)
}

// locate returns path relative to the worktree root, slash-separated.
func locate(root, path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// git resolves symlinks in root, so resolve them in path too.
	if dir, err := filepath.EvalSymlinks(filepath.Dir(absolutePath)); err == nil {
		absolutePath = filepath.Join(dir, filepath.Base(absolutePath))
	}
	relativePath, err := filepath.Rel(root, absolutePath)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("not in the worktree %s: %s", root, path)
	}
	return filepath.ToSlash(relativePath), nil
}

// show writes the requests behind the changed lines, then the file as a diff with the
// origin of each changed line.
func show(w io.Writer, lines []origin.Line) error {
	ew := &errWriter{w: w}
	requests := review.Requests(lines)
	notes := notesByRequest(lines)
	var ids []string
	for _, e := range requests {
		ids = append(ids, e.Session, e.Parent)
	}
	short := review.ShortIDs(ids)
	tag := func(e *agent.Edit) string {
		if e == nil {
			return "unknown"
		}
		return review.Tag(e, short)
	}
	unknown := false
	for _, l := range lines {
		if (l.Old == 0 || l.New == 0) && l.Edit == nil {
			unknown = true
		}
	}

	for _, e := range requests {
		ew.printf("%s  %s", tag(e), review.Visible(e.Session))
		if e.Fix != nil {
			// A fix dr applied: the comment it was for, and what the session sent with it.
			ew.printf("\n    comment: %s\n", indent(e.Fix.Comment))
			if e.Fix.Note != "" {
				ew.printf("    note: %s\n", excerpt(e.Fix.Note))
			}
			continue
		}
		if e.Parent != "" {
			ew.printf("  (subagent of %s)", short[e.Parent])
		}
		// The whole request, since its later lines often hold the constraints.
		ew.printf("\n    request: %s\n", indent(e.Prompt))
		for _, note := range notes[request{e.Agent, e.Session, e.Turn}] {
			ew.printf("    note: %s\n", note)
		}
		if e.Reply != "" {
			ew.printf("    reply: %s\n", excerpt(e.Reply))
		}
	}
	if unknown {
		ew.printf("unknown  %s\n", review.NoOrigin)
	}
	if len(requests) > 0 || unknown {
		ew.printf("\n")
	}

	width, last := 0, 0
	for _, l := range lines {
		if l.Old == 0 || l.New == 0 {
			width = max(width, len(tag(l.Edit)))
		}
		last = max(last, l.New)
	}
	digits := len(strconv.Itoa(last))
	for _, l := range lines {
		who, number := "", strconv.Itoa(l.New)
		if l.Old == 0 || l.New == 0 {
			who = tag(l.Edit)
		}
		if l.New == 0 {
			number = "-"
		}
		ew.printf("%-*s  %*s  %s\n", width, who, digits, number, review.Visible(l.Text))
	}
	return ew.err
}

// errWriter writes to w until a write fails and then remembers the error, so a run of
// writes needs one check at the end.
type errWriter struct {
	w   io.Writer
	err error // the first write error
}

// printf writes like fmt.Fprintf, or does nothing once a write has failed.
func (ew *errWriter) printf(format string, args ...any) {
	if ew.err == nil {
		_, ew.err = fmt.Fprintf(ew.w, format, args...)
	}
}

// request is one request of a session.
type request struct {
	agent, session string
	turn           int
}

// notesByRequest returns what the agent said just before the edits behind lines, as
// excerpts by request, in the order the edits were made and each text once.
func notesByRequest(lines []origin.Line) map[request][]string {
	var edits []*agent.Edit
	seen := make(map[*agent.Edit]bool)
	for _, l := range lines {
		if e := l.Edit; e != nil && e.Note != "" && !seen[e] {
			seen[e] = true
			edits = append(edits, e)
		}
	}
	slices.SortStableFunc(edits, func(a, b *agent.Edit) int { return a.Time.Compare(b.Time) })
	notes := make(map[request][]string)
	for _, e := range edits {
		k := request{e.Agent, e.Session, e.Turn}
		if s := excerpt(e.Note); !slices.Contains(notes[k], s) {
			notes[k] = append(notes[k], s)
		}
	}
	return notes
}

// excerpt returns s on one line, cut to 120 characters, made visible.
func excerpt(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > 120 {
		s = string(runes[:119]) + "…"
	}
	return review.Visible(s)
}

// indent returns s without surrounding space, made visible, with its later lines indented
// under the first in the list of requests.
func indent(s string) string {
	return strings.ReplaceAll(review.Visible(strings.TrimSpace(s)), "\n", "\n    ")
}
