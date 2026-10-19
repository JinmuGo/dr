//go:build unix

package agent

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// piModelList is what pi --list-models prints: a table of models, by provider and then
// model alphabetically.
const piModelList = `provider      model              context  max-out  thinking  images
anthropic     claude-haiku-5-5   1M       128K     yes       yes
anthropic     claude-opus-5-5    1M       128K     yes       yes
openai-codex  gpt-6-luna         1M       128K     yes       yes
`

func TestModelsPi(t *testing.T) {
	dir := fakeAgents(t, map[string]string{"stdout": piModelList, "stderr": `Warning: No models match pattern "claude-bridge/claude-opus-5-5"` + "\n"})
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Models(context.Background(), Dirs{Home: home, State: t.TempDir()}, "pi")
	if err != nil {
		t.Fatal(err)
	}
	// Newest first by the numbers in the ids; the two 5.5 models keep pi's order.
	want := []Model{{ID: "openai-codex/gpt-6-luna"}, {ID: "anthropic/claude-haiku-5-5"}, {ID: "anthropic/claude-opus-5-5"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Without extensions, as a question runs, so the list has only the models it can use.
	if got, want := recorded(t, dir, "args"), "--list-models\n--no-extensions\n--offline\n"; got != want {
		t.Errorf("got arguments %q, want %q", got, want)
	}
	if got := strings.TrimSpace(recorded(t, dir, "cwd")); got != home {
		t.Errorf("ran in %s, want the home directory %s", got, home)
	}
}

func TestModelsPiFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   map[string]string
		wantErr string // what the error starts with
	}{
		{
			name:    "pi exits with an error",
			files:   map[string]string{"stderr": "Error: no model registry\n", "exit": "1"},
			wantErr: "Error: no model registry",
		},
		{
			name:    "pi prints what is not its table",
			files:   map[string]string{"stdout": `{"models":[]}` + "\n"},
			wantErr: "read pi's model list: ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAgents(t, tc.files)
			_, err := Models(context.Background(), Dirs{Home: t.TempDir()}, "pi")
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
				t.Errorf("got error %v, want one starting %q", err, tc.wantErr)
			}
		})
	}
}
