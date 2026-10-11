# Roadmap

These features are planned, not built. The plans may change before or while they are built.

## Later

- `:ship`: commit, push and open a PR. The plan is under "Plan: `:ship`" below.
- PR review: see reviews that other people or bots left on a PR in the same screen and decide whether to apply them. The plan is under "Plan: PR review" below.
- An origin cache.
- A file header that stays at the top while scrolling, folding folders in the tree, and a filter box for file names, as GitHub has.
- Known limits. Each has one place to fix.
  - A repo with no commits does not open because there is no merge-base. Fix it in the base lookup in `internal/git/`.
  - Lines in files Codex moved are unknown. Fix `internal/agent/codex.go` to carry over the state of the old path.
  - CRLF files show `\r` at line ends. Fix it in `review.Visible`, the one place that changes screen text.
  - If a file's path in base is now a regular directory, it shows as a directory, not a deleted file. Fix `review.File` to treat it as deleted when the base side is a file.
  - `go.mod` is colored as AMPL because chroma matches the name pattern `*.mod` to the AMPL grammar. Fix it where `internal/tui/highlight.go` picks the lexer.
  - Hunk headers do not show the function name like GitHub does. `review.Hunks` needs to find the function line before the hunk.
  - A binary file stays viewed when only its content changes, because its hash covers the status and not the bytes. Fix it in `File.Hash` in `internal/review/` by hashing both sides' bytes.
  - Files moved with `git mv` are shown as one deleted and one added file, and the added file's lines are unknown. Fix it by following renames in `internal/git/` and `internal/origin/`.

### Plan: `:ship`

`:ship` goes through these steps in order and can stop at any of them.

1. Ready: wait until no question or fix request is in flight.
2. Target: pick the PR's base branch, the default branch unless `dr <ref>` opened another. Pin the base SHA to the merge-base of that branch and HEAD, and redraw the diff from it.
3. Draft: ask the fork of the session that wrote the most lines for a commit message, a PR title and a body, and open them in `$EDITOR`.
4. Commit: read HEAD into a temporary index, add the changed files, and make commit C with `git commit-tree`. No ref and not the real index changes yet. With no changed file, C is HEAD.
5. Review: when the person has set a `review_cmd`, dr runs it with the pinned base, never through an agent. The worktree's tree must equal C's tree before and after it runs, and the result must name the same repository and base. A failure, a timeout or a skipped review does not pass. Findings show as threads; fixing them starts again at step 4. `:ship!` goes on after a person has read them.
6. Confirm: before anything reaches a remote, one screen shows the remote URL, the target ref, C's SHA, the PR repository, base and head, title, body and the review result. dr records exactly these values on confirmation and uses only them from here on.
7. Push: move the branch with `git update-ref <branch> C <old HEAD>`, which fails if HEAD moved in between. Bring the real index to C only for paths that still match the old HEAD; paths staged in between are left alone and reported. Then `git push <remote URL> C:<ref>` and `gh pr create --repo --base --head --title --body-file`, or only the push when the PR exists.

A failing step stops `:ship` and shows the steps left. A change to the worktree or HEAD after step 4, or to the confirmed values, drops the review result and the confirmation, and `:ship` starts again at step 4. dr never force-pushes.

### Plan: PR review

Reviews left on a PR come into the same screen as threads, and are handled with the same keys. Text from a PR is untrusted input, so only sentences a person has read and kept go into a fix request.

1. Fetch: `gh api graphql` reads review threads on lines (path, side, first and last line, base and head SHA, diff hunk, resolved, outdated), review bodies and PR comments, with their authors and whether they are bots. `:pull` fetches at once, and dr checks every minute while it is open. Each thread and comment is recorded with its GitHub id and a hash of its body, so a fetch never doubles them and an edited comment is kept as a new version.
2. Place: fetch the base and head commits. A comment on the new side is moved through the diff from head to the worktree. A comment on the old side goes on the same deleted hunk in dr's diff. When the lines it points at changed since, or GitHub marks it outdated, it shows dim and its place is only a hint. When no single place fits, it shows as a thread on the whole PR.
3. Intent: for each new thread, a fork of the session that wrote the lines, run without tools, gets the thread, the code and the lines' origin, and returns as JSON what the reviewer wants, a judgment (apply, apply in part, do not apply, ask back) and a one-line reason. The result records the comment version and the worktree's tree hash, and is stale once either changes; a stale judgment cannot request a fix or resolve a thread.
4. Decide: `c` opens the comment box filled with that intent, and what the person keeps becomes the fix request. The reviewer's own words go in only when the person quotes them, marked as text from the PR. `a` asks the same fork more, `r` drafts a reply, and `x` marks the thread resolved. A GitHub suggestion applies directly only when its lines are still exactly as they were when it was written.
5. Send: fixes go up through `:ship` to the PR's branch. Replies and resolves go through `:reply`, which reads the threads and the head SHA again right before sending, shows every reply in full on one screen for approval, records each attempt and its result, and marks each reply with a hidden `<!-- dr:<batch>:<thread> -->` comment. A reply whose result is unknown is never sent again on its own; dr looks for its mark first.

Runs that read text from a PR get no tools: Claude Code with `--safe-mode --tools ""`, OMP with `--no-tools`, and Codex with its shell tool turned off.
