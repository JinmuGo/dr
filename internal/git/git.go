// Package git runs the git commands dr needs.
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// Root returns the top directory of the worktree that contains dir.
func Root(dir string) (string, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// Path returns the path of name in the git directory of the worktree at root, as git
// rev-parse --git-path gives it but absolute. Each worktree has its own git directory.
func Path(root, name string) (string, error) {
	out, err := run(root, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(out, "\n")
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return path, nil
}

// Branch returns the current branch, or "" when HEAD is detached.
func Branch(root string) (string, error) {
	out, err := run(root, "branch", "--show-current")
	return strings.TrimSpace(out), err
}

// Base returns the merge-base of HEAD and ref. An empty ref means DefaultBranch.
func Base(root, ref string) (string, error) {
	if ref == "" {
		ref = DefaultBranch(root)
	}
	out, err := run(root, "merge-base", "HEAD", ref)
	return strings.TrimSpace(out), err
}

// DefaultBranch returns the ref of the default branch: origin's HEAD, origin/main,
// origin/master, main or master, the first that exists, or HEAD without any of them.
func DefaultBranch(root string) string {
	if out, err := run(root, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(out)
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"} {
		if _, err := run(root, "rev-parse", "--verify", "-q", ref); err == nil {
			return ref
		}
	}
	return "HEAD"
}

// Commit returns the commit ref names.
func Commit(root, ref string) (string, error) {
	out, err := run(root, "rev-parse", "--verify", "-q", ref+"^{commit}")
	return strings.TrimSpace(out), err
}

// Start returns the earliest time the changes since base can come from: the commit time of
// base, the author time of a commit after it, or when the branch was made. A rebase moves
// base after the work, but rebased commits keep their author time and the branch keeps
// its reflog.
func Start(root, base, branch string) (time.Time, error) {
	committed, err := run(root, "log", "-1", "--format=%ct", base)
	if err != nil {
		return time.Time{}, err
	}
	authored, err := run(root, "log", "--format=%at", base+"..HEAD")
	if err != nil {
		return time.Time{}, err
	}
	times := strings.Fields(committed + authored)
	if branch != "" {
		// Lines like "main@{1791545719}". A branch without a reflog has none.
		reflog, _ := run(root, "reflog", "show", "--date=unix", "--format=%gd", "refs/heads/"+branch, "--")
		for _, l := range strings.Fields(reflog) {
			if i := strings.LastIndex(l, "@{"); i >= 0 {
				times = append(times, strings.TrimSuffix(l[i+2:], "}"))
			}
		}
	}
	var start time.Time
	for _, s := range times {
		seconds, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("git: parse time: %w", err)
		}
		if t := time.Unix(seconds, 0); start.IsZero() || t.Before(start) {
			start = t
		}
	}
	return start, nil
}

// Worktrees returns the top directories of all worktrees of the repository.
func Worktrees(root string) ([]string, error) {
	out, err := run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, line := range strings.Split(out, "\n") {
		if dir, ok := strings.CutPrefix(line, "worktree "); ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// Checkout returns the top directory of the worktree that has branch checked out, or ""
// when none has. A worktree whose directory is gone does not count.
func Checkout(root, branch string) (string, error) {
	out, err := run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	for _, block := range strings.Split(out, "\n\n") {
		lines := strings.Split(block, "\n")
		dir, ok := strings.CutPrefix(lines[0], "worktree ")
		if ok && slices.Contains(lines, "branch refs/heads/"+branch) && !slices.ContainsFunc(lines, isPrunable) {
			return dir, nil
		}
	}
	return "", nil
}

// isPrunable reports whether line of git worktree list --porcelain marks a worktree whose
// directory is gone.
func isPrunable(line string) bool {
	return line == "prunable" || strings.HasPrefix(line, "prunable ")
}

// Type returns what path is in commit: "blob" for a file or a symlink, "tree" for a
// directory, "commit" for a submodule, or "" when commit does not have path.
func Type(root, commit, path string) (string, error) {
	out, err := run(root, "--literal-pathspecs", "ls-tree", "-z", commit, "--", path)
	if err != nil || out == "" {
		return "", err
	}
	// The entry is "<mode> <type> <object>\t<path>\x00".
	meta, _, _ := strings.Cut(out, "\t")
	fields := strings.Fields(meta)
	if len(fields) != 3 {
		return "", fmt.Errorf("git ls-tree: parse entry: %q", out)
	}
	return fields[1], nil
}

// Show returns the blob at path in commit: the content of a file, or the path a symlink
// points to.
func Show(root, commit, path string) (string, error) {
	return run(root, "show", commit+":"+path)
}

// Changed returns the paths that differ between base and the worktree, untracked files
// included and ignored files left out, sorted. A renamed file shows as its old path and
// its new path. A submodule, or a repository inside the worktree that is not tracked,
// shows as the path of its directory.
func Changed(root, base string) ([]string, error) {
	tracked, err := run(root, "diff", "--name-only", "--no-renames", "-z", base, "--")
	if err != nil {
		return nil, err
	}
	untracked, err := run(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(tracked+untracked, "\x00") {
		if p != "" {
			// git lists an untracked repository as "dir/".
			paths = append(paths, strings.TrimSuffix(p, "/"))
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}
