package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/JinmuGo/dr/internal/tui"
)

func TestRun(t *testing.T) {
	// What dr writes and the exit code it returns: 2 with the usage for arguments it does
	// not take, 1 with the error, 0 otherwise.
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, home := filepath.Join(tmp, "repo"), filepath.Join(tmp, "home")
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "a.md"), "a\n")
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")
	t.Chdir(repo)
	errNoHome := errors.New("no home")
	info, _ := debug.ReadBuildInfo() // TestVersion covers what versionOf makes of it
	for _, tc := range []struct {
		name           string
		args           []string
		homeErr        error
		code           int
		stdout, stderr string
	}{
		{"a flag", []string{"-h"}, nil, 2, "", usage + "\n"},
		{"blame without a path", []string{"blame"}, nil, 2, "", usage + "\n"},
		{"two refs", []string{"a", "b"}, nil, 2, "", usage + "\n"},
		{"blame with two paths", []string{"blame", "x", "y"}, nil, 2, "", usage + "\n"},
		{"a flag without a home", []string{"-h"}, errNoHome, 2, "", usage + "\n"},
		{"blame outside the worktree", []string{"blame", "../outside.md"}, nil, 1, "", "dr: not in the worktree " + repo + ": ../outside.md\n"},
		{"no home", nil, errNoHome, 1, "", "dr: no home\n"},
		{"no changed files", nil, nil, 0, "No changed files\n", ""},
		{"version", []string{"--version"}, errNoHome, 0, "dr " + versionOf(version, info) + "\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := run(tc.args, &stdout, &stderr, func() (string, error) { return home, tc.homeErr })
			if code != tc.code || stdout.String() != tc.stdout || stderr.String() != tc.stderr {
				t.Errorf("run(%q): got %d, stdout %q, stderr %q; want %d, stdout %q, stderr %q",
					tc.args, code, stdout.String(), stderr.String(), tc.code, tc.stdout, tc.stderr)
			}
		})
	}
}

func TestShowWholeRequest(t *testing.T) {
	// The file and the session log can hold sequences that drive the terminal or reorder
	// what it shows.
	prompt := strings.Repeat("rate limiter를 짜줘. ", 20) + "\n셸 명령은 실행하지 마.\x1b[31m"
	e := &agent.Edit{
		Agent: "claude", Session: "s\x1b]0;x\x07", Parent: "p\u202e", Turn: 1,
		Prompt: prompt, Reply: "다 짰어요.\u009b2J", Note: "짤게요.\u2066",
	}
	var b strings.Builder
	if err := show(&b, []origin.Line{{Text: "\tx := \"\x1b[2K\u202e\"", New: 1, Edit: e}}, nil); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if i := strings.IndexFunc(got, func(r rune) bool {
		return r != '\n' && r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r))
	}); i >= 0 {
		t.Errorf("got a control character at byte %d of %q", i, got)
	}
	for _, want := range []string{
		"  \tx := \"\\x1b[2K\\u202e\"\n", // the file line
		"\n    셸 명령은 실행하지 마.\\x1b[31m\n", // the last line of a long request
	} {
		if !strings.Contains(got, want) {
			t.Errorf("got\n%s\nwant it to contain %q", got, want)
		}
	}
}

func TestShowNotes(t *testing.T) {
	start := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	edit := func(minute int, note string) *agent.Edit {
		return &agent.Edit{
			Agent: "omp", Session: "s", Turn: 1, Time: start.Add(time.Duration(minute) * time.Minute),
			Prompt: "notes.md를 고쳐줘", Reply: "고쳤어요", Note: note,
		}
	}
	// The notes follow the order of the edits, not of the lines. Two calls of one response
	// share what it said, and an edit may have nothing said.
	said := "a를 A로,\nc를 C로 바꿀게요."
	lines := []origin.Line{
		{Text: "B", New: 1, Edit: edit(3, "Replacing b")},
		{Text: "A", New: 2, Edit: edit(1, said)},
		{Text: "C", New: 3, Edit: edit(2, said)},
		{Text: "D", New: 4, Edit: edit(4, "")},
	}
	var b strings.Builder
	if err := show(&b, lines, nil); err != nil {
		t.Fatal(err)
	}
	want := "    request: notes.md를 고쳐줘\n" +
		"    note: a를 A로, c를 C로 바꿀게요.\n" +
		"    note: Replacing b\n" +
		"    reply: 고쳤어요\n"
	if got := b.String(); !strings.Contains(got, want) {
		t.Errorf("got\n%s\nwant it to contain\n%s", got, want)
	}
}

func TestShowUnread(t *testing.T) {
	// A line without an origin in a file with edits dr could not read names those edits.
	var b strings.Builder
	if err := show(&b, []origin.Line{{Text: "b", New: 1}}, []string{"omp ast_edit"}); err != nil {
		t.Fatal(err)
	}
	want := "unknown  Not in any session record dr can read. dr could not read the omp ast_edit edits to this file, which may have written it.\n"
	if got := b.String(); !strings.HasPrefix(got, want) {
		t.Errorf("got\n%s\nwant it to start with\n%s", got, want)
	}
	// The name of a tool comes from a log, so it cannot write to the terminal.
	b.Reset()
	if err := show(&b, []origin.Line{{Text: "b", New: 1}}, []string{"claude mcp__edit\x1b[2J\u202e"}); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); strings.ContainsAny(got, "\x1b\u202e") || !strings.Contains(got, "claude mcp__edit") {
		t.Errorf("got %q, want the tool's name without its control characters", got)
	}
}

func TestOpens(t *testing.T) {
	// With no changed file, the screen opens only when :undo can revert the last fix dr
	// applied: its file is still as the fix left it. Not when that fix was undone, or its
	// file was reverted outside dr, even with an older fix in place, as :undo reverts only
	// the last.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "b\n") // as the fix left it
	writeFile(t, filepath.Join(root, "r.go"), "a\n") // reverted outside dr
	after := fmt.Sprintf("%x", sha256.Sum256([]byte("b\n")))
	inPlace := state.Applied{ID: "f1", Path: "a.go", First: 1, Old: []string{"a"}, New: []string{"b"}, After: after}
	reverted := state.Applied{ID: "f2", Path: "r.go", First: 1, Old: []string{"a"}, New: []string{"b"}, After: after}
	for _, tc := range []struct {
		name  string
		files []review.File
		log   state.Records
		want  bool
	}{
		{"changed files", []review.File{{Path: "a.go"}}, state.Records{}, true},
		{"nothing", nil, state.Records{}, false},
		{"a fix in place", nil, state.Records{Applied: []state.Applied{inPlace}}, true},
		{"a fix undone", nil, state.Records{Applied: []state.Applied{inPlace}, Undone: []state.Undone{{Applied: "f1"}}}, false},
		{"a fix reverted outside dr", nil, state.Records{Applied: []state.Applied{reverted}}, false},
		{"the last fix reverted, an older one in place", nil, state.Records{Applied: []state.Applied{inPlace, reverted}}, false},
		{"the last fix in place, an older one reverted", nil, state.Records{Applied: []state.Applied{reverted, inPlace}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opens(root, tc.files, tc.log); got != tc.want {
				t.Errorf("opens(%d files, %d applied, %d undone) = %t, want %t", len(tc.files), len(tc.log.Applied), len(tc.log.Undone), got, tc.want)
			}
		})
	}
}

func TestChangesEmptyWithUndo(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		"GIT_AUTHOR_DATE": "2026-10-09T12:00:00Z", "GIT_COMMITTER_DATE": "2026-10-09T12:00:00Z",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "a.md"), "b\n")
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".codex/sessions/bad.jsonl"), "not json, from "+root+"\n")
	dir := filepath.Join(root, ".git", state.DirName)
	for _, undoable := range []bool{false, true} {
		t.Run(fmt.Sprintf("undoable=%t", undoable), func(t *testing.T) {
			// An unreadable fix record must survive the fast path even with no changed files.
			writeFile(t, state.LogPath(dir), "not json\n")
			if undoable {
				applied := state.Applied{
					Time: time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC),
					ID:   "fix", Agent: "codex", Session: "session", Path: "a.md", First: 1, Old: []string{"a"}, New: []string{"b"},
					After: fmt.Sprintf("%x", sha256.Sum256([]byte("b\n"))),
				}
				if err := state.NewLog(state.LogPath(dir)).Append(applied); err != nil {
					t.Fatal(err)
				}
			}
			r, files, err := changes(root, home, "", &agent.Loader{})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatalf("changes: got %+v, want no files", files)
			}
			if len(r.Problems) != 1 || r.Problems[0].Agent != review.FixAgent {
				t.Errorf("problems: got %+v, want only the dr log problem", r.Problems)
			}
			saved := tui.Load(dir)
			if got := opens(r.Root, files, saved.Log); got != undoable {
				t.Errorf("opens: got %t, want %t", got, undoable)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	vcs := func(revision, modified string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261011123635-abc1234def56+dirty"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: revision}, {Key: "vcs.modified", Value: modified},
		}}
	}
	for _, tc := range []struct {
		name    string
		ldflags string
		info    *debug.BuildInfo
		want    string
	}{
		{"release build", "0.1.0", &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}, "0.1.0"},
		{"go install", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, "0.1.0"},
		{"clean checkout", "", vcs("abc1234def5678", "false"), "devel (abc1234)"},
		{"modified checkout", "", vcs("abc1234def5678", "true"), "devel (abc1234, modified)"},
		{"no revision", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "devel"},
		{"no build info", "", nil, "devel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionOf(tc.ldflags, tc.info); got != tc.want {
				t.Errorf("versionOf(%q, %+v) = %q, want %q", tc.ldflags, tc.info, got, tc.want)
			}
		})
	}
}

func TestCompared(t *testing.T) {
	// The empty screen names what the diff compared: the default branch by its short name
	// without a ref, and the base as the ref's own commit only when it is.
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "one")
	runGit(t, repo, "branch", "side")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "two")
	runGit(t, repo, "switch", "-q", "side")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "three")
	runGit(t, repo, "switch", "-q", "main")
	commit := func(ref string) string {
		out, err := exec.Command("git", "-C", repo, "rev-parse", ref).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	one, two := commit("main~1"), commit("main")
	for _, tc := range []struct {
		ref, base string
		want      tui.Base
	}{
		{"", two, tui.Base{Ref: "main", Commit: two}},
		{"main~1", one, tui.Base{Ref: "main~1", Commit: one}},
		{"side", one, tui.Base{Ref: "side", Commit: one, MergeBase: true}},
	} {
		if got := compared(repo, tc.ref, tc.base); got != tc.want {
			t.Errorf("compared(%q) = %+v, want %+v", tc.ref, got, tc.want)
		}
	}
}

func TestReviewed(t *testing.T) {
	// dr <branch> opens the worktree that has the branch checked out when that is another
	// worktree, and otherwise keeps the branch as the ref to compare with, as before.
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, feature, gone := filepath.Join(tmp, "repo"), filepath.Join(tmp, "feature"), filepath.Join(tmp, "gone")
	runGit(t, tmp, "init", "-q", "-b", "main", repo)
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "one")
	runGit(t, repo, "branch", "loose")
	runGit(t, repo, "worktree", "add", "-q", "-b", "feature", feature)
	runGit(t, repo, "worktree", "add", "-q", "-b", "deleted", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, in, ref string
		dir, want     string
	}{
		{"no ref", repo, "", repo, ""},
		{"a branch checked out in another worktree", repo, "feature", feature, ""},
		{"the default branch, checked out in another worktree", feature, "main", feature, "main"},
		{"the branch of this worktree", feature, "feature", feature, "feature"},
		{"a branch no worktree has", repo, "loose", repo, "loose"},
		{"a branch named in full", repo, "refs/heads/feature", repo, "refs/heads/feature"},
		{"a branch whose worktree is gone", repo, "deleted", repo, "deleted"},
	} {
		dir, ref, err := reviewed(tc.in, tc.ref)
		if err != nil || dir != tc.dir || ref != tc.want {
			t.Errorf("%s: reviewed(%q, %q) = %q, %q, %v; want %q, %q", tc.name, tc.in, tc.ref, dir, ref, err, tc.dir, tc.want)
		}
	}
}
