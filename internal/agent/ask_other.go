//go:build !unix

package agent

import "os/exec"

// ownGroup leaves cmd's cancel as exec.CommandContext sets it: it kills the CLI.
func ownGroup(*exec.Cmd) {}

// killGroup does nothing: without process groups there is no group to kill.
func killGroup(*exec.Cmd) {}
