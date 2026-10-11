package agent

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// claude is Claude Code. It forks a session by its id, run in the session's directory, and
// cannot resume a subagent's session.
var claude = agent{
	name:    "claude",
	logs:    ".claude/projects",
	parse:   parseClaude,
	command: "claude",
	canFork: func(e *Edit) error {
		if e.Parent != "" {
			return errors.New("claude cannot resume a subagent's session")
		}
		return nil
	},
	ask:    askClaude,
	open:   openClaude,
	models: claudeModels,
}

type claudeRecord struct {
	Type                      string          `json:"type"`
	SessionID                 string          `json:"sessionId"`
	AgentID                   string          `json:"agentId"`
	IsSidechain               bool            `json:"isSidechain"`
	IsMeta                    bool            `json:"isMeta"`
	IsCompactSummary          bool            `json:"isCompactSummary"`
	IsVisibleInTranscriptOnly bool            `json:"isVisibleInTranscriptOnly"`
	Timestamp                 string          `json:"timestamp"`
	Cwd                       string          `json:"cwd"`
	GitBranch                 string          `json:"gitBranch"`
	ToolUseResult             json.RawMessage `json:"toolUseResult"`
	Message                   struct {
		ID      string          `json:"id"` // of the response, the same on every record of it
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	PermissionDecision struct {
		Decision string `json:"decision"` // "accept" when the tool was allowed to run
	} `json:"permissionDecision"`
}

// claudeBlock is a block of the content of a message.
type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`          // of a tool_use
	Name      string          `json:"name"`        // of the tool a tool_use calls
	Input     json.RawMessage `json:"input"`       // of a tool_use
	ToolUseID string          `json:"tool_use_id"` // of a tool_result
	IsError   bool            `json:"is_error"`    // of a tool_result
}

// claudeResult is what Claude Code logs for a Write or an Edit. Its structuredPatch turns
// tabs into spaces, so dr does not use it.
type claudeResult struct {
	Type         string  `json:"type"` // "create" or "update" for a Write
	FilePath     string  `json:"filePath"`
	Content      *string `json:"content"`
	OriginalFile *string `json:"originalFile"`
	OldString    *string `json:"oldString"`
	NewString    *string `json:"newString"`
	ReplaceAll   bool    `json:"replaceAll"`
}

// claudeInput is the input of a Write or an Edit call.
type claudeInput struct {
	FilePath   string  `json:"file_path"`
	Content    *string `json:"content"`     // of a Write
	OldString  *string `json:"old_string"`  // of an Edit
	NewString  *string `json:"new_string"`  // of an Edit
	ReplaceAll bool    `json:"replace_all"` // of an Edit
}

// parseClaude reads a Claude Code session log. A subagent logs to its own file, or in older
// versions to its parent's file with its records marked, and counts its own requests.
func parseClaude(records [][]byte) []Edit {
	var edits []Edit
	turns := make(map[string][]turn)
	// Claude Code logs each block of a response as a record of its own and runs a tool as
	// soon as its block is logged, so the result of one tool call can come before the next
	// block of the same response. What the agent said before a tool call is therefore what
	// that response said before it.
	said := make(map[string]string)       // by message id
	notes := make(map[string]string)      // by tool_use id
	models := make(map[string]string)     // by tool_use id
	calls := make(map[string]claudeBlock) // Write and Edit calls not answered yet, by tool_use id
	var start time.Time
	for _, raw := range records {
		var record claudeRecord
		if json.Unmarshal(raw, &record) != nil {
			continue
		}
		t := parseTime(record.Timestamp)
		if start.IsZero() {
			start = t // a fork starts with records of its own before the ones it copied
		}
		session, parent := record.SessionID, ""
		if record.IsSidechain {
			session, parent = record.AgentID, record.SessionID
		}
		switch {
		case record.Type == "assistant":
			if s := text(record.Message.Content); s != "" && len(turns[session]) > 0 {
				turns[session][len(turns[session])-1].reply = s
			}
			var blocks []claudeBlock
			_ = json.Unmarshal(record.Message.Content, &blocks) // content that is a string has no blocks
			id := record.Message.ID
			for _, b := range blocks {
				switch b.Type {
				case "text":
					said[id] = addText(said[id], b.Text)
				case "tool_use":
					notes[b.ID] = said[id]
					if model := record.Message.Model; model != "<synthetic>" { // a message Claude Code made up
						models[b.ID] = model
					}
					if b.Name == "Write" || b.Name == "Edit" {
						calls[b.ID] = b
					}
				}
			}
		case record.Type != "user":
		default:
			var blocks []claudeBlock
			_ = json.Unmarshal(record.Message.Content, &blocks) // content that is a string has no blocks
			e := Edit{
				Agent: "claude", Session: session, Parent: parent, Turn: len(turns[session]),
				Time: t, Branch: record.GitBranch, Cwd: record.Cwd, start: start,
			}
			// A Write or an Edit logs what it did in toolUseResult. In a subagent's log Claude
			// Code 2.1.295 logs no toolUseResult, and then the call a result answers says
			// what changed, if the call was allowed and did not fail.
			hasResult := len(record.ToolUseResult) > 0 && record.ToolUseResult[0] == '{'
			for _, b := range blocks {
				if b.Type != "tool_result" {
					continue
				}
				e.Note, e.Model = notes[b.ToolUseID], models[b.ToolUseID]
				call, ok := calls[b.ToolUseID]
				delete(calls, b.ToolUseID) // a call is answered once
				if !ok || hasResult || b.IsError || record.PermissionDecision.Decision != "accept" {
					continue
				}
				if edit, ok := claudeEdit(e, claudeCallResult(call)); ok {
					edits = append(edits, edit)
				}
			}
			switch {
			case hasResult:
				var result claudeResult
				if json.Unmarshal(record.ToolUseResult, &result) == nil {
					if edit, ok := claudeEdit(e, result); ok {
						edits = append(edits, edit)
					}
				}
			case len(record.ToolUseResult) == 0 || string(record.ToolUseResult) == "null":
				if s, ok := claudePrompt(record); ok {
					turns[session] = append(turns[session], turn{prompt: s})
				}
			}
		}
	}
	return finish(edits, turns)
}

// claudeEdit returns e with the change result says a Write or an Edit made, and whether
// result says one.
func claudeEdit(e Edit, result claudeResult) (Edit, bool) {
	e.Path = result.FilePath
	switch {
	case e.Path == "":
		return e, false
	case result.Content != nil && (result.Type == "create" || result.Type == "update"):
		if result.Type == "update" {
			e.Before = result.OriginalFile
		}
		e.Apply = whole(*result.Content)
		e.key = keyOf(e.Agent, e.Time, e.Path, *result.Content)
	case result.OldString != nil && result.NewString != nil:
		e.Before = result.OriginalFile
		e.Apply = replace(*result.OldString, *result.NewString, result.ReplaceAll)
		e.key = keyOf(e.Agent, e.Time, e.Path, *result.OldString, *result.NewString, strconv.FormatBool(result.ReplaceAll))
	default:
		return e, false
	}
	return e, true
}

// claudeCallResult returns the toolUseResult Claude Code would log for call, a Write or an
// Edit, short of the file before the call, which only a toolUseResult holds. A Write then
// reads like one that replaced a file the log does not hold, so the same Write in a log
// that has its toolUseResult gets the same key.
func claudeCallResult(call claudeBlock) claudeResult {
	var input claudeInput
	if json.Unmarshal(call.Input, &input) != nil {
		return claudeResult{}
	}
	if call.Name == "Write" {
		return claudeResult{Type: "update", FilePath: input.FilePath, Content: input.Content}
	}
	return claudeResult{FilePath: input.FilePath, OldString: input.OldString, NewString: input.NewString, ReplaceAll: input.ReplaceAll}
}

// claudePrompt returns the request in a user record. A record Claude Code added itself,
// such as a note, a background task's notice, the output of a command or a mark that the
// person interrupted, is not a request. A slash command is, written as "/name args".
func claudePrompt(r claudeRecord) (string, bool) {
	if r.IsMeta || r.IsCompactSummary || r.IsVisibleInTranscriptOnly {
		return "", false
	}
	s := text(r.Message.Content)
	if strings.HasPrefix(s, "<command-name>") || strings.HasPrefix(s, "<command-message>") {
		name := between(s, "<command-name>", "</command-name>")
		return strings.TrimSpace(name + " " + between(s, "<command-args>", "</command-args>")), name != ""
	}
	for _, p := range []string{"<task-notification>", "<local-command-", "<bash-", "[Request interrupted"} {
		if strings.HasPrefix(s, p) {
			return "", false
		}
	}
	return s, s != ""
}

func between(s, start, end string) string {
	_, rest, _ := strings.Cut(s, start)
	in, _, _ := strings.Cut(rest, end)
	return strings.TrimSpace(in)
}

// askClaude asks Claude Code: claude -p resumes the fork to continue, or a fork of the
// session, or starts a new session. The Read, Grep and Glob tools are all it gets, and
// --safe-mode turns hooks and MCP off. It prints one JSON object: the answer in result, the
// session that answered in session_id, and the models that answered as the keys of
// modelUsage. When is_error is set, result says why there is no answer.
func askClaude(ctx context.Context, _ Dirs, q Question) (Answer, error) {
	args := []string{"-p"}
	switch {
	case q.Fork != "":
		args = append(args, "--resume", q.Fork)
	case q.Session != "":
		args = append(args, "--resume", q.Session, "--fork-session")
	}
	args = append(args, "--safe-mode", "--tools", "Read,Grep,Glob")
	if q.Model != "" {
		args = append(args, "--model", q.Model)
	}
	args = append(args, "--output-format", "json")
	stdout, stderr, err := run(ctx, "claude", q.Cwd, q.Prompt, args)
	var out struct {
		IsError    bool            `json:"is_error"`
		Result     string          `json:"result"`
		SessionID  string          `json:"session_id"`
		ModelUsage json.RawMessage `json:"modelUsage"`
	}
	read := json.Unmarshal(stdout, &out) == nil
	text := strings.TrimSpace(out.Result)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return Answer{}, notInstalled("claude")
	case read && out.IsError && text != "":
		return Answer{}, errors.New(cut(text))
	case err != nil:
		return Answer{}, failed("claude", err, stderr)
	case !read || out.IsError || text == "":
		return Answer{}, errors.New("claude gave no answer")
	}
	return Answer{Fork: out.SessionID, Text: text, Model: answerModel(out.ModelUsage, q.Model)}, nil
}

// openClaude returns the arguments that open a fork of the session in Claude Code: claude
// --resume with --fork-session, with the session's model when it is known. Claude Code has
// no flag for the reasoning effort.
func openClaude(q Question) []string {
	args := []string{"--resume", q.Session, "--fork-session"}
	if q.Model != "" {
		args = append(args, "--model", q.Model)
	}
	return args
}

// answerModel returns which of the models in usage, the modelUsage of Claude's output,
// answered: model when it is one of them, or else the first that contains model ignoring
// case, since an alias such as sonnet names its family, or else the first.
func answerModel(usage json.RawMessage, model string) string {
	d := json.NewDecoder(bytes.NewReader(usage))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return ""
	}
	first, family := "", ""
	for d.More() {
		t, err := d.Token()
		if err != nil {
			break
		}
		key, _ := t.(string)
		if key == model {
			return key
		}
		if first == "" {
			first = key
		}
		if family == "" && model != "" && strings.Contains(strings.ToLower(key), strings.ToLower(model)) {
			family = key
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			break
		}
	}
	return cmp.Or(family, first)
}

// claudeModels returns the aliases claude --model takes for the latest model of each
// family. Claude Code has no command that lists its models.
func claudeModels(context.Context, Dirs) ([]Model, error) {
	return []Model{
		{ID: "fable", Name: "Latest Fable"},
		{ID: "opus", Name: "Latest Opus"},
		{ID: "sonnet", Name: "Latest Sonnet"},
		{ID: "haiku", Name: "Latest Haiku"},
	}, nil
}
