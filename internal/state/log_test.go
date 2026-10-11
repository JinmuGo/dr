package state_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/state"
)

var at = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func TestLogAppendRead(t *testing.T) {
	// The directory is made when missing. Each record is a line of its own that starts with
	// its type, and reads back as it was written.
	path := filepath.Join(t.TempDir(), "git", "dr", "log.jsonl")
	l := state.NewLog(path)
	if records, bad, err := l.Read(); err != nil || bad != 0 || len(records.Questions) != 0 {
		t.Fatalf("a missing log: got %+v, %d bad lines, %v; want nothing", records, bad, err)
	}
	q := state.Question{
		Time: at, ID: "0123456789abcdef", Agent: "claude", Session: "s1", Relation: "editor", Path: "a.go",
		Lines:    []state.Line{{Kind: "-", Text: "\trate int"}, {Kind: "-", Text: "\tburst int"}, {Kind: "+", Text: "\trate float64"}},
		Selected: []int{0, 2},
		Before:   []state.Line{{Kind: " ", Text: "type Limiter struct {"}},
		After:    []state.Line{},
		Text:     "Why float64?",
	}
	fork := state.Fork{Time: at.Add(time.Second), Agent: "claude", Session: "s1", Fork: "f1", Base: at.Add(-time.Hour)}
	answer := state.Answer{Time: at.Add(2 * time.Second), Question: q.ID, Fork: "f1", Model: "claude-opus-5-5", TookMS: 7012, Text: "Because."}
	for _, r := range []state.Record{q, fork, answer} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i, typ := range []string{"question", "fork", "answer"} {
		var record map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &record); err != nil || !strings.HasPrefix(lines[i], `{"type":"`+typ+`",`) {
			t.Errorf("line %d: got %s, %v; want a %s record", i, lines[i], err, typ)
		}
	}
	records, bad, err := l.Read()
	if err != nil || bad != 0 {
		t.Fatalf("got %d bad lines, %v", bad, err)
	}
	want := state.Records{Forks: []state.Fork{fork}, Questions: []state.Question{q}, Answers: map[string]state.Answer{q.ID: answer}}
	if !reflect.DeepEqual(records, want) {
		t.Errorf("read back\n%+v\nwant\n%+v", records, want)
	}
	// The lines asked about are those selected, or all of them in a record that has none
	// selected, as one written before there were selections.
	q.Selected = nil
	for _, tc := range []struct {
		q    state.Question
		want []int
	}{{records.Questions[0], []int{0, 2}}, {q, []int{0, 1, 2}}} {
		if got := tc.q.Asked(); !slices.Equal(got, tc.want) {
			t.Errorf("Asked of %v: got %v, want %v", tc.q.Selected, got, tc.want)
		}
	}
}

func TestLogConcurrent(t *testing.T) {
	// Appends from commands that run at the same time each make a whole line.
	l := state.NewLog(filepath.Join(t.TempDir(), "log.jsonl"))
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			a := state.Answer{Time: at, Question: strconv.Itoa(i), Text: strings.Repeat("answer ", 200)}
			if err := l.Append(a); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	records, bad, err := l.Read()
	if len(records.Answers) != 50 || bad != 0 || err != nil {
		t.Errorf("got %d answers, %d bad lines, %v; want 50 answers", len(records.Answers), bad, err)
	}
}

func TestLogBadLines(t *testing.T) {
	// Lines that are not records are skipped and counted. A record of a type a newer dr may
	// write is skipped without a count. Only a question for a new session has no session.
	path := filepath.Join(t.TempDir(), "log.jsonl")
	log := strings.Join([]string{
		`{"type":"question","time":"2026-10-09T12:00:00Z","id":"q1","agent":"claude","session":"s1","path":"a.go","lines":[{"kind":"+","text":"x"}],"before":[],"after":[],"text":"Why?"}`,
		`not json`,
		`{"type":"question","id":"q2","session":"s1","path":"a.go","lines":[]}`,                                       // no lines
		`{"type":"question","id":"q3","session":"s1","path":"a.go","lines":[{"kind":"+","text":"x"}],"selected":[1]}`, // a line it does not have
		`{"type":"question","id":"q4","agent":"omp","session":"","relation":"new","path":"a.go","lines":[{"kind":"+","text":"x"}],"text":"Why?"}`,
		`{"type":"question","id":"q5","agent":"omp","session":"","relation":"editor","path":"a.go","lines":[{"kind":"+","text":"x"}]}`, // no session
		`{"type":"answer","text":"for no question"}`,
		`{"type":"fork","agent":"claude","session":"s1"}`, // no fork
		`{"type":"fork","session":"s1","fork":"f1"}`,      // no agent
		`{"type":"fork","agent":"omp","session":"","fork":"n1"}`,
		`{"type":"submit","text":"a later record"}`,
		``,
		`{"type":"answer","question":"q1","text":"Because.","error":""}`,
		`{"type":"answer","question":`, // cut off when dr stopped
	}, "\n")
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	records, bad, err := state.NewLog(path).Read()
	if err != nil || bad != 8 || len(records.Questions) != 2 || records.Questions[1].ID != "q4" || records.Answers["q1"].Text != "Because." ||
		len(records.Forks) != 1 || records.Forks[0].Fork != "n1" {
		t.Errorf("got %+v, %d bad lines, %v; want questions q1 with its answer and q4, the fork n1 and 8 bad lines", records, bad, err)
	}
	if _, _, err := state.NewLog(t.TempDir()).Read(); err == nil {
		t.Error("reading a directory: got no error")
	}
}

func TestFork(t *testing.T) {
	// The newest fork of a session goes on while no edit in the review the session knows of
	// is newer than the fork's base. The newest new session of an agent always goes on.
	records := state.Records{Forks: []state.Fork{
		{Agent: "claude", Session: "s1", Fork: "old", Base: at},
		{Agent: "claude", Session: "s1", Fork: "new", Base: at.Add(time.Hour)},
		{Agent: "codex", Session: "s2", Fork: "other", Base: at},
		{Agent: "omp", Fork: "first"},
		{Agent: "omp", Fork: "second"},
	}}
	for _, tc := range []struct {
		name, agent, session string
		newest               time.Time
		want                 string
	}{
		{"edits older than the base", "claude", "s1", at.Add(30 * time.Minute), "new"},
		{"the edit the fork was made after", "claude", "s1", at.Add(time.Hour), "new"},
		{"a newer edit", "claude", "s1", at.Add(2 * time.Hour), ""},
		{"no fork of the session", "claude", "s3", at, ""},
		{"a fork of another agent's session", "claude", "s2", at, ""},
		{"another session", "codex", "s2", at, "other"},
		{"a new session", "omp", "", at.Add(time.Hour), "second"},
		{"no new session of the agent", "claude", "", at, ""},
	} {
		if got := records.Fork(tc.agent, tc.session, tc.newest); got != tc.want {
			t.Errorf("%s: got fork %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPlace(t *testing.T) {
	line := func(s string) state.Line {
		return state.Line{Kind: s[:1], Text: s[1:]}
	}
	diff := func(texts ...string) []state.Line {
		var out []state.Line
		for _, s := range texts {
			out = append(out, line(s))
		}
		return out
	}
	q := state.Question{Before: diff(" a", " b"), Lines: diff("-c", "+C"), After: diff(" d", " e")}
	for _, tc := range []struct {
		name  string
		lines []state.Line
		at    int
		ok    bool
	}{
		{"where it was", diff(" a", " b", "-c", "+C", " d", " e"), 2, true},
		{"moved down", diff("+new", " x", " a", " b", "-c", "+C", " d", " e", " f"), 4, true},
		{"its line changed", diff(" a", " b", "-c", "+CC", " d", " e"), 0, false},
		{"a line before changed", diff(" a", " B", "-c", "+C", " d", " e"), 2, true},
		{"a line after changed", diff(" a", " b", "-c", "+C", " D", " e"), 2, true},
		{"lines on both sides changed", diff(" a", " B", "-c", "+C", " D", " e"), 2, true},
		{
			"lines on both sides changed and its lines twice",
			diff(" a", " B", "-c", "+C", " D", " e", " a", " X", "-c", "+C", " Y", " e"), 0, false,
		},
		{
			"lines before twice, lines after once",
			diff(" a", " b", "-c", "+C", " X", " a", " b", "-c", "+C", " Y", " Z", "-c", "+C", " d", " e"), 11, true,
		},
		{
			"lines after twice, lines before once",
			diff(" X", "-c", "+C", " d", " e", " Y", "-c", "+C", " d", " e", " a", " b", "-c", "+C", " Z"), 12, true,
		},
		{"an exact match first", diff(" X", "-c", "+C", " d", " e", " a", " b", "-c", "+C", " d", " e"), 7, true},
		{"twice", diff(" a", " b", "-c", "+C", " d", " e", " a", " b", "-c", "+C", " d", " e"), 0, false},
		{"an added line no longer added", diff(" a", " b", "-c", " C", " d", " e"), 0, false},
	} {
		at, ok := state.Place(tc.lines, q.Before, q.Lines, q.After)
		if ok != tc.ok || ok && at != tc.at {
			t.Errorf("%s: got %d, %t; want %d, %t", tc.name, at, ok, tc.at, tc.ok)
		}
	}
	// At the top of a file there are fewer lines before.
	top := state.Question{Before: []state.Line{}, Lines: diff("+x"), After: diff(" y")}
	if at, ok := state.Place(diff("+x", " y", " z"), top.Before, top.Lines, top.After); !ok || at != 0 {
		t.Errorf("at the top: got %d, %t, want 0, true", at, ok)
	}
	// Empty lines do not find a place with only some of the lines around them.
	if at, ok := state.Place(diff(" a", " b"), diff(" a"), nil, diff(" X")); ok {
		t.Errorf("no lines, the line after changed: got %d, true, want no place", at)
	}
}
