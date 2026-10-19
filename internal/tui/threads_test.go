package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// panelText returns the threads panel of the screen without its colors, a line each.
func panelText(t *testing.T, m *model) string {
	t.Helper()
	list, _ := m.layout()
	var lines []string
	for _, l := range strings.Split(screen(t, m), "\n")[:m.diffHeight()] {
		lines = append(lines, strings.TrimRight(ansi.Cut(ansi.Strip(l), 0, list), " "))
	}
	return strings.Join(lines, "\n")
}

func TestThreads(t *testing.T) {
	// t shows the threads in place of the file tree and gives them the keys: a question
	// waiting for its answer, then the answer that came while the origin box did not show
	// it, first, above a comment :submit sends. Enter goes to the answer and reads it.
	m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{answer: answer})
	typeKeys(m, "gg24jcuse errors.Is<enter>")
	cmd := typeAndKeep(m, "gg4ja<enter>")
	typeKeys(m, "<esc>gg24jt")
	if m.mode() != modeThreads || !m.threads.shown {
		t.Fatalf("after t: mode %v, panel shown %t; want THREADS", m.mode(), m.threads.shown)
	}
	threads := m.threadList()
	if len(threads) != 2 || threads[0].rank != threadWaiting || threads[1].rank != threadDraft || m.threads.sel != threads[1].id {
		t.Fatalf("threads %+v, selected %q; want the waiting question, then the comment under the cursor selected", threads, m.threads.sel)
	}
	if s := panelText(t, m); !strings.Contains(s, "Threads") || !strings.Contains(s, "a.go:2") || !strings.Contains(s, "use errors.Is") || strings.Contains(s, "Files") {
		t.Errorf("the panel shows no threads in place of the file tree:\n%s", s)
	}

	send(m, cmd())
	threads = m.threadList()
	if threads[0].rank != threadNew || !threads[0].newest().unread || m.originBox.open {
		t.Fatalf("after the answer: first thread rank %d, unread %t, box open %t; want an unread answer first", threads[0].rank, threads[0].newest().unread, m.originBox.open)
	}
	if s := panelText(t, m); !strings.Contains(s, "1 new") || !strings.Contains(s, "◉") {
		t.Errorf("the panel does not tell of the new answer:\n%s", s)
	}

	typeKeys(m, "k<enter>")
	if m.threads.focus || !m.threads.shown || !m.originBox.open || m.cursor != 4 || threads[0].newest().unread {
		t.Errorf("after Enter: focus %t, shown %t, box open %t, cursor %d, unread %t; want the answer read on row 4 with the panel kept",
			m.threads.focus, m.threads.shown, m.originBox.open, m.cursor, threads[0].newest().unread)
	}
	if !strings.Contains(boxText(m), "토큰 버킷이라") {
		t.Errorf("the box does not show the answer:\n%s", boxText(m))
	}

	// t gives the keys back to the panel, Esc gives them to the diff, and t in the panel
	// shows the file tree again.
	typeKeys(m, "<esc>t")
	if m.mode() != modeThreads {
		t.Errorf("t with the panel shown: mode %v, want THREADS", m.mode())
	}
	typeKeys(m, "<esc>")
	if m.mode() != modeNormal || !m.threads.shown {
		t.Errorf("Esc in the panel: mode %v, shown %t; want NORMAL with the panel kept", m.mode(), m.threads.shown)
	}
	typeKeys(m, "tt")
	if m.threads.shown || m.mode() != modeNormal || !strings.Contains(panelText(t, m), "Files") {
		t.Errorf("t in the panel: shown %t, mode %v; want the file tree back", m.threads.shown, m.mode())
	}
}

func TestThreadsNarrow(t *testing.T) {
	// A screen too narrow for the file tree beside the diff has no room for the threads.
	m := askScreen(testFiles(), Saved{}, &fakeAsk{})
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	typeKeys(m, "t")
	if m.threads.shown || m.mode() != modeNormal || !strings.Contains(m.message, "Widen the screen") {
		t.Errorf("t on a narrow screen: shown %t, mode %v, message %q", m.threads.shown, m.mode(), m.message)
	}
}

func TestThreadsUnreadOutOfView(t *testing.T) {
	// An answer that comes while the origin box shows an earlier answer on the same line stays
	// new until the box scrolls to it.
	long := answer
	long.Text = strings.Repeat("이유가 길어요.\n", 30)
	m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{answer: long})
	typeKeys(m, "gg4ja<enter>")
	cmd := typeAndKeep(m, "a<enter>")
	typeKeys(m, "gg")
	send(m, cmd())
	qs := m.questionsAt(m.cursor)
	if len(qs) != 2 || qs[0].unread || !qs[1].unread {
		t.Fatalf("with the box at the top: unread %t, %t; want only the follow-up new", qs[0].unread, len(qs) > 1 && qs[1].unread)
	}
	typeKeys(m, "G")
	if qs[1].unread {
		t.Error("the follow-up stays new after the box scrolled to its answer")
	}
}

func TestThreadsLongQuestion(t *testing.T) {
	// An answer under a question longer than the origin box stays new while the box shows
	// only the question.
	m := askScreen(testFiles(), Saved{Dir: t.TempDir()}, &fakeAsk{answer: answer})
	cmd := typeAndKeep(m, "gg4ja"+strings.Repeat("why this way ", 150)+"<enter>")
	typeKeys(m, "ggjjj") // into the question's text
	send(m, cmd())
	q := m.questionsAt(m.cursor)[0]
	if !q.unread {
		t.Fatal("the answer is read while the box shows only the question")
	}
	typeKeys(m, "G")
	if q.unread {
		t.Error("the answer stays new after the box scrolled to it")
	}
}

func TestThreadsEarlierComment(t *testing.T) {
	// A comment of an earlier run whose lines changed is listed, read, and Enter opens the
	// box of its file's header, where it shows.
	dir := t.TempDir()
	m := commentScreen(testFiles(), dir)
	typeKeys(m, "gg4jcone<enter>")
	files := testFiles()
	for i, l := range files[0].Lines {
		if l.Text == "Changed 2" {
			files[0].Lines[i].Text = "Changed two"
		}
	}
	m = commentScreen(files, dir)
	threads := m.threadList()
	if len(threads) != 1 || threads[0].c == nil || threads[0].c.placed() || threads[0].rank != threadRead {
		t.Fatalf("threads %+v, want the earlier comment, read", threads)
	}
	typeKeys(m, "Gt<enter>")
	if !m.originBox.open || m.rows[m.cursor].kind != fileRow || !strings.Contains(boxText(m), "EARLIER COMMENTS · 1") {
		t.Errorf("Enter on the earlier comment: box open %t on row %d:\n%s", m.originBox.open, m.cursor, boxText(m))
	}
}
