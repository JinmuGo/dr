package tui

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
)

// fakeOpen stands for agent.Open: it keeps the questions it gets and returns a command that
// names the agent and the session, run in the session's directory, or fails with err.
type fakeOpen struct {
	questions []agent.Question
	err       error
}

func (f *fakeOpen) open(q agent.Question) (*exec.Cmd, error) {
	f.questions = append(f.questions, q)
	if f.err != nil {
		return nil, f.err
	}
	return &exec.Cmd{Path: q.Agent, Args: []string{q.Agent, "fork", q.Session}, Dir: q.Cwd}, nil
}

// openScreen returns the screen on files in a 125×24 terminal with the agents in installed
// installed, all of them when it is nil, that runs the editor and the agents through ed and
// opens sessions through op.
func openScreen(files []review.File, saved Saved, installed []string, ed *fakeEditor, op *fakeOpen) *model {
	m := askScreen(files, saved, &fakeAsk{answer: answer, installed: installed})
	m.execute, m.open = ed.execute, op.open
	return m
}

// ran returns the arguments and the directory of the commands ed ran.
func ran(ed *fakeEditor) [][]string {
	var got [][]string
	for _, c := range ed.cmds {
		got = append(got, append(slices.Clone(c.Args), "in "+c.Dir))
	}
	return got
}

func TestOpenSession(t *testing.T) {
	// ga on a line a session wrote opens a fork of that session in its agent, in the
	// session's directory with its model. When the agent exits, the diff is rebuilt with what
	// the fork changed, and the cursor stays on its line.
	ed, op := &fakeEditor{}, &fakeOpen{}
	m := openScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, nil, ed, op)
	typeKeys(m, "4jga")
	want := agent.Question{Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5"}
	if len(op.questions) != 1 || op.questions[0] != want {
		t.Errorf("opened %+v, want %+v", op.questions, want)
	}
	if got := ran(ed); !slices.EqualFunc(got, [][]string{{"claude", "fork", claudeSession, "in /work"}}, slices.Equal) {
		t.Errorf("ran %q, want the fork of claude %s in /work", got, claudeSession)
	}
	if findRow(m, "+Line 5 edited") < 0 || rowName(m, m.cursor) != "+Changed 2" || m.message != "" {
		t.Errorf("rebuilt %t with the cursor on %q and %q in the status line; want rebuilt, the cursor kept and no message",
			findRow(m, "+Line 5 edited") >= 0, rowName(m, m.cursor), m.message)
	}
}

func TestOpenSessionOthers(t *testing.T) {
	// ga opens the session a would ask: the parent of a Claude subagent, and for lines
	// without an origin the session that last edited the file.
	for _, tc := range []struct {
		name, keys string
		want       agent.Question
	}{
		{
			name: "the parent of a Claude subagent", keys: "32G",
			want: agent.Question{Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5"},
		},
		{
			name: "the session that edited the file last", keys: "16j",
			want: agent.Question{Agent: "omp", Session: ompSession, Log: subagent.Log, Cwd: "/work"},
		},
		{
			name: "a deleted line the editor of the file did not write", keys: "34G",
			want: agent.Question{Agent: "codex", Session: "c0dex000-3333", Cwd: "/work", Model: "gpt-6.1-sol", Effort: "medium"},
		},
	} {
		ed, op := &fakeEditor{}, &fakeOpen{}
		m := openScreen(askFiles(), Saved{}, nil, ed, op)
		typeKeys(m, tc.keys+"ga")
		if len(op.questions) != 1 || op.questions[0] != tc.want || len(ed.cmds) != 1 {
			t.Errorf("%s: opened %+v and ran %q, want %+v run", tc.name, op.questions, ran(ed), tc.want)
		}
	}
}

func TestOpenSessionRefusals(t *testing.T) {
	// ga runs nothing for lines no session wrote or edited, nor for lines a cannot ask
	// about, and says why as a does, nor when the agent cannot open the session. The
	// selection stays.
	for _, tc := range []struct {
		keys, want string
		installed  []string // the agents installed, all when nil
		openErr    error
	}{
		{keys: "40Gga", want: noSession},
		{keys: "39GVjga", want: noSession},
		{keys: "ga", want: "Move to a changed line to ask"},
		{keys: "2jga", want: "Move to a changed line to ask"},
		{keys: "12jV4jga", want: "Some selected lines have no origin"},
		{keys: "3jV9jga", want: "The selection mixes sessions. Select lines from one session"},
		{keys: "40Gga", want: "No agent CLI is installed to ask (claude, codex or omp)", installed: []string{}},
		{keys: "4jga", want: "claude is not installed or not in PATH", openErr: errors.New("claude is not installed or not in PATH")},
	} {
		ed := &fakeEditor{}
		m := openScreen(askFiles(), Saved{}, tc.installed, ed, &fakeOpen{err: tc.openErr})
		typeKeys(m, tc.keys)
		if m.message != tc.want || m.tone == toneOK || len(ed.cmds) != 0 {
			t.Errorf("%q: message %q, tone %d, ran %q; want the warning %q and nothing run", tc.keys, m.message, m.tone, ran(ed), tc.want)
		}
		if selecting := strings.Contains(tc.keys, "V"); m.selecting != selecting {
			t.Errorf("%q: selecting %t, want %t", tc.keys, m.selecting, selecting)
		}
	}
}

func TestOpenSessionExitError(t *testing.T) {
	// An agent that exits with an error still has the diff rebuilt, and the status line
	// tells of the error.
	ed := &fakeEditor{err: errors.New("exit status 1")}
	m := openScreen(testFiles(), Saved{Reload: reloading(editedFiles())}, nil, ed, &fakeOpen{})
	typeKeys(m, "4jga")
	if want := "claude exited: exit status 1"; m.message != want || m.tone == toneOK || findRow(m, "+Line 5 edited") < 0 || rowName(m, m.cursor) != "+Changed 2" {
		t.Errorf("message %q, rebuilt %t, the cursor on %q; want the warning %q, rebuilt, the cursor kept",
			m.message, findRow(m, "+Line 5 edited") >= 0, rowName(m, m.cursor), want)
	}
}

func TestOpenSessionBox(t *testing.T) {
	// ga in the open origin box opens the session of the lines its newest question asked
	// about, though that session is still answering, as the fork is a new one. The origin
	// box closes, as a selection ends, when the agent starts.
	for _, tc := range []struct {
		name string
		keys string // before ga, the last of which may leave a question in flight
		want agent.Question
	}{
		{
			name: "the box with a question in flight", keys: "4ja<enter>",
			want: agent.Question{Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5"},
		},
		{
			name: "the box of a line", keys: "34G<enter>",
			want: agent.Question{Agent: "codex", Session: "c0dex000-3333", Cwd: "/work", Model: "gpt-6.1-sol", Effort: "medium"},
		},
		{
			name: "a selection", keys: "32GV",
			want: agent.Question{Agent: "claude", Session: claudeSession, Cwd: "/work", Model: "claude-opus-5-5"},
		},
	} {
		ed, op := &fakeEditor{}, &fakeOpen{}
		m := openScreen(askFiles(), Saved{Reload: reloading(askFiles())}, nil, ed, op)
		question := typeAndKeep(m, tc.keys)
		if !m.reading && !m.selecting {
			t.Fatalf("%s: neither the origin box open nor a selection before ga", tc.name)
		}
		opened := typeAndKeep(m, "ga")
		if len(op.questions) != 1 || op.questions[0] != tc.want || m.reading || m.selecting {
			t.Errorf("%s: opened %+v, origin box open %t, selecting %t; want %+v opened, the box closed, no selection",
				tc.name, op.questions, m.reading, m.selecting, tc.want)
		}
		if opened == nil {
			t.Fatalf("%s: ga gave no command", tc.name)
		}
		send(m, opened())
		if len(ed.cmds) != 1 || m.message != "" {
			t.Errorf("%s: ran %q with %q in the status line; want the agent run once and no message", tc.name, ran(ed), m.message)
		}
		if question != nil {
			send(m, question())
		}
	}
}

func TestOpenSessionEmpty(t *testing.T) {
	// ga does nothing when no changed file is left.
	ed, op := &fakeEditor{}, &fakeOpen{}
	m := openScreen(testFiles(), Saved{Reload: reloading(nil)}, nil, ed, op)
	typeKeys(m, "4je")
	if len(m.files) != 0 {
		t.Fatalf("%d files after the rebuild, want none", len(m.files))
	}
	typeKeys(m, "<esc>") // which clears the status line
	empty := screen(t, m)
	for _, k := range keyPresses("ga") {
		if _, cmd := m.Update(k); cmd != nil {
			t.Errorf("%s: got a command, want none", k)
		}
	}
	if len(op.questions) != 0 || len(ed.cmds) != 1 || screen(t, m) != empty {
		t.Errorf("opened %+v, ran %q, screen\n%s\nwant nothing opened or changed", op.questions, ran(ed), screen(t, m))
	}
}
