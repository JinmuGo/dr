package tui

import (
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
)

// behindHint is the line b writes under the screen it shows: on the line the terminal's
// cursor is on, which is empty after what Pi, or a shell, left above it.
const behindHint = "\r\x1b[2K\x1b[7m dr: any key comes back \x1b[0m"

// behindMsg tells that the key that ends b's view of the screen under dr came, with the
// error reading it ended with.
type behindMsg struct{ err error }

// behind is what b runs while dr gives up the terminal: Bubble Tea leaves the screen dr
// draws on, so the terminal shows what it showed before dr opened, such as the session of
// the Pi that opened dr. It writes a hint under it and waits for one key, in raw mode so
// that any key, ctrl+c too, comes back without the terminal sending a signal. It erases the hint before dr draws again.
type behind struct {
	in  io.Reader
	out io.Writer
}

func (b *behind) SetStdin(r io.Reader)  { b.in = r }
func (b *behind) SetStdout(w io.Writer) { b.out = w }
func (b *behind) SetStderr(io.Writer)   {}

// behindSettle is how long b waits after the first bytes of the key for the rest of it, an
// escape sequence that comes in more than one read or a paste, before it drops them.
const behindSettle = 30 * time.Millisecond

func (b *behind) Run() error {
	f, ok := b.in.(interface{ Fd() uintptr })
	tty := ok && term.IsTerminal(f.Fd())
	if tty {
		state, err := term.MakeRaw(f.Fd())
		if err != nil {
			return err
		}
		defer func() { _ = term.Restore(f.Fd(), state) }()
	}
	if _, err := io.WriteString(b.out, behindHint); err != nil {
		return err
	}
	var key [64]byte
	_, err := b.in.Read(key[:])
	if err == nil && tty {
		// Nothing of the key that comes back may reach dr as keys of its own.
		time.Sleep(behindSettle)
		err = flushInput(f.Fd())
	}
	_, _ = io.WriteString(b.out, "\r\x1b[2K")
	return err
}

// showBehind returns the command that shows the screen under dr until a key comes.
func (m *model) showBehind() tea.Cmd {
	return tea.Exec(&behind{}, func(err error) tea.Msg { return behindMsg{err: err} })
}
