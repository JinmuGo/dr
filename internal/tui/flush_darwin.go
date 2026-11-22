package tui

import "golang.org/x/sys/unix"

// flushInput drops what the terminal at fd has read and dr has not, as tcflush(TCIFLUSH)
// does: TIOCFLUSH with FREAD, which is 1.
func flushInput(fd uintptr) error {
	return unix.IoctlSetPointerInt(int(fd), unix.TIOCFLUSH, 1)
}
