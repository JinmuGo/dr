package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The patterns below keep personal content out of the repository. Fixtures are trimmed from
// real session logs, which hold the maintainer's home directory, account IDs and email
// address, and the repository is public, so every file that can go public is checked. The
// [r] and [e] classes keep the patterns from matching their own text.

// nameEnd ends a path element: a slash, a backslash, a quote, a backtick or a space.
const nameEnd = `/\\"'` + "`" + `\s`

var (
	// linuxHome captures the user name after /home. Allowed are dev, the name fixtures are
	// rewritten to, and names starting with a dot, as in /home/.codex for tool directories
	// under a fake home.
	linuxHome = regexp.MustCompile(`/hom[e]/([^` + nameEnd + `]+)`)

	// otherHomes capture the name in the other forms session logs hold: macOS, Windows and
	// Claude Code's encoded project directory, where each / and . became a dash.
	otherHomes = []*regexp.Regexp{
		regexp.MustCompile(`/Use[r]s/([^` + nameEnd + `]+)`),
		regexp.MustCompile(`(?i)C:\\+Use[r]s\\+([^` + nameEnd + `]+)`),
		regexp.MustCompile(`-Use[r]s-([^-` + nameEnd + `]+)`),
	}

	// email captures the domain of an email address. Only the reserved example.com and
	// example.org are allowed.
	email = regexp.MustCompile(`[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,})`)

	// accountKey matches a JSON key that holds an account ID or an email address, also
	// escaped inside a JSON string. It applies only under testdata directories, where session
	// logs land; code may name these keys.
	accountKey = regexp.MustCompile(`\\*"(creator_user_id|creator_account_id|account_id|user_id|email)\\*"\s*:`)
)

// exampleName is the home directory name CONTRIBUTING.md uses to show what to replace.
const exampleName = "you"

// finding is one piece of personal content in a file.
type finding struct {
	path string
	line int
	what string
}

func (f finding) String() string {
	return fmt.Sprintf("%s:%d: %s", f.path, f.line, f.what)
}

func TestNoPersonalContent(t *testing.T) {
	found, err := scanTree(".")
	if errors.Is(err, errNotWorkTree) {
		t.Skip("module root is not a git work tree, so the files that can go public are unknown")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Error(f)
	}
}

func TestScanFile(t *testing.T) {
	// The inputs are built from placeholders so that this file does not hold what it tests.
	r := strings.NewReplacer("{home}", "/home/", "{users}", "/Users/", "{win}", `C:\Users\`, "{enc}", "-Users-", "{at}", "@")
	for _, tc := range []struct {
		name string
		path string
		line string
		want bool
	}{
		{"home with a dot in the name", "internal/agent/testdata/s.jsonl", `"cwd":"{home}dev.smith/x"`, true},
		{"home starting with dev", "blame_test.go", `const spike = "{home}devon/x"`, true},
		{"home in a docs file", "docs/design.md", "Sessions live in `{home}jane/.claude`.", true},
		{"macOS home", "README.md", "{users}jane/Codes/x", true},
		{"Windows home", "internal/agent/testdata/s.jsonl", `"cwd":"{win}jane\\x"`, true},
		{"encoded home", "internal/agent/testdata/s.jsonl", `"{enc}jane-Codes-x/s.jsonl"`, true},
		{"example name outside CONTRIBUTING", "README.md", "{users}you/x", true},
		{"email", "SECURITY.md", "Write to jane{at}corp.io.", true},
		{"creator_user_id", "internal/agent/testdata/s.jsonl", `{"creator_user_id":"u1"}`, true},
		{"creator_account_id", "internal/agent/testdata/s.jsonl", `{"creator_account_id":"a1"}`, true},
		{"account_id", "internal/review/testdata/s.json", `{"account_id": "a1"}`, true},
		{"user_id escaped in a string", "internal/agent/testdata/s.jsonl", `"text":"{\"user_id\":\"u1\"}"`, true},
		{"email key", "testdata/blame/s.golden", `{"email":"x"}`, true},
		{"home dev", "internal/agent/testdata/s.jsonl", `"cwd":"{home}dev/x"`, false},
		{"home dev at the end", "blame_test.go", `cwd := "{home}dev"`, false},
		{"tool directory under a fake home", "internal/agent/ask_test.go", `mkdir -p "$d{home}.codex/sessions"`, false},
		{"CONTRIBUTING example", "CONTRIBUTING.md", "such as `{users}you/...`, `{home}you/...` and `{enc}you-...`.", false},
		{"email at example.com", "README.md", "jane{at}example.com", false},
		{"email at example.org", "README.md", "jane{at}example.org", false},
		{"account key outside testdata", "internal/agent/codex.go", `UserID string ` + "`" + `json:"user_id"` + "`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := r.Replace(tc.line)
			found := scanFile(tc.path, []byte(line))
			if got := len(found) > 0; got != tc.want {
				t.Errorf("scanFile(%q, %q) = %v, want caught %v", tc.path, line, found, tc.want)
			}
		})
	}
}

func TestScanTree(t *testing.T) {
	personal := "/Use" + "rs/jane/x\n"
	root := t.TempDir()
	write := func(name, data string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "init", "-q")
	tracked := map[string]string{
		".fixtures/testdata/s.jsonl": `{"user_id":"u1"}` + "\n",
		".github/workflows/ci.yml":   personal,
		"docs/notes.md":              personal,
		"bin/tool":                   "\x00" + personal,
		"CONTRIBUTING.md":            "Replace `/Use" + "rs/you/...`.\n",
		"main.go":                    "package main\n",
		".gitignore":                 "/dr\n",
	}
	for name, data := range tracked {
		write(name, data)
	}
	if err := os.Symlink("/Use"+"rs/jane/x", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	write("internal/agent/testdata/new.jsonl", personal)
	write("dr", "\x00"+personal)
	write(".git/info/notes", personal)

	found, err := scanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range found {
		got = append(got, f.path)
	}
	slices.Sort(got)
	want := []string{
		".fixtures/testdata/s.jsonl",
		".github/workflows/ci.yml",
		"bin/tool",
		"docs/notes.md",
		"internal/agent/testdata/new.jsonl",
		"link",
	}
	if !slices.Equal(got, want) {
		t.Errorf("scanTree found %v, want personal content in %v", found, want)
	}
}

func TestScanTreeOutsideWorkTree(t *testing.T) {
	if _, err := scanTree(t.TempDir()); !errors.Is(err, errNotWorkTree) {
		t.Errorf("scanTree outside a work tree: got error %v, want %v", err, errNotWorkTree)
	}
}

// errNotWorkTree reports that the module root is not in a git work tree, as in a source
// archive, so the files that can go public are unknown.
var errNotWorkTree = errors.New("not a git work tree")

// scanTree reports personal content in the files under root that can go public: those git
// tracks, and new files it does not ignore. It scans every byte, binaries included, and for a
// symlink the target text, never the file it points to. It never reads .git.
func scanTree(root string) ([]finding, error) {
	if _, err := gitOutput(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errNotWorkTree
		}
		return nil, err
	}
	out, err := gitOutput(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var found []finding
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rel == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			// A tracked file deleted from the work tree has nothing to scan.
			continue
		}
		if err != nil {
			return nil, err
		}
		var data []byte
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return nil, err
			}
			data = []byte(target)
		case info.Mode().IsRegular():
			if data, err = os.ReadFile(p); err != nil {
				return nil, err
			}
		default:
			// A submodule is a directory; its own repository is checked on its own.
			continue
		}
		found = append(found, scanFile(rel, data)...)
	}
	return found, nil
}

// gitOutput runs git in dir with the GIT_ variables of the environment removed, so that a
// caller such as a git hook cannot point it at another repository.
func gitOutput(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		cmd.Env = append(cmd.Env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(abs))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// scanFile reports personal content in data, the text of the file at rel, a slash-separated
// path from the module root.
func scanFile(rel string, data []byte) []finding {
	contributing := rel == "CONTRIBUTING.md"
	testdata := slices.Contains(strings.Split(path.Dir(rel), "/"), "testdata")
	var found []finding
	n := 0
	for line := range strings.Lines(string(data)) {
		n++
		report := func(format string, args ...any) {
			found = append(found, finding{path: rel, line: n, what: fmt.Sprintf(format, args...)})
		}
		for _, m := range linuxHome.FindAllStringSubmatch(line, -1) {
			name := m[1]
			if name == "dev" || strings.HasPrefix(name, ".") || contributing && name == exampleName {
				continue
			}
			report("home directory %q", m[0])
		}
		for _, re := range otherHomes {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				if contributing && m[1] == exampleName {
					continue
				}
				report("home directory %q", m[0])
			}
		}
		for _, m := range email.FindAllStringSubmatch(line, -1) {
			if strings.EqualFold(m[1], "example.com") || strings.EqualFold(m[1], "example.org") {
				continue
			}
			report("email address %q", m[0])
		}
		if testdata {
			for _, m := range accountKey.FindAllStringSubmatch(line, -1) {
				report("account key %q", m[1])
			}
		}
	}
	return found
}
