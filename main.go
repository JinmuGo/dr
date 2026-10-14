// Command dr helps a person understand and fix code that coding agents wrote.
//
// dr [<ref>] opens the files changed since the merge-base of HEAD and ref, the default
// branch when ref is left out, in a diff screen that shows the agent session and the
// request behind each changed line. When ref is a branch checked out in another worktree,
// dr opens that worktree instead, compared with the default branch. dr blame <path> writes
// the same for one file.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/JinmuGo/dr/internal/tui"
)

const usage = `usage: dr [<ref>]
       dr blame <path>
       dr --version`

// version is the release version, set by release builds with -X main.version=<v>.
var version string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.UserHomeDir))
}

// run runs the command in args, writing to stdout and stderr, and returns the exit code.
// userHome returns the home directory, under which the agents keep their session logs; run
// calls it only for a command it takes.
func run(args []string, stdout, stderr io.Writer, userHome func() (string, error)) int {
	var cmd func(home string) error
	switch {
	case len(args) == 1 && args[0] == "--version":
		info, _ := debug.ReadBuildInfo()
		_, _ = fmt.Fprintln(stdout, "dr", versionOf(version, info))
		return 0
	case len(args) == 2 && args[0] == "blame":
		cmd = func(home string) error { return blame(stdout, stderr, ".", home, args[1]) }
	case len(args) == 0:
		cmd = func(home string) error { return browse(stdout, home, "") }
	case len(args) == 1 && args[0] != "blame" && !strings.HasPrefix(args[0], "-"):
		cmd = func(home string) error { return browse(stdout, home, args[0]) }
	default:
		_, _ = fmt.Fprintln(stderr, usage) // as for any message to the user, a failed write is not reported
		return 2
	}
	home, err := userHome()
	if err == nil {
		err = cmd(home)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "dr:", err)
		return 1
	}
	return 0
}

// versionOf returns the version dr reports: ldflags when a release build set it, else the
// main module version that go install records without its leading v, else devel with the
// VCS revision the build came from. A build in a git checkout records both a revision and,
// since Go 1.24, a pseudo-version as the module version, so a recorded revision means devel.
// info is nil when the binary holds no build info.
func versionOf(ldflags string, info *debug.BuildInfo) string {
	if ldflags != "" {
		return ldflags
	}
	if info == nil {
		return "devel"
	}
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if v := info.Main.Version; revision == "" && v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}
	if revision == "" {
		return "devel"
	}
	revision = revision[:min(len(revision), 7)]
	if modified {
		return "devel (" + revision + ", modified)"
	}
	return "devel (" + revision + ")"
}

// browse opens the diff screen on the files changed since the merge-base of HEAD and ref,
// or on the worktree ref names, as reviewed says.
// The files marked viewed, the questions asked, the comments and fixes and, when an agent
// lets dr choose where, the sessions dr starts to ask them are kept in the worktree's git
// directory. The screen holds the lock on the worktree while it is open, so only one dr
// applies fixes to it; when it cannot take the lock, the screen opens without applying
// fixes. A terminal keeps the empty screen visible until the user quits. Without a terminal,
// it reports no changes on stdout unless :undo can revert a fix. Agent logs are read under home.
func browse(stdout io.Writer, home, ref string) (err error) {
	worktree, ref, err := reviewed(".", ref)
	if err != nil {
		return err
	}
	var loader agent.Loader
	repo, files, err := changes(worktree, home, ref, &loader)
	if err != nil {
		return err
	}
	dir, err := git.Path(repo.Root, state.DirName)
	if err != nil {
		return err
	}
	saved := tui.Load(dir)
	output, ok := stdout.(*os.File)
	interactive := ok && term.IsTerminal(output.Fd())
	if !interactive && !opens(repo.Root, files, saved.Log) {
		_, _ = fmt.Fprintln(stdout, "No changed files")
		return nil
	}
	saved.Base = compared(repo.Root, ref, repo.Base)
	dirs := agent.Dirs{Home: home, State: dir}
	asker := tui.Asker{
		Ask: func(ctx context.Context, q agent.Question) (agent.Answer, error) {
			return agent.Ask(ctx, dirs, q)
		},
		Models: func(ctx context.Context, name string) ([]agent.Model, error) {
			return agent.Models(ctx, dirs, name)
		},
		Installed: agent.Installed,
		Root:      repo.Root,
	}
	unlock, lockErr := state.Lock(dir)
	if lockErr != nil {
		saved.Lock = lockErr
	} else {
		defer func() { err = errors.Join(err, unlock()) }()
	}
	saved.Reload = func() ([]review.File, []agent.Problem, error) {
		again, files, err := changes(repo.Root, home, ref, &loader)
		if err != nil {
			return nil, nil, err
		}
		return files, again.Problems, nil
	}
	return tui.Run(files, repo.Problems, saved, asker)
}

// opens reports whether browse has changed files or a fix to undo in the worktree at root.
// Without either, only an interactive terminal opens the screen.
func opens(root string, files []review.File, log state.Records) bool {
	return len(files) > 0 || tui.Undoable(root, log)
}

// reviewed returns the directory of the worktree dr opens for ref given in dir, and the ref
// it compares that worktree with. When ref is a branch checked out in another worktree and
// not the default branch, that is the other worktree and the default branch; refs/heads/
// before the branch name keeps the branch a ref to compare with. Otherwise it is dir and ref.
func reviewed(dir, ref string) (string, string, error) {
	if ref == "" {
		return dir, ref, nil
	}
	root, err := git.Root(dir)
	if err != nil {
		return "", "", err
	}
	other, err := git.Checkout(root, ref)
	if err != nil || other == "" || other == root || ref == shortName(git.DefaultBranch(root)) {
		return dir, ref, err
	}
	return other, "", nil
}

// shortName returns the branch name of ref, a ref DefaultBranch returns: main for
// refs/heads/main, origin/main and refs/remotes/origin/main.
func shortName(ref string) string {
	ref = strings.TrimPrefix(strings.TrimPrefix(ref, "refs/remotes/"), "refs/heads/")
	return strings.TrimPrefix(ref, "origin/")
}

// compared returns what the diff of the worktree at root compares it with, base: ref, or
// the default branch when ref is "", named as git names it for people, and whether base is
// the merge-base of HEAD and that ref or the ref's own commit.
func compared(root, ref, base string) tui.Base {
	if ref == "" {
		ref = git.DefaultBranch(root)
		ref = strings.TrimPrefix(strings.TrimPrefix(ref, "refs/remotes/"), "refs/heads/")
	}
	commit, err := git.Commit(root, ref)
	return tui.Base{Ref: ref, Commit: base, MergeBase: err != nil || commit != base} // an unknown commit is not claimed
}

// changes opens the worktree that holds dir and returns it with the files changed since
// the merge-base of HEAD and ref, each with the agent edit behind each changed line.
// It reads session logs under home only when files changed, using loader to reuse unchanged
// logs across calls while git state and dr's fixes are read anew.
func changes(dir, home, ref string, loader *agent.Loader) (*review.Repo, []review.File, error) {
	repo, err := review.Open(dir, home, ref, loader)
	if err != nil {
		return nil, nil, err
	}
	paths, err := repo.Changed()
	if err != nil {
		return nil, nil, err
	}
	files, err := repo.Files(paths)
	if err != nil {
		return nil, nil, err
	}
	return repo, files, nil
}
