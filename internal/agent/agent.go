// Package agent reads the file edits that coding agents record in their session logs, and
// asks the agents about them and opens their sessions through their CLIs. Each agent has a
// file of its own, and agents lists it.
package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Edit is one change an agent made to a file, as its session log records it, or a fix of
// the agent's that dr applied, as dr's log records it.
type Edit struct {
	Agent   string    // the name of the agent, as agents has it
	Session string    // id of the session that made the edit
	Parent  string    // id of the session that started it, when it is a subagent
	Log     string    // the session log the edit was read from
	Turn    int       // which request of the session led to the edit, from 1
	Prompt  string    // that request
	Reply   string    // what the agent said last for that request
	Note    string    // what the agent said just before the edit, for the same request
	Time    time.Time // when the edit happened
	Branch  string    // git branch the session was on, "" when the log does not say
	Cwd     string    // working directory of the session
	Path    string    // absolute path of the edited file
	Model   string    // the model that made the edit, "" when the log does not say
	Effort  string    // the reasoning effort it was made with, "" when the log does not say

	// Before is the whole file before the edit, when the log has it.
	Before *string
	// Apply returns the file after the edit, given the file before it.
	// It reports false when the edit does not fit that file.
	Apply func(before string) (after string, ok bool)
	// Fix is set on an edit dr made: a fix it applied for a comment. It is nil for an edit
	// read from a session log.
	Fix *Fix

	key   string    // the same in every session log that holds a copy of the edit
	start time.Time // when the session started
}

// Problem is a session log dr could not fully read.
type Problem struct {
	Agent, Path string
	Bad         int // lines that are not JSON
}

// agent is a coding agent dr knows: where it keeps its session logs and how to read them,
// and how to ask it and open its sessions through its CLI. The file of each agent makes one,
// and agents lists it.
type agent struct {
	name    string                        // as Edit.Agent has it
	logs    string                        // where it keeps its session logs, relative to the home directory
	parse   func(records [][]byte) []Edit // reads the records of one session log
	command string                        // its CLI, which Installed looks for in PATH
	// canFork returns nil when the agent can fork the session that made e, or why it cannot.
	canFork func(e *Edit) error
	// ask asks q through the CLI, as Ask says, and gives up when ctx ends.
	ask func(ctx context.Context, dirs Dirs, q Question) (Answer, error)
	// open returns the arguments of the CLI that open a fork of q.Session for the person to
	// work in, as Open says.
	open func(q Question) []string
	// models returns the models the CLI can be asked with, as Models says.
	models func(ctx context.Context, dirs Dirs) ([]Model, error)
}

// agents are the agents dr knows, in the order dr prefers them.
var agents = []agent{claude, codex, omp}

// Names returns the names of the agents dr knows, in the order dr prefers them.
func Names() []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.name
	}
	return names
}

// Installed reports whether the CLI of the agent named name is installed: in PATH.
func Installed(name string) bool {
	a, ok := named(name)
	if !ok {
		return false
	}
	_, err := exec.LookPath(a.command)
	return err == nil
}

// CanFork returns nil when dr can ask a fork of the session that made e, or why it cannot.
func CanFork(e *Edit) error {
	a, ok := named(e.Agent)
	if !ok {
		return unknown(e.Agent)
	}
	return a.canFork(e)
}

// named returns the agent named name, and whether dr knows it.
func named(name string) (agent, bool) {
	for _, a := range agents {
		if a.name == name {
			return a, true
		}
	}
	return agent{}, false
}

// Load reads the session logs under home that mention one of roots and returns the edits
// made at or after since, in the order they happened.
func Load(home string, since time.Time, roots []string) ([]Edit, []Problem) {
	rootBytes := make([][]byte, len(roots))
	for i, root := range roots {
		rootBytes[i] = []byte(root)
	}
	var edits []Edit
	var problems []Problem
	for _, a := range agents {
		// The walk skips what it cannot read, so it returns no error.
		_ = filepath.WalkDir(filepath.Join(home, a.logs), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil // a missing log directory just means the agent is not used
			}
			if info, err := d.Info(); err != nil || info.ModTime().Before(since) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				problems = append(problems, Problem{Agent: a.name, Path: path})
				return nil
			}
			if !mentions(data, rootBytes) {
				return nil
			}
			records, bad := split(data)
			if bad > 0 {
				problems = append(problems, Problem{Agent: a.name, Path: path, Bad: bad})
			}
			for _, e := range a.parse(records) {
				if !e.Time.Before(since) {
					e.Log = path
					edits = append(edits, e)
				}
			}
			return nil
		})
	}
	edits = dropCopies(edits)
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].Time.Before(edits[j].Time) })
	return edits, problems
}

func mentions(data []byte, roots [][]byte) bool {
	for _, root := range roots {
		if bytes.Contains(data, root) {
			return true
		}
	}
	return false
}

// split returns the JSON lines of a log and how many complete lines are not JSON.
// An agent may be writing the last line right now, so an unfinished last line is skipped.
func split(data []byte) (records [][]byte, bad int) {
	lines := bytes.Split(data, []byte("\n"))
	if n := len(lines) - 1; n >= 0 && !json.Valid(lines[n]) {
		lines = lines[:n]
	}
	for _, l := range lines {
		switch {
		case len(bytes.TrimSpace(l)) == 0:
		case json.Valid(l):
			records = append(records, l)
		default:
			bad++
		}
	}
	return records, bad
}

// dropCopies keeps one copy of each edit. A forked session carries the records of the
// session it came from, times included, so the copy in the session that started first
// is the original. When two sessions started at the same time it is not clear which one
// made the edit, so both copies go.
func dropCopies(edits []Edit) []Edit {
	type pick struct {
		index int
		tie   bool
	}
	picks := make(map[string]*pick, len(edits))
	for i, e := range edits {
		p, ok := picks[e.key]
		if !ok {
			picks[e.key] = &pick{index: i}
			continue
		}
		switch picked := edits[p.index]; {
		case e.Session == picked.Session:
			// One session in two files, such as an OMP session that moved. Keep one.
		case e.start.Before(picked.start):
			p.index, p.tie = i, false
		case e.start.Equal(picked.start):
			p.tie = true
		}
	}
	var kept []Edit
	for i, e := range edits {
		if p := picks[e.key]; p.index == i && !p.tie {
			kept = append(kept, e)
		}
	}
	return kept
}

// keyOf identifies an edit by what it did, so the copies of it in forked sessions match.
func keyOf(agent string, t time.Time, path string, payload ...string) string {
	h := sha256.New()
	for _, s := range append([]string{agent, t.UTC().Format(time.RFC3339Nano), path}, payload...) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return string(h.Sum(nil))
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// text returns the text of a message: a string, or the text blocks of a list of blocks.
// Codex calls a text block "Text" in agent messages and "text" in user messages.
func text(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if strings.EqualFold(b.Type, "text") {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// addText returns said with s added on a line of its own, without surrounding space.
func addText(said, s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return said
	case said == "":
		return s
	}
	return said + "\n" + s
}

// turn is one request in a session and what the agent said last for it.
type turn struct {
	prompt, reply string
}

// finish fills in the request and the reply of each edit once the whole log is read,
// because the reply comes after the edits.
func finish(edits []Edit, turns map[string][]turn) []Edit {
	for i := range edits {
		e := &edits[i]
		if sessionTurns := turns[e.Session]; e.Turn > 0 && e.Turn <= len(sessionTurns) {
			e.Prompt, e.Reply = sessionTurns[e.Turn-1].prompt, sessionTurns[e.Turn-1].reply
		}
	}
	return edits
}

// whole is an edit that writes the whole file.
func whole(after string) func(string) (string, bool) {
	return func(string) (string, bool) { return after, true }
}

// replace is an edit that replaces oldText with newText. Claude replaces oldText only when
// it occurs once, unless it was asked to replace all.
func replace(oldText, newText string, all bool) func(string) (string, bool) {
	return func(before string) (string, bool) {
		if oldText == "" {
			// Claude creates a file by replacing nothing.
			return newText, before == ""
		}
		switch n := strings.Count(before, oldText); {
		case all && n > 0:
			return strings.ReplaceAll(before, oldText, newText), true
		case n == 1:
			return strings.Replace(before, oldText, newText, 1), true
		}
		return "", false
	}
}
