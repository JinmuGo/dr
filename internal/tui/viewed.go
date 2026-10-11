package tui

import (
	"maps"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// viewedBox is the label of the Viewed box on a file header, after its check box.
const viewedBox = " Viewed"

// savedMsg tells that a save of the viewed files ended, with the error when it failed.
type savedMsg struct{ err error }

// saver writes the viewed files from commands, which can run at the same time. It writes one
// version at a time and none older than the last it wrote, so the newest stays whatever
// order the commands end in.
type saver struct {
	path    string // "" when the screen keeps nothing
	mu      sync.Mutex
	written int // the version last written
}

// save writes v, the version-th change of the viewed files, unless a later one is written.
func (s *saver) save(version int, v state.Viewed) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version <= s.written || s.path == "" {
		return nil
	}
	if err := state.Save(s.path, v); err != nil {
		return err
	}
	s.written = version
	return nil
}

// toggleViewed marks file i viewed and folds it to its header, or marks it not viewed and
// unfolds it, as v does. The cursor goes to its header, which keeps its row. It returns the
// command that saves the change.
func (m *model) toggleViewed(i int) tea.Cmd {
	f := &m.files[i]
	f.viewed, f.changed = !f.viewed, false
	if f.viewed {
		m.viewed[f.key] = f.hash
	} else {
		delete(m.viewed, f.key)
	}
	m.place()
	m.cursor = m.headRow(i)
	m.top = min(m.top, m.cursor) // the top was in the rows that folded
	m.enteredAt = -1             // the split view has other rows now
	return m.saveViewed()
}

// saveViewed returns the command that saves the viewed files as they are now.
func (m *model) saveViewed() tea.Cmd {
	m.version++
	s, version, v := m.saver, m.version, maps.Clone(m.viewed)
	return func() tea.Msg {
		return savedMsg{s.save(version, v)}
	}
}

// saved shows in the status line why a save of the viewed files failed.
func (m *model) saved(msg savedMsg) {
	if msg.err != nil {
		m.warn("could not save viewed state: " + review.Visible(msg.err.Error()))
	}
}
