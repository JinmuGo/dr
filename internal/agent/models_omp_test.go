//go:build unix

package agent

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ompModelList is what omp models --json prints: chat models of two providers, with an
// embedding and an image model between them.
const ompModelList = `{"models":[` +
	`{"provider":"anthropic","kind":"chat","id":"claude-opus-5-5","selector":"anthropic/claude-opus-5-5","name":"Claude Opus 5.5","contextWindow":1000000},` +
	`{"provider":"openai","kind":"embedding","id":"text-embedding-4","selector":"openai/text-embedding-4","name":"Text Embedding 4"},` +
	`{"provider":"openai-codex","kind":"chat","id":"gpt-6-luna","selector":"openai-codex/gpt-6-luna","name":"GPT-6-Luna"},` +
	`{"provider":"openai","kind":"image","id":"gpt-image-3","selector":"openai/gpt-image-3","name":"GPT Image 3"},` +
	`{"provider":"anthropic","kind":"chat","id":"claude-haiku-5-5","selector":"anthropic/claude-haiku-5-5","name":"Claude Haiku 5.5"}` +
	`]}` + "\n"

func TestModelsOMP(t *testing.T) {
	dir := fakeAgents(t, map[string]string{"stdout": ompModelList})
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Models(context.Background(), Dirs{Home: home, State: t.TempDir()}, "omp")
	if err != nil {
		t.Fatal(err)
	}
	// Newest first by the numbers in the ids; the two 5.5 models keep omp's order.
	want := []Model{
		{ID: "openai-codex/gpt-6-luna", Name: "GPT-6-Luna"},
		{ID: "anthropic/claude-opus-5-5", Name: "Claude Opus 5.5"},
		{ID: "anthropic/claude-haiku-5-5", Name: "Claude Haiku 5.5"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := recorded(t, dir, "args"), "models\n--json\n"; got != want {
		t.Errorf("got arguments %q, want %q", got, want)
	}
	if got := strings.TrimSpace(recorded(t, dir, "cwd")); got != home {
		t.Errorf("ran in %s, want the home directory %s", got, home)
	}
}

func TestModelsOMPFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		files   map[string]string
		wantErr string // what the error starts with
	}{
		{
			name:    "omp exits with an error",
			files:   map[string]string{"stderr": "loading\nError: no model registry\n", "exit": "1"},
			wantErr: "Error: no model registry",
		},
		{
			name:    "omp exits with nothing on stderr",
			files:   map[string]string{"exit": "2"},
			wantErr: "omp: exit status 2",
		},
		{
			name:    "omp prints what is not JSON",
			files:   map[string]string{"stdout": "anthropic/claude-opus-5-5\n"},
			wantErr: "read omp's model list: ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAgents(t, tc.files)
			_, err := Models(context.Background(), Dirs{Home: t.TempDir()}, "omp")
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
				t.Errorf("got error %v, want one starting %q", err, tc.wantErr)
			}
		})
	}
}

func TestModelsOMPMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Models(context.Background(), Dirs{Home: t.TempDir()}, "omp")
	if want := "omp is not installed or not in PATH"; err == nil || err.Error() != want {
		t.Errorf("got error %v, want %q", err, want)
	}
}
