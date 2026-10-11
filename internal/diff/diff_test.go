package diff

import (
	"slices"
	"testing"
)

func TestWords(t *testing.T) {
	for _, tc := range []struct {
		a, b         string
		aWant, bWant []string
	}{
		{"x := 1", "x := 2", []string{"1"}, []string{"2"}},
		{"return err", `return fmt.Errorf("open: %w", err)`, nil, []string{`fmt.Errorf("open: %w", `, ")"}},
		{"foo bar baz", "foo qux quux baz", []string{"bar"}, []string{"qux quux"}},
		{"foo bar", "baz qux", []string{"foo bar"}, []string{"baz qux"}}, // a space between two changes is changed
		{"\tsum += x", "    sum += x", []string{"\t"}, []string{"    "}},
		{"한글 주석이에요", "한글 설명이에요", []string{"주석이에요"}, []string{"설명이에요"}},
		{"same", "same", nil, nil},
		{"", "new", nil, []string{"new"}},
	} {
		aParts, bParts := Words(tc.a, tc.b)
		if a, b := texts(tc.a, aParts), texts(tc.b, bParts); !slices.Equal(a, tc.aWant) || !slices.Equal(b, tc.bWant) {
			t.Errorf("Words(%q, %q) = %q, %q; want %q, %q", tc.a, tc.b, a, b, tc.aWant, tc.bWant)
		}
	}
}

// texts returns the parts of s.
func texts(s string, parts [][2]int) []string {
	var out []string
	for _, p := range parts {
		out = append(out, s[p[0]:p[1]])
	}
	return out
}
