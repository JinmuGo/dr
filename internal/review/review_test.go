package review_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/origin"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
)

// spike is where the sessions in agent/testdata worked. The test moves them to a new repo.
const spike = "/home/dev/Codes/personal/dr-spike2"

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

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// tag says who made a changed line: "claude #1", "?" for no origin, "" for a line that
// did not change.
func tag(l origin.Line) string {
	switch {
	case l.Old > 0 && l.New > 0:
		return ""
	case l.Edit == nil:
		return "?"
	}
	return fmt.Sprintf("%s #%d", l.Edit.Agent, l.Edit.Turn)
}

func TestRepo(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir()) // git reports paths with symlinks resolved
	if err != nil {
		t.Fatal(err)
	}
	repo, home := filepath.Join(tmp, "repo"), filepath.Join(tmp, "home")
	for k, v := range map[string]string{
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
	writeFile(t, filepath.Join(repo, "moved.md"), "moved\n")
	writeFile(t, filepath.Join(repo, "empty/.gitkeep"), "")
	writeFile(t, filepath.Join(repo, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(repo, "linked/a.go"), "package a\n")
	writeFile(t, filepath.Join(repo, "filed/a.go"), "package a\n")
	// A repository the branch tracks as a submodule, and a symlink to a directory.
	writeFile(t, filepath.Join(repo, "sub/README"), "sub\n")
	git(t, filepath.Join(repo, "sub"), "init", "-q", "-b", "main")
	git(t, filepath.Join(repo, "sub"), "add", ".")
	git(t, filepath.Join(repo, "sub"), "commit", "-q", "-m", "sub")
	if err := os.Symlink("omp", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")

	// What the agents left, with one line a person changed afterwards.
	claude := readFile(t, "testdata/claude-ratelimit.go")
	const agentLine, humanLine = "\tif n <= 0 {\n", "\tif n < 1 {\n"
	if !strings.Contains(claude, agentLine) {
		t.Fatalf("testdata/claude-ratelimit.go has no %q", agentLine)
	}
	writeFile(t, filepath.Join(repo, "claude/ratelimit.go"), strings.Replace(claude, agentLine, humanLine, 1))
	writeFile(t, filepath.Join(repo, "codex/ratelimit.go"), readFile(t, "testdata/codex-ratelimit.go"))
	writeFile(t, filepath.Join(repo, "omp/notes.md"), "a\nB\nc\nd\n")
	writeFile(t, filepath.Join(repo, "omp/other.md"), "y\nz\n")
	// What a person did outside the sessions.
	for _, path := range []string{"gone.md", "empty/.gitkeep"} {
		if err := os.Remove(filepath.Join(repo, path)); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "mv", "moved.md", "renamed.md")
	writeFile(t, filepath.Join(repo, "debug.log"), "ignored\n")
	writeFile(t, filepath.Join(repo, "image.bin"), "\x89PNG\x00\x01")
	// Paths that are not files: the symlink points to another directory, the submodule is
	// on a new commit, and a repository the branch does not track is in the worktree.
	if err := os.Remove(filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("claude", filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, filepath.Join(repo, "sub"), "commit", "-q", "--allow-empty", "-m", "next")
	git(t, repo, "init", "-q", "nested")
	// Directories that are no longer directories: one is a symlink to a directory outside
	// with a file of the same name, the other a file.
	outside := filepath.Join(tmp, "outside")
	writeFile(t, filepath.Join(outside, "a.go"), "package outside\n")
	for _, dir := range []string{"linked", "filed"} {
		if err := os.RemoveAll(filepath.Join(repo, dir)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(repo, "linked")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "filed"), "now a file\n")

	const fork = "fe5968c4-0984-42cd-ab47-059e1d663eef"
	for dest, name := range map[string]string{
		".claude/projects/spike/ec6e8b8a.jsonl":        "claude-ec6e8b8a-34cf-47ad-b851-09985c776249.jsonl",
		".claude/projects/spike/" + fork + ".jsonl":    "claude-" + fork + ".jsonl",
		".codex/sessions/2026/10/09/rollout-1.jsonl":   "codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl",
		".omp/agent/sessions/spike/1.jsonl":            "omp-01a1f000-0000-7000-8000-000000000001.jsonl",
		".omp/agent/sessions/spike/1/0-Subagent.jsonl": "omp-01a1f000-0000-7000-8000-000000000002.jsonl",
	} {
		log := readFile(t, filepath.Join("../agent/testdata", name))
		writeFile(t, filepath.Join(home, dest), strings.ReplaceAll(log, spike, repo))
	}

	r, err := review.Open(repo, home, "", &agent.Loader{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Root != repo {
		t.Errorf("root: got %q, want %q", r.Root, repo)
	}
	file := func(t *testing.T, path string) review.File {
		t.Helper()
		f, err := r.File(path)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}

	t.Run("changed", func(t *testing.T) {
		got, err := r.Changed()
		if err != nil {
			t.Fatal(err)
		}
		// A rename is the old path and the new one; the ignored debug.log is not there. The
		// untracked repository is its directory, without the slash git lists it with.
		want := []string{
			"claude/ratelimit.go", "codex/ratelimit.go", "empty/.gitkeep", "filed", "filed/a.go", "gone.md",
			"image.bin", "link", "linked", "linked/a.go", "moved.md", "nested", "omp/notes.md", "omp/other.md",
			"renamed.md", "sub",
		}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("notes", func(t *testing.T) {
		lines := file(t, "omp/notes.md").Lines
		got := make([]string, 0, len(lines))
		for _, l := range lines {
			got = append(got, strings.TrimSpace(tag(l)+" "+l.Text))
		}
		// The write claims only the line it changed, and deletes the base line it replaced.
		want := []string{"a", "omp #1 old", "omp #2 B", "c", "omp #3 d"}
		if !slices.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("subagent", func(t *testing.T) {
		lines := file(t, "omp/other.md").Lines
		if len(lines) != 3 || tag(lines[0]) != "omp #3" || tag(lines[1]) != "omp #3" || tag(lines[2]) != "omp #1" ||
			lines[2].Edit.Parent != lines[0].Edit.Session {
			t.Errorf("got %+v, want omp #3, omp #3 and omp #1 from a subagent of the first line's session", lines)
		}
	})

	for _, tc := range []struct{ path, unknown string }{
		{"claude/ratelimit.go", strings.TrimSuffix(humanLine, "\n")},
		{"codex/ratelimit.go", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var unknown []string
			for _, l := range file(t, tc.path).Lines {
				switch {
				case l.Edit == nil && l.New > 0:
					unknown = append(unknown, l.Text)
				case l.Edit == nil:
					t.Errorf("line %q was deleted", l.Text)
				case l.Edit.Session == fork:
					t.Errorf("line %q comes from the fork, not the session it copied", l.Text)
				case strings.Contains(l.Text, "func New(") && l.Edit.Turn != 1,
					strings.Contains(l.Text, "AllowN(n int) bool {") && l.Edit.Turn != 2:
					// The first request wrote the limiter, the second added AllowN.
					t.Errorf("line %q comes from request %d", l.Text, l.Edit.Turn)
				}
			}
			if strings.Join(unknown, "\n") != tc.unknown {
				t.Errorf("lines without an origin: got %q, want %q", unknown, tc.unknown)
			}
		})
	}

	t.Run("deleted", func(t *testing.T) {
		if lines := file(t, "gone.md").Lines; len(lines) != 1 || lines[0].Old != 1 || lines[0].New != 0 || lines[0].Edit != nil {
			t.Errorf("gone.md: got %+v, want its one line deleted without an origin", lines)
		}
		if f := file(t, "empty/.gitkeep"); len(f.Lines) != 0 || f.NoDiff != "" {
			t.Errorf("empty/.gitkeep: got %+v, want no lines", f)
		}
		if _, err := r.File("missing.md"); err == nil {
			t.Error("missing.md: got no error, want one for a file in neither base nor the worktree")
		}
	})

	t.Run("binary", func(t *testing.T) {
		if f := file(t, "image.bin"); f.NoDiff != review.Binary || f.Lines != nil {
			t.Errorf("got %+v, want a binary file without lines", f)
		}
	})

	t.Run("not a file", func(t *testing.T) {
		// git stores a symlink as the path it points to, and so does dr.
		want := []origin.Line{{Text: "omp", Old: 1}, {Text: "claude", New: 1}}
		if got := file(t, "link").Lines; !slices.Equal(got, want) {
			t.Errorf("link: got %+v, want %+v", got, want)
		}
		for _, path := range []string{"nested", "sub"} {
			if f := file(t, path); f.NoDiff != review.Directory || f.Lines != nil {
				t.Errorf("%s: got %+v, want a directory without lines", path, f)
			}
		}
	})

	t.Run("under a symlink or a file", func(t *testing.T) {
		// Like git, dr finds no file under a symlink or a file, though the symlink leads to one.
		want := []origin.Line{{Text: "package a", Old: 1}}
		for _, path := range []string{"linked/a.go", "filed/a.go"} {
			if got := file(t, path).Lines; !slices.Equal(got, want) {
				t.Errorf("%s: got %+v, want %+v", path, got, want)
			}
		}
		// The symlink and the file are new paths of their own.
		for path, text := range map[string]string{"linked": outside, "filed": "now a file"} {
			if got, want := file(t, path).Lines, []origin.Line{{Text: text, New: 1}}; !slices.Equal(got, want) {
				t.Errorf("%s: got %+v, want %+v", path, got, want)
			}
		}
	})
}

func TestStatus(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "emptied.go"), "package a\n")
	writeFile(t, filepath.Join(repo, "filled.go"), "")
	writeFile(t, filepath.Join(repo, "gone-empty"), "")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	writeFile(t, filepath.Join(repo, "emptied.go"), "")
	writeFile(t, filepath.Join(repo, "filled.go"), "package a\n")
	if err := os.Remove(filepath.Join(repo, "gone-empty")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "new-empty"), "")
	writeFile(t, filepath.Join(repo, "new.bin"), "\x00\x01")

	r, err := review.Open(repo, t.TempDir(), "", &agent.Loader{}) // a home without session logs
	if err != nil {
		t.Fatal(err)
	}
	// The status says whether the file is there in the base commit and in the worktree, not
	// what its lines did.
	for _, tc := range []struct {
		path   string
		status byte
	}{
		{"new-empty", 'A'},
		{"gone-empty", 'D'},
		{"emptied.go", 'M'},
		{"filled.go", 'M'},
		{"new.bin", 'A'},
	} {
		f, err := r.File(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if f.Status != tc.status {
			t.Errorf("%s: got status %q, want %q", tc.path, f.Status, tc.status)
		}
	}
}

func TestRepoLazyLogs(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
	} {
		t.Setenv(k, v)
	}
	writeFile(t, filepath.Join(repo, "a.md"), "a\n")
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	// A broken dr record is available immediately; agent problems wait for a file request.
	fixLog := state.LogPath(filepath.Join(root, ".git", state.DirName))
	writeFile(t, fixLog, "not json\n")
	log := filepath.Join(home, ".codex/sessions/bad.jsonl")
	writeFile(t, log, "not json, from "+root+"\n")
	r, err := review.Open(repo, home, "", &agent.Loader{})
	if err != nil {
		t.Fatal(err)
	}
	checkFixProblem := func() {
		t.Helper()
		if len(r.Problems) != 1 || r.Problems[0].Agent != review.FixAgent || r.Problems[0].Path != fixLog {
			t.Fatalf("problems before file request: got %+v, want only %s", r.Problems, fixLog)
		}
	}
	checkFixProblem()
	paths, err := r.Changed()
	if err != nil || len(paths) != 0 {
		t.Fatalf("Changed: got %q, %v, want no paths or error", paths, err)
	}
	for _, empty := range [][]string{nil, {}} {
		files, err := r.Files(empty)
		if err != nil || len(files) != 0 {
			t.Fatalf("Files(%q): got %+v, %v, want no files or error", empty, files, err)
		}
		checkFixProblem()
	}
	for range 2 {
		if _, err := r.File("a.md"); err != nil {
			t.Fatal(err)
		}
		if len(r.Problems) != 2 || r.Problems[0].Agent != "codex" || r.Problems[0].Path != log ||
			r.Problems[1].Agent != review.FixAgent {
			t.Fatalf("problems after file request: got %+v, want codex then dr without duplicates", r.Problems)
		}
	}
}
