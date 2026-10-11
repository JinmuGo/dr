// Package review gathers what dr shows about a worktree: the files changed since the base
// commit, their hunks, the agent edit behind each changed line, who a asks about lines and
// the prompt it sends them, with the kind of answer the tag of a reply tells.
// dr blame and the diff screen both draw from it, so the words they share, such as the tag
// of a request, live here too.
package review

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/origin"
)

// Repo is a worktree and the agent edits made in it since the base commit.
type Repo struct {
	Root     string          // top directory of the worktree
	Base     string          // base commit
	Problems []agent.Problem // session logs dr could not fully read

	origin origin.Repo
	edits  []agent.Edit
}

// Open opens the worktree that holds dir and reads the agent edits made in it from the
// session logs under home, and the fixes dr applied in it from dr's log. The base commit is
// the merge-base of HEAD and ref, or of HEAD and the default branch when ref is "".
func Open(dir, home, ref string) (*Repo, error) {
	root, err := git.Root(dir)
	if err != nil {
		return nil, err
	}
	base, err := git.Base(root, ref)
	if err != nil {
		return nil, err
	}
	branch, err := git.Branch(root)
	if err != nil {
		return nil, err
	}
	since, err := git.Start(root, base, branch)
	if err != nil {
		return nil, err
	}
	worktrees, err := git.Worktrees(root)
	if err != nil {
		return nil, err
	}
	edits, problems := agent.Load(home, since, worktrees)
	fixed, fixProblems, err := fixes(root, since)
	if err != nil {
		return nil, err
	}
	edits, problems = withFixes(edits, fixed), append(problems, fixProblems...)
	return &Repo{
		Root:     root,
		Base:     base,
		Problems: problems,
		origin:   origin.Repo{Root: root, Worktrees: worktrees, Branch: branch},
		edits:    edits,
	}, nil
}

// Changed returns the paths that differ between Base and the worktree, slash-separated and
// sorted. Untracked files count, and a renamed file shows as its old and its new path. A
// submodule or an untracked repository inside the worktree shows as its directory.
func (r *Repo) Changed() ([]string, error) {
	return git.Changed(r.Root, r.Base)
}

// File is the diff of a file from the base commit.
type File struct {
	Path string // slash-separated, relative to the worktree root
	// Status is 'A' when the base commit has no file or submodule at Path, 'D' when the
	// worktree has nothing there, and 'M' otherwise, whatever the file holds.
	Status byte
	// NoDiff says why the file has no line diff, Binary or Directory, and is "" when Lines
	// holds the diff.
	NoDiff string
	Lines  []origin.Line // the whole diff, unchanged lines included
}

// Why a file has no line diff, as dr shows it in place of the diff. dr does not look for
// the origin of such a file.
const (
	Binary    = "Binary file"             // base or current content has a NUL byte
	Directory = "Directory, no line diff" // a directory in the worktree, or a submodule in base
)

// File returns the diff from Base of the file at path, slash-separated and relative to
// Root, with the agent edit behind each changed line. Like git, dr follows no symlink: a
// symlink reads as the path it points to, and a path under a symlink or a file is not in
// the worktree.
func (r *Repo) File(path string) (File, error) {
	baseType, err := git.Type(r.Root, r.Base, path)
	if err != nil {
		return File{}, err
	}
	full := filepath.Join(r.Root, filepath.FromSlash(path))
	info, err := lstat(r.Root, full)
	deleted := errors.Is(err, fs.ErrNotExist) && (baseType == "blob" || baseType == "commit")
	if err != nil && !deleted {
		return File{}, err
	}
	f := File{Path: path, Status: 'M'}
	switch {
	case baseType != "blob" && baseType != "commit":
		f.Status = 'A'
	case deleted:
		f.Status = 'D'
	}
	if baseType == "commit" || !deleted && info.IsDir() {
		f.NoDiff = Directory
		return f, nil
	}
	var before, after string
	if baseType == "blob" {
		if before, err = git.Show(r.Root, r.Base, path); err != nil {
			return File{}, err
		}
	}
	switch {
	case deleted:
	case info.Mode()&fs.ModeSymlink != 0:
		after, err = os.Readlink(full)
	default:
		var data []byte
		data, err = os.ReadFile(full)
		after = string(data)
	}
	if err != nil {
		return File{}, err
	}
	if strings.IndexByte(before, 0) >= 0 || strings.IndexByte(after, 0) >= 0 {
		f.NoDiff = Binary
		return f, nil
	}
	f.Lines = origin.Blame(before, after, r.origin.Edits(r.edits, path))
	return f, nil
}

// lstat is os.Lstat of path, which is under root, that follows no symlink between root and
// path either. Like git, it finds nothing under a symlink or a file there, where the system
// would follow the symlink or fail with ENOTDIR.
func lstat(root, path string) (fs.FileInfo, error) {
	for i := len(root) + 1; i < len(path); i++ {
		if path[i] != filepath.Separator {
			continue
		}
		info, err := os.Lstat(path[:i])
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "lstat", Path: path, Err: fs.ErrNotExist}
		}
	}
	return os.Lstat(path)
}

// Requests returns one edit for each request behind the changed lines, in the order the
// requests were made. Each fix dr applied is a request of its own.
func Requests(lines []origin.Line) []*agent.Edit {
	type request struct {
		agent, session string
		turn           int
		fix            *agent.Fix
	}
	var edits []*agent.Edit
	seen := make(map[request]bool)
	for _, l := range lines {
		e := l.Edit
		if e == nil {
			continue
		}
		if k := (request{e.Agent, e.Session, e.Turn, e.Fix}); !seen[k] {
			seen[k] = true
			edits = append(edits, e)
		}
	}
	slices.SortStableFunc(edits, func(a, b *agent.Edit) int { return a.Time.Compare(b.Time) })
	return edits
}
