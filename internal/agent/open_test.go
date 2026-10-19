//go:build unix

package agent

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestOpen(t *testing.T) {
	// Open forks the session in the agent's CLI, in the session's directory, with the
	// session's model and effort when they are known, and leaves the terminal to the caller.
	cwd, _ := sessionDir(t)
	fakeAgents(t, nil)
	const log = "/home/.omp/agent/sessions/-work/o1.jsonl"
	for _, tc := range []struct {
		name string
		q    Question
		want []string
	}{
		{
			name: "claude with a model",
			q:    Question{Agent: "claude", Session: "c1", Cwd: cwd, Model: "claude-opus-5-5", Effort: "high"},
			want: []string{"--resume", "c1", "--fork-session", "--model", "claude-opus-5-5"},
		},
		{
			name: "claude without a model",
			q:    Question{Agent: "claude", Session: "c1", Cwd: cwd},
			want: []string{"--resume", "c1", "--fork-session"},
		},
		{
			name: "codex with a model and an effort",
			q:    Question{Agent: "codex", Session: "t1", Cwd: cwd, Model: "gpt-6.1-sol", Effort: "medium"},
			want: []string{"fork", "-c", `model="gpt-6.1-sol"`, "-c", `model_reasoning_effort="medium"`, "t1"},
		},
		{
			name: "codex without a model or an effort",
			q:    Question{Agent: "codex", Session: "t1", Cwd: cwd},
			want: []string{"fork", "t1"},
		},
		{
			name: "omp with a model and an effort",
			q:    Question{Agent: "omp", Session: "o1", Log: log, Cwd: cwd, Model: "anthropic/claude-opus-5-5", Effort: "high"},
			want: []string{"--fork", log, "--model", "anthropic/claude-opus-5-5", "--thinking", "high"},
		},
		{
			name: "omp without a model or an effort",
			q:    Question{Agent: "omp", Session: "o1", Log: log, Cwd: cwd},
			want: []string{"--fork", log},
		},
		{
			name: "pi with a model and an effort",
			q:    Question{Agent: "pi", Session: "p1", Log: "/home/.pi/agent/sessions/--work--/p1.jsonl", Cwd: cwd, Model: "openai/gpt-6-astra", Effort: "high"},
			want: []string{"--fork", "/home/.pi/agent/sessions/--work--/p1.jsonl", "--model", "openai/gpt-6-astra", "--thinking", "high"},
		},
	} {
		cmd, err := Open(tc.q)
		if err != nil {
			t.Errorf("%s: got error %v", tc.name, err)
			continue
		}
		if filepath.Base(cmd.Path) != tc.q.Agent || !slices.Equal(cmd.Args[1:], tc.want) || cmd.Dir != cwd {
			t.Errorf("%s: runs %s %q in %s, want %s %q in %s", tc.name, cmd.Path, cmd.Args[1:], cmd.Dir, tc.q.Agent, tc.want, cwd)
		}
		if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
			t.Errorf("%s: the command has its own stdin, stdout or stderr, want the terminal left to the caller", tc.name)
		}
	}
}

func TestOpenFailure(t *testing.T) {
	// Open says why there is nothing to open: dr does not know the agent, the session's
	// directory is not there, there is no session to fork, or the CLI is not installed.
	cwd, _ := sessionDir(t)
	fakeAgents(t, nil)
	for _, tc := range []struct {
		name string
		q    Question
		want string
	}{
		{"an unknown agent", Question{Agent: "gemini", Session: "g1", Cwd: cwd}, "dr cannot ask gemini sessions"},
		{"no directory", Question{Agent: "claude", Session: "c1"}, "the session's log names no directory"},
		{
			"the directory is gone", Question{Agent: "codex", Session: "t1", Cwd: filepath.Join(cwd, "gone")},
			"the session's directory " + filepath.Join(cwd, "gone") + " no longer exists",
		},
		{"no session", Question{Agent: "codex", Cwd: cwd}, "dr has no session of codex to open for these lines"},
	} {
		if cmd, err := Open(tc.q); err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %v, error %v; want %q", tc.name, cmd, err, tc.want)
		}
	}

	t.Setenv("PATH", t.TempDir())
	for _, name := range Names() {
		q := Question{Agent: name, Session: "s1", Log: "/l/s1.jsonl", Cwd: cwd}
		if cmd, err := Open(q); err == nil || err.Error() != name+" is not installed or not in PATH" {
			t.Errorf("%s not in PATH: got %v, error %v; want it not installed", name, cmd, err)
		}
	}
}
