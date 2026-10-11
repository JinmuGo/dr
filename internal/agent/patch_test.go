package agent

import "testing"

func TestPatch(t *testing.T) {
	const unified = "@@ -2,3 +2,3 @@\n b\n-c\n+C\n d\n"
	for _, tc := range []struct {
		name, diff, before, want string
		numbered                 bool
		fails                    bool
	}{
		{name: "where the diff says", diff: unified, before: "a\nb\nc\nd\ne\n", want: "a\nb\nC\nd\ne\n"},
		{name: "lines moved by a change outside the logs", diff: unified, before: "new\nnew\na\nb\nc\nd\ne\n", want: "new\nnew\na\nb\nC\nd\ne\n"},
		{name: "lines that occur twice", diff: unified, before: "x\nx\nb\nc\nd\nb\nc\nd\n", fails: true},
		{name: "lines that are gone", diff: unified, before: "a\nb\nchanged\nd\n", fails: true},
		{name: "unified insertion", diff: "@@ -1,0 +2,2 @@\n+x\n+y\n", before: "a\nb\n", want: "a\nx\ny\nb\n"},
		{name: "numbered", numbered: true, diff: " 1|a\n-2|b\n+2|B\n 3|c\n\n 5|e\n-6|f", before: "a\nb\nc\nd\ne\nf\n", want: "a\nB\nc\nd\ne\n"},
		{name: "numbered added lines before a line", numbered: true, diff: "+1|x\n 1|a", before: "a\nb\n", want: "x\na\nb\n"},
		{name: "numbered added lines alone", numbered: true, diff: "-1|a\n\n+3|z", before: "a\nb\nc\n", want: "b\nc\nz\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hunks, ok := unifiedHunks(tc.diff)
			if tc.numbered {
				hunks, ok = numberedHunks(tc.diff)
			}
			if !ok {
				t.Fatalf("cannot parse %q", tc.diff)
			}
			got, ok := patch(hunks)(tc.before)
			if ok == tc.fails || got != tc.want {
				t.Errorf("got %q, %v; want %q, %v", got, ok, tc.want, !tc.fails)
			}
		})
	}
}
