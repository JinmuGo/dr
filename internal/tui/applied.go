package tui

import (
	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
)

// fixRows tells who wrote or deleted l when a fix dr applied did, wrapped to width: that dr
// applied a fix the session sent for the comment, the comment, what the session said with
// the fix, and the session, the fork and the model that sent it.
func (m *model) fixRows(l origin.Line, width int) []cardRow {
	t := &m.theme
	e := l.Edit
	var deleted []cardRow
	if l.New == 0 {
		deleted = wrapped("This fix deleted the line", chunk{color: t.del}, width)
	}
	return joinGroups(
		deleted,
		append([]cardRow{m.label("FIX", "")}, wrapped("dr applied a fix this session sent for your comment", chunk{color: t.fg}, width)...),
		m.section("YOUR COMMENT", "", e.Fix.Comment, chunk{color: t.fg}, width),
		m.section("NOTE", "", e.Fix.Note, chunk{color: t.muted, italic: true}, width),
		m.sessionRows(e, e.Fix.Fork),
	)
}

// requestText returns what was asked for the edit e: the request of its session, or the
// comment for a fix dr applied.
func requestText(e *agent.Edit) string {
	if e.Fix != nil {
		return e.Fix.Comment
	}
	return e.Prompt
}
