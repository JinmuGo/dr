<p align="center">
  <a href="https://github.com/JinmuGo/dr">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.svg">
      <img src="docs/logo.svg" alt="dr" width="120">
    </picture>
  </a>
</p>

<p align="center"><b>Diff review with the AI that wrote it.</b></p>

<p align="center">
  <a href="https://github.com/JinmuGo/dr/actions/workflows/ci.yml"><img src="https://github.com/JinmuGo/dr/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/JinmuGo/dr"><img src="https://pkg.go.dev/badge/github.com/JinmuGo/dr.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</p>

<p align="center">
  <a href="#install">Install</a> ·
  <a href="#usage">Usage</a> ·
  <a href="#keys">Keys</a> ·
  <a href="#privacy">Privacy</a> ·
  <a href="docs/design.md">Design</a>
</p>

dr shows, for every changed line, which AI agent session wrote it and what you asked for. Ask that session why it wrote a line, leave review comments, and get fixes back from it. dr reads the local session logs of Claude Code, Codex and OMP.

![dr showing which agent session wrote each changed line](docs/demo.gif)

## Why dr

- **See who wrote each line.** `git blame` names you, because you committed. dr names the agent session that wrote the line and the request you gave it.
- **Ask the author.** Press `a` on a line to ask the session that wrote it why. dr asks a fork, so your conversation stays as it was.
- **Get fixes from the author.** Leave comments as in a pull request review, then `:submit`. Each session fixes only the lines it wrote; `y` applies a fix and `x` drops it.
- **Three agents, one diff.** Each session gets its own color, so lines from Claude Code, Codex and OMP sessions sit side by side in one review.
- **Local and exact.** Origins come from the session logs on your machine, with no model calls. Lines dr cannot explain stay unknown. There is no server, no account and no telemetry.

## Quick start

```sh
go install github.com/JinmuGo/dr@latest
cd your-repo   # after an agent has finished its work
dr
```

Move with `j` and `k`. `Enter` shows where the line under the cursor came from, `a` asks its session, `c` leaves a comment, `?` lists every key, and `q` quits.

## Status

Early. Everything above works today, and so do `e`, to fix a line in your editor, and `ga`, to open the session that wrote a line in its agent, right in this terminal. `dr blame <path>` prints the origins of one file as text.

Not built yet: `:ship`, to commit, push and open a pull request from dr, and pull request review, to see what people and bots left on a pull request in the same screen. Both are planned in [docs/roadmap.md](docs/roadmap.md). Why dr works the way it does is in [docs/design.md](docs/design.md).

## Install

dr needs Go 1.26 or later and git. It runs on macOS and Linux; other systems build but are not tested.

```sh
go install github.com/JinmuGo/dr@latest
```

Finding origins needs only the agents' session logs on your machine. Asking, fixing and `ga` also need the CLI of the agent that wrote the lines in your `PATH`: `claude`, `codex` or `omp`.

## Usage

Run it inside a git repository after an agent has finished its work.

```sh
dr              # changes since the merge-base with the default branch
dr <ref>        # changes since the merge-base of HEAD and <ref>
dr blame <path> # origins for one file, as text
```

The base is the merge-base of `HEAD` and the default branch (`origin/HEAD`, else `main` or `master`). dr compares the base with the files in your working tree, so uncommitted changes are included. `dr blame` always uses the default branch.

A session is one conversation with an agent, and a request is one message you sent in it. A line's origin is the session and request whose edit last changed the line.

Each session gets a color. A thin rail next to every changed line has the color of the session that wrote it, dotted when the origin is unknown and teal for a fix dr applied, and the first line a request changed in each hunk is labeled with the session and the request's first line, skipping headings and lines that end with a colon. Each file header has a dot for every session that changed the file. A session is named by its agent and the last four characters of its id, such as `claude c1a0`, and a request by its number in the session, `claude c1a0 #2`.

The status line starts with the mode you are in, then shows the agent and request for the line under the cursor and, on the right, questions being asked, fixes ready and pending comments. The row under it lists the keys of that mode. `Enter` opens the origin box below the line: the session, the request as you wrote it, the note the agent wrote right before the edit, and the reply that ended the request.

Press `v` on a file to mark it viewed, like the Viewed box on GitHub. A viewed file folds to its header and the file tree shows a check next to it. dr remembers viewed files per worktree, inside the git directory. If the file's diff changes after you marked it, the mark is cleared and the header says `Changed since last view`.

## Asking with `a`

On a changed line, `a` opens a box under the line to type your question in. Left empty, it asks "Why did you write this line this way?". `Enter` sends, `Shift-Enter` or `Alt-Enter` starts a new line, and `Esc` cancels. Select several lines with `V` first to ask about all of them. The answer arrives in a few seconds as a thread at the top of the origin box, like a GitHub review comment. Answers are one short sentence unless you ask for more, with a badge for where the reason comes from: `decided` with you, `chose` by the agent, or a `guess`. Press `a` inside the origin box to ask a follow-up.

Type `^` at the start of a word in the box to pick the model that answers. A finder like fzf opens over the diff with its own prompt: typing ranks the models that agent's CLI can run the way fzf does, `↑` `↓` or `Ctrl-P` `Ctrl-N` move, `Enter` or `Tab` picks, `Ctrl-U` clears and `Esc` goes back without a pick. To type a `^` itself at the start of a word, press `^` again in the empty finder. The pick is for that one question, and the next one goes back to the session's own model. A picked model shows on the box's top border and next to the answer. The session's reasoning effort is left out, because it belongs to the session's model.

The list comes from each agent:

- OMP: `omp models`, newest first by the version in the name.
- Codex: the model list it keeps in `~/.codex`.
- Claude Code, which has no list: its aliases `fable`, `opus`, `sonnet` and `haiku`.

Models the review's sessions used come first, and `Use "…" as typed` takes any name the list does not hold.

dr sends the question to a fork of the session that wrote the line, so the original conversation is not changed:

| Agent | Command |
| --- | --- |
| Claude Code | `claude -p --resume <session> --fork-session --safe-mode --tools Read,Grep,Glob --model <model>` |
| Codex | `codex exec fork <session> --ignore-user-config --disable hooks -c sandbox_mode="read-only" -c model="<model>"` |
| OMP | `omp -p --fork <session log> --session-dir <dr state> --tools read,grep,glob --approval-mode always-ask --no-extensions --no-skills --model <model>` |

The fork runs in the session's directory with the session's model and can only read files. Claude Code runs it in safe mode, Codex without your config and hooks. OMP runs it without extensions and skills, and with `always-ask` approval: with no one to approve, every write and every MCP call is refused, and only reads run. OMP keeps its forks in dr's state, not with your sessions. Later questions to the same session continue the same fork, until the session makes new edits. Questions and answers are kept with the viewed state and come back the next time you open dr. Each question resends the whole conversation to the model, so it costs what a new message in that session would cost.

When the author cannot be asked, dr asks the closest session that can, and says so under the answer:

- A line a Claude Code subagent wrote goes to the parent session that started the subagent. Claude Code cannot resume a subagent.
- A line with an unknown origin goes to the session that last edited the same file. If that session changed the line with a shell command, it knows why.
- If no session edited the file, a new session of the agent you used most in this review reads the code and answers. Its answer is a guess, and it says so.

The origin of such a line stays unknown; asking never changes it.

## Fixing with `c`

Press `c` on a line, or on a selection made with `V`, and type how to change it in the box that opens under the line, such as "use errors.Is". `^` picks the model for that comment, as it does for a question. Nothing is sent yet: the comment waits under its lines, like a pending GitHub review comment. When you are done reading, `:submit` sends every comment at once. dr groups them by the session that wrote their lines and sends each session one request, to the same fork `a` asks; comments for one session that picked different models go one model at a time. The fork can only read, so it answers with the new lines as JSON instead of editing files.

Each fix shows under its comment as a small diff, with anything the agent wants you to know. `y` applies it and `x` drops it; to get a different fix, edit the comment and submit again. `:undo` reverts the last fix dr applied. A fix taller than the screen can be read to its end: press `Enter` on the comment's last line, and the comments of that line scroll with its origin card.

dr writes the file itself, and only when it is safe:

- only regular files inside the worktree, never through a symlink;
- only when the file is exactly as it was when the request went out, or changed since only by fixes dr applied;
- only the comment's lines, through a temporary file renamed into place.

A line dr wrote this way shows `fix` in place of a request number, its origin box shows your comment, and `a` on it asks the fork that sent the fix. Only one dr per worktree applies fixes; another dr on the same worktree can still read, ask and comment, and a band over its diff says so.

For changes too big for a comment, press `ga` on a line. dr opens a fork of the session that wrote it in the agent's own CLI, in this terminal, the way `e` opens your editor: `claude --resume <session> --fork-session`, `codex fork <session>` or `omp --fork <session log>`, with the session's model. The agent remembers the whole conversation and works with your usual tools, settings and approvals; the original session is not touched. When you leave the agent, dr reads the session logs again and redraws the diff, so the lines the fork changed show the fork as their origin. For a second after you come back, `Ctrl-C` does nothing, so the second press of a double tap that closed the agent does not close dr too.

## Keys

| Key | Action |
| --- | --- |
| `j` `k` | Down, up a line. Arrows work too. Takes a count (5j) |
| `gg` `G` | Top, bottom. With a count, go to that row (12G) |
| `d` `u` `J` `K` | Half a page down, up |
| `}` `{` | Next, previous change |
| `l` `h` | Next, previous file. Arrows work too. Takes a count (3l) |
| `/` `n` `N` | Search, next, previous. Lowercase queries ignore case. Every match is lit and the status line counts them |
| `Enter` | Open the origin under the line. `j` `k` `d` `u` `J` `K` `gg` `G` scroll it, with the comments and fixes under that line, `a` asks a follow-up, `q` `Esc` `Enter` close |
| `s` | Switch between split and unified. Narrow screens stay unified |
| `v` | Mark the file viewed or not. Viewed files fold to their header |
| `V` | Select lines, as in vim. Moves extend it in the file. `V` or `Esc` ends it. The status line warns when the lines come from more than one session, which `a` and `c` cannot take |
| `a` | Ask the session that wrote the line or the selection, in a box under the line. The answer comes in the origin box |
| `e` | Edit the line in your editor: `$VISUAL`, else `$EDITOR`, else `vi`. The diff is rebuilt when it closes |
| `c` | Leave a change comment on the line or selection, or edit the one there, in a box under the line. Saving it empty removes it |
| `ga` | Open a fork of the session that wrote the line in its agent, in this terminal. The diff is redrawn when you leave it |
| In the box | `Enter` sends or saves, `Shift-Enter` or `Alt-Enter` (or `Ctrl-J`) starts a new line, `^` at the start of a word opens the model finder, `Esc` or `Ctrl-C` cancels. Arrows, `Ctrl-A` `Ctrl-E`, word moves, `Ctrl-U` and `Ctrl-W` edit |
| `:` | Type a command. A list above the status line shows each command and what it would do now |
| `:submit` | Send all pending comments, one request per session |
| `y` `x` | Apply, drop the fix under the comment |
| `:undo` | Revert the last fix dr applied |
| `Esc` | Clear a pending count or key, else the search |
| `?` | Help |
| `q` | Quit |
| Mouse | Drag the line between tree and diff to resize the tree, all the way left to fold it. The wheel scrolls. Click a file in the tree to go to it, or the Viewed box on a header to mark it. Hold Shift or Option to select text |

## How it works

dr runs git blame over agent edits. It takes the file at the base commit and replays, in time order, every edit an agent session made to it. Each line remembers the last edit that changed it. A line no edit explains is shown as unknown: a person changed it, or a tool that leaves no record, like a shell command. dr does not guess.

Finding origins makes no model calls and no network requests. It reads these files on your machine. `a` and `:submit` call a model through the agent's own CLI, and `ga` opens the agent for you to work in. What dr reads, writes and sends is under Privacy below.

| Agent | Session logs |
| --- | --- |
| Claude Code | `~/.claude/projects/<cwd>/<session>.jsonl`, subagents in `<session>/subagents/` |
| Codex | `~/.codex/sessions/<date>/rollout-*.jsonl` |
| OMP | `~/.omp/agent/sessions/<cwd>/` |

Session log formats are not public APIs and can change. If dr cannot read some records, those lines become unknown and a band over the diff names the agent. `dr blame` writes the same warning to stderr.

## Privacy

dr has no server, no account and no telemetry.

- **Reads:** the files of your repository and the agents' session logs on your machine, listed above. It opens the logs that changed since the base commit, and parses only those that mention the repository.
- **Writes:** its own state in the repository's git directory (`git rev-parse --git-path dr`), and the fixes you apply with `y`, only to regular files inside the worktree. While Codex answers, its last message goes through a temporary file that dr removes right after.
- **Sends:** nothing by itself. `a` and `:submit` run the agent's own CLI on a fork of the session, so the agent sends that conversation and your question or comment to its model provider, as a new message in that session would. `ga` opens the agent for you to work in, with your own settings. `^` lists models from the agent's CLI or the list it keeps.
- **Kept by the agents:** each fork is a session the agent records. Claude Code and Codex keep the forks dr makes with your other sessions; OMP keeps them in dr's state directory, except the forks `ga` opens, which it keeps with your other sessions.

## Edit notes

If an agent writes a sentence right before an edit, dr shows it as that edit's note. For Claude Code this is the text before the tool call in the same response, for Codex the progress message before the file change, and for OMP the text in the same message, or the one-line intent on the tool call when there is no text.

Agents do not always write one. Asking your agent to write one sentence before each edit that involves a decision makes the notes more useful.

## Limits

- A repository with no commits does not open, because there is no merge-base.
- Renames are not followed. A moved file shows as one deleted and one added file, and its lines are unknown unless an agent edited them after the move. Lines in files that Codex moved are unknown too.
- CRLF files show `\r` at line ends.
- If a deleted file's path is now a directory, it is shown as a directory, not a deleted file.
- `go.mod` is highlighted with the wrong grammar.
- Hunk headers do not show the enclosing function name.
- dr knows how to read and ask Claude Code, Codex and OMP. Other agents can be added; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
