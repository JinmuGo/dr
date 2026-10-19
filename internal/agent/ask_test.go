//go:build unix

package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeCLI stands for claude, codex, omp and pi. It records its arguments, stdin and working
// directory in $FAKE_DIR, and in $FAKE_DIR/sessions whether the directory after
// --session-dir was there. It writes $FAKE_DIR/answer to the file after -o, prints
// $FAKE_DIR/stdout and $FAKE_DIR/stderr and exits with the code in $FAKE_DIR/exit. With
// $FAKE_DIR/sleep it first starts a child and waits for it, so that it has to be killed.
// With $FAKE_DIR/leave it starts a child that keeps its stdout open and does not wait for
// it. With $FAKE_DIR/rollout it writes that as the session log of Codex thread t1 under the
// home directory $FAKE_DIR/home. Asked for its models with --list-models, it prints
// $FAKE_DIR/models alone when there is one.
const fakeCLI = `#!/bin/sh
d=$FAKE_DIR
if [ "$1" = --list-models ] && [ -f "$d/models" ]; then cat "$d/models"; exit 0; fi
: > "$d/args"
out=
sessions=
prev=
for a in "$@"; do
	printf '%s\n' "$a" >> "$d/args"
	if [ "$prev" = -o ]; then out=$a; fi
	if [ "$prev" = --session-dir ]; then sessions=$a; fi
	prev=$a
done
cat > "$d/stdin"
pwd -P > "$d/cwd"
if [ -n "$sessions" ] && [ -d "$sessions" ]; then echo yes > "$d/sessions"; fi
if [ -f "$d/sleep" ]; then
	sleep 60 &
	echo $! > "$d/child"
	echo $$ > "$d/pid"
	wait
fi
if [ -f "$d/leave" ]; then
	sleep 60 &
	echo $! > "$d/child"
fi
if [ -f "$d/rollout" ]; then
	mkdir -p "$d/home/.codex/sessions/2026/10/10"
	cp "$d/rollout" "$d/home/.codex/sessions/2026/10/10/rollout-2026-10-10T12-00-00-t1.jsonl"
fi
if [ -n "$out" ] && [ -f "$d/answer" ]; then cat "$d/answer" > "$out"; fi
if [ -f "$d/stdout" ]; then cat "$d/stdout"; fi
if [ -f "$d/stderr" ]; then cat "$d/stderr" >&2; fi
if [ -f "$d/exit" ]; then exit "$(cat "$d/exit")"; fi
`

// fakeAgents puts the fake claude, codex, omp and pi first in PATH, with files in the directory
// they read from, which may name subdirectories, and returns that directory.
func fakeAgents(t *testing.T, files map[string]string) string {
	t.Helper()
	bin, dir := t.TempDir(), t.TempDir()
	for _, name := range Names() {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(fakeCLI), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	return dir
}

// fakeDirs returns the directories Ask works with for the fakes in dir: the home directory
// they write to, and a state directory that does not exist yet.
func fakeDirs(t *testing.T, dir string) Dirs {
	t.Helper()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return Dirs{Home: home, State: filepath.Join(t.TempDir(), "git", "dr")}
}

// recorded returns what the fake recorded in dir under name.
func recorded(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sessionDir returns a directory for a session to have run in, and the same with its
// symlinks resolved, as the fake sees it.
func sessionDir(t *testing.T) (dir, resolved string) {
	t.Helper()
	dir = t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, resolved
}

const prompt = "[dr] Review question. Do not change any code, just answer.\na.go:2\n>\tx := 1\nWhy?"

// ompAnswer is what omp --mode json prints after the session event for an answer by MODEL
// of Anthropic, which first reads a file.
const ompAnswer = `{"type":"agent_start"}
{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"[dr] Review question."}]}}
{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"[dr] Review question."}]}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"..."},{"type":"text","text":"Let me read it."},{"type":"toolCall","id":"t1","name":"read","arguments":{"path":"a.go"}}],"provider":"anthropic","model":"claude-opus-5-5","stopReason":"toolUse"}}
{"type":"message_end","message":{"role":"toolResult","toolCallId":"t1","toolName":"read","content":[{"type":"text","text":"package a"}]}}
{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"Bec"}],"provider":"anthropic","model":"MODEL"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Because.\n"}],"provider":"anthropic","model":"MODEL","stopReason":"stop"}}
{"type":"turn_end"}
{"type":"agent_end"}
`

// ompOut is what omp --mode json prints when session answers by model of Anthropic.
func ompOut(session, model string) string {
	return `{"type":"session","version":3,"id":"` + session + `","timestamp":"2026-10-10T12:00:00.000Z","cwd":"/r"}` + "\n" +
		strings.ReplaceAll(ompAnswer, "MODEL", model)
}

// ompArgs returns the arguments omp gets: head, the flags of every question with SESSIONS
// for the state directory's sessions, and tail.
func ompArgs(head []string, tail ...string) []string {
	flags := []string{"--session-dir", "SESSIONS", "--tools", "read,grep,glob", "--no-extensions", "--no-skills",
		"--no-title", "--approval-mode", "always-ask", "--mode", "json"}
	return slices.Concat(head, flags, tail)
}

// piArgs returns the arguments pi gets: head, the flags of every question with SESSIONS for
// the state directory's sessions, and tail.
func piArgs(head []string, tail ...string) []string {
	flags := []string{"--session-dir", "SESSIONS", "--tools", "read,grep,find", "--no-mcp", "--no-extensions",
		"--no-skills", "--no-prompt-templates", "--no-approve", "--mode", "json"}
	return slices.Concat(head, flags, tail)
}

func TestAsk(t *testing.T) {
	cwd, realCwd := sessionDir(t)
	events := `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Because."}}
{"type":"turn.completed","usage":{"input_tokens":57832,"cached_input_tokens":40704,"output_tokens":618}}
`
	codexArgs := func(model ...string) []string {
		args := []string{"--json", "-o", "ANSWER", "--ignore-user-config", "--disable", "hooks",
			"-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`}
		for _, m := range model {
			args = append(args, "-c", m)
		}
		return args
	}
	const sessionLog = ".omp/agent/sessions/-r/2026-10-09T12-00-00-000Z_s1.jsonl"
	for _, tc := range []struct {
		name  string
		q     Question
		log   string // where the session's log is, under a directory of its own, to set q.Log to
		files map[string]string
		args  []string // with ANSWER for the file after -o, LOG for q.Log and SESSIONS for the state directory's sessions
		want  Answer
	}{
		{
			name: "claude, a new fork",
			q:    Question{Agent: "claude", Session: "s1", Model: "claude-opus-5-5"},
			files: map[string]string{"stdout": `{"type":"result","subtype":"success","is_error":false,"session_id":"f1",` +
				`"result":" Because.\n","modelUsage":{"claude-haiku-4-5":{"inputTokens":1},"claude-opus-5-5":{"inputTokens":2}}}`},
			args: []string{"-p", "--resume", "s1", "--fork-session", "--safe-mode", "--tools", "Read,Grep,Glob",
				"--model", "claude-opus-5-5", "--output-format", "json"},
			want: Answer{Fork: "f1", Text: "Because.", Model: "claude-opus-5-5"},
		},
		{
			name:  "claude, the fork continued, another model answering",
			q:     Question{Agent: "claude", Session: "s1", Model: "claude-opus-5-5", Fork: "f1"},
			files: map[string]string{"stdout": `{"is_error":false,"session_id":"f1","result":"Again.","modelUsage":{"claude-sonnet-5":{}}}`},
			args: []string{"-p", "--resume", "f1", "--safe-mode", "--tools", "Read,Grep,Glob",
				"--model", "claude-opus-5-5", "--output-format", "json"},
			want: Answer{Fork: "f1", Text: "Again.", Model: "claude-sonnet-5"},
		},
		{
			name:  "claude, the model not known",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"stdout": `{"session_id":"f1","result":"Because.","modelUsage":{"claude-opus-5-5":{},"claude-haiku-4-5":{}}}`},
			args:  []string{"-p", "--resume", "s1", "--fork-session", "--safe-mode", "--tools", "Read,Grep,Glob", "--output-format", "json"},
			want:  Answer{Fork: "f1", Text: "Because.", Model: "claude-opus-5-5"}, // the first
		},
		{
			name:  "claude, a new session",
			q:     Question{Agent: "claude"},
			files: map[string]string{"stdout": `{"session_id":"n1","result":"Because.","modelUsage":{"claude-opus-5-5":{}}}`},
			args:  []string{"-p", "--safe-mode", "--tools", "Read,Grep,Glob", "--output-format", "json"},
			want:  Answer{Fork: "n1", Text: "Because.", Model: "claude-opus-5-5"},
		},
		{
			name:  "codex, a new fork",
			q:     Question{Agent: "codex", Session: "s1", Model: "gpt-6.1-sol", Effort: "medium"},
			files: map[string]string{"stdout": events, "answer": "Because.\n"},
			args: slices.Concat([]string{"exec", "fork"}, codexArgs(`model="gpt-6.1-sol"`, `model_reasoning_effort="medium"`),
				[]string{"s1", "-"}),
			want: Answer{Fork: "t1", Text: "Because.", Model: "gpt-6.1-sol"},
		},
		{
			name:  "codex, the fork continued without an effort",
			q:     Question{Agent: "codex", Session: "s1", Model: "gpt-6.1-sol", Fork: "t1"},
			files: map[string]string{"stdout": events, "answer": "Again."},
			args:  slices.Concat([]string{"exec", "resume"}, codexArgs(`model="gpt-6.1-sol"`), []string{"t1", "-"}),
			want:  Answer{Fork: "t1", Text: "Again.", Model: "gpt-6.1-sol"},
		},
		{
			name:  "codex, the model not known",
			q:     Question{Agent: "codex", Session: "s1"},
			files: map[string]string{"stdout": events, "answer": "Because."},
			args:  slices.Concat([]string{"exec", "fork"}, codexArgs(), []string{"s1", "-"}),
			want:  Answer{Fork: "t1", Text: "Because."},
		},
		{
			name:  "codex, a new session",
			q:     Question{Agent: "codex"},
			files: map[string]string{"stdout": events, "answer": "Because."},
			args:  slices.Concat([]string{"exec"}, codexArgs(), []string{"-"}),
			want:  Answer{Fork: "t1", Text: "Because."},
		},
		{
			name: "codex, the fork's log naming another model",
			q:    Question{Agent: "codex", Session: "s1", Model: "gpt-6.1-sol"},
			files: map[string]string{
				"stdout": events, "answer": "Because.",
				"rollout": `{"timestamp":"2026-10-10T12:00:00.000Z","type":"session_meta","payload":{"id":"t1"}}` + "\n" +
					`{"timestamp":"2026-10-10T12:00:01.000Z","type":"turn_context","payload":{"model":"gpt-6.1-sol"}}` + "\n" +
					`{"timestamp":"2026-10-10T12:00:02.000Z","type":"turn_context","payload":{"model":"gpt-6.1-mini","collaboration_mode":{"mode":"default","settings":{"model":"gpt-6.1-mini","reasoning_effort":null}}}}` + "\n",
				// The log of the session asked, which is not the fork's.
				"home/.codex/sessions/2026/10/09/rollout-2026-10-09T21-20-34-s1.jsonl": `{"timestamp":"2026-10-09T12:00:01.000Z","type":"turn_context","payload":{"model":"gpt-6.1-sol"}}` + "\n",
			},
			args: slices.Concat([]string{"exec", "fork"}, codexArgs(`model="gpt-6.1-sol"`), []string{"s1", "-"}),
			want: Answer{Fork: "t1", Text: "Because.", Model: "gpt-6.1-mini"}, // of its last turn
		},
		{
			name: "codex, the fork's log naming no model for its last turn",
			q:    Question{Agent: "codex", Session: "s1", Model: "gpt-6.1-sol"},
			files: map[string]string{
				"stdout": events, "answer": "Because.",
				"rollout": `{"timestamp":"2026-10-10T12:00:01.000Z","type":"turn_context","payload":{"model":"gpt-6-astra"}}` + "\n" +
					`{"timestamp":"2026-10-10T12:00:02.000Z","type":"turn_context","payload":{"cwd":"/r"}}` + "\n",
			},
			args: slices.Concat([]string{"exec", "fork"}, codexArgs(`model="gpt-6.1-sol"`), []string{"s1", "-"}),
			want: Answer{Fork: "t1", Text: "Because.", Model: "gpt-6.1-sol"},
		},
		{
			// The answer is the last assistant message, and the fork the session of the event.
			name:  "omp, a new fork",
			q:     Question{Agent: "omp", Session: "s1", Model: "anthropic/claude-opus-5-5", Effort: "medium"},
			log:   sessionLog,
			files: map[string]string{"stdout": ompOut("f1", "claude-opus-5-5")},
			args:  ompArgs([]string{"-p", "--fork", "LOG"}, "--model", "anthropic/claude-opus-5-5", "--thinking", "medium"),
			want:  Answer{Fork: "f1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			name:  "omp, a subagent's log",
			q:     Question{Agent: "omp", Session: "sub1", Model: "anthropic/claude-haiku-4-5"},
			log:   ".omp/agent/sessions/-r/2026-10-09T12-00-00-000Z_s1/0-Task.jsonl",
			files: map[string]string{"stdout": ompOut("f2", "claude-haiku-4-5")},
			args:  ompArgs([]string{"-p", "--fork", "LOG"}, "--model", "anthropic/claude-haiku-4-5"),
			want:  Answer{Fork: "f2", Text: "Because.", Model: "anthropic/claude-haiku-4-5"},
		},
		{
			name:  "omp, the fork continued, another model answering",
			q:     Question{Agent: "omp", Session: "s1", Model: "anthropic/claude-opus-5-5", Effort: "high", Fork: "f1"},
			log:   sessionLog,
			files: map[string]string{"stdout": ompOut("f1", "claude-sonnet-5")},
			args:  ompArgs([]string{"-p", "--resume", "f1"}, "--model", "anthropic/claude-opus-5-5", "--thinking", "high"),
			want:  Answer{Fork: "f1", Text: "Because.", Model: "anthropic/claude-sonnet-5"},
		},
		{
			name:  "omp, a new session",
			q:     Question{Agent: "omp"},
			files: map[string]string{"stdout": ompOut("n1", "claude-opus-5-5")},
			args:  ompArgs([]string{"-p"}),
			want:  Answer{Fork: "n1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			name:  "omp, the new session continued without a session event",
			q:     Question{Agent: "omp", Fork: "n1"},
			files: map[string]string{"stdout": strings.ReplaceAll(ompAnswer, "MODEL", "claude-opus-5-5")},
			args:  ompArgs([]string{"-p", "--resume", "n1"}),
			want:  Answer{Fork: "n1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			// Pi prints the same events as OMP.
			name:  "pi, a new fork",
			q:     Question{Agent: "pi", Session: "s1", Model: "anthropic/claude-opus-5-5", Effort: "medium"},
			log:   ".pi/agent/sessions/--r--/2026-10-09T12-00-00-000Z_s1.jsonl",
			files: map[string]string{"models": piModelList, "stdout": ompOut("f1", "claude-opus-5-5")},
			args:  piArgs([]string{"-p", "--fork", "LOG"}, "--model", "anthropic/claude-opus-5-5", "--thinking", "medium"),
			want:  Answer{Fork: "f1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			// Without extensions, a model only an extension provides is not there. The question
			// goes to the first model of pi's settings that is.
			name: "pi, the fork continued, with a model of an extension",
			q:    Question{Agent: "pi", Session: "s1", Model: "claude-bridge/claude-opus-5-5", Effort: "high", Fork: "f1"},
			log:  ".pi/agent/sessions/--r--/2026-10-09T12-00-00-000Z_s1.jsonl",
			files: map[string]string{
				"models": piModelList,
				"home/.pi/agent/settings.json": `{"defaultProvider":"claude-bridge","defaultModel":"claude-opus-5-5",` +
					`"enabledModels":["claude-bridge/claude-opus-5-5","openai-codex/gpt-6-luna"]}`,
				"stdout": strings.ReplaceAll(ompOut("f1", "gpt-6-luna"), `"anthropic"`, `"openai-codex"`),
			},
			args: piArgs([]string{"-p", "--session", "f1"}, "--model", "openai-codex/gpt-6-luna", "--thinking", "high"),
			want: Answer{Fork: "f1", Text: "Because.", Model: "openai-codex/gpt-6-luna"},
		},
		{
			// With none of pi's settings there either, pi chooses.
			name:  "pi, a new fork, no model there",
			q:     Question{Agent: "pi", Session: "s1", Model: "claude-bridge/claude-opus-5-5"},
			log:   ".pi/agent/sessions/--r--/2026-10-09T12-00-00-000Z_s1.jsonl",
			files: map[string]string{"models": piModelList, "stdout": ompOut("f1", "claude-opus-5-5")},
			args:  piArgs([]string{"-p", "--fork", "LOG"}),
			want:  Answer{Fork: "f1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			// When pi cannot list its models, the model goes as it is, and pi says what is wrong.
			name:  "pi, a new session, its models not listed",
			q:     Question{Agent: "pi", Model: "anthropic/claude-opus-5-5"},
			files: map[string]string{"stdout": ompOut("n1", "claude-opus-5-5")},
			args:  piArgs([]string{"-p"}, "--model", "anthropic/claude-opus-5-5"),
			want:  Answer{Fork: "n1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
		{
			name:  "pi, a new session",
			q:     Question{Agent: "pi"},
			files: map[string]string{"stdout": ompOut("n1", "claude-opus-5-5")},
			args:  piArgs([]string{"-p"}),
			want:  Answer{Fork: "n1", Text: "Because.", Model: "anthropic/claude-opus-5-5"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeAgents(t, tc.files)
			dirs := fakeDirs(t, dir)
			sessions := filepath.Join(dirs.State, "sessions")
			const logData = `{"type":"session","version":3,"id":"s1","cwd":"/r"}` + "\n"
			if tc.log != "" {
				tc.q.Log = filepath.Join(t.TempDir(), tc.log)
				if err := os.MkdirAll(filepath.Dir(tc.q.Log), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tc.q.Log, []byte(logData), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			tc.q.Cwd, tc.q.Prompt = cwd, prompt
			got, err := Ask(context.Background(), dirs, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if got.Took <= 0 {
				t.Errorf("took %v, want the time the agent ran", got.Took)
			}
			got.Took = 0
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
			args := strings.Split(strings.TrimSuffix(recorded(t, dir, "args"), "\n"), "\n")
			if i := slices.Index(args, "-o"); i >= 0 && i+1 < len(args) {
				if _, err := os.Stat(args[i+1]); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("the answer file %s is left: %v", args[i+1], err)
				}
				args[i+1] = "ANSWER"
			}
			for i, a := range args {
				switch {
				case a == sessions:
					args[i] = "SESSIONS"
				case tc.q.Log != "" && a == tc.q.Log:
					args[i] = "LOG"
				}
			}
			if !slices.Equal(args, tc.args) {
				t.Errorf("args:\n got %q\nwant %q", args, tc.args)
			}
			if stdin := recorded(t, dir, "stdin"); stdin != prompt {
				t.Errorf("stdin %q, want the prompt %q", stdin, prompt)
			}
			if got := strings.TrimSpace(recorded(t, dir, "cwd")); got != realCwd {
				t.Errorf("ran in %s, want the session's directory %s", got, realCwd)
			}
			if tc.q.Agent == "omp" || tc.q.Agent == "pi" {
				// The session that answers keeps its log in the state directory, which is there
				// before the agent runs and only the user can read.
				info, err := os.Stat(sessions)
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Errorf("the state directory's sessions: %v, %v; want a directory with mode 0700", info, err)
				}
				if seen := recorded(t, dir, "sessions"); seen != "yes\n" {
					t.Errorf("%s saw the sessions directory %q, want it there before it ran", tc.q.Agent, seen)
				}
			}
			if tc.log != "" {
				// The session's own log does not change.
				if data, err := os.ReadFile(tc.q.Log); err != nil || string(data) != logData {
					t.Errorf("the session's log holds %q, %v; want it unchanged", data, err)
				}
			}
		})
	}
}

func TestAskFailure(t *testing.T) {
	cwd, _ := sessionDir(t)
	long := strings.Repeat("é", 300)
	for _, tc := range []struct {
		name    string
		q       Question
		files   map[string]string
		noState bool // ask without a state directory
		want    string
	}{
		{
			name:  "claude exits with an error",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"stderr": "starting\nError: No conversation found with session ID: s1\n\n", "exit": "1"},
			want:  "Error: No conversation found with session ID: s1",
		},
		{
			name:  "claude says is_error",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"stdout": `{"is_error":true,"result":"Prompt is too long","session_id":"f1"}`, "exit": "1"},
			want:  "Prompt is too long",
		},
		{
			name:  "claude answers nothing",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"stdout": `{"is_error":false,"result":"","session_id":"f1"}`},
			want:  "claude gave no answer",
		},
		{
			name:  "a long reason",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"stderr": long, "exit": "1"},
			want:  long[:2*maxReason], // two bytes each
		},
		{
			name:  "nothing on stderr",
			q:     Question{Agent: "claude", Session: "s1"},
			files: map[string]string{"exit": "3"},
			want:  "claude: exit status 3",
		},
		{
			name:  "codex turn failed",
			q:     Question{Agent: "codex", Session: "s1"},
			files: map[string]string{"stdout": `{"type":"thread.started","thread_id":"t1"}` + "\n" + `{"type":"turn.failed","error":{"message":"model not found"}}`, "stderr": "error\n", "exit": "1"},
			want:  "model not found",
		},
		{
			name:  "codex error event",
			q:     Question{Agent: "codex", Session: "s1"},
			files: map[string]string{"stdout": `{"type":"error","message":"stream disconnected"}`, "exit": "1"},
			want:  "stream disconnected",
		},
		{
			name:  "codex exits with an error",
			q:     Question{Agent: "codex", Session: "s1"},
			files: map[string]string{"stderr": "error: unexpected argument '--safe' found\n", "exit": "2"},
			want:  "error: unexpected argument '--safe' found",
		},
		{
			name:  "codex writes no answer",
			q:     Question{Agent: "codex", Session: "s1"},
			files: map[string]string{"stdout": `{"type":"thread.started","thread_id":"t1"}`},
			want:  "codex gave no answer",
		},
		{
			name:  "omp exits with an error",
			q:     Question{Agent: "omp", Session: "s1", Log: "/gone/s1.jsonl"},
			files: map[string]string{"stdout": `{"type":"agent_start"}`, "stderr": "Error: session file not found: /gone/s1.jsonl\n", "exit": "1"},
			want:  "Error: session file not found: /gone/s1.jsonl",
		},
		{
			name:  "omp exits with nothing on stderr",
			q:     Question{Agent: "omp", Fork: "f1"},
			files: map[string]string{"stdout": ompOut("f1", "claude-opus-5-5"), "exit": "3"},
			want:  "omp: exit status 3",
		},
		{
			name: "omp answers nothing",
			q:    Question{Agent: "omp"},
			files: map[string]string{"stdout": `{"type":"session","id":"n1"}` + "\n" +
				`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"Why?"}]}}` + "\n" + `{"type":"agent_end"}` + "\n"},
			want: "omp gave no answer",
		},
		{
			name: "omp ends on a message without text",
			q:    Question{Agent: "omp", Fork: "f1"},
			files: map[string]string{"stdout": `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Let me write it."}]}}` + "\n" +
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"t1","name":"write","arguments":{}}],"stopReason":"toolUse"}}` + "\n"},
			want: "omp gave no answer",
		},
		{
			name:  "pi exits with an error",
			q:     Question{Agent: "pi", Fork: "f1"},
			files: map[string]string{"stderr": "No session found matching 'f1'\n", "exit": "1"},
			want:  "No session found matching 'f1'",
		},
		{
			name: "pi ends on an error of the provider",
			q:    Question{Agent: "pi", Fork: "f1"},
			files: map[string]string{"stdout": `{"type":"session","id":"f1"}` + "\n" +
				`{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"OpenAI API error (400): model not supported"}}` + "\n"},
			want: "OpenAI API error (400): model not supported",
		},
		{
			name: "pi stops on an error partway through the answer",
			q:    Question{Agent: "pi", Fork: "f1"},
			files: map[string]string{"stdout": `{"type":"session","id":"f1"}` + "\n" +
				`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Because the"}],"stopReason":"error","errorMessage":"stream ended"}}` + "\n"},
			want: "stream ended",
		},
		{
			name:  "pi answers nothing",
			q:     Question{Agent: "pi"},
			files: map[string]string{"stdout": `{"type":"session","id":"n1"}` + "\n" + `{"type":"agent_end"}` + "\n"},
			want:  "pi gave no answer",
		},
		{
			name:    "pi without a state directory",
			q:       Question{Agent: "pi"},
			noState: true,
			want:    "dr has no state directory to keep the session in",
		},
		{
			name:    "omp without a state directory",
			q:       Question{Agent: "omp"},
			noState: true,
			want:    "dr has no state directory to keep the session in",
		},
		{
			name: "the session's directory is gone",
			q:    Question{Agent: "claude", Session: "s1", Cwd: filepath.Join(cwd, "gone")},
			want: "the session's directory " + filepath.Join(cwd, "gone") + " no longer exists",
		},
		{
			name: "an agent dr does not know",
			q:    Question{Agent: "gemini", Session: "s1"},
			want: "dr cannot ask gemini sessions",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeAgents(t, tc.files)
			dirs := fakeDirs(t, dir)
			if tc.noState {
				dirs.State = ""
			}
			if tc.q.Cwd == "" {
				tc.q.Cwd = cwd
			}
			tc.q.Prompt = prompt
			if _, err := Ask(context.Background(), dirs, tc.q); err == nil || err.Error() != tc.want {
				t.Errorf("got error %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAskMissing(t *testing.T) {
	cwd, _ := sessionDir(t)
	t.Setenv("PATH", t.TempDir())
	for _, name := range Names() {
		q := Question{Agent: name, Session: "s1", Log: "/l/s1.jsonl", Cwd: cwd, Prompt: prompt}
		_, err := Ask(context.Background(), Dirs{Home: t.TempDir(), State: t.TempDir()}, q)
		if want := name + " is not installed or not in PATH"; err == nil || err.Error() != want {
			t.Errorf("%s: got error %v, want %q", name, err, want)
		}
	}
}

func TestInstalled(t *testing.T) {
	// An agent is installed when its CLI is in PATH.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(fakeCLI), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, name := range []string{"claude", "codex", "omp", "pi", "gemini"} {
		if got, want := Installed(name), name == "codex"; got != want {
			t.Errorf("Installed(%q): got %t, want %t", name, got, want)
		}
	}
}

func TestAskStop(t *testing.T) {
	// A question cancelled or out of time kills the agent and what it started, and returns
	// soon after. The time is cut short here.
	cwd, _ := sessionDir(t)
	for _, tc := range []struct {
		name, agent string
		timeout     time.Duration
		cancel      bool
		want        error
	}{
		{name: "cancelled", agent: "codex", timeout: time.Minute, cancel: true, want: context.Canceled},
		{name: "out of time", agent: "codex", timeout: 2 * time.Second, want: errNoAnswer},
		{name: "omp cancelled", agent: "omp", timeout: time.Minute, cancel: true, want: context.Canceled},
		{name: "pi cancelled", agent: "pi", timeout: time.Minute, cancel: true, want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := fakeAgents(t, map[string]string{"sleep": ""})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				go func() {
					waitFor(t, filepath.Join(dir, "pid"))
					cancel()
				}()
			}
			start := time.Now()
			q := Question{Agent: tc.agent, Session: "s1", Log: "/l/s1.jsonl", Cwd: cwd, Prompt: prompt}
			_, err := ask(ctx, fakeDirs(t, dir), q, tc.timeout)
			if !errors.Is(err, tc.want) {
				t.Errorf("got error %v, want %v", err, tc.want)
			}
			if took := time.Since(start); took > 10*time.Second {
				t.Errorf("took %v to stop", took)
			}
			for _, name := range []string{"pid", "child"} {
				pid, err := strconv.Atoi(strings.TrimSpace(recorded(t, dir, name)))
				if err != nil {
					t.Fatal(err)
				}
				if !gone(pid) {
					t.Errorf("the fake's %s %d still runs", name, pid)
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		})
	}
}

func TestAskLeftChild(t *testing.T) {
	// A CLI that ends and leaves a child holding its output open does not hold Ask up, and
	// the child does not outlive it.
	cwd, _ := sessionDir(t)
	dir := fakeAgents(t, map[string]string{
		"leave":  "",
		"stdout": `{"is_error":false,"session_id":"f1","result":"Because.","modelUsage":{"claude-opus-5-5":{}}}`,
	})
	start := time.Now()
	got, err := Ask(context.Background(), fakeDirs(t, dir), Question{Agent: "claude", Session: "s1", Cwd: cwd, Prompt: prompt})
	if err != nil || got.Text != "Because." {
		t.Errorf("got %+v, %v; want the answer", got, err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v", took)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(recorded(t, dir, "child")))
	if err != nil {
		t.Fatal(err)
	}
	if !gone(pid) {
		t.Errorf("the child %d the fake left still runs", pid)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// waitFor waits until the file at path exists.
func waitFor(t *testing.T, path string) {
	for range 500 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("%s did not appear", path)
}

// gone reports whether process pid ends within a few seconds. A killed process whose
// parent died too is reaped by init, so it may take a moment to go.
func gone(pid int) bool {
	for range 300 {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
