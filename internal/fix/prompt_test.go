package fix

import (
	"slices"
	"strings"
	"testing"
)

func TestPrompt(t *testing.T) {
	comments := []Comment{
		{N: 1, Path: "ratelimit.go", First: 14, Lines: []string{"\tburst  float64"}, Text: "say why it is float64\nin a comment\n\nkeep it short"},
		{
			N: 2, Path: "d/limit.go", First: 50, Lines: []string{"\tif x {\r", "\t\treturn 1\r", "\t}\r"},
			Deleted: []Deleted{{Before: 1, Text: "\t\treturn 0\r"}, {Before: 1, Text: "\t\t// old"}, {Before: 3, Text: "\t// gone"}},
			Text:    "return a named constant",
		},
	}
	earlier := []Earlier{
		{Path: "ratelimit.go", First: 3, Last: 5, Text: "use errors.Is", Applied: true},
		{Path: "d/limit.go", First: 9, Last: 9, Text: "rename\nit", Applied: false},
	}
	p := Prompt(comments, earlier)
	lines := strings.Split(p, "\n")
	if !strings.HasPrefix(lines[0], "[dr] Fix request.") {
		t.Errorf("the prompt starts with %q, want the [dr] Fix request. line", lines[0])
	}

	// The earlier fixes come first, a line each with the range, what happened and the text.
	for i, want := range [][]string{{"ratelimit.go:3-5", "applied", "use errors.Is"}, {"d/limit.go:9-9", "dropped", "rename it"}} {
		at := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, want[0]) })
		if at < 0 {
			t.Errorf("earlier fix %d: no line has %q", i, want[0])
			continue
		}
		for _, w := range want[1:] {
			if !strings.Contains(lines[at], w) {
				t.Errorf("earlier fix %d: line %q has no %q", i, lines[at], w)
			}
		}
		if first := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "1. ") }); at > first {
			t.Errorf("earlier fix %d is on line %d, after the first comment on line %d", i, at, first)
		}
	}

	// The comments are numbered with their ranges, quote every line in order, current lines
	// without their "\r", and end with their request.
	var quoted []string
	for _, l := range lines {
		if strings.HasPrefix(l, ">") || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "Request: ") ||
			strings.HasPrefix(l, "1. ") || strings.HasPrefix(l, "2. ") {
			quoted = append(quoted, l)
		}
	}
	want := []string{
		"1. ratelimit.go:14-14",
		">\tburst  float64",
		"Request: say why it is float64",
		"2. d/limit.go:50-52",
		">\tif x {",
		"-\t\treturn 0",
		"-\t\t// old",
		">\t\treturn 1",
		">\t}",
		"-\t// gone",
		"Request: return a named constant",
	}
	if len(quoted) != len(want) {
		t.Fatalf("comment lines:\n%q\nwant\n%q", quoted, want)
	}
	for i := range want {
		if !strings.HasPrefix(quoted[i], want[i]) {
			t.Errorf("comment line %d is %q, want it to start with %q", i, quoted[i], want[i])
		}
	}
	// A request of several lines goes on under its first, each further line indented by two
	// spaces and a blank one left blank, so the next comment's number stands out.
	if want := "\nRequest: say why it is float64\n  in a comment\n\n  keep it short\n2. d/limit.go:50-52"; !strings.Contains(p, want) {
		t.Errorf("the prompt\n%s\nhas no\n%s", p, want)
	}
	if !strings.Contains(p, answerShape) {
		t.Errorf("the prompt has no %s", answerShape)
	}
	if strings.Contains(p, "\r") {
		t.Errorf("the prompt holds \\r: %q", p)
	}
}
