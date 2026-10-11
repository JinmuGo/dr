package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestModelsClaude(t *testing.T) {
	// Claude Code lists no models, so these are the aliases of the latest of each family.
	t.Setenv("PATH", t.TempDir())
	got, err := Models(context.Background(), Dirs{Home: t.TempDir()}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	want := []Model{
		{ID: "fable", Name: "Latest Fable"},
		{ID: "opus", Name: "Latest Opus"},
		{ID: "sonnet", Name: "Latest Sonnet"},
		{ID: "haiku", Name: "Latest Haiku"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestModelsCodex(t *testing.T) {
	const cache = ".codex/models_cache.json"
	// The fixture lists hidden models and ties of priority out of order.
	byPriority := []Model{
		{ID: "gpt-6.1-sol", Name: "GPT-6.1-Sol"},
		{ID: "gpt-6.1-sol-codex", Name: "GPT-6.1-Sol-Codex"},
		{ID: "gpt-6-luna", Name: "GPT-6-Luna"},
		{ID: "gpt-6-mini", Name: "GPT-6-Mini"},
	}
	for _, tc := range []struct {
		name      string
		home      map[string]string // files under the home directory
		codexHome map[string]string // files under $CODEX_HOME, which is unset when nil
		want      []Model
		wantErr   string // what the error starts with
	}{
		{
			name: "the cache in the home directory",
			home: map[string]string{cache: fixture(t, "codex-models_cache.json")},
			want: byPriority,
		},
		{
			name:      "CODEX_HOME wins over the home directory",
			home:      map[string]string{cache: fixture(t, "codex-models_cache.json")},
			codexHome: map[string]string{"models_cache.json": `{"models":[{"slug":"gpt-7","display_name":"GPT-7","visibility":"list","priority":1}]}`},
			want:      []Model{{ID: "gpt-7", Name: "GPT-7"}},
		},
		{
			name:      "no cache in CODEX_HOME",
			home:      map[string]string{cache: fixture(t, "codex-models_cache.json")},
			codexHome: map[string]string{},
			wantErr:   "codex has no model list yet; run codex once",
		},
		{
			name:    "no cache",
			home:    map[string]string{},
			wantErr: "codex has no model list yet; run codex once",
		},
		{
			name:    "a cache that is not JSON",
			home:    map[string]string{cache: "<html>"},
			wantErr: "read codex's model list: ",
		},
		{
			name: "no visible models",
			home: map[string]string{cache: `{"models":[{"slug":"codex-auto-review","visibility":"hide","priority":0}]}`},
			want: []Model{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEX_HOME", "")
			if tc.codexHome != nil {
				t.Setenv("CODEX_HOME", fakeHome(t, tc.codexHome))
			}
			got, err := Models(context.Background(), Dirs{Home: fakeHome(t, tc.home)}, "codex")
			if tc.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Errorf("got error %v, want one starting %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModelsCodexUnreadable(t *testing.T) {
	// A directory where the cache should be cannot be read as a file.
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".codex", "models_cache.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Models(context.Background(), Dirs{Home: home}, "codex")
	if want := "read codex's model list: "; err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("got error %v, want one starting %q", err, want)
	}
}

func TestModelsUnknown(t *testing.T) {
	_, err := Models(context.Background(), Dirs{Home: t.TempDir()}, "gemini")
	if want := "dr cannot ask gemini sessions"; err == nil || err.Error() != want {
		t.Errorf("got error %v, want %q", err, want)
	}
}

func TestNewerModel(t *testing.T) {
	// omp's alphabetical order; ties (the two 5.1 models) must keep it.
	ids := []string{
		"anthropic/claude-3-5-sonnet-20240620",
		"anthropic/claude-fable-5-1",
		"anthropic/claude-haiku-5-1",
		"anthropic/claude-opus-4-1",
		"anthropic/claude-opus-5-5",
		"openai/gpt-5.6-sol",
		"openai/gpt-6-astra",
		"openai/gpt-6.1-sol",
	}
	slices.SortStableFunc(ids, newerModel)
	want := []string{
		"openai/gpt-6.1-sol",
		"openai/gpt-6-astra",
		"openai/gpt-5.6-sol",
		"anthropic/claude-opus-5-5",
		"anthropic/claude-fable-5-1",
		"anthropic/claude-haiku-5-1",
		"anthropic/claude-opus-4-1",
		"anthropic/claude-3-5-sonnet-20240620",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("got %v, want %v", ids, want)
	}
}

func TestAnswerModel(t *testing.T) {
	const usage = `{"claude-opus-5-5":{"inputTokens":1},"claude-sonnet-5-5":{"inputTokens":2}}`
	for _, tc := range []struct {
		model, want string
	}{
		{model: "sonnet", want: "claude-sonnet-5-5"},
		{model: "Sonnet", want: "claude-sonnet-5-5"},
		{model: "claude-sonnet-5-5", want: "claude-sonnet-5-5"},
		{model: "claude-opus-5-5", want: "claude-opus-5-5"},
		{model: "haiku", want: "claude-opus-5-5"},
		{model: "", want: "claude-opus-5-5"},
	} {
		if got := answerModel([]byte(usage), tc.model); got != tc.want {
			t.Errorf("answerModel(%q): got %q, want %q", tc.model, got, tc.want)
		}
	}
	// An exact match wins over an earlier key that contains it.
	const both = `{"claude-sonnet-5-5-beta":{},"claude-sonnet-5-5":{}}`
	if got, want := answerModel([]byte(both), "claude-sonnet-5-5"), "claude-sonnet-5-5"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
