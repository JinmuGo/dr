package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// codexSessions is where Codex keeps its session logs, relative to the home directory.
const codexSessions = ".codex/sessions"

// codex is Codex. It forks any session by its id.
var codex = agent{
	name:    "codex",
	logs:    codexSessions,
	parse:   parseCodex,
	command: "codex",
	canFork: func(*Edit) error { return nil },
	ask:     askCodex,
	open:    openCodex,
	models:  codexModels,
}

type codexRecord struct {
	Timestamp string       `json:"timestamp"`
	Type      string       `json:"type"`
	Payload   borrowedJSON `json:"payload"`
}

type codexMeta struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
	Git       struct {
		Branch string `json:"branch"`
	} `json:"git"`
}

// codexTurn is what Codex logs in a turn_context record of the turn that follows it.
type codexTurn struct {
	Model             string `json:"model"`
	CollaborationMode struct {
		Settings struct {
			ReasoningEffort string `json:"reasoning_effort"`
		} `json:"settings"`
	} `json:"collaboration_mode"`
}

type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		Type    string                 `json:"type"`
		Status  string                 `json:"status"`
		Phase   string                 `json:"phase"`
		Content borrowedJSON           `json:"content"`
		Changes map[string]codexChange `json:"changes"`
	} `json:"item"`
}

type codexChange struct {
	Type        string  `json:"type"` // "add", "delete" or "update"
	Content     string  `json:"content"`
	UnifiedDiff string  `json:"unified_diff"`
	MovePath    *string `json:"move_path"`
}

// parseCodex reads a Codex rollout. A fork copies the conversation but not the file
// changes, so its changes are all its own.
func parseCodex(records *recordReader) []Edit {
	var edits []Edit
	var turns []turn
	var said string // what the agent said since the last request or file change
	var meta codexMeta
	var model, effort string // of the turn going on
	final := false           // whether the last request has its final answer
	for records.next() {
		var record codexRecord
		if json.Unmarshal(records.line, &record) != nil {
			continue
		}
		switch record.Type {
		case "session_meta":
			if meta.ID == "" {
				_ = json.Unmarshal(record.Payload, &meta)
			}
			continue
		case "turn_context":
			var c codexTurn
			if json.Unmarshal(record.Payload, &c) == nil {
				model, effort = c.Model, c.CollaborationMode.Settings.ReasoningEffort
			}
			continue
		}
		var event codexEvent
		if record.Type != "event_msg" || json.Unmarshal(record.Payload, &event) != nil || event.Type != "item_completed" {
			continue
		}
		switch item := event.Item; item.Type {
		case "UserMessage":
			if s := text(item.Content); s != "" {
				turns = append(turns, turn{prompt: s})
				final = false
			}
			said = ""
		case "AgentMessage":
			s := text(item.Content)
			if s != "" && len(turns) > 0 && !final {
				turns[len(turns)-1].reply = s
				final = item.Phase == "final_answer"
			}
			said = addText(said, s)
		case "FileChange":
			note := said
			said = ""
			if item.Status != "completed" {
				continue
			}
			t := parseTime(record.Timestamp)
			paths := make([]string, 0, len(item.Changes))
			for path := range item.Changes {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			for _, path := range paths {
				change := item.Changes[path]
				e := Edit{Agent: "codex", Turn: len(turns), Time: t, Path: path, Note: note, Model: model, Effort: effort}
				switch {
				case change.Type == "add":
					e.Apply = whole(change.Content)
				case change.Type == "delete":
					e.Before = &change.Content
					e.Apply = whole("")
				case change.Type != "update":
					continue
				case change.MovePath != nil && *change.MovePath != "" && *change.MovePath != path:
					// The file moved. dr does not follow it, so its lines under the new
					// path have no origin.
					e.Apply = whole("")
				default:
					hunks, ok := unifiedHunks(change.UnifiedDiff)
					if !ok {
						continue
					}
					e.Apply = patch(hunks)
				}
				e.key = keyOf(e.Agent, t, path, change.Type, change.Content, change.UnifiedDiff)
				edits = append(edits, e)
			}
		}
	}
	start := parseTime(meta.Timestamp)
	for i := range edits {
		e := &edits[i]
		e.Session, e.Branch, e.Cwd, e.start = meta.ID, meta.Git.Branch, meta.Cwd, start
	}
	return finish(edits, map[string][]turn{meta.ID: turns})
}

// askCodex asks Codex: codex exec resumes the fork to continue, or forks the session, or
// starts a new session, in a read-only sandbox, without the user's config and without
// hooks. With --json it prints one JSON event a line: thread.started names the thread that
// answers, and turn.failed or error say why a turn failed. The answer is the last message,
// which -o writes to a file. No event names the model, so the model that answered is read
// from the session log of that thread under the home directory, and is the one asked for
// when that log does not say.
func askCodex(ctx context.Context, dirs Dirs, q Question) (Answer, error) {
	file, err := os.CreateTemp("", "dr-answer-*.txt")
	if err != nil {
		return Answer{}, fmt.Errorf("make a file for the answer: %w", err)
	}
	answerPath := file.Name()
	_ = file.Close() // empty, so there is nothing to lose
	defer func() { _ = os.Remove(answerPath) }()
	args := []string{"exec"}
	var thread string
	switch {
	case q.Fork != "":
		args, thread = append(args, "resume"), q.Fork
	case q.Session != "":
		args, thread = append(args, "fork"), q.Session
	}
	args = append(args, "--json", "-o", answerPath, "--ignore-user-config", "--disable", "hooks",
		"-c", `sandbox_mode="read-only"`, "-c", `approval_policy="never"`)
	if q.Model != "" {
		args = append(args, "-c", "model="+strconv.Quote(q.Model))
	}
	if q.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(q.Effort))
	}
	if thread != "" {
		args = append(args, thread)
	}
	args = append(args, "-")
	stdout, stderr, err := run(ctx, "codex", q.Cwd, q.Prompt, args)
	fork, failure := codexEvents(stdout)
	answer, _ := os.ReadFile(answerPath) // a run that failed may have left it empty
	text := strings.TrimSpace(string(answer))
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return Answer{}, notInstalled("codex")
	case err == nil && fork != "" && text != "":
		model := codexModel(dirs.Home, fork)
		if model == "" {
			model = q.Model
		}
		return Answer{Fork: fork, Text: text, Model: model}, nil
	case failure != "":
		return Answer{}, errors.New(cut(failure))
	case err != nil:
		return Answer{}, failed("codex", err, stderr)
	case fork == "":
		return Answer{}, errors.New("codex named no thread for the fork")
	}
	return Answer{}, errors.New("codex gave no answer")
}

// openCodex returns the arguments that open a fork of the session in Codex: codex fork, with
// the session's model and reasoning effort when they are known, quoted as config values.
func openCodex(q Question) []string {
	args := []string{"fork"}
	if q.Model != "" {
		args = append(args, "-c", "model="+strconv.Quote(q.Model))
	}
	if q.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(q.Effort))
	}
	return append(args, q.Session)
}

// codexEvents returns the thread that the events codex exec --json printed say started, and
// why the turn failed when they say it did.
func codexEvents(stdout []byte) (thread, failure string) {
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		switch event.Type {
		case "thread.started":
			thread = event.ThreadID
		case "turn.failed":
			failure = event.Error.Message
		case "error":
			failure = event.Message
		}
	}
	return thread, failure
}

// codexModel returns the model of the last turn in the session log of Codex thread under
// home, or "" when there is no such log or its last turn names no model.
func codexModel(home, thread string) string {
	path := ""
	suffix := "-" + thread + ".jsonl"
	// The walk skips what it cannot read, so it returns no error.
	_ = filepath.WalkDir(filepath.Join(home, codexSessions), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), suffix) {
			path = p
			return fs.SkipAll
		}
		return nil
	})
	if path == "" {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	records := newRecordReader(file)
	model := ""
	for records.next() {
		var record codexRecord
		var turn codexTurn
		if json.Unmarshal(records.line, &record) == nil && record.Type == "turn_context" && json.Unmarshal(record.Payload, &turn) == nil {
			model = turn.Model
		}
	}
	if records.err != nil {
		return ""
	}
	return model
}

// codexModels returns the models Codex's own model picker shows: those its cache
// models_cache.json, in $CODEX_HOME or else ~/.codex, lists as visible, by priority.
func codexModels(_ context.Context, dirs Dirs) ([]Model, error) {
	home := cmp.Or(os.Getenv("CODEX_HOME"), filepath.Join(dirs.Home, ".codex"))
	data, err := os.ReadFile(filepath.Join(home, "models_cache.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("codex has no model list yet; run codex once")
	}
	if err != nil {
		return nil, fmt.Errorf("read codex's model list: %w", err)
	}
	var cache struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
			Priority    int    `json:"priority"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("read codex's model list: %w", err)
	}
	listed := cache.Models[:0]
	for _, m := range cache.Models {
		if m.Visibility == "list" {
			listed = append(listed, m)
		}
	}
	sort.SliceStable(listed, func(i, j int) bool { return listed[i].Priority < listed[j].Priority })
	models := make([]Model, len(listed))
	for i, m := range listed {
		models[i] = Model{ID: m.Slug, Name: m.DisplayName}
	}
	return models, nil
}
