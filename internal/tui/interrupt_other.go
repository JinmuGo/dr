//go:build !unix

package tui

import "os"

// killedByInterrupt reports false: off unix only exit status 130 tells of an interrupt.
func killedByInterrupt(*os.ProcessState) bool {
	return false
}
