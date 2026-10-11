package origin

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/agent"
)

func whole(after string) func(string) (string, bool) {
	return func(string) (string, bool) { return after, true }
}

// render writes one line per diff line: who made it, its old and new line numbers, its text.
func render(lines []Line, names map[*agent.Edit]string) string {
	var b strings.Builder
	for _, l := range lines {
		who := names[l.Edit]
		switch {
		case l.Old > 0 && l.New > 0:
			who = "="
		case l.Edit == nil:
			who = "?"
		}
		fmt.Fprintf(&b, "%s %d %d %s\n", who, l.Old, l.New, l.Text)
	}
	return b.String()
}

func TestBlame(t *testing.T) {
	// e1 rewrites the whole file but changes two lines. Then a person adds h outside the
	// logs, e2 deletes d, e3 does not fit, and a shell command adds w.
	e1 := &agent.Edit{Apply: whole("a\nB\nc\nd\ne\n")}
	e2 := &agent.Edit{Before: new("a\nh\nB\nc\nd\ne\n"), Apply: whole("a\nh\nB\nc\ne\n")}
	e3 := &agent.Edit{Apply: func(string) (string, bool) { return "", false }}
	names := map[*agent.Edit]string{e1: "e1", e2: "e2", e3: "e3"}

	got := render(Blame("a\nb\nc\nd\n", "a\nh\nB\nc\ne\nw\n", []*agent.Edit{e1, e2, e3}), names)
	want := `= 1 1 a
e1 2 0 b
? 0 2 h
e1 0 3 B
= 3 4 c
e2 4 0 d
e1 0 5 e
? 0 6 w
`
	if got != want {
		t.Errorf("got\n%swant\n%s", got, want)
	}

	// A line deleted outside the logs has no origin either.
	if got, want := render(Blame("a\nb\n", "a\n", nil), nil), "= 1 1 a\n? 2 0 b\n"; got != want {
		t.Errorf("got\n%swant\n%s", got, want)
	}

	// A change that was undone is not in the diff, whether a person or an agent undid it.
	e4 := &agent.Edit{Apply: whole("a\nB\n")}
	e5 := &agent.Edit{Apply: whole("a\nb\n")}
	for _, edits := range [][]*agent.Edit{{e4}, {e4, e5}} {
		if got, want := render(Blame("a\nb\n", "a\nb\n", edits), nil), "= 1 1 a\n= 2 2 b\n"; got != want {
			t.Errorf("after %d edits got\n%swant\n%s", len(edits), got, want)
		}
	}
}

func TestBlameDeletedAgain(t *testing.T) {
	// A deletes b and B puts it back, so b is a base line again and whoever deletes it
	// next deleted it.
	deleteB := &agent.Edit{Apply: whole("a\n")}
	restoreB := &agent.Edit{Apply: whole("a\nb\n")}
	deleteAgain := &agent.Edit{Apply: whole("a\n")}
	names := map[*agent.Edit]string{deleteB: "A", restoreB: "B", deleteAgain: "C"}
	for _, tc := range []struct {
		edits []*agent.Edit
		want  string
	}{
		{[]*agent.Edit{deleteB, restoreB}, "= 1 1 a\n? 2 0 b\n"},
		{[]*agent.Edit{deleteB, restoreB, deleteAgain}, "= 1 1 a\nC 2 0 b\n"},
	} {
		if got := render(Blame("a\nb\n", "a\n", tc.edits), names); got != tc.want {
			t.Errorf("after %d edits got\n%swant\n%s", len(tc.edits), got, tc.want)
		}
	}
}

func TestBlameLongFile(t *testing.T) {
	// The lines all differ and are too many for the LCS table, so the lines that occur
	// once in each version anchor the match.
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}
	base := strings.Join(lines, "\n") + "\n"
	lines[0], lines[len(lines)-1] = "first", "last"
	current := strings.Join(lines, "\n") + "\n"
	e := &agent.Edit{Apply: whole(current)}

	got := make(map[string]int)
	for _, l := range Blame(base, current, []*agent.Edit{e}) {
		switch {
		case l.Old > 0 && l.New > 0:
			got["unchanged"]++
		case l.Edit != e:
			got["without an origin"]++
		case l.New > 0:
			got["added by the edit"]++
		default:
			got["deleted by the edit"]++
		}
	}
	want := map[string]int{"unchanged": 4998, "added by the edit": 2, "deleted by the edit": 2}
	if !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestEdits(t *testing.T) {
	r := Repo{Root: "/w", Worktrees: []string{"/r", "/r/.wt/x", "/w"}, Branch: "feat"}
	edits := []agent.Edit{
		{Session: "this worktree on this branch", Path: "/w/f.go", Cwd: "/w", Branch: "feat"},
		{Session: "this worktree on another branch", Path: "/w/f.go", Cwd: "/w/sub", Branch: "main"},
		{Session: "this worktree from a session elsewhere", Path: "/w/f.go", Cwd: "/r", Branch: "main"},
		{Session: "this worktree without a branch", Path: "/w/f.go", Cwd: "/w"},
		{Session: "another worktree on this branch", Path: "/r/f.go", Cwd: "/r", Branch: "feat"},
		{Session: "another worktree on another branch", Path: "/r/f.go", Cwd: "/r", Branch: "main"},
		{Session: "another worktree without a branch", Path: "/r/f.go", Cwd: "/r"},
		{Session: "nested worktree on this branch", Path: "/r/.wt/x/f.go", Cwd: "/r/.wt/x", Branch: "feat"},
		{Session: "nested worktree from its parent", Path: "/r/.wt/x/f.go", Cwd: "/r", Branch: "feat"},
		{Session: "another file", Path: "/w/g.go", Cwd: "/w", Branch: "feat"},
		{Session: "outside the repository", Path: "/elsewhere/f.go", Cwd: "/elsewhere", Branch: "feat"},
	}
	kept := r.Edits(edits, "f.go")
	got := make([]string, 0, len(kept))
	for _, e := range kept {
		got = append(got, e.Session)
	}
	want := []string{
		"this worktree on this branch",
		"this worktree from a session elsewhere",
		"this worktree without a branch",
		"another worktree on this branch",
		"nested worktree on this branch",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
