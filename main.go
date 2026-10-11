// Command dr helps a person understand and fix code that coding agents wrote.
//
// dr [<ref>] opens the files changed since the merge-base of HEAD and ref, the default
// branch when ref is left out, in a diff screen that shows the agent session and the
// request behind each changed line. dr blame <path> writes the same for one file.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

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
	os.Exit(run(os.Args[1:]))
}

// run runs the command in args and returns the exit code.
func run(args []string) int {
	var err error
	switch {
	case len(args) == 1 && args[0] == "--version":
		info, _ := debug.ReadBuildInfo()
		fmt.Println("dr", versionOf(version, info))
		return 0
	case len(args) == 2 && args[0] == "blame":
		err = blame(os.Stdout, args[1])
	case len(args) == 0:
		err = browse("")
	case len(args) == 1 && args[0] != "blame" && !strings.HasPrefix(args[0], "-"):
		err = browse(args[0])
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dr:", err)
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

// browse opens the diff screen on the files changed since the merge-base of HEAD and ref.
// The files marked viewed, the questions asked, the comments and fixes and, when an agent
// lets dr choose where, the sessions dr starts to ask them are kept in the worktree's git
// directory. The screen holds the lock on the worktree while it is open, so only one dr
// applies fixes to it; when it cannot take the lock, the screen opens without applying
// fixes. With no changed file, it opens only when fixes dr applied are still in place.
func browse(ref string) (err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	repo, files, err := changes(".", home, ref)
	if err != nil {
		return err
	}
	dir, err := git.Path(repo.Root, "dr")
	if err != nil {
		return err
	}
	saved := tui.Load(dir)
	if !opens(files, saved.Log) {
		fmt.Println("No changed files")
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
		again, files, err := changes(repo.Root, home, ref)
		if err != nil {
			return nil, nil, err
		}
		return files, again.Problems, nil
	}
	return tui.Run(files, repo.Problems, saved, asker)
}

// opens reports whether browse opens the screen on files: when a file changed, or when none
// did but log holds fixes dr applied that are still in place, which :undo on the empty
// screen can revert.
func opens(files []review.File, log state.Records) bool {
	return len(files) > 0 || len(log.InPlace()) > 0
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

// changes opens the worktree that holds dir, reading the agent edits from the session logs
// under home again, and returns it with the files changed since the merge-base of HEAD and
// ref, each with the agent edit behind each changed line.
func changes(dir, home, ref string) (*review.Repo, []review.File, error) {
	repo, err := review.Open(dir, home, ref)
	if err != nil {
		return nil, nil, err
	}
	paths, err := repo.Changed()
	if err != nil {
		return nil, nil, err
	}
	files := make([]review.File, len(paths))
	for i, path := range paths {
		if files[i], err = repo.File(path); err != nil {
			return nil, nil, err
		}
	}
	return repo, files, nil
}
