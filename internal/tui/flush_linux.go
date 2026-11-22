package tui

import "golang.org/x/sys/unix"

// flushInput drops what the terminal at fd has read and dr has not, as tcflush(TCIFLUSH)
// does.
func flushInput(fd uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH)
}
