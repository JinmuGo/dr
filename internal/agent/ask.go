package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// askTimeout is how long Ask waits for an answer.
	askTimeout = 3 * time.Minute
	// maxOutput caps what Ask keeps of what a CLI writes to stdout, and maxErrors what it
	// keeps of the end of what it writes to stderr, where the reason of a failure is.
	maxOutput = 4 << 20
	maxErrors = 64 << 10
	// maxReason is how many characters of a reason for a failure Ask keeps.
	maxReason = 200
)

// errNoAnswer is the reason when the agent does not answer in askTimeout.
var errNoAnswer = errors.New("no answer in 3 minutes")

// Dirs are the directories Ask works with besides the one the CLI runs in.
type Dirs struct {
	Home string // the home directory, under which the agents keep their session logs
	// State is dr's state directory. An agent that lets dr choose where the sessions dr
	// starts keep their logs keeps them under State/sessions.
	State string
}

// Question is a question for an agent about some lines. It continues the fork Fork when
// that is set, or else goes to a new fork of the session Session, or else, when both are "",
// to a new session. A fork is a copy of a session, so asking it does not change the session.
type Question struct {
	Agent   string // the name of the agent
	Session string // id of the session to fork, "" for a new session
	Log     string // the session's log, which an agent may fork from
	Cwd     string // where the CLI runs: the session's directory, or the repository root for a new session
	Model   string // the model to ask, "" to leave it to the agent
	Effort  string // the reasoning effort to ask with, "" to leave it to the agent
	Fork    string // the fork or new session to continue, "" to make one
	Prompt  string
}

// Answer is what an agent answered.
type Answer struct {
	Fork  string // what to continue next time: the fork or the new session that answered
	Text  string
	Model string        // the model that answered, "" when not known
	Took  time.Duration // how long the agent took
}

// Ask sends q.Prompt on stdin to the CLI of the agent q names, run in q.Cwd, and returns
// the answer. Each agent is asked with tools that only read files, and without hooks. Ask
// gives up after 3 minutes. When ctx ends first, it kills the CLI and what the CLI started,
// and returns ctx.Err(). Its other errors say why there is no answer, in words for the
// person who asked.
func Ask(ctx context.Context, dirs Dirs, q Question) (Answer, error) {
	return ask(ctx, dirs, q, askTimeout)
}

// ask is Ask with the time it waits for an answer.
func ask(ctx context.Context, dirs Dirs, q Question, timeout time.Duration) (Answer, error) {
	a, ok := named(q.Agent)
	if !ok {
		return Answer{}, unknown(q.Agent)
	}
	if err := checkDir(q.Cwd); err != nil {
		return Answer{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	answer, err := a.ask(runCtx, dirs, q)
	switch {
	case ctx.Err() != nil:
		return Answer{}, ctx.Err()
	case runCtx.Err() != nil:
		return Answer{}, errNoAnswer
	case err != nil:
		return Answer{}, err
	}
	answer.Took = time.Since(start)
	return answer, nil
}

// checkDir returns nil when cwd, the directory of a session, is there, or else why not.
func checkDir(cwd string) error {
	if cwd == "" {
		return errors.New("the session's log names no directory")
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return fmt.Errorf("the session's directory %s no longer exists", cwd)
	}
	return nil
}

// run runs the CLI name with args in dir, with input on its stdin, and returns the start of
// what it wrote to stdout and the end of what it wrote to stderr. The CLI runs in a process
// group of its own where the system has them: when ctx ends the whole group is killed, and
// so is what is left of it when the CLI ends, such as a child it did not wait for.
func run(ctx context.Context, name, dir, input string, args []string) (stdout []byte, stderr string, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(input)
	out, errOut := &head{room: maxOutput}, &tail{keep: maxErrors}
	cmd.Stdout, cmd.Stderr = out, errOut
	ownGroup(cmd)
	cmd.WaitDelay = time.Second // then pipes that a process left open are closed
	err = cmd.Run()
	killGroup(cmd)
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil // the CLI ended well; a child it left kept its output open
	}
	return out.data, string(errOut.data), err
}

// failed returns why the CLI name failed with err: the last line it wrote to stderr, or err
// when it wrote nothing there.
func failed(name string, err error, stderr string) error {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return errors.New(cut(last))
	}
	return fmt.Errorf("%s: %w", name, err)
}

func notInstalled(name string) error {
	return fmt.Errorf("%s is not installed or not in PATH", name)
}

// unknown returns that dr does not know the agent name, so it cannot ask it.
func unknown(name string) error {
	return fmt.Errorf("dr cannot ask %s sessions", name)
}

// cut returns s cut to maxReason characters.
func cut(s string) string {
	if r := []rune(s); len(r) > maxReason {
		return string(r[:maxReason])
	}
	return s
}

// head keeps the first bytes written to it, room more of them, and drops the rest, so a CLI
// that writes too much neither blocks nor fills memory.
type head struct {
	data []byte
	room int
}

func (h *head) Write(p []byte) (int, error) {
	n := min(len(p), h.room)
	h.data = append(h.data, p[:n]...)
	h.room -= n
	return len(p), nil
}

// tail keeps the last keep bytes written to it, or a few more.
type tail struct {
	data []byte
	keep int
}

func (t *tail) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > 2*t.keep {
		t.data = append(t.data[:0], t.data[len(t.data)-t.keep:]...)
	}
	return len(p), nil
}
