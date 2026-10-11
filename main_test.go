package main

import (
	"os"
	"os/exec"
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

func TestShowWholeRequest(t *testing.T) {
	// The file and the session log can hold sequences that drive the terminal or reorder
	// what it shows.
	prompt := strings.Repeat("rate limiter를 짜줘. ", 20) + "\n셸 명령은 실행하지 마.\x1b[31m"
	e := &agent.Edit{
		Agent: "claude", Session: "s\x1b]0;x\x07", Parent: "p\u202e", Turn: 1,
		Prompt: prompt, Reply: "다 짰어요.\u009b2J", Note: "짤게요.\u2066",
	}
	var b strings.Builder
	if err := show(&b, []origin.Line{{Text: "\tx := \"\x1b[2K\u202e\"", New: 1, Edit: e}}); err != nil {
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
	if err := show(&b, lines); err != nil {
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

func TestOpens(t *testing.T) {
	// With no changed file, the screen opens only when a fix dr applied is still in place,
	// for :undo to revert.
	applied := []state.Applied{{ID: "f1", Path: "a.go", First: 1, Old: []string{"a"}, New: []string{"b"}}}
	for _, tc := range []struct {
		name  string
		files []review.File
		log   state.Records
		want  bool
	}{
		{"changed files", []review.File{{Path: "a.go"}}, state.Records{}, true},
		{"nothing", nil, state.Records{}, false},
		{"a fix in place", nil, state.Records{Applied: applied}, true},
		{"a fix undone", nil, state.Records{Applied: applied, Undone: []state.Undone{{Applied: "f1"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opens(tc.files, tc.log); got != tc.want {
				t.Errorf("opens(%d files, %d applied, %d undone) = %t, want %t", len(tc.files), len(tc.log.Applied), len(tc.log.Undone), got, tc.want)
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
