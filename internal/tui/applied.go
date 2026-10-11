package tui

import (
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
)

// fixText tells who wrote or deleted l when a fix dr applied did: the session the comment
// went to, that dr applied a fix it sent for the comment, the comment, what the session
// said with the fix, and the fork and the model that sent it.
func (m *model) fixText(l origin.Line) []paragraph {
	e := l.Edit
	paragraphs := []paragraph{
		{text: "Session", tone: heading},
		{text: review.Tag(e, m.short), tone: strong},
		{text: review.Visible(e.Session), tone: quiet},
	}
	if l.New == 0 {
		paragraphs = append(paragraphs, paragraph{text: "This fix deleted the line", tone: removal})
	}
	paragraphs = append(paragraphs, paragraph{}, paragraph{text: "dr applied a fix this session sent for your comment"})
	paragraphs = append(paragraphs, section("Comment", e.Fix.Comment)...)
	paragraphs = append(paragraphs, section("Note", e.Fix.Note)...)
	var sent []string
	if e.Fix.Fork != "" {
		sent = append(sent, "fork "+e.Fix.Fork)
	}
	if e.Model != "" {
		sent = append(sent, modelName(e.Model))
	}
	return append(paragraphs, section("Sent by", strings.Join(sent, " · "))...)
}

// requestText returns what was asked for the edit e: the request of its session, or the
// comment for a fix dr applied.
func requestText(e *agent.Edit) string {
	if e.Fix != nil {
		return e.Fix.Comment
	}
	return e.Prompt
}
