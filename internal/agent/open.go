package agent

import (
	"errors"
	"fmt"
	"os/exec"
)

// Open returns the command that opens a fork of the session q.Session in the CLI of the
// agent q names, run in q.Cwd, for the person to go on working in it, with the session's
// model q.Model and reasoning effort q.Effort when they are known. Unlike a question, the
// fork is the person's own working session: the CLI runs as they set it up, with all its
// tools, their config and hooks, and keeps the fork with their other sessions, where dr
// finds its edits. Open sets no Stdin, Stdout or Stderr, so that the caller can hand the CLI
// the terminal. Its errors say why there is nothing to open, in words for the person who
// asked.
func Open(q Question) (*exec.Cmd, error) {
	a, ok := named(q.Agent)
	if !ok {
		return nil, unknown(q.Agent)
	}
	if q.Session == "" {
		return nil, fmt.Errorf("dr has no session of %s to open for these lines", q.Agent)
	}
	if err := checkDir(q.Cwd); err != nil {
		return nil, err
	}
	cmd := exec.Command(a.command, a.open(q)...)
	switch {
	case errors.Is(cmd.Err, exec.ErrNotFound):
		return nil, notInstalled(a.command)
	case cmd.Err != nil:
		return nil, fmt.Errorf("find %s: %w", a.command, cmd.Err)
	}
	cmd.Dir = q.Cwd
	return cmd, nil
}
