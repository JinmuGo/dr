package review_test

import (
	"slices"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

var at = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// Edits of a review. c1 is a Claude Code session that started the subagent a1.
var (
	c1      = &agent.Edit{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5", Turn: 1, Time: at}
	c1Later = &agent.Edit{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5", Turn: 2, Time: at.Add(5 * time.Minute)}
	a1      = &agent.Edit{
		Agent: "claude", Session: "a1", Parent: "c1", Log: "/h/c1/subagents/agent-a1.jsonl", Cwd: "/w/sub", Model: "claude-haiku-4-5",
		Turn: 1, Time: at.Add(10 * time.Minute),
	}
	x1     = &agent.Edit{Agent: "codex", Session: "x1", Log: "/h/x1.jsonl", Cwd: "/w", Model: "gpt-6.1-sol", Effort: "medium", Turn: 1, Time: at.Add(2 * time.Minute)}
	x1More = &agent.Edit{Agent: "codex", Session: "x1", Log: "/h/x1.jsonl", Cwd: "/w", Model: "gpt-6.1-sol", Effort: "medium", Turn: 2, Time: at.Add(4 * time.Minute)}
	o1     = &agent.Edit{
		Agent: "omp", Session: "o1", Log: "/h/o1.jsonl", Cwd: "/w", Model: "anthropic/claude-opus-5-5", Effort: "high",
		Turn: 1, Time: at.Add(3 * time.Minute),
	}
	oSub = &agent.Edit{
		Agent: "omp", Session: "o3", Parent: "o1", Log: "/h/o1/0-Task.jsonl", Cwd: "/w", Model: "anthropic/claude-haiku-4-5",
		Turn: 1, Time: at.Add(6 * time.Minute),
	}
	oNoLog = &agent.Edit{Agent: "omp", Session: "o2", Cwd: "/w", Turn: 1, Time: at.Add(time.Minute)}
)

// added and deleted return a line the diff adds or deletes, which e wrote or deleted; nil
// for none known. unchanged is a line the diff leaves alone.
func added(e *agent.Edit) origin.Line   { return origin.Line{Text: "x", New: 1, Edit: e} }
func deleted(e *agent.Edit) origin.Line { return origin.Line{Text: "x", Old: 1, Edit: e} }

var unchanged = origin.Line{Text: "x", Old: 1, New: 1}

// asking returns what Target decides with in a review of lines, where only the agents in
// installed are installed and the targets in busy are answering, as agent and session.
func asking(lines []origin.Line, installed []string, busy ...[2]string) review.Asking {
	return review.Asking{
		Lines:     lines,
		Root:      "/repo",
		Short:     map[string]string{"c1": "c1a0de00", "a1": "a1b2c3d4", "x1": "c0dex000", "o1": "0a1f0001"},
		Installed: func(name string) bool { return slices.Contains(installed, name) },
		Busy:      func(name, session string) bool { return slices.Contains(busy, [2]string{name, session}) },
	}
}

func TestTarget(t *testing.T) {
	all := []string{"claude", "codex", "omp"}
	newSession := func(name string) review.Target {
		return review.Target{Relation: review.NewSession, Ask: agent.Question{Agent: name, Cwd: "/repo"}}
	}
	for _, tc := range []struct {
		name        string
		lines, file []origin.Line // asked about, and of their file
		review      []origin.Line // of every file
		installed   []string
		want        review.Target
		why         string
	}{
		{
			// The author is asked as its newest edit among the lines tells, and knows of its
			// edits in other files.
			name:   "the author",
			lines:  []origin.Line{unchanged, deleted(c1), added(c1)},
			file:   []origin.Line{deleted(c1), added(c1), added(x1)},
			review: []origin.Line{deleted(c1), added(c1), added(x1), added(c1Later)},
			want: review.Target{
				Relation: review.Author, Ask: agent.Question{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5"},
				Base: c1Later.Time, Edit: c1,
			},
		},
		{
			name:   "an OMP subagent, which can be forked",
			lines:  []origin.Line{added(oSub)},
			file:   []origin.Line{added(o1), added(oSub)},
			review: []origin.Line{added(o1), added(oSub)},
			want: review.Target{
				Relation: review.Author,
				Ask:      agent.Question{Agent: "omp", Session: "o3", Log: "/h/o1/0-Task.jsonl", Cwd: "/w", Model: "anthropic/claude-haiku-4-5"},
				Base:     oSub.Time, Edit: oSub,
			},
		},
		{
			// A Claude subagent cannot be resumed. The session that started it is asked as its
			// own newest edit tells, and knows of the subagent's edits.
			name:   "the parent of a Claude subagent",
			lines:  []origin.Line{added(a1)},
			file:   []origin.Line{added(a1)},
			review: []origin.Line{added(c1), added(a1)},
			want: review.Target{
				Relation: review.Parent, Ask: agent.Question{Agent: "claude", Session: "c1", Log: "/h/c1.jsonl", Cwd: "/w", Model: "claude-opus-5-5"},
				Base: a1.Time, Edit: a1,
			},
		},
		{
			name:   "a parent without edits of its own",
			lines:  []origin.Line{added(a1)},
			file:   []origin.Line{added(a1)},
			review: []origin.Line{added(a1)},
			want:   review.Target{Relation: review.Parent, Ask: agent.Question{Agent: "claude", Session: "c1", Cwd: "/w/sub"}, Base: a1.Time, Edit: a1},
		},
		{
			name:   "an author dr cannot fork that has no parent",
			lines:  []origin.Line{added(oNoLog)},
			file:   []origin.Line{added(oNoLog)},
			review: []origin.Line{added(oNoLog), added(x1)},
			want:   review.Target{Relation: review.NewSession, Ask: agent.Question{Agent: "codex", Cwd: "/repo"}, Edit: oNoLog},
		},
		{
			// Lines without an origin go to the session behind the newest edit of the file.
			name:   "the session that edited the file last",
			lines:  []origin.Line{added(nil), unchanged, added(nil)},
			file:   []origin.Line{added(c1), added(nil), unchanged, added(nil), deleted(o1)},
			review: []origin.Line{added(c1), added(nil), unchanged, added(nil), deleted(o1), added(c1Later)},
			want: review.Target{
				Relation: review.Editor,
				Ask:      agent.Question{Agent: "omp", Session: "o1", Log: "/h/o1.jsonl", Cwd: "/w", Model: "anthropic/claude-opus-5-5", Effort: "high"},
				Base:     o1.Time,
			},
		},
		{
			name:   "a file a Claude subagent edited last",
			lines:  []origin.Line{deleted(nil)},
			file:   []origin.Line{added(c1), added(a1), deleted(nil)},
			review: []origin.Line{added(c1), added(a1), deleted(nil), added(x1)},
			want:   newSession("claude"),
		},
		{
			name:   "a new session of the agent behind the most edits",
			lines:  []origin.Line{added(nil)},
			file:   []origin.Line{added(nil)},
			review: []origin.Line{added(nil), added(x1), added(o1), added(x1More)},
			want:   newSession("codex"),
		},
		{
			name:   "the lines of an edit count once",
			lines:  []origin.Line{added(nil)},
			file:   []origin.Line{added(nil)},
			review: []origin.Line{added(nil), added(c1), added(c1), added(c1), added(x1), added(x1More)},
			want:   newSession("codex"),
		},
		{
			name:   "a tie goes to the agent with the newest edit",
			lines:  []origin.Line{added(nil)},
			file:   []origin.Line{added(nil)},
			review: []origin.Line{added(nil), added(o1), added(c1)},
			want:   newSession("omp"),
		},
		{
			name:      "an agent not installed",
			lines:     []origin.Line{added(nil)},
			file:      []origin.Line{added(nil)},
			review:    []origin.Line{added(nil), added(c1), added(c1Later), added(x1)},
			installed: []string{"omp", "codex"},
			want:      newSession("codex"),
		},
		{
			name:      "no edits in the review",
			lines:     []origin.Line{added(nil)},
			file:      []origin.Line{added(nil)},
			review:    []origin.Line{added(nil)},
			installed: []string{"omp", "codex"},
			want:      newSession("codex"),
		},
		{
			name:      "no agent installed",
			lines:     []origin.Line{added(nil)},
			file:      []origin.Line{added(nil)},
			review:    []origin.Line{added(nil), added(c1)},
			installed: []string{},
			why:       "No agent CLI is installed to ask (claude, codex or omp)",
		},
		{
			name:   "two sessions",
			lines:  []origin.Line{added(c1), unchanged, added(x1)},
			file:   []origin.Line{added(c1), unchanged, added(x1)},
			review: []origin.Line{added(c1), unchanged, added(x1)},
			why:    "The selection mixes sessions. Select lines from one session",
		},
		{
			name:   "known and unknown lines",
			lines:  []origin.Line{added(nil), deleted(c1)},
			file:   []origin.Line{added(nil), deleted(c1)},
			review: []origin.Line{added(nil), deleted(c1)},
			why:    "Some selected lines have no origin",
		},
		{
			name:   "unchanged lines",
			lines:  []origin.Line{unchanged, unchanged},
			file:   []origin.Line{unchanged, unchanged, added(c1)},
			review: []origin.Line{unchanged, unchanged, added(c1)},
			why:    "The selection has no changed lines",
		},
	} {
		installed := tc.installed
		if installed == nil {
			installed = all
		}
		got, why := asking(tc.review, installed).Target(tc.lines, tc.file)
		if got != tc.want || why != tc.why {
			t.Errorf("%s:\n got %+v, %q\nwant %+v, %q", tc.name, got, why, tc.want, tc.why)
		}
	}
}

func TestTargetBusy(t *testing.T) {
	// A session takes one question at a time, whatever it is asked as, and so does the new
	// session of each agent. Other sessions can be asked meanwhile.
	lines := []origin.Line{added(c1), added(x1), added(nil)}
	unknown := []origin.Line{added(nil)}
	for _, tc := range []struct {
		name        string
		lines, file []origin.Line
		installed   []string
		why         string
	}{
		{name: "the busy author", lines: []origin.Line{added(c1)}, file: lines, why: "Still waiting for claude c1a0de00 to answer"},
		{name: "the busy session as the editor", lines: unknown, file: []origin.Line{added(c1), added(nil)}, why: "Still waiting for claude c1a0de00 to answer"},
		{name: "another session", lines: []origin.Line{added(x1)}, file: lines},
		{name: "the busy new session", lines: unknown, file: unknown, installed: []string{"omp"}, why: "Still waiting for omp in a new session to answer"},
		{name: "a new session of another agent", lines: unknown, file: unknown, installed: []string{"codex"}},
	} {
		installed := tc.installed
		if installed == nil {
			installed = []string{"claude", "codex", "omp"}
		}
		a := asking(lines, installed, [2]string{"claude", "c1"}, [2]string{"omp", ""})
		if _, why := a.Target(tc.lines, tc.file); why != tc.why {
			t.Errorf("%s: got %q, want %q", tc.name, why, tc.why)
		}
	}
}

func TestWho(t *testing.T) {
	short := map[string]string{"c1": "c1a0de00"}
	for _, tc := range []struct{ agent, session, want string }{
		{"claude", "c1", "claude c1a0de00"},
		{"omp", "", "omp in a new session"},
		{"codex", "x\x1b1", `codex x\x1b1`}, // not shortened, made visible
	} {
		if got := review.Who(tc.agent, tc.session, short); got != tc.want {
			t.Errorf("Who(%q, %q): got %q, want %q", tc.agent, tc.session, got, tc.want)
		}
	}
}
