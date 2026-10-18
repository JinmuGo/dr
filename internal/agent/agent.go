// Package agent reads the file edits that coding agents record in their session logs, and
// asks the agents about them and opens their sessions through their CLIs. Each agent has a
// file of its own, and agents lists it.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
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
	// Unread names the tool of an edit the log records but dr cannot read, such as a tool
	// dr does not know or a record in a shape it does not know. Apply of such an edit
	// reports false, so the lines it changed have no origin. It is "" for an edit dr read.
	Unread string

	key   string    // the same in every session log that holds a copy of the edit
	start time.Time // when the session started
}

// Problem is a session log dr could not fully read.
type Problem struct {
	Agent, Path string
	Bad         int // lines that are not JSON
	// Unread counts the edits the log records that dr could not read and that name no file
	// dr can tell, so no file can say so.
	Unread int
}

// agent is a coding agent dr knows: where it keeps its session logs and how to read them,
// and how to ask it and open its sessions through its CLI. The file of each agent makes one,
// and agents lists it.
type agent struct {
	name    string                             // as Edit.Agent has it
	logs    string                             // where it keeps its session logs, relative to the home directory
	parse   func(records *recordReader) []Edit // reads the records of one session log
	command string                             // its CLI, which Installed looks for in PATH
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

// Loader reuses unchanged session logs between loads. Its zero value is ready to use.
// A Loader must not be copied after its first use.
type Loader struct {
	mu         sync.Mutex
	home       string
	since      time.Time
	roots      []string
	generation uint64
	entries    map[string]*cachedLog
}

type cachedLog struct {
	stamp   logStamp
	edits   []Edit
	problem *Problem
}

type logStamp struct {
	info   os.FileInfo
	change time.Time
	digest [sha256.Size]byte
}

// Load reads the session logs under home that mention one of roots and returns the edits
// made at or after since, in the order they happened. It reads the logs in parallel, one
// per CPU at a time, and gives the same result as reading them one by one.
func (l *Loader) Load(home string, since time.Time, roots []string) ([]Edit, []Problem) {
	l.mu.Lock()
	if l.home != home || !l.since.Equal(since) || !slices.Equal(l.roots, roots) {
		l.home, l.since, l.roots = home, since, slices.Clone(roots)
		l.entries = nil
	}
	l.generation++
	generation, entries := l.generation, l.entries
	l.mu.Unlock()

	rootBytes := make([][]byte, len(roots))
	for i, root := range roots {
		rootBytes[i] = []byte(root)
	}
	var logs []sessionLog
	for _, a := range agents {
		// The walk skips what it cannot read, so it returns no error.
		_ = filepath.WalkDir(filepath.Join(home, a.logs), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil // a missing log directory just means the agent is not used
			}
			if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
				logs = append(logs, sessionLog{agent: a, path: path, cached: entries[path]})
			}
			return nil
		})
	}
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(logs)) {
		wg.Go(func() {
			for i := range next {
				logs[i].read(since, rootBytes)
			}
		})
	}
	for i := range logs {
		next <- i
	}
	close(next)
	wg.Wait()
	// Publish one immutable snapshot. A newer overlapping load owns the next snapshot.
	updated := make(map[string]*cachedLog, len(logs))
	for _, log := range logs {
		if log.cached != nil {
			updated[log.path] = log.cached
		}
	}
	l.mu.Lock()
	if l.generation == generation {
		l.entries = updated
	}
	l.mu.Unlock()
	var edits []Edit
	var problems []Problem
	for _, l := range logs {
		edits = append(edits, l.edits...)
		if l.problem != nil {
			problems = append(problems, *l.problem)
		}
	}
	edits = dropCopies(edits)
	// The caller owns the aggregate, including the optional mutable values.
	for i := range edits {
		if edits[i].Before != nil {
			edits[i].Before = new(*edits[i].Before)
		}
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].Time.Before(edits[j].Time) })
	return edits, problems
}

// sessionLog is a session log Load reads, and what it read there.
type sessionLog struct {
	agent  agent
	path   string
	cached *cachedLog

	edits   []Edit   // the edits made at or after the time Load was given
	problem *Problem // why the log could not be fully read, nil when it could
}

// read reads the edits made at or after since from l when it mentions one of roots.
func (l *sessionLog) read(since time.Time, roots [][]byte) {
	cached := l.cached
	l.cached = nil
	file, err := os.Open(l.path)
	if err != nil {
		l.problem = &Problem{Agent: l.agent.name, Path: l.path}
		return
	}
	defer func() { _ = file.Close() }()
	before, err := stampLog(file)
	if err != nil {
		l.problem = &Problem{Agent: l.agent.name, Path: l.path}
		return
	}
	if cached != nil && before.equal(cached.stamp) && logUnchanged(file, l.path, before) {
		l.edits, l.problem, l.cached = cached.edits, cached.problem, cached
		return
	}
	matched, err := mentions(file, roots)
	if err != nil {
		l.problem = &Problem{Agent: l.agent.name, Path: l.path}
		return
	}
	if matched {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			l.problem = &Problem{Agent: l.agent.name, Path: l.path}
			return
		}
		if !l.readRecords(file, since) {
			return
		}
	}
	if logUnchanged(file, l.path, before) {
		l.cached = &cachedLog{stamp: before, edits: l.edits, problem: l.problem}
	}
}

// readRecords discards all edits on I/O failure, but retains edits around malformed lines.
func (l *sessionLog) readRecords(reader io.Reader, since time.Time) bool {
	records := newRecordReader(reader)
	edits := l.agent.parse(records)
	if records.err != nil {
		l.edits = nil
		l.problem = &Problem{Agent: l.agent.name, Path: l.path}
		return false
	}
	unread := 0
	for _, e := range edits {
		switch {
		case e.Time.Before(since):
		case e.Unread != "" && e.Path == "":
			unread++
		default:
			e.Log = l.path
			l.edits = append(l.edits, e)
		}
	}
	if records.bad > 0 || unread > 0 {
		l.problem = &Problem{Agent: l.agent.name, Path: l.path, Bad: records.bad, Unread: unread}
	}
	return true
}

func logUnchanged(file *os.File, path string, before logStamp) bool {
	after, err := stampLog(file)
	if err != nil || !before.equal(after) {
		return false
	}
	current, err := os.Stat(path)
	return err == nil && after.matches(current)
}

// stampLog checks the opened file, not the path that may have been replaced.
// Platforms without a change time hash the content and rewind the same descriptor.
func stampLog(file *os.File) (logStamp, error) {
	info, err := file.Stat()
	if err != nil {
		return logStamp{}, err
	}
	stamp := logStamp{info: info, change: logChangeTime(info)}
	if stamp.change.IsZero() {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return logStamp{}, err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return logStamp{}, err
		}
		copy(stamp.digest[:], hash.Sum(nil))
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return logStamp{}, err
		}
	}
	return stamp, nil
}

func (s logStamp) matches(info os.FileInfo) bool {
	return os.SameFile(s.info, info) && s.info.Size() == info.Size() &&
		s.info.Mode() == info.Mode() && s.info.ModTime().Equal(info.ModTime()) &&
		s.change.Equal(logChangeTime(info))
}

func (s logStamp) equal(other logStamp) bool {
	return s.matches(other.info) && s.digest == other.digest
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
func text(raw []byte) string {
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

// unread returns e as an edit of path with tool that dr cannot read. A relative path is
// taken from the session's directory cwd. With no path, Load counts the edit in the log's
// Problem, since no file can say so.
func unread(e Edit, tool, path, cwd string) Edit {
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	if path != "" {
		path = filepath.Clean(path)
	}
	e.Path, e.Unread, e.Before = path, tool, nil
	e.Apply = func(string) (string, bool) { return "", false }
	e.key = keyOf(e.Agent, e.Time, e.Path, "unread", tool)
	return e
}

// fileTool returns the files a call of a tool dr does not read seems to change: when the
// tool's name says it edits, writes or patches, the paths its arguments name. A tool
// that names no file, such as a to-do list, changes none.
func fileTool(name string, args []byte) []string {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "edit") && !strings.Contains(lower, "write") && !strings.Contains(lower, "patch") {
		return nil
	}
	return namedPaths(args)
}

// namedPaths returns the files a JSON object names in a field for a path, such as path,
// file_path, filePath or paths, leaving out device paths such as xd://resolve.
func namedPaths(object []byte) []string {
	var named struct {
		Path         string   `json:"path"`
		FilePath     string   `json:"file_path"`
		FilePathCase string   `json:"filePath"`
		NotebookPath string   `json:"notebook_path"`
		Paths        []string `json:"paths"`
	}
	if json.Unmarshal(object, &named) != nil {
		return nil
	}
	var paths []string
	for _, p := range append([]string{named.Path, named.FilePath, named.FilePathCase, named.NotebookPath}, named.Paths...) {
		if p != "" && !strings.Contains(p, "://") && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}
	return paths
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
