package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JinmuGo/dr/internal/state"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/blame")

// spike is where most sessions in internal/agent/testdata worked, and e2e where the Claude
// Code session with a subagent did. TestBlame moves them to a new repo.
const (
	spike = "/home/dev/Codes/personal/dr-spike2"
	e2e   = "/home/dev/Codes/personal/dr-k-e2e"
)

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestBlame runs dr blame in a repo the sessions in internal/agent/testdata worked in and
// compares what it writes with testdata/blame/<case>.golden. go test -run TestBlame
// -update rewrites the golden files.
func TestBlame(t *testing.T) {
	goldenDir, err := filepath.Abs(filepath.Join("testdata", "blame"))
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, home := filepath.Join(tmp, "repo"), filepath.Join(tmp, "home")
	for k, v := range map[string]string{
		"HOME":              home, // where dr finds the session logs
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
		// The base commit comes before the sessions.
		"GIT_AUTHOR_DATE": "2026-10-09T12:00:00Z", "GIT_COMMITTER_DATE": "2026-10-09T12:00:00Z",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "omp/notes.md"), "a\nold\nc\n")
	writeFile(t, filepath.Join(repo, "omp/other.md"), "x\n")
	writeFile(t, filepath.Join(repo, "gone.md"), "gone\n")
	writeFile(t, filepath.Join(repo, "fixed.md"), "one\ntwo\n")
	runGit(t, repo, "init", "-q", "-b", "main")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")

	// What the agents left, with one line a person changed afterwards.
	claude := readFile(t, "internal/review/testdata/claude-ratelimit.go")
	writeFile(t, filepath.Join(repo, "claude/ratelimit.go"), strings.Replace(claude, "\tif n <= 0 {\n", "\tif n < 1 {\n", 1))
	writeFile(t, filepath.Join(repo, "codex/ratelimit.go"), readFile(t, "internal/review/testdata/codex-ratelimit.go"))
	writeFile(t, filepath.Join(repo, "omp/notes.md"), "a\nB\nc\nd\n")
	writeFile(t, filepath.Join(repo, "omp/other.md"), "y\nz\n")
	writeFile(t, filepath.Join(repo, "claude-subagent/set.go"), readFile(t, "internal/review/testdata/claude-set.go"))
	// What a person did outside the sessions.
	if err := os.Remove(filepath.Join(repo, "gone.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "image.bin"), "\x89PNG\x00\x01")
	writeFile(t, filepath.Join(repo, "escape.txt"), "\x1b]0;title\x07\tred\x1b[31m\u009b2J\u202eevil\n")
	// Two fixes dr applied in turn for comments to the Codex session.
	writeFile(t, filepath.Join(repo, "fixed.md"), "one\nTWO\nthree\n")
	fixAt := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	for _, r := range []state.Record{
		state.Applied{
			Time: fixAt, ID: "00000000000000f1", Text: "make it loud", Agent: "codex", Session: "01a1209b-a5af-7992-ba5a-c5fdd6a39255",
			Fork: "fork-1", Path: "fixed.md", First: 2, Old: []string{"two"}, New: []string{"TWO", "2b"},
		},
		state.Applied{
			Time: fixAt.Add(time.Minute), ID: "00000000000000f2", Text: "spell the number", Note: "three is the next one.",
			Agent: "codex", Session: "01a1209b-a5af-7992-ba5a-c5fdd6a39255", Fork: "fork-1", Path: "fixed.md", First: 3,
			Old: []string{"2b"}, New: []string{"three"},
		},
	} {
		if err := state.NewLog(filepath.Join(repo, ".git", "dr", "log.jsonl")).Append(r); err != nil {
			t.Fatal(err)
		}
	}
	moves := strings.NewReplacer(spike, repo, e2e, filepath.Join(repo, "claude-subagent"))
	for dest, name := range map[string]string{
		".claude/projects/spike/ec6e8b8a.jsonl":                             "claude-ec6e8b8a-34cf-47ad-b851-09985c776249.jsonl",
		".claude/projects/spike/fe5968c4-0984-42cd-ab47-059e1d663eef.jsonl": "claude-fe5968c4-0984-42cd-ab47-059e1d663eef.jsonl",
		".claude/projects/e2e/d3598b6f.jsonl":                               "claude-d3598b6f-f161-41e2-87a0-eeffe6014022.jsonl",
		".claude/projects/e2e/d3598b6f/subagents/agent-aefd1f72.jsonl":      "claude-aefd1f72d41d72e65.jsonl",
		".codex/sessions/2026/10/09/rollout-1.jsonl":                        "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl",
		".omp/agent/sessions/spike/1.jsonl":                                 "omp-01a1f000-0000-7000-8000-000000000001.jsonl",
		".omp/agent/sessions/spike/1/0-Subagent.jsonl":                      "omp-01a1f000-0000-7000-8000-000000000002.jsonl",
	} {
		log := readFile(t, filepath.Join("internal/agent/testdata", name))
		writeFile(t, filepath.Join(home, dest), moves.Replace(log))
	}

	t.Chdir(repo)
	for _, tc := range []struct{ name, path string }{
		{"claude", "claude/ratelimit.go"}, // with a line a person changed
		{"codex", "codex/ratelimit.go"},
		{"omp", "omp/notes.md"}, // with a line it deleted
		{"subagent", "omp/other.md"},
		{"claude-subagent", "claude-subagent/set.go"}, // its log has no toolUseResult
		{"deleted", "gone.md"},
		{"fix", "fixed.md"}, // with two fixes dr applied
		{"binary", "image.bin"},
		{"escape", "escape.txt"},
		{"outside", "../outside.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := blame(&out, tc.path); err != nil {
				fmt.Fprintf(&out, "dr: %v\n", err) // what run writes for the error
			}
			// The worktree is a new directory each time.
			got := bytes.ReplaceAll(out.Bytes(), []byte(repo), []byte("$REPO"))
			path := filepath.Join(goldenDir, tc.name+".golden")
			if *update {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (go test -run TestBlame -update writes it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("dr blame %s: got\n%s\nwant (%s)\n%s", tc.path, got, path, want)
			}
		})
	}
}
