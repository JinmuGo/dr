package fix

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Why Apply and Undo refuse.
var (
	errChanged = errors.New("the file changed since the fix was requested; submit the comment again")
	errOverlap = errors.New("another fix changed these lines")
	errAfter   = errors.New("the file changed after the fix was applied")
	errPath    = errors.New("not a clean relative path inside the worktree")
	errSymlink = errors.New("a symlink is on the path")
	errRegular = errors.New("not a regular file")
	errNoLines = errors.New("the comment has no lines")
)

// Read returns the content of the file at path, slash-separated and relative to root, and
// its sha256 in hex. It reads only a regular file inside root that no symlink leads to, the
// files Apply writes.
func Read(root, path string) (data []byte, hash string, err error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = r.Close() }() // closing a directory loses nothing
	_, _, data, err = load(r, path)
	if err != nil {
		return nil, "", err
	}
	return data, sum(data), nil
}

// Apply replaces the lines of c in its file under root with lines, the fix the session
// sent for c, and returns the change it made. sent is the sha256 of the file when c was
// sent, and since are the changes dr applied to the file after that, oldest first.
//
// The file must be what those changes made of the file as sent: each change must start
// from the file the one before it left. A change wholly before c's range moves the range
// by the lines it added or removed, one wholly after it leaves the range, and one that
// overlaps it makes Apply refuse. The lines at the range must still be c.Lines. A string
// of lines that holds "\n" is split into lines. Apply keeps the file's mode and whether it
// ends with a newline, and the new lines take the "\r" endings of the lines they replace.
// It writes the file by renaming a temporary file over it.
func Apply(root string, c Comment, sent string, lines []string, since []Change) (Change, error) {
	if len(c.Lines) == 0 {
		return Change{}, fmt.Errorf("%s: %w", c.Path, errNoLines)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = r.Close() }() // closing a directory loses nothing
	name, info, data, err := load(r, c.Path)
	if err != nil {
		return Change{}, err
	}
	first, end := c.First, c.First+len(c.Lines) // end is the line after the range
	want := sent
	for _, ch := range since {
		if ch.Before != want {
			return Change{}, fmt.Errorf("%s: %w", c.Path, errChanged)
		}
		want = ch.After
		switch {
		case ch.First+len(ch.Old) <= first:
			moved := len(ch.New) - len(ch.Old)
			first, end = first+moved, end+moved
		case ch.First >= end:
		default:
			return Change{}, fmt.Errorf("%s: %w", c.Path, errOverlap)
		}
	}
	if sum(data) != want {
		return Change{}, fmt.Errorf("%s: %w", c.Path, errChanged)
	}
	file, final := fileLines(data)
	if first < 1 || end-1 > len(file) || !slices.Equal(file[first-1:end-1], c.Lines) {
		return Change{}, fmt.Errorf("%s: %w", c.Path, errChanged)
	}
	old := slices.Clone(file[first-1 : end-1])
	written := endings(split(lines), file, first-1, end-1, final)
	content := render(slices.Concat(file[:first-1], written, file[end-1:]), final)
	if err := write(r, name, info.Mode(), content); err != nil {
		return Change{}, err
	}
	return Change{Path: c.Path, First: first, Old: old, New: written, Before: want, After: sum(content)}, nil
}

// Undo puts c.Old back in place of c.New in the file under root, which must be as c left
// it, and writes the file the way Apply does. The file c left does not always tell whether
// the file before c ended with a newline, or whether c's last new line was an empty last
// line, so Undo writes the one of those contents whose sha256 is c.Before.
func Undo(root string, c Change) error {
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }() // closing a directory loses nothing
	name, info, data, err := load(r, c.Path)
	if err != nil {
		return err
	}
	if sum(data) != c.After {
		return fmt.Errorf("%s: %w", c.Path, errAfter)
	}
	read, final := fileLines(data)
	start, end := c.First-1, c.First-1+len(c.New)
	// render drops an empty last line when the file ends without a newline.
	for _, file := range [][]string{read, append(slices.Clip(read), "")} {
		if start < 0 || end > len(file) || !slices.Equal(file[start:end], c.New) {
			continue
		}
		lines := slices.Concat(file[:start], c.Old, file[end:])
		for _, newline := range []bool{final, !final} {
			if content := render(lines, newline); sum(content) == c.Before {
				return write(r, name, info.Mode(), content)
			}
		}
	}
	return fmt.Errorf("%s: %w", c.Path, errAfter)
}

// load returns the native form of path, slash-separated and relative to r, and the info and
// the content of the file there. It refuses a path that is not clean, is absolute or leaves
// r, a symlink at the file or at a directory between r and it, and anything but a regular
// file. r refuses to follow a symlink that a directory turns into later out of it.
func load(r *os.Root, path string) (name string, info fs.FileInfo, data []byte, err error) {
	name = filepath.FromSlash(path)
	if !filepath.IsLocal(name) || filepath.ToSlash(filepath.Clean(name)) != path {
		return "", nil, nil, fmt.Errorf("%q: %w", path, errPath)
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err = r.Lstat(filepath.Join(parts[:i+1]...))
		switch {
		case err != nil:
			return "", nil, nil, err
		case info.Mode()&fs.ModeSymlink != 0:
			return "", nil, nil, fmt.Errorf("%s: %w", path, errSymlink)
		case i < len(parts)-1 && !info.IsDir():
			return "", nil, nil, fmt.Errorf("%s: %s is not a directory", path, strings.Join(parts[:i+1], "/"))
		case i == len(parts)-1 && !info.Mode().IsRegular():
			return "", nil, nil, fmt.Errorf("%s: %w", path, errRegular)
		}
	}
	data, err = r.ReadFile(name)
	if err != nil {
		return "", nil, nil, err
	}
	return name, info, data, nil
}

// fileLines returns the lines of data without their "\n", each keeping any "\r", and
// whether data ends with a newline.
func fileLines(data []byte) (lines []string, final bool) {
	s := string(data)
	if s == "" {
		return nil, false
	}
	final = strings.HasSuffix(s, "\n")
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n"), final
}

// render joins lines into a file's content, ending it with a newline when final. No lines
// make an empty file.
func render(lines []string, final bool) []byte {
	if len(lines) == 0 {
		return nil
	}
	s := strings.Join(lines, "\n")
	if final {
		s += "\n"
	}
	return []byte(s)
}

// endings returns lines, which have no "\r", with the line endings of file[start:stop], the
// lines they replace: each ends with "\r" when one of those does. When those end the file
// with neither "\r" nor a newline, the last of lines does too, and the line before them
// tells whether the file uses "\r" when they hold none.
func endings(lines, file []string, start, stop int, final bool) []string {
	old := file[start:stop]
	bare := stop == len(file) && !final && !hasCR(old[len(old)-1])
	crlf := slices.ContainsFunc(old, hasCR) || bare && start > 0 && hasCR(file[start-1])
	if !crlf {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if bare && i == len(lines)-1 {
			out[i] = l
			continue
		}
		out[i] = l + "\r"
	}
	return out
}

// hasCR reports whether line ends with "\r".
func hasCR(line string) bool {
	return strings.HasSuffix(line, "\r")
}

// write replaces the file at name in r with data and gives it mode, through a temporary
// file with a random name in its directory that it renames over the file, so the file is
// never half written. r refuses a name that leaves it, even through a symlink that a
// directory turned into after load checked it.
func write(r *os.Root, name string, mode fs.FileMode, data []byte) error {
	tmp := filepath.Join(filepath.Dir(name), "."+filepath.Base(name)+".dr-"+rand.Text())
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	err = f.Chmod(mode.Perm())
	if err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = r.Rename(tmp, name)
	}
	if err != nil {
		_ = r.Remove(tmp) // the error that matters is err
	}
	return err
}

// sum returns the sha256 of data in hex.
func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
