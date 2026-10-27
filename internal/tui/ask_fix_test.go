package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/agent"
)

func TestAskToComment(t *testing.T) {
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "2jVjjaWhy both?<enter>")
	q := m.questions[len(m.questions)-1]
	typeKeys(m, "c")
	if m.drafting == nil || m.originBox.open {
		t.Fatal("c in the ask thread did not open a comment draft")
	}
	if !slices.Equal(m.drafting.lines, q.lines) {
		t.Fatalf("comment lines %v, want question lines %v", m.drafting.lines, q.lines)
	}
	if !strings.Contains(m.entry.text, "Why both?") || !strings.Contains(m.entry.text, answer.Text) {
		t.Fatalf("draft omitted the discussion: %q", m.entry.text)
	}
	if len(m.comments) != 0 {
		t.Fatal("draft saved before confirmation")
	}
	typeKeys(m, "<enter>")
	if len(m.comments) != 1 || !slices.Equal(m.comments[0].picked(), q.lines) {
		t.Fatal("saved comment did not preserve the question range")
	}
	if len(m.pendingComments()) != 1 {
		t.Fatal("comment is not ready for :submit")
	}
	if got := m.fork(session{q.Agent, q.Session}, newestEdits(m.files)[session{q.Agent, q.Session}].Time); got != "fork-1" {
		t.Fatalf("fix fork = %q, want fork-1", got)
	}
}

func TestAskToFixApplyAndUndo(t *testing.T) {
	repo, home := codexRepo(t)
	f := &fakeAsk{answer: answer}
	m, _ := repoScreen(t, repo, home, f, fixClock())
	path := filepath.Join(repo, "codex/ratelimit.go")
	before := readFile(t, path)
	typeAll(m, "14jaExplain this field<enter>c<enter>")
	if len(m.comments) != 1 {
		t.Fatal("missing comment from ask")
	}
	f.answer = agent.Answer{Fork: "fork-1", Text: `{"fixes":[{"comment":1,"new_lines":["\t// Tokens per second.","\trate        float64"]}]}`}
	typeAll(m, ":submit<enter>")
	asked := f.asked()
	if len(asked) != 2 || asked[1].Fork != "fork-1" || !strings.Contains(asked[1].Prompt, "Explain this field") || !strings.Contains(asked[1].Prompt, answer.Text) {
		t.Fatalf("fix request did not continue the discussion: %+v", asked)
	}
	if readFile(t, path) != before {
		t.Fatal("file changed before y")
	}
	if m.comments[0].fix == nil || m.comments[0].fix.Error != "" {
		t.Fatalf("fix not ready: %+v", m.comments[0].fix)
	}
	typeAll(m, "y")
	if !strings.Contains(readFile(t, path), "// Tokens per second.") {
		t.Fatal("y did not apply the fix")
	}
	typeAll(m, ":undo<enter>")
	if readFile(t, path) != before {
		t.Fatal("undo did not restore the file")
	}
}

func TestAskToCommentPending(t *testing.T) {
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	_ = typeAndKeep(m, "4ja<enter>")
	typeKeys(m, "c")
	if m.drafting != nil || !m.originBox.open || len(m.comments) != 0 {
		t.Fatal("pending answer allowed a fix draft or closed the thread")
	}
}

func TestAskToCommentKeepsExisting(t *testing.T) {
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "4jcKeep this instruction<enter>a<enter>c")
	if m.drafting == nil || m.drafting.editing == nil || m.entry.text != "Keep this instruction" {
		t.Fatal("ask conversion replaced an existing comment")
	}
}

func TestAskToCommentCancel(t *testing.T) {
	m := askScreen(testFiles(), Saved{}, &fakeAsk{answer: answer})
	typeKeys(m, "4ja<enter>c<esc>")
	if m.drafting != nil || len(m.comments) != 0 {
		t.Fatal("cancelling the draft left a comment")
	}
}
