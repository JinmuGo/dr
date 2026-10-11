package state_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/state"
)

func TestCommentRecords(t *testing.T) {
	// Comment records read back as they were written, in order. The newest record of a
	// comment replaces the earlier ones where the comment was first made, and one with no
	// text removes it.
	l := state.NewLog(filepath.Join(t.TempDir(), "log.jsonl"))
	c := state.Comment{
		Time: at, ID: "0123456789abcdef", Agent: "claude", Session: "s1", Relation: "author", Path: "a.go",
		Lines:    []state.Line{{Kind: "-", Text: "\trate int"}, {Kind: "-", Text: "\tburst int"}, {Kind: "+", Text: "\trate float64"}},
		Selected: []int{0, 2},
		Before:   []state.Line{{Kind: " ", Text: "type Limiter struct {"}},
		After:    []state.Line{},
		Text:     "use a named type",
	}
	other := c
	other.ID, other.Selected, other.Text = "fedcba9876543210", nil, "add a comment"
	edited := c
	edited.Time, edited.Text = at.Add(time.Minute), "use errors.Is"
	removed := other
	removed.Time, removed.Text = at.Add(2*time.Minute), ""
	written := []state.Comment{c, other, edited, removed}
	for _, r := range written {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	records, bad, err := l.Read()
	if err != nil || bad != 0 {
		t.Fatalf("got %d bad lines, %v", bad, err)
	}
	if !reflect.DeepEqual(records.Comments, written) {
		t.Errorf("read back\n%+v\nwant\n%+v", records.Comments, written)
	}
	if got := records.CurrentComments(); !reflect.DeepEqual(got, []state.Comment{edited}) {
		t.Errorf("current comments\n%+v\nwant\n%+v", got, []state.Comment{edited})
	}
	// Removing a comment and adding it again brings it back where it was first made.
	again := removed
	again.Text = "add a comment after all"
	records.Comments = append(records.Comments, again)
	if got := records.CurrentComments(); !reflect.DeepEqual(got, []state.Comment{edited, again}) {
		t.Errorf("after adding it again: got %+v", got)
	}
	for _, tc := range []struct {
		c    state.Comment
		want []int
	}{{c, []int{0, 2}}, {other, []int{0, 1, 2}}} {
		if got := tc.c.Picked(); !slices.Equal(got, tc.want) {
			t.Errorf("Picked of %v: got %v, want %v", tc.c.Selected, got, tc.want)
		}
	}
}

func TestCommentBadLines(t *testing.T) {
	// A comment record without an ID, a session to go to, a path or lines, or that selects
	// a line it does not have, is skipped and counted.
	path := filepath.Join(t.TempDir(), "log.jsonl")
	const line = `[{"kind":"+","text":"x"}]`
	log := strings.Join([]string{
		`{"type":"comment","id":"c1","agent":"claude","session":"s1","relation":"author","path":"a.go","lines":` + line + `,"text":"fix"}`,
		`{"type":"comment","agent":"claude","session":"s1","path":"a.go","lines":` + line + `,"text":"no id"}`,
		`{"type":"comment","id":"c2","session":"s1","path":"a.go","lines":` + line + `,"text":"no agent"}`,
		`{"type":"comment","id":"c3","agent":"claude","path":"a.go","lines":` + line + `,"text":"no session"}`,
		`{"type":"comment","id":"c4","agent":"claude","session":"s1","lines":` + line + `,"text":"no path"}`,
		`{"type":"comment","id":"c5","agent":"claude","session":"s1","path":"a.go","lines":[],"text":"no lines"}`,
		`{"type":"comment","id":"c6","agent":"claude","session":"s1","path":"a.go","lines":` + line + `,"selected":[1]}`,
		`{"type":"comment","id":"c7","agent":"claude","session":"s1","path":"a.go","lines":` + line + `,"text":""}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	records, bad, err := state.NewLog(path).Read()
	var ids []string
	for _, c := range records.Comments {
		ids = append(ids, c.ID)
	}
	if err != nil || bad != 6 || !slices.Equal(ids, []string{"c1", "c7"}) {
		t.Errorf("got comments %v, %d bad lines, %v; want c1 and c7 and 6 bad lines", ids, bad, err)
	}
}
