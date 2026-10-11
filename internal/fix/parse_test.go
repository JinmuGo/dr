package fix

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	comments := []Comment{{N: 1}, {N: 2}}
	fixed := func(note string, lines ...string) Result {
		if lines == nil {
			lines = []string{}
		}
		return Result{Fixed: true, Lines: lines, Note: note}
	}
	for _, tc := range []struct {
		name string
		text string
		want []Result
	}{
		{
			name: "bare",
			text: `{"fixes":[{"comment":1,"new_lines":["\tA"],"note":""},{"comment":2,"new_lines":["B"],"note":"see c.go"}]}`,
			want: []Result{fixed("", "\tA"), fixed("see c.go", "B")},
		},
		{
			name: "in a fence",
			text: "```json\n{\"fixes\": [\n  {\"comment\": 2, \"new_lines\": [\"B\"], \"note\": \"\"},\n  {\"comment\": 1, \"new_lines\": [\"A\"], \"note\": \"\"}\n]}\n```",
			want: []Result{fixed("", "A"), fixed("", "B")},
		},
		{
			name: "with prose and braces around it",
			text: "Here is the fix for {both}:\n{\"fixes\":[{\"comment\":1,\"new_lines\":[\"A\"]},{\"comment\":2,\"new_lines\":[\"B\"]}]}\nDone {}.",
			want: []Result{fixed("", "A"), fixed("", "B")},
		},
		{
			name: "newlines inside strings",
			text: `{"fixes":[{"comment":1,"new_lines":["\tA1\n\tA2","A3\r\n"]},{"comment":2,"new_lines":["B\n\nC"]}]}`,
			want: []Result{fixed("", "\tA1", "\tA2", "A3"), fixed("", "B", "", "C")},
		},
		{
			name: "deleting and leaving lines",
			text: `{"fixes":[{"comment":1,"new_lines":[]},{"comment":2,"note":"already right"}]}`,
			want: []Result{fixed(""), {Note: "already right"}},
		},
		{
			name: "leaving lines without a reason",
			text: `{"fixes":[{"comment":1,"new_lines":["A"]},{"comment":2,"new_lines":null,"note":""}]}`,
			want: []Result{fixed("", "A"), {Problem: problemNoReason}},
		},
		{
			name: "missing and unknown numbers",
			text: `{"fixes":[{"comment":2,"new_lines":["B"]},{"comment":7,"new_lines":["X"]},{"new_lines":["Y"]}]}`,
			want: []Result{{Problem: problemMissing}, fixed("", "B")},
		},
		{
			name: "a duplicate number",
			text: `{"fixes":[{"comment":1,"new_lines":["A"]},{"comment":2,"new_lines":["B"]},{"comment":1,"new_lines":["A2"]}]}`,
			want: []Result{{Problem: problemDuplicate}, fixed("", "B")},
		},
		{
			name: "a fix not in the asked form",
			text: `{"fixes":[{"comment":1,"new_lines":"A"},{"comment":2,"new_lines":["B"]}]}`,
			want: []Result{{Problem: problemShape}, fixed("", "B")},
		},
		{
			name: "an empty list",
			text: `{"fixes":[]}`,
			want: []Result{{Problem: problemMissing}, {Problem: problemMissing}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.text, comments)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(got, tc.want, equalResult) {
				t.Errorf("Parse(%q):\n got %+v\nwant %+v", tc.text, got, tc.want)
			}
		})
	}
}

func equalResult(a, b Result) bool {
	return a.Fixed == b.Fixed && slices.Equal(a.Lines, b.Lines) && a.Note == b.Note && a.Problem == b.Problem
}

func TestParseNoFixes(t *testing.T) {
	long := "I could not do that, " + strings.Repeat("because reasons ", 40)
	for _, text := range []string{
		"",
		long,
		"```json\n{\"fix\": []}\n```",
		`{"fixes": null}`,
		`{"fixes": {"comment": 1}}`,
		`{"fixes": [`,
	} {
		results, err := Parse(text, []Comment{{N: 1}})
		if err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", text, results)
			continue
		}
		if len(err.Error()) > 200 {
			t.Errorf("Parse(%q): the error is %d bytes, want the answer cut", text, len(err.Error()))
		}
	}
}
