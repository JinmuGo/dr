package review

import (
	"slices"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/origin"
)

func TestPrompt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines []origin.Line
		want  string // after the first line and before the question
	}{
		{"one added line", []origin.Line{{Text: "\tburst  float64", New: 14}}, "ratelimit.go:14\n>\tburst  float64\n"},
		{"one deleted line", []origin.Line{{Text: "\trate   int", Old: 15}}, "ratelimit.go, deleted line 15 of the base file. Lines marked - were deleted.\n-\trate   int\n"},
		{
			"a mixed selection",
			[]origin.Line{{Text: "\tburst  float64", Old: 14, New: 14}, {Text: "\trate   int", Old: 15}, {Text: "\trate   float64", New: 15}, {Text: "\ttokens float64", New: 16}},
			"ratelimit.go:14-16. Lines marked - were deleted.\n>\tburst  float64\n-\trate   int\n>\trate   float64\n>\ttokens float64\n",
		},
		{
			"deleted lines only",
			[]origin.Line{{Text: "a", Old: 14}, {Text: "b", Old: 15}, {Text: "c", Old: 16}},
			"ratelimit.go, deleted lines 14-16 of the base file. Lines marked - were deleted.\n-a\n-b\n-c\n",
		},
	} {
		want := "[dr] Review question. Do not change any code, just answer.\n" + tc.want + "Why?" + questionEnd
		if got := Prompt(Author, "ratelimit.go", tc.lines, "Why?"); got != want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, want)
		}
	}
	// A session that did not write the lines first learns how it relates to them, and the
	// end of the question asks for what it can know.
	lines := []origin.Line{{Text: "\tburst  float64", New: 14}}
	for _, tc := range []struct {
		relation   Relation
		intro, end string
	}{
		{Parent, parentIntro, parentEnd},
		{Editor, editorIntro, editorEnd},
		{NewSession, newIntro, newEnd},
	} {
		want := "[dr] Review question. Do not change any code, just answer.\n" + tc.intro + "\nratelimit.go:14\n>\tburst  float64\nWhy?" + tc.end
		if got := Prompt(tc.relation, "ratelimit.go", lines, "Why?"); got != want {
			t.Errorf("%s:\n got %q\nwant %q", tc.relation, got, want)
		}
	}
	// Each end of the question asks for the tags the session can give, then one sentence of
	// at most 25 words: a new session knows only a guess.
	for _, tc := range []struct {
		relation Relation
		tags     []string
	}{
		{Author, []string{"[decided]", "[chose]", "[guess]"}},
		{Parent, []string{"[decided]", "[chose]", "[guess]"}},
		{Editor, []string{"[decided]", "[chose]", "[guess]"}},
		{NewSession, []string{"[guess]"}},
	} {
		got := Prompt(tc.relation, "ratelimit.go", lines, "Why?")
		_, end, _ := strings.Cut(got, "\nWhy?")
		for _, tag := range []string{"[decided]", "[chose]", "[guess]"} {
			if want := slices.Contains(tc.tags, tag); strings.Contains(end, tag) != want {
				t.Errorf("%s: the end of the question %q asks for %s: %t, want %t", tc.relation, end, tag, !want, want)
			}
		}
		if !strings.Contains(end, "one sentence of at most 25 words") || !strings.HasSuffix(end, "No preamble, no restating the question or the code, no suggestions.") {
			t.Errorf("%s: the end of the question %q does not ask for one sentence of at most 25 words", tc.relation, end)
		}
	}
}

func TestAnswerKind(t *testing.T) {
	// The tag of an answer is at its start after any space, or else at its end, in any case,
	// also wrapped in ** or backticks. The box shows the answer without it.
	for _, tc := range []struct {
		text, kind, rest string
	}{
		{"[decided] Agreed in the first request.", Decided, "Agreed in the first request."},
		{" \n[chose] I picked it.", Chose, "I picked it."},
		{"[GUESS] Probably for speed.", Guess, "Probably for speed."},
		{"[Decided]\nWe agreed.", Decided, "We agreed."},
		{"**[chose]** I picked it.", Chose, "I picked it."},
		{"`[guess]` Maybe.", Guess, "Maybe."},
		{"I picked it. [chose]", Chose, "I picked it."},
		{"토큰 버킷이라서요. **[Decided]**\n", Decided, "토큰 버킷이라서요."},
		{"It was agreed. `[DECIDED]`", Decided, "It was agreed."},
		{"[guess] Maybe. [chose]", Guess, "Maybe. [chose]"}, // the start goes first
		{"No tag.", "", "No tag."},
		{"[unsure] Maybe.", "", "[unsure] Maybe."},
		{"A [chose] in the middle is no tag.", "", "A [chose] in the middle is no tag."},
		{"", "", ""},
	} {
		if kind, rest := AnswerKind(tc.text); kind != tc.kind || rest != tc.rest {
			t.Errorf("%q: got %q and %q, want %q and %q", tc.text, kind, rest, tc.kind, tc.rest)
		}
	}
}
