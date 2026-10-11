//go:build unix

package agent

import (
	"os/exec"
	"syscall"
)

// ownGroup runs cmd in a process group of its own and makes its cancel kill the whole
// group, so the tools the CLI started end with it.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// killGroup kills what is left of the process group of cmd, which ownGroup made, once the
// CLI ended: processes it started and did not wait for.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // ESRCH when nothing is left
	}
}
