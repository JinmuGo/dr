# Using dr

Run dr in a git repository after an agent has finished its work.

```sh
dr              # changes since the merge-base with the default branch
dr <ref>        # changes since the merge-base of HEAD and <ref>
dr <branch>     # the worktree that has <branch> checked out, against the default branch
dr blame <path> # the origins of one file, as text
dr --version
```

dr compares the merge-base with the files in your working tree, so uncommitted changes are included. The default branch is `origin/HEAD`, else `main` or `master`. `dr blame` always uses the default branch.

When `<branch>` is checked out in another worktree, such as one an agent works in, `dr <branch>` opens that worktree as if you ran `dr` there: questions, comments and fixes go to its files. The default branch and the branch of the worktree you are in stay refs to compare with. To compare with a branch checked out elsewhere, name it in full: `dr refs/heads/<branch>`.

## Opening from OMP

Install dr on `PATH`, then install the OMP plugin:

```sh
omp plugin install github:JinmuGo/dr#v0.1.3
```

Restart OMP after installing or updating the plugin. When the agent has finished its turn, run `/dr`. Only worktrees with changes against the default branch's merge-base are offered. Committed, staged, unstaged and untracked changes count; ignored files do not. If only one worktree has changes, it opens directly. If none do, OMP shows a "No changes to review" dialog without launching dr. Press Enter or Esc to dismiss it.

The diff opens in the same terminal. Press `q` in dr to return to the same OMP session and continue typing. The picker is repository-wide, not filtered to the current AI session.

Use `/dr <branch>` to open a worktree directly without the filtered picker. Other refs follow `dr <ref>` comparison rules. This also lets you open a clean worktree where a saved fix can still be undone. Explicit refs retain dr's behavior: with no changes and no fix to undo, dr returns immediately.

For local development, run `omp plugin link /path/to/dr` with the repository root or an extracted release archive. Keep that directory in place while linked. To remove the plugin, run `omp plugin uninstall dr-omp` and restart OMP.

To keep a local build after its development worktree is removed, install a commit-addressed archive instead of linking the checkout:

```sh
package_dir="${XDG_DATA_HOME:-$HOME/.local/share}/dr/omp-plugins"
mkdir -p "$package_dir"
archive="$package_dir/dr-omp-$(git rev-parse HEAD).tgz"
git archive --format=tar.gz --prefix=package/ --output="$archive" HEAD \
  package.json integrations/omp/index.ts LICENSE
omp plugin install "dr-omp@file:$archive"
```

The archive contains committed files only. Keep it in place: the package manager records its path as a `file:` dependency. Restart OMP after installation.

## Reading the diff

A session is one conversation with an agent, and a request is one message you sent in it. A line's origin is the session and request whose edit last changed it. A line that no recorded edit explains is unknown: you changed it, or a shell command did. dr does not guess.

- Each session has a color. The rail next to a changed line has the color of its session, dotted when the origin is unknown and teal for a fix dr applied.
- The first line a request changed in each hunk carries a label such as `claude c1a0 #2`: the agent, the end of the session id and the request number.
- The status line shows the mode and the origin of the line under the cursor, and on the right the questions in flight, fixes ready and pending comments. The row under it lists the keys of the mode.
- `Enter` opens the origin card under the line: the session, your request, the note the agent wrote right before the edit, and the reply that ended the request.
- `v` marks a file viewed, like the Viewed box on GitHub, and folds it. If the file's diff changes later, the mark is cleared.

## Asking with `a`

`a` opens a box under the line. Type a question, or press `Enter` to ask "Why did you write this line this way?". Select lines with `V` first to ask about all of them.

dr sends the question to a fork of the session that wrote the line. The fork can only read files, and your conversation is not changed. The answer arrives in a few seconds at the top of the origin card, with a badge for where the reason comes from: `decided` with you, `chose` by the agent, or a `guess`. `a` in the origin card asks a follow-up. Each question resends the session's conversation to its model, so it costs what a new message in that session would.

When the author cannot be asked, dr asks the closest session that can and says so:

- A line a Claude Code subagent wrote goes to the session that started the subagent.
- An unknown line goes to the session that last edited the file.
- If no session edited the file, a new session of the agent you used most reads the code, and its answer is marked as a guess.

Type `^` at the start of a word in the box to pick another model for that one question.

## Fixing with `c`

`c` leaves a comment on the line or the selection, such as "use errors.Is". Nothing is sent yet: the comment waits under its lines, like a pending GitHub review comment. `:submit` sends every comment at once, one request per session, to the same fork `a` asks.

Each fix shows under its comment as a small diff. `y` applies it, `x` drops it, and `:undo` reverts the last fix dr applied. To get a different fix, edit the comment and submit again. dr writes a fix only to the comment's lines in a regular file inside the worktree, and only if the file has not changed since the request went out, apart from fixes dr applied. Only one dr per worktree applies fixes.

To fix a line by hand, `e` opens it in your editor. For a change too big for a comment, `ga` opens a fork of the session that wrote the line in its agent, right in this terminal. When you leave the agent, dr redraws the diff with the fork's edits.

## Keys

| Key | Action |
| --- | --- |
| `j` `k` | Down, up a line. Arrows work too. Takes a count (5j) |
| `gg` `G` | Top, bottom. With a count, go to that row (12G) |
| `d` `u` `J` `K` | Half a page down, up |
| `}` `{` | Next, previous change |
| `l` `h` | Next, previous file. Arrows work too. Takes a count (3l) |
| `/` `n` `N` | Search, next, previous. A lowercase query ignores case |
| `Enter` | Open the origin card. `j` `k` `d` `u` `J` `K` `gg` `G` scroll it, `a` asks a follow-up, `q` `Esc` `Enter` close |
| `s` | Split or unified view. Narrow screens stay unified |
| `v` | Mark the file viewed or not |
| `V` | Select lines, as in vim. `V` or `Esc` ends it |
| `a` | Ask the session that wrote the line or the selection |
| `c` | Leave a comment on the line or the selection, or edit the one there. Saving it empty removes it |
| `:submit` | Send all pending comments |
| `y` `x` | Apply, drop the fix under the comment |
| `:undo` | Revert the last fix dr applied |
| `e` | Edit the line in `$VISUAL`, else `$EDITOR`, else `vi` |
| `ga` | Open the session that wrote the line in its agent, in this terminal |
| `:` | Type a command. A list shows each command and what it would do now |
| `Esc` | Clear a pending count or key, else the search |
| `?` | Help |
| `q` | Quit |

In the `a` and `c` box:

| Key | Action |
| --- | --- |
| `Enter` | Send or save |
| `Shift-Enter` `Alt-Enter` `Ctrl-J` | New line |
| `^` | Pick a model, at the start of a word |
| `Esc` `Ctrl-C` | Cancel |

With the mouse, drag the line between the tree and the diff to resize the tree, all the way left to fold it. The wheel scrolls. Click a file in the tree to go to it, or the Viewed box on a file header to mark it. Hold Shift, or Option on macOS, to select text.
