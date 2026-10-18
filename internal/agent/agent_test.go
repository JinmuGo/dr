package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fixtures are trimmed logs of a Claude Code and a Codex session that each wrote a
// rate limiter in two requests under spike, and an OMP session and subagent written by hand.
const (
	spike    = "/home/dev/Codes/personal/dr-spike2"
	original = "ec6e8b8a-34cf-47ad-b851-09985c776249"
	fork     = "fe5968c4-0984-42cd-ab47-059e1d663eef"
)

var since = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// fakeHome lays out fixtures as the session logs of a home directory. logs maps a path
// under the home directory to the fixture's content.
func fakeHome(t *testing.T, logs map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for path, data := range logs {
		path = filepath.Join(home, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoad(t *testing.T) {
	home := fakeHome(t, map[string]string{
		".claude/projects/spike/" + original + ".jsonl":                      fixture(t, "claude-"+original+".jsonl"),
		".codex/sessions/2026/10/09/rollout-01a1209b.jsonl":                  fixture(t, "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl"),
		".omp/agent/sessions/spike/01a1f000-1.jsonl":                         fixture(t, "omp-01a1f000-0000-7000-8000-000000000001.jsonl"),
		".omp/agent/sessions/spike/01a1f000-1/0-Task.jsonl":                  fixture(t, "omp-01a1f000-0000-7000-8000-000000000002.jsonl"),
		".omp/agent/sessions/elsewhere/2026-10-09T12-00-00-000Z_other.jsonl": `{"type":"session","id":"other","cwd":"/elsewhere"}`,
	})
	var loader Loader
	edits, problems := loader.Load(home, since, []string{spike})
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}

	type step struct {
		agent, session, parent string
		turn                   int
		prompt, reply, note    string
	}
	for _, tc := range []struct {
		path, start, want string
		steps             []step
	}{
		{
			path: "claude/ratelimit.go", want: "func (l *Limiter) AllowN(n int) bool",
			steps: []step{
				// Claude said nothing before its edits.
				{"claude", original, "", 1, "ratelimit.go 하나에 토큰 버킷", "`ratelimit.go`에 mutex로 보호되고", ""},
				{"claude", original, "", 2, "AllowN(n int) bool 을 추가하고", "`AllowN(n)`을 추가하고 `Allow`가", ""},
				{"claude", original, "", 2, "AllowN(n int) bool 을 추가하고", "`AllowN(n)`을 추가하고 `Allow`가", ""},
			},
		},
		{
			path: "codex/ratelimit.go", want: "func (l *Limiter) AllowN(n int) bool",
			steps: []step{
				// The reply is the final answer, and the note the commentary before the edit.
				{
					"codex", "01a1209b-a5af-7992-ba5a-c5fdd6a39255", "", 1, "ratelimit.go 하나에 토큰 버킷", "`ratelimit.go`에 동시 호출에 안전한 토큰 버킷과",
					"`ratelimit.go`에 동시 호출에 안전한 토큰 버킷을 작성하겠습니다.",
				},
				{
					"codex", "01a1209b-a5af-7992-ba5a-c5fdd6a39255", "", 2, "AllowN(n int) bool 을 추가하고", "`AllowN(n int)`을 추가해",
					"`AllowN(n int)`을 추가하고 `Allow()`가 `AllowN(1)`을 호출하도록 바꾸겠습니다.",
				},
			},
		},
		{
			path: "omp/notes.md", want: "a\nB\nc\nd\n",
			steps: []step{
				// The note is what the agent said before the call, or its intent when it said nothing.
				{"omp", "01a1f000-0000-7000-8000-000000000001", "", 1, "notes.md에 a, b, c", "notes.md를 썼어요.", "Writing notes"},
				{"omp", "01a1f000-0000-7000-8000-000000000001", "", 2, "b를 대문자로", "b를 B로 바꿨어요.", "b를 B로 바꿀게요."},
				// A note that a wait was interrupted is not a request; a request may start with a tag.
				{"omp", "01a1f000-0000-7000-8000-000000000001", "", 3, "<task>끝에 d를 더하고", "d를 더하고 other.md를 고쳤어요.", "Appending d"},
			},
		},
		{
			path: "omp/other.md", start: "x\n", want: "y\nz\n",
			steps: []step{
				{"omp", "01a1f000-0000-7000-8000-000000000001", "", 3, "<task>끝에 d를 더하고", "d를 더하고", "Replacing x"},
				// The subagent gave the intent only as an argument of the call.
				{"omp", "01a1f000-0000-7000-8000-000000000002", "01a1f000-0000-7000-8000-000000000001", 1, "Complete the assigned task", "z를 더했어요.", "Appending z"},
			},
		},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var got []Edit
			for _, e := range edits {
				if e.Path == spike+"/"+tc.path {
					got = append(got, e)
				}
			}
			if len(got) != len(tc.steps) {
				t.Fatalf("got %d edits, want %d", len(got), len(tc.steps))
			}
			file := tc.start
			for i, e := range got {
				s := tc.steps[i]
				if e.Agent != s.agent || e.Session != s.session || e.Parent != s.parent || e.Turn != s.turn ||
					!strings.HasPrefix(e.Prompt, s.prompt) || !strings.HasPrefix(e.Reply, s.reply) {
					t.Errorf("edit %d: got %s %s parent %q #%d %.30q / %.30q, want %+v",
						i, e.Agent, e.Session, e.Parent, e.Turn, e.Prompt, e.Reply, s)
				}
				if e.Note != s.note {
					t.Errorf("edit %d: got note %q, want %q", i, e.Note, s.note)
				}
				if e.Before != nil {
					file = *e.Before
				}
				after, ok := e.Apply(file)
				if !ok {
					t.Fatalf("edit %d does not apply to %q", i, file)
				}
				file = after
			}
			if !strings.Contains(file, tc.want) {
				t.Errorf("file after the edits:\n%s\nwant it to contain %q", file, tc.want)
			}
		})
	}

	later, _ := loader.Load(home, time.Date(2026, 10, 9, 12, 20, 50, 0, time.UTC), []string{spike + "/claude"})
	if len(later) != 2 {
		t.Errorf("Load since 12:20:50 got %d edits, want the 2 Claude edits after it", len(later))
	}
}

func TestModels(t *testing.T) {
	// The model that made each edit, and for Codex and OMP the reasoning effort it was made
	// with: of its turn for Codex, the thinking level the log last changed to for OMP.
	home := fakeHome(t, map[string]string{
		".claude/projects/spike/" + original + ".jsonl":     fixture(t, "claude-"+original+".jsonl"),
		".codex/sessions/2026/10/09/rollout-01a1209b.jsonl": fixture(t, "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl"),
		".omp/agent/sessions/spike/01a1f000-1.jsonl":        fixture(t, "omp-01a1f000-0000-7000-8000-000000000001.jsonl"),
	})
	edits, _ := new(Loader).Load(home, since, []string{spike})
	want := map[string][2]string{
		"claude": {"claude-opus-5-5", ""}, "codex": {"gpt-6-astra", "medium"}, "omp": {"anthropic/claude-opus-5-5", "medium"},
	}
	seen := make(map[string]bool)
	for _, e := range edits {
		seen[e.Agent] = true
		if got := [2]string{e.Model, e.Effort}; got != want[e.Agent] {
			t.Errorf("%s edit of %s: got model and effort %q, want %q", e.Agent, e.Path, got, want[e.Agent])
		}
	}
	if len(seen) != len(want) {
		t.Errorf("got edits of %v, want of each of %v", seen, want)
	}
}

func TestCodexTurns(t *testing.T) {
	// Each edit takes the model and the effort of the latest turn_context before it, the
	// effort only from the collaboration mode.
	var records [][]byte
	add := func(typ, payload string) {
		records = append(records, []byte(`{"timestamp":"2026-10-09T12:00:0`+strconv.Itoa(len(records))+`.000Z","type":"`+typ+`","payload":`+payload+`}`))
	}
	change := `{"type":"item_completed","item":{"type":"FileChange","status":"completed","changes":{"/r/f.go":{"type":"add","content":"x\n"}}}}`
	add("session_meta", `{"id":"s","timestamp":"2026-10-09T12:00:00.000Z","cwd":"/r"}`)
	add("turn_context", `{"model":"gpt-6-astra","collaboration_mode":{"mode":"default","settings":{"model":"gpt-6-astra","reasoning_effort":"high"}},"effort":"high"}`)
	add("event_msg", change)
	add("turn_context", `{"model":"gpt-6.1-sol","collaboration_mode":{"mode":"default","settings":{"model":"gpt-6.1-sol","reasoning_effort":null}},"effort":"low"}`)
	add("event_msg", change)
	var got [][2]string
	for _, e := range parseCodex(newRecordReader(bytes.NewReader(bytes.Join(records, []byte("\n"))))) {
		got = append(got, [2]string{e.Model, e.Effort})
	}
	want := [][2]string{{"gpt-6-astra", "high"}, {"gpt-6.1-sol", ""}}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOMPModels(t *testing.T) {
	// Each edit takes the model and the thinking level the log last changed to before it.
	var records [][]byte
	add := func(record string) {
		records = append(records, []byte(record))
	}
	write := func(id string) {
		add(`{"type":"message","timestamp":"2026-10-09T12:00:01.000Z","message":{"role":"assistant",` +
			`"content":[{"type":"toolCall","id":"` + id + `","name":"write","arguments":{"path":"/r/f.go","content":"x\n"}}]}}`)
		add(`{"type":"message","timestamp":"2026-10-09T12:00:02.000Z","message":{"role":"toolResult","toolCallId":"` + id + `","toolName":"write","details":{}}}`)
	}
	add(`{"type":"session","id":"s","timestamp":"2026-10-09T12:00:00.000Z","cwd":"/r"}`)
	write("t0")
	add(`{"type":"model_change","model":"anthropic/claude-opus-5-5"}`)
	add(`{"type":"thinking_level_change","thinkingLevel":"high"}`)
	write("t1")
	add(`{"type":"model_change","model":"openai/gpt-6.1-sol"}`)
	write("t2")
	add(`{"type":"thinking_level_change","thinkingLevel":"low"}`)
	write("t3")
	var got [][2]string
	for _, e := range parseOMP(newRecordReader(bytes.NewReader(bytes.Join(records, []byte("\n"))))) {
		got = append(got, [2]string{e.Model, e.Effort})
	}
	want := [][2]string{{"", ""}, {"anthropic/claude-opus-5-5", "high"}, {"openai/gpt-6.1-sol", "high"}, {"openai/gpt-6.1-sol", "low"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadLogs(t *testing.T) {
	// Each edit names the session log it was read from; a subagent's edit, the subagent's.
	logs := map[string]string{ // by session
		original:                               ".claude/projects/spike/" + original + ".jsonl",
		"01a1209b-a5af-7992-ba5a-c5fdd6a39255": ".codex/sessions/2026/10/09/rollout-01a1209b.jsonl",
		"01a1f000-0000-7000-8000-000000000001": ".omp/agent/sessions/spike/01a1f000-1.jsonl",
		"01a1f000-0000-7000-8000-000000000002": ".omp/agent/sessions/spike/01a1f000-1/0-Task.jsonl",
	}
	home := fakeHome(t, map[string]string{
		logs[original]: fixture(t, "claude-"+original+".jsonl"),
		logs["01a1209b-a5af-7992-ba5a-c5fdd6a39255"]: fixture(t, "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl"),
		logs["01a1f000-0000-7000-8000-000000000001"]: fixture(t, "omp-01a1f000-0000-7000-8000-000000000001.jsonl"),
		logs["01a1f000-0000-7000-8000-000000000002"]: fixture(t, "omp-01a1f000-0000-7000-8000-000000000002.jsonl"),
	})
	edits, _ := new(Loader).Load(home, since, []string{spike})
	seen := make(map[string]bool)
	for _, e := range edits {
		seen[e.Session] = true
		if want := filepath.Join(home, logs[e.Session]); e.Log != want {
			t.Errorf("%s edit of %s: got log %s, want %s", e.Agent, e.Path, e.Log, want)
		}
	}
	if len(seen) != len(logs) {
		t.Errorf("got edits of the sessions %v, want of each of %v", seen, logs)
	}
}

func TestCanFork(t *testing.T) {
	for _, tc := range []struct {
		e    Edit
		want string // why dr cannot fork the session, "" when it can
	}{
		{Edit{Agent: "claude", Session: "s1"}, ""},
		{Edit{Agent: "claude", Session: "a1", Parent: "s1"}, "claude cannot resume a subagent's session"},
		{Edit{Agent: "codex", Session: "t1"}, ""},
		{Edit{Agent: "omp", Session: "o1", Log: "/l/o1.jsonl"}, ""},
		{Edit{Agent: "omp", Session: "o2", Parent: "o1", Log: "/l/o1/0-Task.jsonl"}, ""}, // a subagent forks from its own log
		{Edit{Agent: "omp", Session: "o1"}, "the session's log is not known"},
		{Edit{Agent: "gemini", Session: "g1"}, "dr cannot ask gemini sessions"},
	} {
		got := ""
		if err := CanFork(&tc.e); err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("%s %s parent %q log %q: got %q, want %q", tc.e.Agent, tc.e.Session, tc.e.Parent, tc.e.Log, got, tc.want)
		}
	}
	if got, want := Names(), []string{"claude", "codex", "omp"}; !slices.Equal(got, want) {
		t.Errorf("Names: got %q, want %q", got, want)
	}
}

func TestLoadForks(t *testing.T) {
	originalLog := fixture(t, "claude-"+original+".jsonl")
	forked := fixture(t, "claude-"+fork+".jsonl")
	// A session that is the same as the original but started at the same time cannot be
	// told apart from it.
	twin := strings.ReplaceAll(originalLog, original, "0e0e0e0e-0000-4000-8000-000000000000")
	// A fork opened later to keep working makes edits of its own.
	more := forked + `{"type":"user","sessionId":"` + fork + `","timestamp":"2026-10-09T12:30:00.000Z","cwd":"` + spike + `/claude","gitBranch":"main","message":{"role":"user","content":"Limiter에 Reset을 더해줘"}}` + "\n" +
		`{"type":"user","sessionId":"` + fork + `","timestamp":"2026-10-09T12:30:10.000Z","cwd":"` + spike + `/claude","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]},"toolUseResult":{"filePath":"` + spike + `/claude/ratelimit.go","oldString":"package ratelimit\n","newString":"package ratelimit\n\n// Reset is new.\n","replaceAll":false,"originalFile":null}}` + "\n"

	for _, tc := range []struct {
		name string
		logs map[string]string
		want []string // the session of each edit
	}{
		{"original read first", map[string]string{"a.jsonl": originalLog, "b.jsonl": forked}, []string{original, original, original}},
		{"fork read first", map[string]string{"a.jsonl": forked, "b.jsonl": originalLog}, []string{original, original, original}},
		{"same start", map[string]string{"a.jsonl": originalLog, "b.jsonl": twin}, nil},
		{"new edit in a fork", map[string]string{"a.jsonl": originalLog, "b.jsonl": more}, []string{original, original, original, fork}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := make(map[string]string, len(tc.logs))
			for name, data := range tc.logs {
				logs[".claude/projects/spike/"+name] = data
			}
			edits, _ := new(Loader).Load(fakeHome(t, logs), since, []string{spike})
			got := make([]string, 0, len(edits))
			for _, e := range edits {
				got = append(got, e.Session)
			}
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("sessions of the edits: got %v, want %v", got, tc.want)
			}
			if tc.name == "new edit in a fork" {
				// The fork carries the two original requests and the two review questions dr sent.
				const prompt = "Limiter에 Reset을 더해줘"
				if e := edits[3]; e.Turn != 5 || e.Prompt != prompt {
					t.Errorf("fork edit: got #%d %q, want #5 %q", e.Turn, e.Prompt, prompt)
				}
			}
		})
	}
}

func TestClaudeRequests(t *testing.T) {
	var records [][]byte
	add := func(r map[string]any) {
		r["sessionId"], r["timestamp"] = "s", fmt.Sprintf("2026-10-09T12:00:%02d.000Z", len(records))
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, data)
	}
	say := func(text string, extra ...string) {
		r := map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}}
		for _, k := range extra {
			r[k] = true
		}
		add(r)
	}
	edit := func() {
		add(map[string]any{"type": "user", "toolUseResult": map[string]any{
			"filePath": "/r/f.go", "oldString": "a", "newString": "b", "originalFile": nil,
		}})
	}
	say("Peek을 더해줘")
	edit()
	say("<task-notification>background task done</task-notification>")
	edit()
	say("<task>Len도 더해줘</task>")
	edit()
	say("<command-name>/fix</command-name>\n<command-message>fix</command-message>\n<command-args>12</command-args>")
	say("[Request interrupted by user]")
	say("Caveat: the messages below were generated by the user while running local commands.", "isMeta")
	edit()

	edits := parseClaude(newRecordReader(bytes.NewReader(bytes.Join(records, []byte("\n")))))
	got := make([]string, 0, len(edits))
	for _, e := range edits {
		got = append(got, fmt.Sprintf("#%d %s", e.Turn, e.Prompt))
	}
	want := []string{"#1 Peek을 더해줘", "#1 Peek을 더해줘", "#2 <task>Len도 더해줘</task>", "#3 /fix 12"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestClaudeNotes(t *testing.T) {
	var records [][]byte
	add := func(r map[string]any) {
		r["sessionId"], r["timestamp"] = "s", fmt.Sprintf("2026-10-09T12:00:%02d.000Z", len(records))
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, data)
	}
	// Claude Code logs each block of a response as a record of its own.
	block := func(response string, b map[string]any) {
		add(map[string]any{"type": "assistant", "message": map[string]any{"id": response, "role": "assistant", "content": []any{b}}})
	}
	say := func(response, text string) {
		block(response, map[string]any{"type": "text", "text": text})
	}
	call := func(response, id string) {
		block(response, map[string]any{"type": "tool_use", "id": id, "name": "Edit"})
	}
	result := func(id string) {
		add(map[string]any{
			"type":          "user",
			"message":       map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id}}},
			"toolUseResult": map[string]any{"filePath": "/r/f.go", "oldString": "a", "newString": "b", "originalFile": nil},
		})
	}
	add(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "f.go를 고쳐줘"}})
	say("m1", "\n\na를 b로 바꿀게요.")
	say("m1", "테스트는 그다음에 돌릴게요.\n")
	call("m1", "t1")
	result("t1")
	call("m2", "t2") // the next response says nothing
	result("t2")
	// Claude Code runs a tool before it logs the rest of the response.
	say("m3", "두 곳을 더 고칠게요.")
	call("m3", "t3")
	result("t3")
	call("m3", "t4")
	result("t4")

	var got []string
	for _, e := range parseClaude(newRecordReader(bytes.NewReader(bytes.Join(records, []byte("\n"))))) {
		got = append(got, e.Note)
	}
	want := []string{"a를 b로 바꿀게요.\n테스트는 그다음에 돌릴게요.", "", "두 곳을 더 고칠게요.", "두 곳을 더 고칠게요."}
	if !slices.Equal(got, want) {
		t.Errorf("notes: got %q, want %q", got, want)
	}
}

func TestClaudeModel(t *testing.T) {
	// An edit takes the model of the response that called the tool. A response Claude Code
	// made up itself names the model <synthetic>, which is no model.
	var records [][]byte
	for i, model := range []string{"claude-opus-5-5", "<synthetic>"} {
		id := "t" + strconv.Itoa(i)
		records = append(records,
			[]byte(`{"type":"assistant","sessionId":"s","timestamp":"2026-10-09T12:00:0`+strconv.Itoa(2*i)+`.000Z",`+
				`"message":{"id":"m`+id+`","model":"`+model+`","content":[{"type":"tool_use","id":"`+id+`","name":"Edit"}]}}`),
			[]byte(`{"type":"user","sessionId":"s","timestamp":"2026-10-09T12:00:0`+strconv.Itoa(2*i+1)+`.000Z",`+
				`"message":{"content":[{"type":"tool_result","tool_use_id":"`+id+`"}]},`+
				`"toolUseResult":{"filePath":"/r/f.go","oldString":"a","newString":"b","originalFile":null}}`))
	}
	var got []string
	for _, e := range parseClaude(newRecordReader(bytes.NewReader(bytes.Join(records, []byte("\n"))))) {
		got = append(got, e.Model)
	}
	if want := []string{"claude-opus-5-5", ""}; !slices.Equal(got, want) {
		t.Errorf("models: got %q, want %q", got, want)
	}
}

func TestClaudeSubagent(t *testing.T) {
	// A Claude Code 2.1.295 session whose subagent wrote set.go. The subagent's log has no
	// toolUseResult, so the Write is read from the call its result answers.
	const (
		dir      = "/home/dev/Codes/personal/dr-k-e2e"
		parent   = "d3598b6f-f161-41e2-87a0-eeffe6014022"
		subagent = "aefd1f72d41d72e65"
	)
	subagentLog := ".claude/projects/e2e/" + parent + "/subagents/agent-" + subagent + ".jsonl"
	home := fakeHome(t, map[string]string{
		".claude/projects/e2e/" + parent + ".jsonl": fixture(t, "claude-"+parent+".jsonl"),
		subagentLog: fixture(t, "claude-"+subagent+".jsonl"),
	})
	edits, problems := new(Loader).Load(home, since, []string{dir})
	if len(problems) > 0 {
		t.Fatalf("problems: %+v", problems)
	}
	// Neither the subagent's Read nor the parent's call of the subagent is an edit.
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want the subagent's Write", len(edits))
	}
	e := edits[0]
	if e.Agent != "claude" || e.Session != subagent || e.Parent != parent || e.Turn != 1 ||
		!strings.HasPrefix(e.Prompt, "Create the file "+dir+"/set.go") || !strings.HasPrefix(e.Reply, "I created `set.go`") {
		t.Errorf("got %s %s parent %q #%d %.40q / %.40q, want claude %s parent %s #1",
			e.Agent, e.Session, e.Parent, e.Turn, e.Prompt, e.Reply, subagent, parent)
	}
	// The edit took place when the result was logged, by the model that made the call.
	at := time.Date(2026, 10, 10, 5, 47, 30, 933_000_000, time.UTC)
	if e.Path != dir+"/set.go" || e.Cwd != dir || e.Branch != "main" || !e.Time.Equal(at) || e.Log != filepath.Join(home, subagentLog) {
		t.Errorf("got %s in %s on %s at %v from %s, want set.go in %s on main at %v from the subagent's log",
			e.Path, e.Cwd, e.Branch, e.Time, e.Log, dir, at)
	}
	if e.Note != "" || e.Model != "claude-opus-5-5" || e.Effort != "" {
		t.Errorf("got note %q, model %q, effort %q, want only the model claude-opus-5-5", e.Note, e.Model, e.Effort)
	}
	// The Write replaced the whole file, and the log does not hold the file before it.
	if e.Before != nil {
		t.Errorf("got the file before the Write %q, want none", *e.Before)
	}
	set, err := os.ReadFile("../review/testdata/claude-set.go") // what the subagent wrote
	if err != nil {
		t.Fatal(err)
	}
	if after, ok := e.Apply("package old\n"); !ok || after != string(set) {
		t.Errorf("got %q, %v after the Write, want %q", after, ok, set)
	}
}

func TestClaudeCalls(t *testing.T) {
	// A tool result with no toolUseResult, as in a subagent's log of Claude Code 2.1.295,
	// takes its edit from the Write or Edit call it answers, if the call was allowed and did
	// not fail. A result with toolUseResult takes it from toolUseResult alone.
	call := func(id, name, input string) string {
		return `{"type":"assistant","sessionId":"s","timestamp":"2026-10-09T12:00:00.000Z","message":{"model":"claude-opus-5-5",` +
			`"id":"m` + id + `","type":"message","role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"` + name +
			`","input":` + input + `,"caller":{"type":"direct"}}]}}`
	}
	// result answers the call id, with block added to its tool_result block and record to
	// the record.
	result := func(id, block, record string) string {
		return `{"type":"user","sessionId":"s","timestamp":"2026-10-09T12:00:01.000Z","message":{"role":"user","content":[` +
			`{"tool_use_id":"` + id + `","type":"tool_result","content":"ok"` + block + `}]}` + record + `}`
	}
	const (
		editA    = `{"replace_all":false,"file_path":"/r/f.go","old_string":"a\n","new_string":"A\n"}`
		editB    = `{"replace_all":false,"file_path":"/r/f.go","old_string":"b\n","new_string":"B\n"}`
		editAllB = `{"replace_all":true,"file_path":"/r/f.go","old_string":"b\n","new_string":"B\n"}`
		write    = `{"file_path":"/r/f.go","content":"x\n"}`
		accepted = `,"permissionDecision":{"decision":"accept","source":"config","reasonType":"mode"}`
	)
	// describe says what the log holds of the file before an edit and what the edit makes
	// of that file, or of "a\nb\nb\n" when the log does not hold it.
	describe := func(e Edit) string {
		if e.Unread != "" {
			if _, ok := e.Apply("a\nb\nb\n"); ok {
				return "unread " + e.Unread + " that fits"
			}
			return "unread " + e.Unread + " of " + e.Path
		}
		before, file := "unknown", "a\nb\nb\n"
		if e.Before != nil {
			before, file = strconv.Quote(*e.Before), *e.Before
		}
		if after, ok := e.Apply(file); ok {
			return before + " before, " + strconv.Quote(after) + " after"
		}
		return before + " before, does not fit"
	}
	for _, tc := range []struct {
		name    string
		records []string
		want    []string // the edits, as describe says them
	}{
		{"edit", []string{call("t1", "Edit", editA), result("t1", "", accepted)}, []string{`unknown before, "A\nb\nb\n" after`}},
		// An Edit replaces a string that occurs once, unless it replaces all of them.
		{"edit of a repeated string", []string{call("t1", "Edit", editB), result("t1", "", accepted)}, []string{"unknown before, does not fit"}},
		{"edit of every copy", []string{call("t1", "Edit", editAllB), result("t1", "", accepted)}, []string{`unknown before, "a\nB\nB\n" after`}},
		{"failed", []string{call("t1", "Edit", editA), result("t1", `,"is_error":true`, accepted+`,"toolUseResult":"Error: String to replace not found in file."`)}, nil},
		{"denied", []string{call("t1", "Write", write), result("t1", "", `,"permissionDecision":{"decision":"reject","source":"config"}`)}, nil},
		{"no permission decision", []string{call("t1", "Write", write), result("t1", "", "")}, nil},
		{"no call", []string{result("t1", "", accepted)}, nil},
		{"call answered twice", []string{call("t1", "Write", write), result("t1", "", accepted), result("t1", "", accepted)}, []string{`unknown before, "x\n" after`}},
		// toolUseResult comes first, and it holds the file before the edit.
		{
			"toolUseResult and call",
			[]string{call("t1", "Edit", editA), result("t1", "", accepted+`,"toolUseResult":{"filePath":"/r/f.go","oldString":"a\n","newString":"A\n",`+
				`"originalFile":"a\nc\n","structuredPatch":[],"userModified":false,"replaceAll":false}`)},
			[]string{`"a\nc\n" before, "A\nc\n" after`},
		},
		// An edit dr cannot read is kept as unread, so its file can say so.
		{
			"toolUseResult it does not know",
			[]string{call("t1", "Edit", editA), result("t1", "", accepted+`,"toolUseResult":{"filePath":"/r/f.go","patch":"@@ -1 +1 @@"}`)},
			[]string{"unread Edit of /r/f.go"},
		},
		{
			"arguments it does not know",
			[]string{call("t1", "Edit", `{"path":"/r/f.go","old":"a\n","new":"A\n"}`), result("t1", "", accepted)},
			[]string{"unread Edit of /r/f.go"},
		},
		{
			"tool it does not read",
			[]string{call("t1", "mcp__tools__edit", `{"path":"/r/f.go","edits":[]}`), result("t1", "", "")},
			[]string{"unread mcp__tools__edit of /r/f.go"},
		},
		{"tool it does not read that failed", []string{call("t1", "mcp__tools__write", `{"path":"/r/f.go"}`), result("t1", `,"is_error":true`, "")}, nil},
		{"tool that names no file", []string{call("t1", "TodoWrite", `{"todos":[]}`), result("t1", "", "")}, nil},
		{"tool that does not edit", []string{call("t1", "Read", `{"file_path":"/r/f.go"}`), result("t1", "", "")}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			records := newRecordReader(strings.NewReader(strings.Join(tc.records, "\n")))
			var got []string
			for _, e := range parseClaude(records) {
				got = append(got, describe(e))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOMPProposals(t *testing.T) {
	// ast_edit stages a proposal, and a write to xd://resolve applies it. dr cannot read
	// what it changed, so each file it names has an unread edit, at the time it was
	// applied. A proposal that is never applied changes nothing.
	edits := parseOMP(newRecordReader(strings.NewReader(fixture(t, "omp-01a1f000-0000-7000-8000-000000000003.jsonl"))))
	var got []string
	for _, e := range edits {
		got = append(got, fmt.Sprintf("%s %s #%d %q %s", e.Unread, strings.TrimPrefix(e.Path, spike+"/"), e.Turn, e.Note, e.Time.Format(time.TimeOnly)))
		if _, ok := e.Apply("a\nB\n"); ok {
			t.Errorf("unread edit of %s fits a file", e.Path)
		}
	}
	want := []string{
		`ast_edit omp/lower.md #1 "B를 b로 바꾸는 제안을 적용할게요." 13:00:09`,
		`ast_edit omp/notes.md #1 "B를 b로 바꾸는 제안을 적용할게요." 13:00:09`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodexUnread(t *testing.T) {
	// A change Codex logs in a form dr cannot read is unread. A moved file's lines under its
	// new path have no origin, so the new path has an unread edit.
	changes := func(c string) string {
		return `{"timestamp":"2026-10-09T12:00:01.000Z","type":"event_msg","payload":{"type":"item_completed","item":` +
			`{"type":"FileChange","status":"completed","changes":` + c + `}}}`
	}
	for _, tc := range []struct {
		name, changes string
		want          []string
	}{
		{"diff it does not know", `{"/r/f.go":{"type":"update","unified_diff":"@@ bad"}}`, []string{"unread /r/f.go"}},
		{"kind it does not know", `{"/r/f.go":{"type":"rename"}}`, []string{"unread /r/f.go"}},
		{"update with no diff", `{"/r/f.go":{"type":"update","unified_diff":""}}`, nil},
		{"move", `{"/r/f.go":{"type":"update","unified_diff":"","move_path":"/r/g.go"}}`, []string{"unread /r/g.go", "read /r/f.go"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := `{"timestamp":"2026-10-09T12:00:00.000Z","type":"session_meta","payload":{"id":"s","cwd":"/r"}}` + "\n" + changes(tc.changes)
			var got []string
			for _, e := range parseCodex(newRecordReader(strings.NewReader(log))) {
				if e.Unread != "" {
					got = append(got, "unread "+e.Path)
				} else {
					got = append(got, "read "+e.Path)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
