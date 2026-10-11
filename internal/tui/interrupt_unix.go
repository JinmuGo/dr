//go:build unix

package tui

import (
	"os"
	"syscall"
)

// killedByInterrupt tells whether the process that ended with state was killed by SIGINT.
func killedByInterrupt(state *os.ProcessState) bool {
	status, ok := state.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == syscall.SIGINT
}
