package fix

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// put writes content to the file at path under root, making its directories.
func put(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// content returns the content of the file at path under root.
func content(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// sent reads the file at path under root the way dr does when it sends a fix request, and
// returns its hash.
func sent(t *testing.T, root, path string) string {
	t.Helper()
	_, hash, err := Read(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestApply(t *testing.T) {
	root := t.TempDir()
	put(t, root, "d/a.go", "a\nb\nc\nd\n")
	full := filepath.Join(root, "d", "a.go")
	if err := os.Chmod(full, 0o755); err != nil {
		t.Fatal(err)
	}
	before := sent(t, root, "d/a.go")
	c := Comment{N: 1, Path: "d/a.go", First: 2, Lines: []string{"b", "c"}}
	got, err := Apply(root, c, before, []string{"B"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after := content(t, root, "d/a.go"); after != "a\nB\nd\n" {
		t.Errorf("file %q, want %q", after, "a\nB\nd\n")
	}
	want := Change{Path: "d/a.go", First: 2, Old: []string{"b", "c"}, New: []string{"B"}, Before: before, After: sent(t, root, "d/a.go")}
	if !equalChange(got, want) {
		t.Errorf("Apply returned %+v, want %+v", got, want)
	}
	if info, err := os.Stat(full); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("mode %v, %v; want 0755", info.Mode(), err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(full)); len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want only the file", len(entries))
	}
}

func equalChange(a, b Change) bool {
	return a.Path == b.Path && a.First == b.First && slices.Equal(a.Old, b.Old) && slices.Equal(a.New, b.New) &&
		a.Before == b.Before && a.After == b.After
}

func TestApplyInTurn(t *testing.T) {
	// Two comments are sent against the same file. The first fix applied changes the
	// file, so the second must account for it.
	const file = "1\n2\n3\n4\n5\n6\n7\n8\n"
	for _, tc := range []struct {
		name        string
		first       Comment
		firstLines  []string
		second      Comment
		secondLines []string
		wantFirst   int // the line the second fix lands on
		want        string
		err         error
	}{
		{
			name:  "first before the second",
			first: Comment{First: 2, Lines: []string{"2"}}, firstLines: []string{"2a", "2b", "2c"},
			second: Comment{First: 6, Lines: []string{"6", "7"}}, secondLines: []string{"X"},
			wantFirst: 8, want: "1\n2a\n2b\n2c\n3\n4\n5\nX\n8\n",
		},
		{
			name:  "first deletes lines before the second",
			first: Comment{First: 1, Lines: []string{"1", "2"}}, firstLines: []string{},
			second: Comment{First: 3, Lines: []string{"3"}}, secondLines: []string{"Y"},
			wantFirst: 1, want: "Y\n4\n5\n6\n7\n8\n",
		},
		{
			name:  "first after the second",
			first: Comment{First: 7, Lines: []string{"7"}}, firstLines: []string{"7a", "7b"},
			second: Comment{First: 3, Lines: []string{"3", "4"}}, secondLines: []string{"Z"},
			wantFirst: 3, want: "1\n2\nZ\n5\n6\n7a\n7b\n8\n",
		},
		{
			name:  "first right after the second",
			first: Comment{First: 5, Lines: []string{"5"}}, firstLines: []string{"5a", "5b"},
			second: Comment{First: 3, Lines: []string{"3", "4"}}, secondLines: []string{"Z"},
			wantFirst: 3, want: "1\n2\nZ\n5a\n5b\n6\n7\n8\n",
		},
		{
			name:  "first overlaps the second",
			first: Comment{First: 4, Lines: []string{"4", "5"}}, firstLines: []string{"W"},
			second: Comment{First: 5, Lines: []string{"5", "6"}}, secondLines: []string{"V"},
			err: errOverlap, want: "1\n2\n3\nW\n6\n7\n8\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "a.go", file)
			hash := sent(t, root, "a.go")
			tc.first.Path, tc.second.Path = "a.go", "a.go"
			applied, err := Apply(root, tc.first, hash, tc.firstLines, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Apply(root, tc.second, hash, tc.secondLines, []Change{applied})
			if !errors.Is(err, tc.err) {
				t.Fatalf("second Apply: got %v, want %v", err, tc.err)
			}
			if err == nil && (got.First != tc.wantFirst || got.Before != applied.After) {
				t.Errorf("second Apply landed on line %d after %s, want line %d after %s", got.First, got.Before, tc.wantFirst, applied.After)
			}
			if after := content(t, root, "a.go"); after != tc.want {
				t.Errorf("file %q, want %q", after, tc.want)
			}
		})
	}
}

func TestApplyChanged(t *testing.T) {
	// The file changes outside dr after the request, so the fix is refused and the file
	// is left as it is.
	for _, tc := range []struct {
		name    string
		outside string // what the file becomes outside dr after the request
		since   bool   // whether dr applied another fix before the outside change
	}{
		{name: "lines of the range", outside: "a\nB\nc\n"},
		{name: "lines elsewhere", outside: "a\nb\nc\nd\n"},
		{name: "after another fix", outside: "A2\nb\nc\n", since: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "a.go", "a\nb\nc\n")
			hash := sent(t, root, "a.go")
			var since []Change
			if tc.since {
				ch, err := Apply(root, Comment{Path: "a.go", First: 1, Lines: []string{"a"}}, hash, []string{"A"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				since = append(since, ch)
			}
			put(t, root, "a.go", tc.outside)
			_, err := Apply(root, Comment{Path: "a.go", First: 2, Lines: []string{"b"}}, hash, []string{"x"}, since)
			if !errors.Is(err, errChanged) {
				t.Errorf("Apply: got %v, want errChanged", err)
			}
			if after := content(t, root, "a.go"); after != tc.outside {
				t.Errorf("file %q, want it left %q", after, tc.outside)
			}
		})
	}
}

func TestApplyChangedBeforeAnotherFix(t *testing.T) {
	// The file changed outside dr after the first request and before the second, which
	// dr then applied: the first request no longer knows the file.
	root := t.TempDir()
	put(t, root, "a.go", "a\nb\nc\n")
	first := sent(t, root, "a.go")
	put(t, root, "a.go", "a\nb\nc\nd\n")
	second := sent(t, root, "a.go")
	ch, err := Apply(root, Comment{Path: "a.go", First: 3, Lines: []string{"c"}}, second, []string{"C"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(root, Comment{Path: "a.go", First: 1, Lines: []string{"a"}}, first, []string{"A"}, []Change{ch})
	if !errors.Is(err, errChanged) {
		t.Errorf("Apply: got %v, want errChanged", err)
	}
}

func TestApplySameBlockTwice(t *testing.T) {
	root := t.TempDir()
	put(t, root, "a.go", "x\ny\nmid\nx\ny\n")
	c := Comment{Path: "a.go", First: 4, Lines: []string{"x", "y"}}
	if _, err := Apply(root, c, sent(t, root, "a.go"), []string{"z"}, nil); err != nil {
		t.Fatal(err)
	}
	if after := content(t, root, "a.go"); after != "x\ny\nmid\nz\n" {
		t.Errorf("file %q, want the second copy changed", after)
	}
}

func TestApplyLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		file  string
		c     Comment
		lines []string
		want  string
	}{
		{
			name: "CRLF",
			file: "a\r\nb\r\nc\r\n", c: Comment{First: 2, Lines: []string{"b\r"}},
			lines: []string{"B1", "B2\r"}, want: "a\r\nB1\r\nB2\r\nc\r\n",
		},
		{
			name: "CRLF without a final newline",
			file: "a\r\nb", c: Comment{First: 2, Lines: []string{"b"}},
			lines: []string{"B1", "B2"}, want: "a\r\nB1\r\nB2",
		},
		{
			name: "CRLF range ending the file without a final newline",
			file: "a\r\nb\r\nc", c: Comment{First: 2, Lines: []string{"b\r", "c"}},
			lines: []string{"B", "C"}, want: "a\r\nB\r\nC",
		},
		{
			name: "no final newline",
			file: "a\nb", c: Comment{First: 2, Lines: []string{"b"}},
			lines: []string{"B", "C"}, want: "a\nB\nC",
		},
		{
			name: "no final newline, line before the last",
			file: "a\nb", c: Comment{First: 1, Lines: []string{"a"}},
			lines: []string{"A"}, want: "A\nb",
		},
		{
			name: "lines holding newlines",
			file: "a\nb\n", c: Comment{First: 1, Lines: []string{"a"}},
			lines: []string{"\tA1\n\tA2"}, want: "\tA1\n\tA2\nb\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "a.go", tc.file)
			tc.c.Path = "a.go"
			ch, err := Apply(root, tc.c, sent(t, root, "a.go"), tc.lines, nil)
			if err != nil {
				t.Fatal(err)
			}
			if after := content(t, root, "a.go"); after != tc.want {
				t.Errorf("file %q, want %q", after, tc.want)
			}
			if err := Undo(root, ch); err != nil {
				t.Fatal(err)
			}
			if after := content(t, root, "a.go"); after != tc.file {
				t.Errorf("after Undo the file is %q, want %q", after, tc.file)
			}
		})
	}
}

func TestApplyDelete(t *testing.T) {
	for _, tc := range []struct {
		name string
		file string
		c    Comment
		want string
	}{
		{name: "lines in the middle", file: "a\nb\nc\n", c: Comment{First: 2, Lines: []string{"b"}}, want: "a\nc\n"},
		{name: "the last lines", file: "a\nb\nc", c: Comment{First: 2, Lines: []string{"b", "c"}}, want: "a"},
		{name: "every line", file: "a\nb\n", c: Comment{First: 1, Lines: []string{"a", "b"}}, want: ""},
		{name: "every line without a final newline", file: "a\nb", c: Comment{First: 1, Lines: []string{"a", "b"}}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "a.go", tc.file)
			tc.c.Path = "a.go"
			ch, err := Apply(root, tc.c, sent(t, root, "a.go"), []string{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if after := content(t, root, "a.go"); after != tc.want {
				t.Errorf("file %q, want %q", after, tc.want)
			}
			if err := Undo(root, ch); err != nil {
				t.Fatal(err)
			}
			if after := content(t, root, "a.go"); after != tc.file {
				t.Errorf("after Undo the file is %q, want %q", after, tc.file)
			}
		})
	}
}

func TestUndo(t *testing.T) {
	// Two fixes are undone newest first, each putting back the file it changed.
	root := t.TempDir()
	const file = "a\nb\nc\n"
	put(t, root, "a.go", file)
	hash := sent(t, root, "a.go")
	first, err := Apply(root, Comment{Path: "a.go", First: 1, Lines: []string{"a"}}, hash, []string{"A1", "A2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	middle := content(t, root, "a.go")
	second, err := Apply(root, Comment{Path: "a.go", First: 3, Lines: []string{"c"}}, hash, []string{"C"}, []Change{first})
	if err != nil {
		t.Fatal(err)
	}
	if err := Undo(root, first); !errors.Is(err, errAfter) {
		t.Errorf("Undo of the older fix: got %v, want errAfter", err)
	}
	if err := Undo(root, second); err != nil {
		t.Fatal(err)
	}
	if after := content(t, root, "a.go"); after != middle {
		t.Errorf("after the first Undo the file is %q, want %q", after, middle)
	}
	if err := Undo(root, first); err != nil {
		t.Fatal(err)
	}
	if after := content(t, root, "a.go"); after != file {
		t.Errorf("after the second Undo the file is %q, want %q", after, file)
	}
}

func TestUndoChanged(t *testing.T) {
	root := t.TempDir()
	put(t, root, "a.go", "a\nb\n")
	ch, err := Apply(root, Comment{Path: "a.go", First: 1, Lines: []string{"a"}}, sent(t, root, "a.go"), []string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "a.go", "A\nb\nmore\n")
	if err := Undo(root, ch); !errors.Is(err, errAfter) {
		t.Errorf("Undo: got %v, want errAfter", err)
	}
	if after := content(t, root, "a.go"); after != "A\nb\nmore\n" {
		t.Errorf("file %q, want it left as it was", after)
	}
}

func TestApplyRefusedPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	put(t, outside, "a.go", "a\n")
	put(t, root, "real/a.go", "a\n")
	if err := os.Symlink(filepath.Join(outside, "a.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub", "a.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		err  error
	}{
		{"link.go", errSymlink},
		{"dir/a.go", errSymlink},
		{"sub/a.go", errRegular},
		{"../" + filepath.Base(outside) + "/a.go", errPath},
		{filepath.ToSlash(filepath.Join(outside, "a.go")), errPath},
		{"real/../../a.go", errPath},
		{"./real/a.go", errPath},
		{"real//a.go", errPath},
		{"", errPath},
	} {
		c := Comment{Path: tc.path, First: 1, Lines: []string{"a"}}
		if _, err := Apply(root, c, "", []string{"X"}, nil); !errors.Is(err, tc.err) {
			t.Errorf("Apply(%q): got %v, want %v", tc.path, err, tc.err)
		}
		if err := Undo(root, Change{Path: tc.path, First: 1, Old: []string{"a"}, New: []string{"X"}}); !errors.Is(err, tc.err) {
			t.Errorf("Undo(%q): got %v, want %v", tc.path, err, tc.err)
		}
		if _, _, err := Read(root, tc.path); !errors.Is(err, tc.err) {
			t.Errorf("Read(%q): got %v, want %v", tc.path, err, tc.err)
		}
	}
	if got := content(t, outside, "a.go"); got != "a\n" {
		t.Errorf("the file outside root is %q, want it untouched", got)
	}
}

func TestUndoEmptyLastLine(t *testing.T) {
	// An empty last line in a file with no final newline reads back as one line fewer.
	for _, tc := range []struct {
		name, file string
		c          Comment
	}{
		{"only line", "a", Comment{Path: "a.go", First: 1, Lines: []string{"a"}}},
		{"last line", "a\nb", Comment{Path: "a.go", First: 2, Lines: []string{"b"}}},
		{"last line after crlf", "a\r\nb", Comment{Path: "a.go", First: 2, Lines: []string{"b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "a.go", tc.file)
			ch, err := Apply(root, tc.c, sent(t, root, "a.go"), []string{""}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := Undo(root, ch); err != nil {
				t.Fatalf("Undo of %q: %v", tc.file, err)
			}
			if after := content(t, root, "a.go"); after != tc.file {
				t.Errorf("after Undo the file is %q, want %q", after, tc.file)
			}
		})
	}
}

func TestUndoNoCandidate(t *testing.T) {
	// Undo refuses when no content it can build has the sha256 the file had before.
	for _, file := range []string{"a\nb\n", "a"} {
		root := t.TempDir()
		put(t, root, "a.go", file)
		ch, err := Apply(root, Comment{Path: "a.go", First: 1, Lines: []string{"a"}}, sent(t, root, "a.go"), []string{""}, nil)
		if err != nil {
			t.Fatal(err)
		}
		applied := content(t, root, "a.go")
		ch.Before = sum([]byte("something else\n"))
		if err := Undo(root, ch); !errors.Is(err, errAfter) {
			t.Errorf("Undo of %q: got %v, want errAfter", file, err)
		}
		if after := content(t, root, "a.go"); after != applied {
			t.Errorf("file %q, want it left as %q", after, applied)
		}
	}
}

func TestWriteParentSwappedForSymlink(t *testing.T) {
	// A directory that turns into a symlink out of root after load checked the path must not
	// let write reach the file it points to.
	root := t.TempDir()
	outside := t.TempDir()
	put(t, root, "d/a.go", "a\n")
	put(t, outside, "a.go", "outside\n")
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	name, info, _, err := load(r, "d/a.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "d")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "d")); err != nil {
		t.Fatal(err)
	}
	err = write(r, name, info.Mode(), []byte("X\n"))
	if err == nil || !strings.Contains(err.Error(), "path escapes from parent") {
		t.Errorf("write: got %v, want an error that the path escapes root", err)
	}
	if got := content(t, outside, "a.go"); got != "outside\n" {
		t.Errorf("the file outside root is %q, want it untouched", got)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("outside root holds %d entries, want only a.go", len(entries))
	}
}
