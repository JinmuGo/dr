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
omp plugin install github:JinmuGo/dr#v0.1.6
```

Restart OMP after installing or updating the plugin. When the agent has finished its turn, run `/dr`. If the repository has multiple worktrees, choose the one to review. The diff opens in the same terminal. Press `q` in dr to return to the same OMP session and continue typing.

Use `/dr <branch>` to open a worktree directly without the picker. Other refs follow `dr <ref>` comparison rules. With one worktree, `/dr` reviews OMP's current working directory against the default branch. The diff includes other sessions' changes in the chosen worktree. If there are no changes, dr returns immediately.

For local development, run `omp plugin link /path/to/dr` with the repository root or an extracted release archive. Keep that directory in place while linked. To remove the plugin, run `omp plugin uninstall dr-omp` and restart OMP.

## Opening from Pi

Install dr on `PATH`, then install the package:

```sh
pi install git:github.com/JinmuGo/dr@v0.1.6
```

Restart Pi after installing or updating the package. `/dr` works as it does in OMP: choose a worktree when there are several, or name one with `/dr <branch>`, and press `q` in dr to return to the same Pi session.

For local development, run `pi -e /path/to/dr/integrations/pi/index.ts`, or `pi install /path/to/dr` to load the package from that directory. To remove it, run `pi remove git:github.com/JinmuGo/dr` and restart Pi.

## Reading the diff

A session is one conversation with an agent, and a request is one message you sent in it. A line's origin is the session and request whose edit last changed it. A line that no recorded edit explains is unknown: you changed it, or a shell command did. dr does not guess. When a log records an edit to the file that dr cannot read, such as one by OMP's `ast_edit`, by Pi's `codemode` or by a tool of an MCP server, the line's origin card names that edit instead, and a band at the top says which agent's records could not be read.

- Each session has a color. The rail next to a changed line has the color of its session, dotted when the origin is unknown and teal for a fix dr applied.
- The first line a request changed in each hunk carries a label such as `claude c1a0 #2`: the agent, the end of the session id and the request number.
- The status line shows the mode and the origin of the line under the cursor, and on the right the questions in flight, fixes ready and pending comments. The row under it lists the keys of the mode.
- `Enter` opens the origin card under the line. A line with questions shows its ask thread. Otherwise it shows the session, your request, the note before the edit, the final reply, and when the edit was made, in your time zone.
- `v` marks a file viewed, like the Viewed box on GitHub, and folds it. If the file's diff changes later, the mark is cleared.

## Asking with `a`

`a` opens a box under the line. Type a question, or press `Enter` to ask "Why did you write this line this way?". Select lines with `V` first to ask about all of them.

dr sends the question to a fork of the session that wrote the line. The fork can only read files, and your conversation is not changed. The origin card shows only this line's questions and answers, without the original request, note or reply. This also applies when you reopen a saved thread. Answers carry a badge for where the reason comes from: `decided` with you, `chose` by the agent, or a `guess`. `a` in the origin card asks a follow-up. Each question resends the session's conversation to its model, so it costs what a new message in that session would.

When the author cannot be asked, dr asks the closest session that can and says so:

- A line a Claude Code subagent wrote goes to the session that started the subagent.
- An unknown line goes to the session that last edited the file.
- If no session edited the file, a new session of the agent you used most reads the code, and its answer is marked as a guess.

Type `^` at the start of a word in the box to pick another model for that one question.

Pi runs a question without extensions, so a model that only an extension provides cannot answer it. Pi then answers with its default model, and the answer names that model. The `^` list shows only the models Pi has without extensions. OMP sessions that Pi imported are Pi sessions to dr, and questions about them go to `pi`.

## Fixing with `c`

`c` leaves a comment on the line or the selection, such as "use errors.Is". Nothing is sent yet: the comment waits under its lines, like a pending GitHub review comment. `:submit` sends every comment at once, one request per session, to the same fork `a` asks.

Each fix shows under its comment as a small diff. `y` applies it, `x` drops it, and `:undo` reverts the last fix dr applied. To get a different fix, edit the comment and submit again. dr writes a fix only to the comment's lines in a regular file inside the worktree, and only if the file has not changed since the request went out, apart from fixes dr applied. Only one dr per worktree applies fixes.

To fix a line by hand, `e` opens it in your editor. For a change too big for a comment, `ga` opens a fork of the session that wrote the line in its agent, right in this terminal. When you leave the agent, dr redraws the diff with the fork's edits.

## Following threads with `t`

Questions and fix requests run in the background, so you can keep reviewing while they wait. `t` shows every thread in place of the file tree, the ones that need you first. `◉` is an answer that came while you were elsewhere, or a fix waiting for `y` or `x`, `●` is still waiting, `✗` got no answer or fix, `✎` is a comment `:submit` will send and `○` is read. Enter on a thread opens it where it lives in the diff. An answer counts as read once its origin card shows it. dr forgets what you read when it quits, and quitting stops the questions that are still waiting.

## Colors

dr picks light or dark GitHub colors to match your terminal. To use another theme, set `DR_THEME`, for example in your shell profile:

```sh
export DR_THEME=onedark
```

The themes are `github` (the default), `catppuccin-latte`, `catppuccin-mocha`, `gruvbox`, `onedark` and `tokyonight`. A theme sets the colors of the code, the changed lines and the panels. Unchanged lines keep your terminal's background, and the tinted lines are mixed into it. Pick a light theme for a light terminal and a dark one for a dark terminal, or the text is hard to read. `NO_COLOR=1` turns colors off whatever the theme.

## Split view by default

dr opens the unified view. To open the split view instead, set `DR_VIEW`:

```sh
export DR_VIEW=split
```

`DR_VIEW` takes `unified` (the default) or `split`. The split view needs a diff at least 100 cells wide, so on a narrower screen dr shows the unified view until there is room. `s` still switches between the two.

## Focusing on a file with `space`

`space` opens the diff of the file under the cursor in a box over the screen, without the file tree and the other files. The keys that move stay in the file, `h` and `l` go to the previous and the next file, `s` switches between the split and the unified view, and `v` marks the file viewed. The box is wider than the diff beside the tree, so it shows the split view on screens where the diff stays unified. `space`, `Esc` or `q` closes it with the cursor where you left it. The box shows only the lines: comments, origin cards and search stay in the diff.

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
| `space` | Focus on the file under the cursor in a box. `j` `k` `d` `u` `J` `K` `gg` `G` `{` `}` move inside it, `h` `l` go to another file, `s` splits it, `v` marks it viewed, `space` `Esc` `q` close |
| `v` | Mark the file viewed or not |
| `V` | Select lines, as in vim. `V` or `Esc` ends it |
| `a` | Ask the session that wrote the line or the selection |
| `c` | Leave a comment on the line or the selection, or edit the one there. Saving it empty removes it |
| `:submit` | Send all pending comments |
| `y` `x` | Apply, drop the fix under the comment |
| `:undo` | Revert the last fix dr applied |
| `t` | Show the threads in place of the file tree: every question and comment, new answers and fixes first. `j` `k` move, `Enter` goes to one, `Esc` returns to the diff, `t` brings the files back |
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
