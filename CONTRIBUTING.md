# Contributing

Start with [docs/usage.md](docs/usage.md) to see what dr does. This page covers the code layout, the checks and the rules.

## Layout

The `dr` command is the main package at the repository root. Everything else is under `internal/`, so it can change freely.

| Path | What it does |
| --- | --- |
| `main.go` | The `dr` and `dr blame` commands. Parses arguments and wires packages together |
| `blame.go` | The `dr blame` output |
| `internal/git/` | Base commit, branch, worktrees, file contents |
| `internal/diff/` | Line matching between two versions and word diff inside a line |
| `internal/origin/` | Replays session edits to find the origin of each line |
| `internal/review/` | Data shared by `dr blame` and the screen: changed files, origin per line, hunks, and who `a` asks |
| `internal/agent/` | Reads each agent's session logs and asks the agents through their CLIs, one file per agent |
| `internal/tui/` | The screen and keys: file tree, syntax colors, dark and light themes |
| `internal/fix/` | Fix requests and their answers, and writing fixes to files only when it is safe |
| `internal/state/` | What dr keeps in the worktree's git directory: viewed files, the log of questions, comments and fixes, and the lock |

Test data:

- `testdata/blame/*.golden`: the exact `dr blame` output.
- `internal/agent/testdata/`: trimmed session logs from real sessions.
- `internal/review/testdata/`: files those sessions wrote.

## Branch lifecycle

After a PR is merged, start follow-up work on a new branch from the current main. Do not reuse the merged PR's branch name: its recorded head commit does not include later work.

Before removing a worktree, check its uncommitted and unpublished work, running sessions, and installed integrations. A plugin linked to that directory stops working when the directory is removed. Use a released plugin or a commit-addressed archive outside the worktree for an installation you intend to keep.

A merged PR alone is not proof that a local branch can be deleted. Its head SHA must still match, or the local HEAD must already be an ancestor of the current remote default branch. Keep backup branches separately.

## Checks

Run these before opening a pull request. CI runs the same set.

```sh
gofmt -l .
go vet ./...
golangci-lint run
go test -race -count=1 ./...
govulncheck ./...
go mod tidy -diff
```

`gofmt -l .` and `go mod tidy -diff` should print nothing.

If you change the `dr blame` output, regenerate the golden files and review the diff:

```sh
go test -run TestBlame -update .
git diff testdata/blame
```

## Rules

- Code comments, user-facing text and commit messages are in English.
- Commit subjects follow `type(scope): summary` (for example `feat(tui): add split view`), in the imperative and under 72 characters.
- Follow the [Google Go Style Guide](https://google.github.io/styleguide/go/) and [Effective Go](https://go.dev/doc/effective_go).
- Wrap errors with `fmt.Errorf("...: %w", err)`. No error library.
- Every new dependency needs a reason, written in the pull request.
- TUI tests check behavior: cursor, keys, the contents of the origin box. Do not snapshot the whole screen.
- For screen changes, look at the real screen in a dark terminal, a light terminal and with `NO_COLOR=1`.

## Agent session logs

`review.Open` reads the git comparison and saved fixes first. Session logs are loaded only when `File` or a nonempty `Files` call needs origins. An empty review still opens when the last saved fix can be undone.

A caller-owned `agent.Loader` reuses parsed logs across editor and agent returns. Changing the home directory, worktree list or `since` discards its cache. Changed logs are parsed from the start; deleted or unreadable logs never reuse an old result. Fork copies are resolved on every load, before stable time ordering.

Session parsers consume one JSONL record at a time. The reader has no fixed line-size limit; borrowed JSON belongs only to the current record. Pending tool calls retain decoded, owned values. An I/O error discards that log's partial edits. Invalid complete lines are counted, while an incomplete final line is ignored. Root matching scans the log before parsing, so edits outside the session cwd are still considered. Memory includes the largest record and cached edit data.

Session log formats of Claude Code, Codex and OMP are not public APIs. They change without notice. A change to a parser in `internal/agent/` needs a fixture: a session log from a real session, trimmed to what the test needs, in `internal/agent/testdata/`.

A fixture from a real session must hold nothing personal before you commit it:

- Replace your home directory with `/home/dev` in every form the log holds, such as `/Users/you/...`, `/home/you/...` and Claude Code's encoded project directory `-Users-you-...`. Paths that must match each other, like a session's cwd and the files it edited, have to match after the change.
- Remove names, email addresses, account and organization IDs (Codex writes `creator_user_id` and `creator_account_id`), tokens and keys, the names of private projects or employers, and any instruction or text you would not publish.
- Trim what the test does not read, such as long tool output and system context.

A test fails when any file in the repository holds a home directory other than `/home/dev` or an email address outside `example.com` and `example.org`, or when test data holds an account ID key.

## Adding an agent

Each agent is one file in `internal/agent/`, like `claude.go`, `codex.go` and `omp.go`. Nothing outside that file and the list of agents names an agent, so the screen and `dr blame` need no change.

1. Add `internal/agent/<name>.go` with an `agent` value:
   - `name`: what dr shows, and keeps in `Edit.Agent`.
   - `logs`: where the agent keeps its session logs, relative to the home directory.
   - `parse`: reads the records of one session log into edits. `Loader.Load` sets `Edit.Log`.
   - `command`: the CLI, which `Installed` looks for in PATH.
   - `canFork`: returns nil when dr can fork the session behind an edit, or why it cannot.
   - `ask`: asks a `Question` through the CLI with `run`. It continues the fork in `Fork`, or forks `Session`, or starts a new session when both are empty. The session can only read files and runs no hooks. Return the answer, the model that answered, and in `Answer.Fork` what to continue next time. Report errors with `notInstalled`, `failed` and `cut`, like the other agents.
   - `models`: lists the models the CLI can be asked with, for the `^` picker, in the order the agent lists them. Read the agent's own list, from a command such as `omp models --json` or a file the CLI keeps, rather than writing model names into dr; Claude Code has no list, so `claude.go` names only the aliases its `--model` help documents.
   - `open`: the arguments that open a fork of `Question.Session` interactively for `ga`, with the session's model and effort when known. This is the user's own working session, so give it none of the read-only flags `ask` uses, and let the CLI store it with the user's other sessions.
2. Add the value to `agents` in `internal/agent/agent.go`. When no session can be asked about a line and the review has no edits, a new session of the first installed agent in that list is asked.
3. Add tests:
   - a fixture in `internal/agent/testdata/`: a session log from a real session, trimmed to what the test needs, and a test of the edits `Loader.Load` reads from it;
   - cases in `internal/agent/ask_test.go` with the fake CLI, which `fakeAgents` puts in PATH for every agent: the arguments, stdin and directory for a fork, a continued fork and a new session, the answer and model it reads, and its errors.

## Releasing

Pushing a `v*` tag, such as `v0.1.0`, runs `.github/workflows/release.yml`. GoReleaser builds the archives for macOS and Linux, attaches them and `checksums.txt` to the GitHub release, and pushes the cask to [JinmuGo/homebrew-tap](https://github.com/JinmuGo/homebrew-tap). The install script and the Homebrew cask pick up the new release from there.

From a clean checkout at the current remote `main`, use mise and an authenticated GitHub CLI:

```sh
mise release v0.1.2 --dry-run
mise release v0.1.2
```

Choose a stable `vMAJOR.MINOR.PATCH` tag newer than every existing stable tag. The task requires `origin`'s push URL to point to `JinmuGo/dr` on GitHub and the latest push CI run for that commit to have succeeded. The remote checks and push use that same URL. It pushes only the tag, without creating a local tag. The existing release workflow then publishes the archives and cask; a successful push does not mean publication has finished. If the push loses its connection, check the remote tag and workflow before trying again.

- The `HOMEBREW_TAP_TOKEN` secret must hold a token that can push to the tap.
- `goreleaser release --snapshot --clean` builds the same archives locally into `dist/` without publishing anything.

Changes to `site/` deploy to <https://dr.jinmugo.com> through `.github/workflows/site.yml`, which needs the `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` secrets. The Cloudflare Pages project is `dr-jinmugo-com`, and its custom domain is dr.jinmugo.com.
