package agent

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// piModelsTimeout is how long piModels waits for pi to list its models.
const piModelsTimeout = 20 * time.Second

// pi is Pi. Its logs are in session format 3, like OMP's, and hold the OMP sessions Pi
// imported too. It forks a session from its log and keeps the logs of the sessions dr
// starts in the state directory, so nothing changes under the home directory.
var pi = agent{
	name:    "pi",
	logs:    ".pi/agent/sessions",
	parse:   parsePi,
	command: "pi",
	canFork: func(e *Edit) error {
		if e.Log == "" {
			return errors.New("the session's log is not known")
		}
		return nil
	},
	ask:    askPi,
	open:   openPi,
	models: piModels,
}

// parsePi reads a Pi session log.
func parsePi(records *recordReader) []Edit {
	return parseSessionV3(records, "pi")
}

// piEdit reads what Pi logs for an edit of path: the unified diff in details.patch. It
// returns nil when path is not known, and an edit dr cannot read when the diff is not there.
func piEdit(e Edit, details []byte, path, cwd string) []Edit {
	if path == "" {
		return nil
	}
	var d struct {
		Patch string `json:"patch"`
	}
	_ = json.Unmarshal(details, &d)
	hunks, ok := unifiedHunks(d.Patch)
	if !ok {
		return []Edit{unread(e, "edit", path, cwd)}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	e.Path = filepath.Clean(path)
	e.Apply = patch(hunks)
	e.key = keyOf(e.Agent, e.Time, e.Path, d.Patch)
	return []Edit{e}
}

// fileToolCall finds a call of the edit or write tool in the code of a codemode call.
var fileToolCall = regexp.MustCompile(`\btools\.(edit|write)\s*\(`)

// mentionsFileTool reports whether the code of a codemode call seems to call the edit or
// write tool. It is a guess for a result that lists no calls.
func mentionsFileTool(args []byte) bool {
	var call struct{ Code string }
	return json.Unmarshal(args, &call) == nil && fileToolCall.MatchString(call.Code)
}

// quotedPath finds the path in arguments cut short, which are no longer JSON.
var quotedPath = regexp.MustCompile(`"(?:path|file_path)"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// codemodeEdits returns the edits a codemode call made through the edit and write tools.
// Pi logs only the codemode call, with each call its code made and arguments cut short, so
// dr cannot read the edits and names only their files. When the result lists no calls, a
// call whose code mentions the tools, as mentions says, has an edit of no file dr can tell.
func codemodeEdits(e Edit, details []byte, mentions bool, cwd string) []Edit {
	var d struct {
		Calls []struct {
			Name, Args, Status string
		}
	}
	if json.Unmarshal(details, &d) != nil || d.Calls == nil {
		if !mentions {
			return nil
		}
		return []Edit{unread(e, "codemode", "", cwd)}
	}
	var edits []Edit
	for _, c := range d.Calls {
		if c.Status != "ok" || c.Name != "edit" && c.Name != "write" {
			continue
		}
		paths := namedPaths([]byte(c.Args))
		if len(paths) == 0 {
			if m := quotedPath.FindStringSubmatch(c.Args); m != nil {
				var p string
				if json.Unmarshal([]byte(`"`+m[1]+`"`), &p) == nil && !strings.Contains(p, "://") {
					paths = []string{p}
				}
			}
		}
		if len(paths) == 0 {
			paths = []string{""}
		}
		for _, path := range paths {
			edits = append(edits, unread(e, "codemode", path, cwd))
		}
	}
	return edits
}

// askPi asks Pi: pi -p forks the session from its log with --fork, or goes on in the fork
// or the new session with --session, or starts a new session. The session that answers
// keeps its log under the state directory, which --session-dir names. It gets the read,
// grep and find tools, and no MCP servers, extensions, skills, prompt templates or
// project-local files, so it cannot write and runs no hooks. Without extensions, a model an
// extension provides is not there, so piModel picks the model, and Answer.Model names the
// one that answered. With --mode json Pi prints the same events as OMP.
func askPi(ctx context.Context, dirs Dirs, q Question) (Answer, error) {
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
		args = append(args, "--session", q.Fork)
	case q.Session != "":
		args = append(args, "--fork", q.Log)
	}
	args = append(args, "--session-dir", sessions, "--tools", "read,grep,find", "--no-mcp", "--no-extensions",
		"--no-skills", "--no-prompt-templates", "--no-approve", "--mode", "json")
	if model := piModel(ctx, dirs, q.Model); model != "" {
		args = append(args, "--model", model)
	}
	if q.Effort != "" {
		args = append(args, "--thinking", q.Effort)
	}
	stdout, stderr, err := run(ctx, "pi", q.Cwd, q.Prompt, args)
	session, answer, model := ompEvents(stdout)
	switch why := piError(stdout); {
	case errors.Is(err, exec.ErrNotFound):
		return Answer{}, notInstalled("pi")
	case err != nil:
		return Answer{}, failed("pi", err, stderr)
	case why != "":
		// The answer stopped on the error, so what text it has is not all of it.
		return Answer{}, errors.New(cut(why))
	case answer == "":
		return Answer{}, errors.New("pi gave no answer")
	}
	return Answer{Fork: cmp.Or(session, q.Fork), Text: answer, Model: model}, nil
}

// piModel returns the model to ask with: model when pi lists it without extensions, or
// else the first model of pi's settings it lists, the default and then the models enabled
// for cycling, so that pi does not fall back to a model of its own choosing. It returns ""
// when pi lists none of them, and model when pi cannot list its models, so pi says what is
// wrong with it.
func piModel(ctx context.Context, dirs Dirs, model string) string {
	models, err := piModels(ctx, dirs)
	if err != nil {
		return model
	}
	var settings struct {
		DefaultProvider string   `json:"defaultProvider"`
		DefaultModel    string   `json:"defaultModel"`
		EnabledModels   []string `json:"enabledModels"`
	}
	if data, err := os.ReadFile(filepath.Join(dirs.Home, ".pi", "agent", "settings.json")); err == nil {
		_ = json.Unmarshal(data, &settings) // settings dr cannot read name no model
	}
	for _, id := range append([]string{model, settings.DefaultProvider + "/" + settings.DefaultModel}, settings.EnabledModels...) {
		if slices.ContainsFunc(models, func(m Model) bool { return m.ID == id }) {
			return id
		}
	}
	return ""
}

// piError returns the error of the last assistant message the events of pi --mode json hold,
// such as a model the provider refused, "" when it has none.
func piError(stdout []byte) string {
	why := ""
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Role         string `json:"role"`
				ErrorMessage string `json:"errorMessage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &event) == nil && event.Type == "message_end" && event.Message.Role == "assistant" {
			why = strings.TrimSpace(event.Message.ErrorMessage)
		}
	}
	return why
}

// openPi returns the arguments that open a fork of the session in Pi: pi --fork with the
// session's log, with the session's model and thinking level when they are known.
func openPi(q Question) []string {
	args := []string{"--fork", q.Log}
	if q.Model != "" {
		args = append(args, "--model", q.Model)
	}
	if q.Effort != "" {
		args = append(args, "--thinking", q.Effort)
	}
	return args
}

// piModels returns the models pi --list-models prints as a table, as provider/model, newest
// first by the numbers in their ids (see newerModel) and in pi's order on ties. pi lists them
// alphabetically. Like a question, it runs without extensions, so it lists only the models
// a question can use. It gives up after piModelsTimeout.
func piModels(ctx context.Context, dirs Dirs) ([]Model, error) {
	runCtx, cancel := context.WithTimeout(ctx, piModelsTimeout)
	defer cancel()
	stdout, stderr, err := run(runCtx, "pi", dirs.Home, "", []string{"--list-models", "--no-extensions", "--offline"})
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return nil, notInstalled("pi")
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case runCtx.Err() != nil:
		return nil, errors.New("pi listed no models in 20 seconds")
	case err != nil:
		return nil, failed("pi", err, stderr)
	}
	var models []Model
	lines := bufio.NewScanner(bytes.NewReader(stdout))
	header := true
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		switch {
		case len(fields) < 2:
		case header:
			if fields[0] != "provider" || fields[1] != "model" {
				return nil, fmt.Errorf("read pi's model list: it starts with %q", cut(lines.Text()))
			}
			header = false
		default:
			models = append(models, Model{ID: fields[0] + "/" + fields[1]})
		}
	}
	if header {
		return nil, fmt.Errorf("read pi's model list: it has no table of models in %q", cut(string(stdout)))
	}
	slices.SortStableFunc(models, func(a, b Model) int { return newerModel(a.ID, b.ID) })
	return models, nil
}
