package agent

import (
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestRecordReader(t *testing.T) {
	long := `{"ignored":"` + strings.Repeat("x", 256*1024) + `"}`
	for _, tc := range []struct {
		name, input string
		want        []string
		bad         int
	}{
		{"empty", "", nil, 0},
		{"whitespace", " \t\n\r\n ", nil, 0},
		{"valid EOF", "{}", []string{"{}"}, 0},
		{"unfinished EOF", "{}\n{", []string{"{}"}, 0},
		{"invalid complete", "{\n{}\nnot json\n[", []string{"{}"}, 2},
		{"unknown valid values", "null\n42\n[]\n\"text\"\n{\"type\":\"unknown\"}\n", []string{"null", "42", "[]", `"text"`, `{"type":"unknown"}`}, 0},
		{"long line", long + "\n{}", []string{long, "{}"}, 0},
		{"long unfinished", "{}\n" + long[:len(long)-1], []string{"{}"}, 0},
		{"long invalid complete", long[:len(long)-1] + "\n{}", []string{"{}"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reader := range []io.Reader{strings.NewReader(tc.input), iotest.OneByteReader(strings.NewReader(tc.input))} {
				records := newRecordReader(reader)
				var got []string
				for records.next() {
					got = append(got, strings.TrimSpace(string(records.line)))
				}
				if !slices.Equal(got, tc.want) || records.bad != tc.bad || records.err != nil {
					t.Errorf("got %d records, %d bad, error %v; want %d records, %d bad", len(got), records.bad, records.err, len(tc.want), tc.bad)
				}
			}
		})
	}
}

func TestMentions(t *testing.T) {
	const root = "/repo/worktree"
	longRoot := "/" + strings.Repeat("r", 40*1024)
	for _, tc := range []struct {
		name, input string
		roots       []string
		want        bool
	}{
		{"cross buffer", strings.Repeat("x", 32*1024-3) + root, []string{root}, true},
		{"cross many buffers", longRoot, []string{longRoot}, true},
		{"later root", strings.Repeat("x", 70*1024) + root, []string{"/other", root}, true},
		{"absent", strings.Repeat("x", 70*1024), []string{root}, false},
		{"empty roots", root, nil, false},
		{"empty substring", "", []string{""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roots := make([][]byte, len(tc.roots))
			for i, root := range tc.roots {
				roots[i] = []byte(root)
			}
			got, err := mentions(strings.NewReader(tc.input), roots)
			if got != tc.want || err != nil {
				t.Errorf("got %v, %v; want %v, nil", got, err, tc.want)
			}
		})
	}
	failure := errors.New("read interrupted")
	got, err := mentions(io.MultiReader(strings.NewReader("unrelated"), failingReader{failure}), [][]byte{[]byte(root)})
	if got || !errors.Is(err, failure) {
		t.Errorf("got %v, %v; want false, %v", got, err, failure)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestStreamingFixtures(t *testing.T) {
	for _, tc := range []struct {
		agent   agent
		fixture string
	}{
		{claude, "claude-" + original + ".jsonl"},
		{claude, "claude-aefd1f72d41d72e65.jsonl"},
		{codex, "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl"},
		{omp, "omp-01a1f000-0000-7000-8000-000000000001.jsonl"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			data := fixture(t, tc.fixture)
			baseline := sessionLog{agent: tc.agent, path: "fixture.jsonl"}
			if !baseline.readRecords(strings.NewReader(data), time.Time{}) || len(baseline.edits) == 0 {
				t.Fatal("fixture yielded no edits")
			}
			// Force buffer reuse between every pair of records, including tool calls and results.
			padding := `{"type":"unknown","payload":"` + strings.Repeat("x", 80*1024) + `"}` + "\n"
			expanded := strings.ReplaceAll(data, "\n", "\n"+padding)
			log := sessionLog{agent: tc.agent, path: "fixture.jsonl"}
			if !log.readRecords(strings.NewReader(expanded+"not json\n{unfinished"), time.Time{}) {
				t.Fatal("expanded fixture had an I/O failure")
			}
			if log.problem == nil || log.problem.Bad != 1 {
				t.Fatalf("got problem %+v, want one malformed complete line", log.problem)
			}
			if len(log.edits) != len(baseline.edits) {
				t.Fatalf("got %d edits, want %d", len(log.edits), len(baseline.edits))
			}
			for i, got := range log.edits {
				want := baseline.edits[i]
				before := ""
				if want.Before != nil {
					before = *want.Before
				}
				gotAfter, gotOK := got.Apply(before)
				wantAfter, wantOK := want.Apply(before)
				got.Apply, want.Apply = nil, nil
				if !reflect.DeepEqual(got, want) || gotAfter != wantAfter || gotOK != wantOK {
					t.Errorf("edit %d changed after buffer reuse", i)
				}
			}
			failure := errors.New("stream interrupted")
			broken := sessionLog{agent: tc.agent, path: "broken.jsonl"}
			if broken.readRecords(io.MultiReader(strings.NewReader(data), failingReader{failure}), time.Time{}) || len(broken.edits) != 0 || broken.problem == nil || broken.problem.Bad != 0 {
				t.Errorf("I/O failure got %d edits and problem %+v, want discarded edits and unreadable Problem", len(broken.edits), broken.problem)
			}
		})
	}
}

func TestStreamingLongEditAndForeignCwd(t *testing.T) {
	content := strings.Repeat("owned content ", 20*1024)
	padding := `{"type":"unknown","content":"` + strings.Repeat("x", len(content)+1024) + `"}`
	for _, tc := range []struct {
		agent   agent
		records []string
	}{
		{claude, []string{
			`{"type":"user","sessionId":"s","timestamp":"2026-10-09T12:00:00Z","cwd":"/elsewhere","message":{"content":"write it"}}`,
			`{"type":"assistant","sessionId":"s","message":{"id":"m","content":[{"type":"tool_use","id":"call","name":"Write","input":{"file_path":"/r/f","content":"` + content + `"}}]}}`,
			padding,
			`{"type":"user","sessionId":"s","timestamp":"2026-10-09T12:00:01Z","permissionDecision":{"decision":"accept"},"message":{"content":[{"type":"tool_result","tool_use_id":"call"}]}}`,
			`{"type":"assistant","sessionId":"s","message":{"content":"finished"}}`,
		}},
		{codex, []string{
			`{"type":"session_meta","payload":{"id":"s","cwd":"/elsewhere"}}`,
			`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"UserMessage","content":"write it"}}}`,
			`{"type":"event_msg","timestamp":"2026-10-09T12:00:01Z","payload":{"type":"item_completed","item":{"type":"FileChange","status":"completed","changes":{"/r/f":{"type":"add","content":"` + content + `"}}}}}`,
			padding,
			`{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","phase":"final_answer","content":"finished"}}}`,
		}},
		{omp, []string{
			`{"type":"session","id":"s","cwd":"/elsewhere"}`,
			`{"type":"message","message":{"role":"user","content":"write it"}}`,
			`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"call","name":"write","arguments":{"path":"/r/f","content":"` + content + `"}}]}}`,
			padding,
			`{"type":"message","timestamp":"2026-10-09T12:00:01Z","message":{"role":"toolResult","toolCallId":"call","toolName":"write","details":{}}}`,
			`{"type":"message","message":{"role":"assistant","content":"finished"}}`,
		}},
	} {
		t.Run(tc.agent.name, func(t *testing.T) {
			home := fakeHome(t, map[string]string{tc.agent.logs + "/long.jsonl": strings.Join(tc.records, "\n")})
			edits, problems := new(Loader).Load(home, since, []string{"/r"})
			if len(edits) != 1 || len(problems) != 0 {
				t.Fatalf("got %d edits and %v problems, want one edit and no problems", len(edits), problems)
			}
			edit := edits[0]
			if edit.Path != "/r/f" || edit.Prompt != "write it" || edit.Reply != "finished" {
				t.Errorf("got %q prompt %q reply %q", edit.Path, edit.Prompt, edit.Reply)
			}
			if got, ok := edit.Apply(""); !ok || got != content {
				t.Errorf("got %d bytes, applies %v; want %d bytes", len(got), ok, len(content))
			}
		})
	}
}
