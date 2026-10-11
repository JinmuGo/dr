package main

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
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
