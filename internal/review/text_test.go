package review_test

import (
	"maps"
	"testing"

	"github.com/JinmuGo/dr/internal/review"
)

func TestShortIDs(t *testing.T) {
	// Each id is cut to its shortest suffix of at least 4 characters that no other id
	// shares, as long for every id, and made visible.
	for _, tc := range []struct {
		name string
		ids  []string
		want map[string]string
	}{
		{
			name: "the ends tell them apart",
			ids:  []string{"01a1287a-753e-7af1-8e7c-ec3446d1931b", "01a1287a-2a61-4f0e-b3c4-c6f747d09b1c", ""},
			want: map[string]string{"01a1287a-753e-7af1-8e7c-ec3446d1931b": "931b", "01a1287a-2a61-4f0e-b3c4-c6f747d09b1c": "9b1c"},
		},
		{
			name: "a clash makes every id longer",
			ids:  []string{"aaaa-x1234", "bbbb-y1234", "cccc-0001", "aaaa-x1234"},
			want: map[string]string{"aaaa-x1234": "x1234", "bbbb-y1234": "y1234", "cccc-0001": "-0001"},
		},
		{
			name: "a short id stays whole",
			ids:  []string{"ab", "\x1b[2Jxyz"},
			want: map[string]string{"ab": "ab", "\x1b[2Jxyz": "Jxyz"},
		},
		{
			name: "made visible",
			ids:  []string{"abc\u202e"},
			want: map[string]string{"abc\u202e": `abc\u202e`},
		},
	} {
		if got := review.ShortIDs(tc.ids); !maps.Equal(got, tc.want) {
			t.Errorf("%s: ShortIDs(%q) = %q, want %q", tc.name, tc.ids, got, tc.want)
		}
	}
}

func TestSummary(t *testing.T) {
	// The summary is the first non-empty line that is neither a heading nor ends with a
	// colon, and the first non-empty line when all of them are.
	for _, tc := range []struct {
		prompt, want string
	}{
		{"rate limiter를 짜줘\n셸 명령은 실행하지 마", "rate limiter를 짜줘"},
		{"\n  Complete assignment thoroughly:\n\n# Target\n  PR 6b in dr: origins.  \nmore", "PR 6b in dr: origins."},
		{"Do this:\n# Heading\n", "Do this:"},
		{"  \n\n", ""},
		{"say \x1b[31mhi", `say \x1b[31mhi`},
	} {
		if got := review.Summary(tc.prompt); got != tc.want {
			t.Errorf("Summary(%q) = %q, want %q", tc.prompt, got, tc.want)
		}
	}
}
