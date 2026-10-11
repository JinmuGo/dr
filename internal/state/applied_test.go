package state_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/state"
)

func TestLogApplied(t *testing.T) {
	// Applied and Undone records read back as they were written.
	l := state.NewLog(filepath.Join(t.TempDir(), "log.jsonl"))
	applied := state.Applied{
		Time: at, ID: "00000000000000a1", Comment: "00000000000000c1", Text: "add a one-line comment", Note: "it is float64 like tokens",
		Agent: "claude", Session: "s1", Log: "/home/.claude/projects/w/s1.jsonl", Cwd: "/work", Fork: "f1",
		Model: "claude-opus-5-5", Effort: "high", Path: "ratelimit.go", First: 14,
		Old: []string{"\tburst  float64"}, New: []string{"\t// float64 like tokens.", "\tburst  float64"},
		Before: strings.Repeat("a", 64), After: strings.Repeat("b", 64),
	}
	undone := state.Undone{Time: at.Add(time.Minute), Applied: applied.ID}
	for _, r := range []state.Record{applied, undone} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	records, bad, err := l.Read()
	if err != nil || bad != 0 {
		t.Fatalf("got %d bad lines, %v", bad, err)
	}
	if !reflect.DeepEqual(records.Applied, []state.Applied{applied}) || !reflect.DeepEqual(records.Undone, []state.Undone{undone}) {
		t.Errorf("read back %+v and %+v, want %+v and %+v", records.Applied, records.Undone, applied, undone)
	}
}

func TestLogAppliedBadLines(t *testing.T) {
	// An applied fix needs an ID, the session, the path and a line to start at; an undone
	// fix needs the fix it reverted.
	path := filepath.Join(t.TempDir(), "log.jsonl")
	log := strings.Join([]string{
		`{"type":"applied","id":"a1","agent":"claude","session":"s1","path":"a.go","first":1,"old":["x"],"new":["y"]}`,
		`{"type":"applied","agent":"claude","session":"s1","path":"a.go","first":1}`,           // no id
		`{"type":"applied","id":"a2","agent":"claude","path":"a.go","first":1}`,                // no session
		`{"type":"applied","id":"a3","agent":"claude","session":"s1","first":1}`,               // no path
		`{"type":"applied","id":"a4","agent":"claude","session":"s1","path":"a.go","first":0}`, // no line
		`{"type":"applied","id":"a5","agent":"claude","session":"s1","path":"a.go","first":"1"}`,
		`{"type":"undone","applied":""}`,
		`{"type":"undone","applied":"a1"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	records, bad, err := state.NewLog(path).Read()
	if err != nil || bad != 6 || len(records.Applied) != 1 || records.Applied[0].ID != "a1" || len(records.Undone) != 1 {
		t.Errorf("got %+v, %d bad lines, %v; want the fix a1, its undo and 6 bad lines", records, bad, err)
	}
}

func TestInPlace(t *testing.T) {
	// The fixes left are those no undo reverted, in the order dr applied them. An undo of a
	// fix the log does not have changes nothing.
	fix := func(id string) state.Applied { return state.Applied{ID: id} }
	records := state.Records{
		Applied: []state.Applied{fix("a1"), fix("a2"), fix("a3")},
		Undone:  []state.Undone{{Applied: "a2"}, {Applied: "zz"}},
	}
	got := records.InPlace()
	if want := []state.Applied{fix("a1"), fix("a3")}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := (state.Records{}).InPlace(); len(got) != 0 {
		t.Errorf("no records: got %+v, want no fixes", got)
	}
}
