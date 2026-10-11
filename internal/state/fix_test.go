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

func TestFixRecords(t *testing.T) {
	// Request, Fix and Dropped records read back as they were written, in order.
	l := state.NewLog(filepath.Join(t.TempDir(), "log.jsonl"))
	request := state.Request{
		Time: at, ID: "00000000000000e1", Agent: "codex", Session: "s1", Relation: "author",
		Comments: []string{"00000000000000c1", "00000000000000c2"},
	}
	fixed := state.Fix{
		Time: at.Add(time.Minute), ID: "00000000000000f1", Request: request.ID, Comment: "00000000000000c1", Text: "add a comment",
		Agent: "codex", Session: "s1", Log: "/home/.codex/s1.jsonl", Cwd: "/work", Fork: "f1", Model: "gpt-6-astra", Effort: "medium",
		TookMS: 4200, Path: "ratelimit.go", First: 13, Lines: []string{"\trate float64\r"}, Hash: strings.Repeat("a", 64),
		New: []string{"\t// tokens a second", "\trate float64\r"}, Note: "it was not clear",
	}
	kept := fixed
	kept.ID, kept.Comment, kept.New, kept.Keep, kept.Note = "00000000000000f2", "00000000000000c2", nil, true, "the line is right"
	failed := state.Fix{
		Time: at.Add(time.Minute), ID: "00000000000000f3", Request: request.ID, Comment: "00000000000000c3",
		Agent: "codex", Session: "s1", Path: "ratelimit.go", First: 40, Lines: []string{"x"}, Error: "no answer in 3 minutes",
	}
	dropped := state.Dropped{Time: at.Add(2 * time.Minute), Fix: kept.ID}
	for _, r := range []state.Record{request, fixed, kept, failed, dropped} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	records, bad, err := l.Read()
	if err != nil || bad != 0 {
		t.Fatalf("got %d bad lines, %v", bad, err)
	}
	if !reflect.DeepEqual(records.Requests, []state.Request{request}) ||
		!reflect.DeepEqual(records.Fixes, []state.Fix{fixed, kept, failed}) ||
		!reflect.DeepEqual(records.Dropped, []state.Dropped{dropped}) {
		t.Errorf("read back %+v\n%+v\n%+v", records.Requests, records.Fixes, records.Dropped)
	}
	if got, ok := records.Request(request.ID); !ok || !reflect.DeepEqual(got, request) {
		t.Errorf("Request(%s): got %+v, %t", request.ID, got, ok)
	}
	if _, ok := records.Request("nope"); ok {
		t.Error("Request of an unknown ID: got one")
	}
}

func TestFixBadLines(t *testing.T) {
	// A request needs an ID, the session and its comments; a fix the request, the comment,
	// the session, the path and a line to start at; a drop the fix it dropped.
	path := filepath.Join(t.TempDir(), "log.jsonl")
	const fix = `"request":"r1","comment":"c1","agent":"codex","session":"s1","path":"a.go","first":1`
	log := strings.Join([]string{
		`{"type":"request","id":"r1","agent":"codex","session":"s1","comments":["c1"]}`,
		`{"type":"request","agent":"codex","session":"s1","comments":["c1"]}`,       // no id
		`{"type":"request","id":"r2","agent":"codex","comments":["c1"]}`,            // no session
		`{"type":"request","id":"r3","agent":"codex","session":"s1","comments":[]}`, // no comments
		`{"type":"fix","id":"f1",` + fix + `}`,
		`{"type":"fix",` + fix + `}`,                                                                      // no id
		`{"type":"fix","id":"f2","comment":"c1","agent":"codex","session":"s1","path":"a.go","first":1}`,  // no request
		`{"type":"fix","id":"f3","request":"r1","agent":"codex","session":"s1","path":"a.go","first":1}`,  // no comment
		`{"type":"fix","id":"f4","request":"r1","comment":"c1","agent":"codex","session":"s1","first":1}`, // no path
		`{"type":"fix","id":"f5","request":"r1","comment":"c1","agent":"codex","session":"s1","path":"a.go","first":0}`,
		`{"type":"dropped","fix":"f1"}`,
		`{"type":"dropped","fix":""}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	records, bad, err := state.NewLog(path).Read()
	if err != nil || bad != 9 || len(records.Requests) != 1 || len(records.Fixes) != 1 || records.Fixes[0].ID != "f1" || len(records.Dropped) != 1 {
		t.Errorf("got %+v, %d bad lines, %v; want r1, f1, its drop and 9 bad lines", records, bad, err)
	}
}

func TestShownFix(t *testing.T) {
	// The newest fix for a comment shows under it, unless it came for an older text of the
	// comment, or was dropped or applied. A comment without fixes shows none.
	c := state.Comment{Time: at, ID: "c1", Text: "add a comment"}
	fix := func(id string, minute int) state.Fix {
		return state.Fix{Time: at.Add(time.Duration(minute) * time.Minute), ID: id, Comment: "c1"}
	}
	for _, tc := range []struct {
		name    string
		records state.Records
		want    string // the ID of the fix shown, "" for none
	}{
		{"none", state.Records{}, ""},
		{"the newest", state.Records{Fixes: []state.Fix{fix("f1", 1), fix("f2", 2)}}, "f2"},
		{"at the same time as the comment", state.Records{Fixes: []state.Fix{fix("f1", 0)}}, "f1"},
		{"for an older text", state.Records{Fixes: []state.Fix{fix("f1", -1)}}, ""},
		{"dropped", state.Records{Fixes: []state.Fix{fix("f1", 1)}, Dropped: []state.Dropped{{Fix: "f1"}}}, ""},
		{"an older one dropped", state.Records{Fixes: []state.Fix{fix("f1", 1), fix("f2", 2)}, Dropped: []state.Dropped{{Fix: "f1"}}}, "f2"},
		{"applied", state.Records{Fixes: []state.Fix{fix("f1", 1)}, Applied: []state.Applied{{Time: at.Add(2 * time.Minute), Comment: "c1"}}}, ""},
		{"applied and undone", state.Records{
			Fixes:   []state.Fix{fix("f1", 1)},
			Applied: []state.Applied{{Time: at.Add(2 * time.Minute), ID: "a1", Comment: "c1"}},
			Undone:  []state.Undone{{Applied: "a1"}},
		}, ""},
		{"an earlier fix applied", state.Records{
			Fixes:   []state.Fix{fix("f1", 1), fix("f2", 3)},
			Applied: []state.Applied{{Time: at.Add(2 * time.Minute), ID: "a1", Comment: "c1"}},
		}, "f2"},
		{"for another comment", state.Records{Fixes: []state.Fix{{Time: at, ID: "f9", Comment: "c9"}}}, ""},
	} {
		got, ok := tc.records.Shown(c)
		if ok != (tc.want != "") || got.ID != tc.want {
			t.Errorf("%s: got %q, %t; want %q", tc.name, got.ID, ok, tc.want)
		}
	}
}

func TestResolved(t *testing.T) {
	// A comment is resolved while a fix dr applied for it is in place.
	records := state.Records{
		Applied: []state.Applied{{ID: "a1", Comment: "c1"}, {ID: "a2", Comment: "c2"}},
		Undone:  []state.Undone{{Applied: "a2"}},
	}
	for id, want := range map[string]bool{"c1": true, "c2": false, "c3": false} {
		if got := records.Resolved(id); got != want {
			t.Errorf("Resolved(%s): got %t, want %t", id, got, want)
		}
	}
}

func TestTaken(t *testing.T) {
	// The fixes a session sent that were applied or dropped, in the order they came. A fix
	// applied and then undone counts as not applied; a fix that keeps the lines, a reason no
	// fix came, a fix still waiting for the person and another session's fixes are left out.
	fix := func(id, comment string, minute int) state.Fix {
		return state.Fix{Time: at.Add(time.Duration(minute) * time.Minute), ID: id, Comment: comment, Agent: "codex", Session: "s1", First: 3, Lines: []string{"a", "b"}}
	}
	kept := fix("f4", "c4", 1)
	kept.Keep = true
	failed := fix("f5", "c5", 1)
	failed.Error = "no answer"
	other := fix("f6", "c6", 1)
	other.Session = "s2"
	records := state.Records{
		Fixes:   []state.Fix{fix("f1", "c1", 1), fix("f2", "c2", 1), fix("f3", "c3", 1), kept, failed, other, fix("f7", "c7", 1)},
		Dropped: []state.Dropped{{Fix: "f2"}, {Fix: "f4"}, {Fix: "f6"}},
		Applied: []state.Applied{
			{Time: at.Add(2 * time.Minute), ID: "a1", Comment: "c1"},
			{Time: at.Add(2 * time.Minute), ID: "a3", Comment: "c3"},
			{Time: at.Add(2 * time.Minute), ID: "a5", Comment: "c5"},
		},
		Undone: []state.Undone{{Applied: "a3"}},
	}
	type got struct {
		id      string
		applied bool
	}
	var all []got
	for _, f := range records.Taken("codex", "s1") {
		all = append(all, got{f.ID, f.Applied})
	}
	if want := []got{{"f1", true}, {"f2", false}, {"f3", false}}; !slices.Equal(all, want) {
		t.Errorf("got %v, want %v", all, want)
	}
}
