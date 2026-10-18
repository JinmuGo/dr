package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ompModelsTimeout is how long ompModels waits for omp to list its models.
const ompModelsTimeout = 20 * time.Second

// omp is OMP. It forks a session from its log, a subagent's log too, and keeps the logs of
// the sessions dr starts in the state directory, so nothing changes under the home
// directory.
var omp = agent{
	name:    "omp",
	logs:    ".omp/agent/sessions",
	parse:   parseOMP,
	command: "omp",
	canFork: func(e *Edit) error {
		if e.Log == "" {
			return errors.New("the session's log is not known")
		}
		return nil
	},
	ask:    askOMP,
	open:   openOMP,
	models: ompModels,
}

type ompRecord struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession"`
	Model         string `json:"model"`         // of a model_change, as provider/model
	ThinkingLevel string `json:"thinkingLevel"` // of a thinking_level_change
	Message       struct {
		Role        string       `json:"role"`
		Content     borrowedJSON `json:"content"`
		Attribution string       `json:"attribution"` // "user", or "agent" for text an agent sent
		Steering    bool         `json:"steering"`    // sent while the agent was working
		ToolCallID  string       `json:"toolCallId"`
		ToolName    string       `json:"toolName"`
		IsError     bool         `json:"isError"`
		Details     borrowedJSON `json:"details"`
	} `json:"message"`
}

// ompEdit is what OMP logs for an edit. Once a session grows, OMP drops the whole file
// before and after from older edits and keeps only the diff.
type ompEdit struct {
	Path           string    `json:"path"`
	Op             string    `json:"op"`
	OldText        *string   `json:"oldText"`
	NewText        *string   `json:"newText"`
	Diff           string    `json:"diff"`
	SourcePath     string    `json:"sourcePath"` // where the file was, when the edit moved it
	PerFileResults []ompEdit `json:"perFileResults"`
}

// ompWriteDetails is what OMP logs of a write: where it wrote, or for a write to
// xd://resolve or xd://reject, which proposal it applied or rejected.
type ompWriteDetails struct {
	ResolvedPath string
	Xdev         struct {
		Inner struct {
			Action         string // "apply" or "reject"
			SourceToolName string // the tool that made the proposal
		}
	}
}

// ompStaged is a proposal of a tool OMP stages, such as ast_edit, and the files it changes.
type ompStaged struct {
	tool  string
	paths []string
}

type ompWrite struct {
	Path    string  `json:"path"`
	Content *string `json:"content"` // OMP drops it from older writes too
}

// parseOMP reads an OMP session log. A subagent has a log of its own. An edit was made with
// the model and the thinking level the log last changed to before it.
func parseOMP(records *recordReader) []Edit {
	var edits []Edit
	var turns []turn
	var session, parent, cwd string
	var said string          // what the agent said since the last user message or tool result
	var model, effort string // what the log last changed the model and the thinking level to
	var start time.Time
	writes := make(map[string]ompWrite) // by tool call id
	notes := make(map[string]string)    // by tool call id
	others := make(map[string][]string) // the files a call of another tool seems to change, by tool call id
	var staged ompStaged                // the last proposal not yet applied or rejected
	for records.next() {
		var record ompRecord
		if json.Unmarshal(records.line, &record) != nil {
			continue
		}
		if record.Type == "session" && session == "" {
			session, cwd, start = record.ID, record.Cwd, parseTime(record.Timestamp)
			if record.ParentSession != "" {
				name := strings.TrimSuffix(filepath.Base(record.ParentSession), ".jsonl")
				parent = name[strings.LastIndex(name, "_")+1:]
			}
			continue
		}
		switch record.Type {
		case "model_change":
			model = record.Model
		case "thinking_level_change":
			effort = record.ThinkingLevel
		}
		if record.Type != "message" {
			continue
		}
		message := record.Message
		switch message.Role {
		case "user":
			// A person's request, or the task a parent agent gave a subagent. A note OMP
			// sends while the agent works, such as that a wait was interrupted, is not.
			if s := text(message.Content); s != "" && (message.Attribution != "agent" || !message.Steering) {
				turns = append(turns, turn{prompt: s})
			}
			said = ""
		case "assistant":
			if s := text(message.Content); s != "" && len(turns) > 0 {
				turns[len(turns)-1].reply = s
			}
			var blocks []struct {
				Type, ID, Name, Text, Intent string
				Arguments                    borrowedJSON
			}
			_ = json.Unmarshal(message.Content, &blocks) // content that is a string has no blocks
			for _, b := range blocks {
				switch b.Type {
				case "text":
					said = addText(said, b.Text)
				case "toolCall":
					// When the agent said nothing, the intent OMP has it give with each tool
					// call, a few words such as "Writing notes", tells what the call is for.
					note := cmp.Or(said, b.Intent)
					if note == "" {
						var args struct{ I string }
						_ = json.Unmarshal(b.Arguments, &args)
						note = args.I
					}
					notes[b.ID] = strings.TrimSpace(note)
					var write ompWrite
					switch {
					case b.Name == "write":
						if json.Unmarshal(b.Arguments, &write) == nil {
							writes[b.ID] = write
						}
					case b.Name != "edit":
						if paths := fileTool(b.Name, b.Arguments); len(paths) > 0 {
							others[b.ID] = paths
						}
					}
				}
			}
		case "toolResult":
			said = ""
			if message.IsError {
				continue
			}
			e := Edit{
				Agent: "omp", Turn: len(turns), Time: parseTime(record.Timestamp), Note: notes[message.ToolCallID],
				Model: model, Effort: effort,
			}
			switch message.ToolName {
			case "edit":
				var details ompEdit
				var read []Edit
				if json.Unmarshal(message.Details, &details) == nil {
					read = ompEdits(e, details)
				}
				if len(read) == 0 {
					// A result in a shape dr does not know. The call names its file only in
					// the patch, so the edit names the files the result names, or none.
					paths := namedPaths(message.Details)
					if len(paths) == 0 {
						paths = []string{""}
					}
					for _, path := range paths {
						read = append(read, unread(e, "edit", path, cwd))
					}
				}
				edits = append(edits, read...)
			case "write":
				write, ok := writes[message.ToolCallID]
				var details ompWriteDetails
				_ = json.Unmarshal(message.Details, &details)
				switch resolved := details.Xdev.Inner; {
				case resolved.Action == "apply" && resolved.SourceToolName == staged.tool:
					for _, path := range staged.paths {
						edits = append(edits, unread(e, staged.tool, path, cwd))
					}
					staged = ompStaged{}
					continue
				case resolved.Action != "":
					staged = ompStaged{} // rejected
					continue
				}
				switch {
				case !ok:
					continue
				case details.ResolvedPath != "":
					write.Path = details.ResolvedPath
				case write.Path == "" || strings.Contains(write.Path, "://"):
					continue // a write to an OMP device, not a file
				case !filepath.IsAbs(write.Path):
					write.Path = filepath.Join(cwd, write.Path)
				}
				if write.Content == nil {
					edits = append(edits, unread(e, "write", write.Path, cwd))
					continue
				}
				e.Path = filepath.Clean(write.Path)
				e.Apply = whole(*write.Content)
				e.key = keyOf(e.Agent, e.Time, e.Path, *write.Content)
				edits = append(edits, e)
			default:
				paths, ok := others[message.ToolCallID]
				delete(others, message.ToolCallID) // a call is answered once
				var details struct{ Applied *bool }
				_ = json.Unmarshal(message.Details, &details)
				if ok && details.Applied != nil && !*details.Applied {
					// A proposal, which changes the files once a write to xd://resolve applies it.
					staged = ompStaged{tool: message.ToolName, paths: paths}
					continue
				}
				for _, path := range paths {
					edits = append(edits, unread(e, message.ToolName, path, cwd))
				}
			}
		}
	}
	for i := range edits {
		e := &edits[i]
		e.Session, e.Parent, e.Cwd, e.start = session, parent, cwd, start
	}
	return finish(edits, map[string][]turn{session: turns})
}

// ompEdits turns what OMP logged for one edit call into edits, one per file.
func ompEdits(e Edit, details ompEdit) []Edit {
	if len(details.PerFileResults) > 0 {
		var edits []Edit
		for _, f := range details.PerFileResults {
			edits = append(edits, ompEdits(e, f)...)
		}
		return edits
	}
	if details.Path == "" {
		return nil
	}
	e.Path = filepath.Clean(details.Path)
	moved := details.SourcePath != "" && filepath.Clean(details.SourcePath) != e.Path
	switch {
	case details.Op == "delete":
		e.Before = details.OldText
		e.Apply = whole("")
	case details.NewText != nil:
		if !moved {
			e.Before = details.OldText
		}
		e.Apply = whole(*details.NewText)
	case moved:
		return []Edit{unread(e, "edit", e.Path, "")} // the diff is against the old path
	default:
		hunks, ok := numberedHunks(details.Diff)
		if !ok {
			return []Edit{unread(e, "edit", e.Path, "")}
		}
		e.Apply = patch(hunks)
	}
	e.key = keyOf(e.Agent, e.Time, e.Path, details.Op, deref(details.OldText), deref(details.NewText), details.Diff)
	edits := []Edit{e}
	if moved {
		source := e
		source.Path, source.Before, source.Apply = filepath.Clean(details.SourcePath), details.OldText, whole("")
		source.key = keyOf(e.Agent, e.Time, source.Path, "moved", e.key)
		edits = append(edits, source)
	}
	return edits
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// askOMP asks OMP: omp -p forks the session from its log with --fork, or resumes the fork
// or the new session to continue, or starts a new session. The session that answers keeps
// its log under the state directory, which --session-dir names. It gets the read, grep and
// glob tools, no extensions and no skills. --tools leaves MCP tools reachable, and some of
// them write, so every call that needs approval is set to ask for it, which fails with
// nobody there to approve. With --mode json OMP prints one JSON event a line: session
// names the session that answers, and message_end holds a message whole. The answer is the
// text of the last assistant message, and the model that answered is its provider and model.
func askOMP(ctx context.Context, dirs Dirs, q Question) (Answer, error) {
	if dirs.State == "" {
		return Answer{}, errors.New("dr has no state directory to keep the session in")
	}
	sessions := filepath.Join(dirs.State, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		return Answer{}, fmt.Errorf("make a directory for the session: %w", err)
	}
	args := []string{"-p"}
	switch {
	case q.Fork != "":
		args = append(args, "--resume", q.Fork)
	case q.Session != "":
		args = append(args, "--fork", q.Log)
	}
	args = append(args, "--session-dir", sessions, "--tools", "read,grep,glob", "--no-extensions", "--no-skills",
		"--no-title", "--approval-mode", "always-ask", "--mode", "json")
	if q.Model != "" {
		args = append(args, "--model", q.Model)
	}
	if q.Effort != "" {
		args = append(args, "--thinking", q.Effort)
	}
	stdout, stderr, err := run(ctx, "omp", q.Cwd, q.Prompt, args)
	session, answer, model := ompEvents(stdout)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return Answer{}, notInstalled("omp")
	case err != nil:
		return Answer{}, failed("omp", err, stderr)
	case answer == "":
		return Answer{}, errors.New("omp gave no answer")
	}
	return Answer{Fork: cmp.Or(session, q.Fork), Text: answer, Model: model}, nil
}

// openOMP returns the arguments that open a fork of the session in OMP: omp --fork with the
// session's log, with the session's model and thinking level when they are known.
func openOMP(q Question) []string {
	args := []string{"--fork", q.Log}
	if q.Model != "" {
		args = append(args, "--model", q.Model)
	}
	if q.Effort != "" {
		args = append(args, "--thinking", q.Effort)
	}
	return args
}

// ompEvents returns what the events omp --mode json printed say: the session that answered,
// the text of the last assistant message, and the model that wrote it, as provider/model.
func ompEvents(stdout []byte) (session, answer, model string) {
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		var event struct {
			Type    string `json:"type"`
			ID      string `json:"id"` // of the session, in a session event
			Message struct {
				Role     string          `json:"role"`
				Content  json.RawMessage `json:"content"`
				Provider string          `json:"provider"`
				Model    string          `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		switch m := event.Message; {
		case event.Type == "session" && session == "":
			session = event.ID
		case event.Type == "message_end" && m.Role == "assistant":
			answer, model = text(m.Content), m.Model
			if m.Provider != "" && m.Model != "" {
				model = m.Provider + "/" + m.Model
			}
		}
	}
	return session, answer, model
}

// ompModels returns the chat models omp models --json lists, with the selectors omp
// --model takes, newest first by the numbers in their ids (see newerModel) and in omp's
// order on ties. omp lists them alphabetically and does not expose its own ranking. It
// gives up after ompModelsTimeout.
func ompModels(ctx context.Context, dirs Dirs) ([]Model, error) {
	runCtx, cancel := context.WithTimeout(ctx, ompModelsTimeout)
	defer cancel()
	stdout, stderr, err := run(runCtx, "omp", dirs.Home, "", []string{"models", "--json"})
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, notInstalled("omp")
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case runCtx.Err() != nil:
		return nil, errors.New("omp listed no models in 20 seconds")
	case err != nil:
		return nil, failed("omp", err, stderr)
	}
	var list struct {
		Models []struct {
			Kind     string `json:"kind"`
			Selector string `json:"selector"`
			Name     string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(stdout, &list); err != nil {
		return nil, fmt.Errorf("read omp's model list: %w", err)
	}
	var models []Model
	for _, m := range list.Models {
		if m.Kind == "chat" {
			models = append(models, Model{ID: m.Selector, Name: m.Name})
		}
	}
	slices.SortStableFunc(models, func(a, b Model) int { return newerModel(a.ID, b.ID) })
	return models, nil
}

// newerModel compares the runs of digits in the model ids a and b, after any provider/
// prefix, in order as numbers: it is negative when a's are greater, so newer, and positive
// when b's are. Where one id's numbers begin the other's, the one with more is newer, so
// gpt-6.1 comes before gpt-6.
func newerModel(a, b string) int {
	na, nb := modelNumbers(a), modelNumbers(b)
	for i := range min(len(na), len(nb)) {
		x, y := na[i], nb[i]
		if c := cmp.Compare(len(y), len(x)); c != 0 {
			return c
		}
		if c := strings.Compare(y, x); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(nb), len(na))
}

// modelNumbers returns the runs of digits in the model id after any provider/ prefix,
// without leading zeros, so longer runs are greater numbers.
func modelNumbers(id string) []string {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	var numbers []string
	for len(id) > 0 {
		start := strings.IndexAny(id, "0123456789")
		if start < 0 {
			break
		}
		id = id[start:]
		end := strings.IndexFunc(id, func(r rune) bool { return r < '0' || r > '9' })
		if end < 0 {
			end = len(id)
		}
		numbers = append(numbers, strings.TrimLeft(id[:end], "0"))
		id = id[end:]
	}
	return numbers
}
