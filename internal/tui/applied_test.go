package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/review"
)

// fixFiles returns a file whose -old and +new a fix dr applied for a comment to the Claude
// Code session deleted and wrote, after the session wrote +more. The rows are its header,
// a hunk header, x, -old, +new, +more and y.
func fixFiles() (files []review.File, fix *agent.Edit) {
	fix = &agent.Edit{
		Agent: "claude", Session: claudeSession, Time: start.Add(time.Hour), Cwd: "/work", Model: "anthropic/claude-sonnet-5",
		Fix: &agent.Fix{ID: "00000000000000f1", Comment: "use errors.Is\nand wrap it", Note: "the caller checks it too", Fork: "fork-7"},
	}
	lines := parseDiff("x", "-old", "+new", "+more", "y")
	lines[1].Edit, lines[2].Edit, lines[3].Edit = fix, fix, claude1
	return []review.File{{Path: "a.go", Status: 'M', Lines: lines}}, fix
}

func TestFixOrigin(t *testing.T) {
	files, _ := fixFiles()
	for _, tc := range []struct {
		name, keys string
		want       []string // in the card, in this order
		not        []string
	}{
		{
			// The tag says fix in place of the request, and the card tells the comment, the
			// note, then the session, the fork and the model.
			name: "a fixed line", keys: "4j",
			want: []string{"◆ claude c1a0 fix", "FIX\n", "YOUR COMMENT\nuse errors.Is\nand wrap it", "NOTE\nthe caller checks it too", "session " + claudeSession + " · fork fork-7 · claude-sonnet-5"},
			not:  []string{"#"},
		},
		{
			name: "a line a fix deleted", keys: "3j",
			want: []string{"claude c1a0 fix", "use errors.Is"},
		},
		{
			// The comment stands for the request of the fix.
			name: "the file header",
			want: []string{"claude c1a0 #1", "rate limiter를 짜줘", "claude c1a0 fix", "use errors.Is"},
			not:  []string{"and wrap it"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := end(t, openFiles(t, files, tc.keys))
			text := cardText(m)
			at := 0
			for _, s := range tc.want {
				i := strings.Index(text[at:], s)
				if i < 0 {
					t.Fatalf("origin %q has no %q after its first %d bytes", text, s, at)
				}
				at += i + len(s)
			}
			for _, s := range tc.not {
				if strings.Contains(text, s) {
					t.Errorf("origin %q has %q", text, s)
				}
			}
		})
	}
	t.Run("the status line", func(t *testing.T) {
		m := end(t, openFiles(t, files, "4j"))
		if status := statusLine(t, m); !strings.Contains(status, "claude c1a0 fix") || strings.Contains(status, "#") {
			t.Errorf("status line %q, want the fix tag", status)
		}
	})
}

func TestFixAsk(t *testing.T) {
	files, fix := fixFiles()
	// a on a fixed line asks the session as its author and names the fix.
	m := end(t, openFiles(t, files, "4j"))
	target, why := m.target(m.whom)
	if why != "" || target.Relation != review.Author || target.Edit != fix || target.tag != "claude c1a0 fix" {
		t.Errorf("got %+v, %q; want the author asked about the fix", target, why)
	}
	// A fix is a request apart from the session's own.
	m = end(t, openFiles(t, files, "4jVj"))
	if target, why := m.target(m.whom); why != "" || target.tag != "claude c1a0" {
		t.Errorf("a fixed line and one of the session's: got %+v, %q; want the session named without a request", target, why)
	}
	// The session's newest edit is its own, not the fix the fork sent, so a question to it
	// keeps the model of its own edits.
	if got := newestEdits(m.files)[session{"claude", claudeSession}]; got != claude1 {
		t.Errorf("newest edit of the session: got %+v, want its own", got)
	}
}
