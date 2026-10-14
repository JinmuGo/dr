package review_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

func TestRepoFixes(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, home := filepath.Join(tmp, "repo"), filepath.Join(tmp, "home")
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		// The base commit comes before the sessions and the fixes.
		"GIT_AUTHOR_DATE": "2026-10-09T12:00:00Z", "GIT_COMMITTER_DATE": "2026-10-09T12:00:00Z",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "omp/notes.md"), "a\nold\nc\n")
	writeFile(t, filepath.Join(repo, "undone.md"), "x\n")
	writeFile(t, filepath.Join(repo, "moved.md"), "p\nq\n")
	writeFile(t, filepath.Join(repo, "blank.md"), "old\n")
	writeFile(t, filepath.Join(repo, "refilled.md"), "old\n")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")

	// The OMP session wrote "a B c d" in three requests, then dr applied two fixes of B in
	// turn. A fix of undone.md was undone, which left y there, and the fix of moved.md does
	// not fit the file.
	writeFile(t, filepath.Join(repo, "omp/notes.md"), "a\nB2\nB4\nc\nd\n")
	writeFile(t, filepath.Join(repo, "undone.md"), "y\n")
	writeFile(t, filepath.Join(repo, "moved.md"), "p\nQ\n")
	// A fix left one empty line in blank.md and refilled.md, which dr writes as "\n", and
	// a second fix wrote that line of refilled.md again.
	writeFile(t, filepath.Join(repo, "blank.md"), "\n")
	writeFile(t, filepath.Join(repo, "refilled.md"), "new\n")
	const session = "01a1f000-0000-7000-8000-000000000001"
	log := readFile(t, filepath.Join("../agent/testdata", "omp-"+session+".jsonl"))
	writeFile(t, filepath.Join(home, ".omp/agent/sessions/spike/1.jsonl"), strings.ReplaceAll(log, spike, repo))

	var loader agent.Loader
	open := func(t *testing.T) *review.Repo {
		t.Helper()
		r, err := review.Open(repo, home, "", &loader)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	drProblems := func(r *review.Repo) []agent.Problem {
		var problems []agent.Problem
		for _, p := range r.Problems {
			if p.Agent == "dr" {
				problems = append(problems, p)
			}
		}
		return problems
	}
	if problems := drProblems(open(t)); len(problems) != 0 {
		t.Errorf("without a log: got problems %+v, want none", problems)
	}

	logPath := filepath.Join(repo, ".git", "dr", "log.jsonl")
	fixAt := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	fix := func(id, path string, minute, first int, oldLines, newLines []string) state.Applied {
		return state.Applied{
			Time: fixAt.Add(time.Duration(minute) * time.Minute), ID: id, Comment: "c-" + id, Text: "comment " + id, Note: "note " + id,
			Agent: "omp", Session: session, Log: "/h/1.jsonl", Cwd: repo, Fork: "fork-" + id, Model: "anthropic/claude-opus-5-5",
			Path: path, First: first, Old: oldLines, New: newLines,
		}
	}
	l := state.NewLog(logPath)
	for _, r := range []state.Record{
		fix("f1", "omp/notes.md", 0, 2, []string{"B"}, []string{"B2", "B3"}),
		fix("f2", "omp/notes.md", 1, 3, []string{"B3"}, []string{"B4"}),
		fix("u1", "undone.md", 2, 1, []string{"x"}, []string{"y"}),
		state.Undone{Time: fixAt.Add(3 * time.Minute), Applied: "u1"},
		fix("m1", "moved.md", 4, 2, []string{"z"}, []string{"Q"}),
		fix("e1", "blank.md", 5, 1, []string{"old"}, []string{""}),
		fix("r1", "refilled.md", 6, 1, []string{"old"}, []string{""}),
		fix("r2", "refilled.md", 7, 1, []string{""}, []string{"new"}),
	} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not a record\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r := open(t)
	if got, want := drProblems(r), []agent.Problem{{Agent: "dr", Path: logPath, Bad: 1}}; !slices.Equal(got, want) {
		t.Errorf("with a bad line: got problems %+v, want %+v", got, want)
	}
	// who says who made a changed line: "fix f1" for a fix dr applied.
	who := func(l origin.Line) string {
		switch {
		case l.Old > 0 && l.New > 0:
			return ""
		case l.Edit != nil && l.Edit.Fix != nil:
			return "fix " + l.Edit.Fix.ID
		}
		return tag(l)
	}
	for _, tc := range []struct {
		path string
		want []string
	}{
		// The fixes wrote B2 and then B4. The lines they did not change keep their origins.
		{"omp/notes.md", []string{"a", "omp #1 old", "fix f1 B2", "fix f2 B4", "c", "omp #3 d"}},
		// An undone fix leaves no trace: y has no origin.
		{"undone.md", []string{"? x", "? y"}},
		// A fix that does not fit is skipped.
		{"moved.md", []string{"p", "? q", "? Q"}},
		// A fix that leaves one empty line made that line, and a later fix of it fits.
		{"blank.md", []string{"fix e1 old", "fix e1"}},
		{"refilled.md", []string{"fix r1 old", "fix r2 new"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			f, err := r.File(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, l := range f.Lines {
				got = append(got, strings.TrimSpace(who(l)+" "+l.Text))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("the fix edit", func(t *testing.T) {
		f, err := r.File("omp/notes.md")
		if err != nil {
			t.Fatal(err)
		}
		e := f.Lines[2].Edit
		want := agent.Fix{ID: "f1", Comment: "comment f1", Note: "note f1", Fork: "fork-f1"}
		if e == nil || e.Fix == nil || *e.Fix != want || e.Agent != "omp" || e.Session != session || e.Log != "/h/1.jsonl" ||
			e.Cwd != repo || e.Model != "anthropic/claude-opus-5-5" || e.Path != filepath.Join(repo, "omp", "notes.md") || !e.Time.Equal(fixAt) {
			t.Errorf("got %+v, want the edit of fix f1", e)
		}
	})

	t.Run("an unreadable log", func(t *testing.T) {
		if err := os.Remove(logPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(logPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if got, want := drProblems(open(t)), []agent.Problem{{Agent: "dr", Path: logPath}}; !slices.Equal(got, want) {
			t.Errorf("got problems %+v, want %+v", got, want)
		}
	})
}

func TestTargetFix(t *testing.T) {
	// A fix dr applied for a comment to c1, after c1Later. The comment on a1's lines went to
	// c1, which started a1.
	cFix := &agent.Edit{
		Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5", Time: at.Add(20 * time.Minute),
		Fix: &agent.Fix{ID: "f1", Comment: "use errors.Is", Fork: "k1"},
	}
	ask := agent.Question{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5"}
	all := []string{"claude", "codex", "omp"}
	for _, tc := range []struct {
		name          string
		lines, review []origin.Line
		want          review.Target
	}{
		{
			// The fork that sent the fix knows of it, so the fix is not newer than its base.
			name:   "a fixed line",
			lines:  []origin.Line{added(cFix)},
			review: []origin.Line{added(c1), added(cFix), added(c1Later)},
			want:   review.Target{Relation: review.Author, Ask: ask, Base: c1Later.Time, Edit: cFix},
		},
		{
			name:   "fixed lines with the session's own",
			lines:  []origin.Line{added(c1), unchanged, added(cFix)},
			review: []origin.Line{added(c1), added(cFix)},
			want:   review.Target{Relation: review.Author, Ask: ask, Base: c1.Time, Edit: cFix},
		},
		{
			name:   "a fix of a Claude subagent's lines",
			lines:  []origin.Line{added(cFix)},
			review: []origin.Line{added(a1), added(cFix)},
			want:   review.Target{Relation: review.Author, Ask: ask, Base: a1.Time, Edit: cFix},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, why := asking(tc.review, all).Target(tc.lines, tc.review)
			if why != "" || got != tc.want {
				t.Errorf("got %+v, %q; want %+v", got, why, tc.want)
			}
		})
	}
}

func TestTagFix(t *testing.T) {
	short := map[string]string{"c1": "c1a0de00"}
	fixed := &agent.Edit{Agent: "claude", Session: "c1", Fix: &agent.Fix{ID: "f1"}}
	if got, want := review.Tag(fixed, short), "claude c1a0de00 fix"; got != want {
		t.Errorf("a fix: got %q, want %q", got, want)
	}
	// Each fix is a request of its own, apart from the session's turns.
	other := &agent.Edit{Agent: "claude", Session: "c1", Time: at.Add(time.Hour), Fix: &agent.Fix{ID: "f2"}}
	got := review.Requests([]origin.Line{added(c1), added(fixed), added(other), added(fixed)})
	if want := []*agent.Edit{fixed, c1, other}; !slices.Equal(got, want) {
		t.Errorf("requests: got %v, want %v", got, want)
	}
}
