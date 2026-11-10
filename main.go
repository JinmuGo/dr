// Command dr helps a person understand and fix code that coding agents wrote.
//
// dr [<ref>] opens the files changed since the merge-base of HEAD and ref, the default
// branch when ref is left out, in a diff screen that shows the agent session and the
// request behind each changed line. When ref is a branch checked out in another worktree,
// dr opens that worktree instead, compared with the default branch. Without ref, when the
// repository has more than one worktree, dr first asks which to open. dr blame <path>
// writes the same for one file. dr --review <file> [<ref>] adds :approve and :request,
// which decide the review, and writes the decision with the comments to file as JSON when
// dr quits, for the agent that wrote the changes to act on.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/JinmuGo/dr/internal/agent"
	"github.com/JinmuGo/dr/internal/git"
	"github.com/JinmuGo/dr/internal/review"
	"github.com/JinmuGo/dr/internal/state"
	"github.com/JinmuGo/dr/internal/tui"
)

const usage = `usage: dr [<ref>]
       dr --review <file> [<ref>]
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
		cmd = func(home string) error { return browse(stdout, home, "", "") }
	case len(args) == 1 && args[0] != "blame" && !strings.HasPrefix(args[0], "-"):
		cmd = func(home string) error { return browse(stdout, home, args[0], "") }
	case (len(args) == 2 || len(args) == 3) && args[0] == "--review" && args[1] != "" && !strings.HasPrefix(args[1], "-") &&
		(len(args) == 2 || !strings.HasPrefix(args[2], "-")):
		ref := ""
		if len(args) == 3 {
			ref = args[2]
		}
		cmd = func(home string) error { return browse(stdout, home, ref, args[1]) }
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
// or on the worktree ref names, as reviewed says. Without ref it opens the worktree chosen
// says, and nothing when the person cancels. With report, the path of a file, the screen
// offers :approve and :request, and browse writes what was decided to that file when the
// screen closes, with no decision when there was nothing to review or the person cancelled.
// With report and no ref, browse reviews the worktree dr runs in without asking which: the
// harness that waits for the report chose it.
// The files marked viewed, the questions asked, the comments and fixes and, when an agent
// lets dr choose where, the sessions dr starts to ask them are kept in the worktree's git
// directory. The screen holds the lock on the worktree while it is open, so only one dr
// applies fixes to it; when it cannot take the lock, the screen opens without applying
// fixes. With no changed file, it opens only when :undo can revert the last fix dr applied,
// and otherwise says so on stdout. The agents' session logs are read under home.
func browse(stdout io.Writer, home, ref, report string) (err error) {
	worktree := "."
	written := false // the report was written with what the screen decided
	if report != "" {
		defer func() {
			if err == nil && !written {
				err = review.WriteReport(report, review.Report{Verdict: review.VerdictNone})
			}
		}()
	}
	if ref == "" && report == "" {
		worktree, err = chosen(worktree, func(list []git.Worktree, current int) (int, error) {
			return tui.Choose(list, current, home, os.Getenv("DR_THEME"))
		})
	} else {
		worktree, ref, err = reviewed(worktree, ref)
	}
	if err != nil || worktree == "" {
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
	saved.Theme = os.Getenv("DR_THEME")
	saved.View = os.Getenv("DR_VIEW")
	if !opens(repo.Root, files, saved.Log) {
		_, _ = fmt.Fprintln(stdout, "No changed files")
		return nil
	}
	saved.Base = compared(repo.Root, ref, repo.Base)
	saved.Review = report != ""
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
	outcome, err := tui.Run(files, repo.Problems, saved, asker)
	if err != nil || report == "" {
		return err
	}
	written = true
	return review.WriteReport(report, review.Report{
		Verdict:  outcome.Verdict,
		Root:     repo.Root,
		Ref:      saved.Base.Ref,
		Base:     repo.Base,
		Files:    outcome.Files, // of the diff as it was at the decision, e and ga included
		Comments: outcome.Comments,
	})
}

// opens reports whether browse opens the screen on files in the worktree at root: when a
// file changed, or when none did but :undo on the empty screen can revert the last fix dr
// applied, as log has the fixes.
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

// chosen returns the directory of the worktree dr opens when started in dir without a ref:
// dir when the repository has one worktree, else the one choose returns the index of, given
// the worktrees and the index of the one that holds dir, -1 for none. It returns "" when
// choose returns -1, for none.
func chosen(dir string, choose func(list []git.Worktree, current int) (int, error)) (string, error) {
	root, err := git.Root(dir)
	if err != nil {
		return "", err
	}
	list, err := git.Checkouts(root)
	if err != nil || len(list) < 2 {
		return dir, err
	}
	i, err := choose(list, slices.IndexFunc(list, func(w git.Worktree) bool { return w.Dir == root }))
	if err != nil || i < 0 {
		return "", err
	}
	return list[i].Dir, nil
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
