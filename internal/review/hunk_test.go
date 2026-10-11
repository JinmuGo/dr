package review_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// numbered returns lines "1\n2\n...", from 1 to n, with the lines in changes replaced.
func numbered(n int, changes map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		s, ok := changes[i]
		if !ok {
			s = fmt.Sprint(i)
		}
		b.WriteString(s + "\n")
	}
	return b.String()
}

func TestHunks(t *testing.T) {
	for _, tc := range []struct {
		name          string
		base, current string
		context       int
		want          []string // each hunk as its header and its lines marked like a unified diff
	}{
		{
			name: "no change",
			base: "a\nb\n", current: "a\nb\n", context: 3,
		},
		{
			name: "context touches",
			base: numbered(10, nil), current: numbered(10, map[int]string{1: "x", 8: "y"}), context: 3,
			want: []string{"@@ -1,10 +1,10 @@ -1 +x  2  3  4  5  6  7 -8 +y  9  10"},
		},
		{
			name: "context apart",
			base: numbered(11, nil), current: numbered(11, map[int]string{1: "x", 9: "y"}), context: 3,
			want: []string{"@@ -1,4 +1,4 @@ -1 +x  2  3  4", "@@ -6,6 +6,6 @@  6  7  8 -9 +y  10  11"},
		},
		{
			name: "context cut at the ends",
			base: numbered(5, nil), current: numbered(5, map[int]string{3: "x"}), context: 3,
			want: []string{"@@ -1,5 +1,5 @@  1  2 -3 +x  4  5"},
		},
		{
			name: "new file",
			base: "", current: "a\nb\n", context: 3,
			want: []string{"@@ -0,0 +1,2 @@ +a +b"},
		},
		{
			name: "deleted file",
			base: "a\nb\n", current: "", context: 3,
			want: []string{"@@ -1,2 +0,0 @@ -a -b"},
		},
		{
			name: "added without context",
			base: "a\nb\nc\n", current: "a\nb\nX\nc\n", context: 0,
			want: []string{"@@ -2,0 +3,1 @@ +X"},
		},
		{
			name: "deleted without context",
			base: "a\nb\nc\n", current: "a\nc\n", context: 0,
			want: []string{"@@ -2,1 +1,0 @@ -b"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, h := range review.Hunks(origin.Blame(tc.base, tc.current, nil), tc.context) {
				s := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldCount, h.NewStart, h.NewCount)
				for _, l := range h.Lines {
					mark := " "
					switch {
					case l.Old == 0:
						mark = "+"
					case l.New == 0:
						mark = "-"
					}
					s += " " + mark + l.Text
				}
				got = append(got, s)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Hunks(%q, %q, %d):\ngot  %q\nwant %q", tc.base, tc.current, tc.context, got, tc.want)
			}
		})
	}
}

func TestHash(t *testing.T) {
	file := func(base, current string) review.File {
		return review.File{Path: "f.go", Status: 'M', Lines: origin.Blame(base, current, nil)}
	}
	base := numbered(20, nil)
	viewed := file(base, numbered(20, map[int]string{10: "x"}))
	for _, tc := range []struct {
		name string
		f    review.File
		same bool
	}{
		{name: "the same diff", f: file(base, numbered(20, map[int]string{10: "x"})), same: true},
		{
			// A rebase on a change far from the hunk moves it but shows the same lines.
			name: "moved", same: true,
			f: file("0\n"+numbered(20, map[int]string{1: "one"}), "0\n"+numbered(20, map[int]string{1: "one", 10: "x"})),
		},
		{name: "another text", f: file(base, numbered(20, map[int]string{10: "y"}))},
		{name: "another change", f: file(base, numbered(20, map[int]string{10: "x", 11: "y"}))},
		{name: "kinds swapped", f: file(numbered(20, map[int]string{10: "x"}), base)},
		{name: "another context line", f: file(numbered(20, map[int]string{8: "z"}), numbered(20, map[int]string{8: "z", 10: "x"}))},
		{name: "another status", f: review.File{Path: "f.go", Status: 'A', Lines: viewed.Lines}},
		{name: "binary", f: review.File{Path: "f.go", Status: 'M', NoDiff: review.Binary}},
	} {
		if got := tc.f.Hash(3) == viewed.Hash(3); got != tc.same {
			t.Errorf("%s: same hash %t, want %t", tc.name, got, tc.same)
		}
	}
	if h := viewed.Hash(3); len(h) != 64 {
		t.Errorf("got hash %q, want 64 hex digits of sha256", h)
	}
}
