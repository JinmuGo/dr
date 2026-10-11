# dr

A terminal review tool that helps a person understand code that agents wrote and fix it line by line. The name comes from diff review.

Committing, opening PRs and handling PR reviews are planned, not built; their plans are in [roadmap.md](roadmap.md).

## Principles

Keep it simple, the way Go is.

- dr does three things only. It shows the origin of every line, asks the session that wrote a line, and fixes lines.
- Keys are the ones you know from vim. dr adds its own only for its work: `a`, `c`, `e`, `ga` for the three things, `s` and `v` for split and Viewed the way GitHub has them, and `:submit` to send comments. Every key is in [the key list in the README](../README.md#keys).
- What is not known is shown as unknown. Lines with no origin found are not filled in by guessing.
- Almost no settings. The defaults work as they are.
- Dependencies start with Bubble Tea and Lip Gloss. Every new one comes with a written reason. `chroma` colors code per language, as GitHub and tuicr do, which the standard library alone cannot. `github.com/junegunn/fzf/src/algo` ranks and lights the model finder's matches exactly as fzf does. It brings fzf's `src/util`, `go-shellwords` and `go-isatty`, and its version is pinned because `src/algo` is not an API fzf promises to keep. Running the real fzf program was rejected: it leaves the screen and needs a fallback where fzf is missing. teatest is used by tests only.
- Go code follows the Google Go Style Guide and Effective Go. Errors are wrapped with the standard `%w` instead of an error library, to avoid a dependency.
- When screen behavior is unclear, look at how [hunk](https://github.com/modem-dev/hunk) and [tuicr](https://github.com/agavra/tuicr) do it.
- Add things when they are needed.

## The flow

1. When an agent finishes, open `dr` in the repo. The default range is from the merge-base with the default branch to the working tree. `dr <ref>` changes the base.
2. The agent and request number for the cursor line are always on the status line. `Enter` opens the origin box right below that line: which session of which agent wrote it in which request, the request as written, the note the agent wrote right before that edit, and the reply the agent wrote when it finished that request. Inside the box, `j` `k` read to the end and `Esc` closes it.
3. For more, `a` asks that session. The answer lands in the origin box in about 10 seconds.
4. Leave a change comment with `c` on each line that needs fixing, then send them all with `:submit`. Fix a line by hand with `e`.

## Origins

- Read straight from session logs. No model is called, so it shows at once and nothing is made up.
- A line with no origin found is shown as unknown. A person changed it, or an agent changed it with a shell command like sed. Such lines are fixed with `e` only.
- The origin of a deleted line is the session of the edit that deleted it.
- If the agent wrote something right before an edit, it is shown as that edit's note. For Claude Code it is the text before the tool call in the same response, for Codex the progress message before that file change, for OMP the text in the same message. When OMP has no such text, the one-line intent on the tool call is shown instead. Getting agents to leave this sentence is the job of an agent skill.

## Asking: `a`

- `a` opens an input box right under the line, like tuicr's comment box, with "Why did you write this line this way?" as a placeholder ("Why did you delete this line?" on a deleted line). Type your own, or press `Enter` to send the placeholder. `Shift-Enter`, `Alt-Enter` or `Ctrl-J` starts a new line. Select several lines with `V` to ask about the range; the changed lines in it must come from one session. The box has a rounded outline in its own color, violet to ask and amber to comment, the only colored outline on the screen; without colors it is drawn with heavy lines. Its top border names the session, the range and the model, and its bottom border says what sending does. It comes before the line's comment boxes, so the box and its cursor stay on the screen however long those are.
- The question goes to a fork of the session that wrote the line. The original session is not touched.
- The answer starts with a tag that says where the reason comes from: `[decided]` when it was agreed with the user, `[chose]` when the agent chose it, `[guess]` when the agent does not remember or cannot know. Then comes one sentence of at most 25 words, in the language of the conversation, unless the question asks for more. dr shows the tag as a colored badge and the sentence as the answer.
- Questions show as a thread at the top of the origin box, the way GitHub shows review comments: `you` with the question, then who answered with the badge, the model and how long it took, then the answer. A model other than the session's is shown in the warning color. While an answer is on its way, the elapsed seconds count up. If the box is closed when the answer comes, a notice at the top right says where it is, and `Enter` reads it. `a` inside the box asks a follow-up about the same lines.
- Asking again about any line of the same session continues the same fork, so the agent sees the earlier questions. The origin box of a line shows the questions about that line.
- When the author cannot be asked, the question goes to the closest session that can, and the answer says so. A line a Claude Code subagent wrote goes to the parent session, because Claude Code cannot resume a subagent. A line with an unknown origin goes to the session that last edited the same file; if it changed the line with a shell command, it knows why. If no session edited the file, a new session of the agent used most in this review reads the code and answers as a guess. The origin stays unknown either way.
- Unchanged lines and a selection that mixes sessions cannot be asked, and dr says why. A session takes one question at a time; different sessions can be asked at the same time.

## Fixing: `e` `c`

- `e` opens `$VISUAL`, else `$EDITOR`, else `vi`, at that line of that file, the way git runs an editor. A deleted line opens where it was deleted, and a file header at the file's first change. Save and close, and the diff is rebuilt from the files and the session logs, with the cursor on the same line. A deleted or binary file has nothing to edit, and dr says so. When no changed file is left, the screen says `No changed files`. dr also opens on that screen when nothing differs from base but a fix it applied is still in place, so `:undo` works after a fix that brought a file back to base. The rebuild also reads new session edits, so a fork that does not know a session's newer edits is not asked again. Only the newest rebuild is shown, so a slow earlier one never brings back an old diff.
- `c` leaves a change comment on that line or the range selected with `V`, typed in the same input box: how to change it, such as "use errors.Is" or "add a one-line comment". Nothing is sent yet. The comment shows in a box under its last line like a pending GitHub review comment, naming the session it goes to, and the file tree and the file header count a file's comments with `✎`. `c` on a commented line opens the box on its comment to edit it, and saving it empty removes it. A selection that overlaps a comment without being its range cannot take another one.
- `:submit` sends every pending comment at once, the way GitHub and tuicr submit a review. dr groups the comments by the session that wrote their lines and sends each group to that session's fork in one request, the same fork `a` asks. Each fork sends back one fix per comment that changes only that comment's lines. A session still answering a question or an earlier request is skipped, and dr names it; the other sessions still get theirs. While a request waits, its comments show how long it has waited. A comment can pick its model with `^` as a question can. Comments for one session that picked different models make one request per model; the first goes, and dr holds back the others until the next `:submit`, because a session takes one request at a time.
- Questions with `a` do not wait for `:submit`. A question needs its answer while you read, so it goes out right away.
- A comment's lines must all come from one session. A range that mixes sessions, or lines without an origin, cannot take a comment because there is no single session to ask. dr says so; narrow the range, or fix those lines with `e`. Lines a Claude Code subagent wrote go to the parent session, as questions do. A comment covers the current file's lines in its range, so deleted lines alone cannot take one.
- Fixes are overlaid on the diff under their comments, as a small diff with the agent's note under it. `y` applies one and `x` drops it. A dropped fix leaves its comment pending, so it can be edited and submitted again. Editing a comment drops its fix, because the fix answered the old text. A comment whose request is still waiting cannot be edited.
- If lines outside a comment also need to change, the agent does not change them and only says where. Widen the comment and submit again. One comment covers at most 200 lines.
- Large changes do not happen in dr. `ga` opens a fork of the session that wrote the line in the agent's own CLI, in this terminal, the way `e` opens the editor, to work with the agent directly. It goes to the session `a` would ask: the author, the parent of a Claude Code subagent, or the session that last edited the file for a line with no origin. Lines changed there are recorded in the fork's session log, so when the agent exits and dr rebuilds the diff, it finds their origins. Codex exits on one `Ctrl-C`, so while the agent or the editor has the terminal, and for a second after it hands the terminal back, `Ctrl-C` does not quit dr. Leaving the agent with `Ctrl-C` is not reported as an error.
- `:undo` reverts the last fix dr applied, and its comment is pending again. If the file changed after the fix was applied, it does not revert.

## The look

The diff stays in the middle, and the origin of every changed line shows in the diff itself. The background stays quiet, and color carries meaning only: blue for focus, violet for questions, amber for comments and warnings, teal for fixes. Only the box that takes keys has a colored outline. The screen works in dark and light terminals and with `NO_COLOR`.

- The palette is tinted from the terminal's own background, with quiet added and deleted lines.
- The status line starts with a mode pill (`NORMAL`, `VISUAL`, `ORIGIN`, `ASK`, `COMMENT`, `SEARCH`, `COMMAND`, `HELP`), then the file and line and the cursor line's origin with the first line of its request, and on the right what is in flight: questions being asked, fixes ready, pending comments. The last row lists the keys of the current mode.
- The tree's title shows the viewed count over a progress line. The file header shows the counts in bold and the diffstat as GitHub's five squares, drawn with Nerd Font's fa-stop (U+F04D) so neighbors keep a gap: `■` fills the whole cell in common fonts and runs into a bar. A terminal without Nerd Font symbols shows boxes there, which is accepted. The header of the file at the top of the diff stays there while scrolling.
- Each session gets a color in the order it first appears in the review and keeps it when the diff is rebuilt; the six colors repeat after the sixth session. A one-column rail between the line numbers and the sign shows the color of the line's session, dotted for unknown lines and teal for fixes dr applied. The file header has a dot for each session that changed the file.
- In each hunk, the first line of a request's first run ends with a label: the session tag and the request's summary; later runs show only the rail. A label goes after the code, on its own side in the split view, and is left out when the whole tag does not fit; only the summary is ever cut. The summary is the request's first line that is not a Markdown heading and does not end with a colon, because OMP subagent prompts start with `Complete assignment thoroughly:`. The status line and the origin box use it too.
- Session tags end with the last four characters of the session id, more when two sessions of the review share them, because OMP session ids are UUIDv7, whose start is a time.
- There is no legend of session colors. A review can hold twenty or more sessions, most of them subagents, so each of the six colors stands for several sessions and a legend cannot tell them apart. The label on each run and the status line name the session exactly, so the tree keeps its room for files.
- The origin box, the question thread, and the comment and fix boxes are cards: a band on the left in the line's color that continues the rail, a filled background, a header row, small capital section labels, badges as colored chips, and a scroll bar when the text does not fit. The open origin card sits right under its row, and while it is open it and the comment cards under the same row scroll as one block, so a fix longer than the screen can be read to its end before `y`. A fix's lines wrap in its card instead of being cut.
- `^` in the `a` and `c` box opens a finder drawn like fzf: its own prompt, a count line, matches ranked and lit by fzf's algorithm, and a preview of the highlighted model.
- Some things float over the diff. The help is a centered box over a dimmed screen, grouped by task. `:` lists the commands with what each would do now. Short notices sit in a box at the top right for answers that arrive while their box is closed, submit results, and fixes that arrive or are applied, dropped or reverted; each stays until the next key. A band over the diff says when session records could not be read, and a band and a `read-only` chip show while another dr holds the lock.
- Search lights every match and counts them, `Enter` keeps the match lit, and `Esc` clears the search when no count is pending. A selection is blue and says before `a` or `c` when it mixes sessions. The empty screen says what was compared and why it is empty. Narrow screens drop the request text from labels and the status line first.

## Layout

| Path | What it does |
| --- | --- |
| `main.go` | The `dr` and `dr blame` commands. Only parses arguments and wires things together |
| `blame.go` | The `dr blame` output |
| `internal/git/` | Base, branch, worktrees, file contents |
| `internal/diff/` | Line matching between two versions and word diff inside a line |
| `internal/origin/` | Replays session edits to find the origin of each line |
| `internal/review/` | Data shared by `dr blame` and the screen: changed files, origin per line, hunks, and the question prompt with its answer tags |
| `internal/agent/` | Reads each agent's session logs and asks forks |
| `internal/tui/` | Screen and keys. File tree, syntax colors, colors for dark and light terminals |
| `internal/fix/` | Fix requests and their answers, and writing fixes to files only when it is safe |
| `internal/state/` | State dr keeps between runs: viewed files, the log of questions, comments and fixes, and the lock |

All packages live under `internal/`. Nothing outside can import them, so their insides can change freely. There is one command, so there is no `cmd/`; `main.go` at the root is the command.

An agent is one file under `internal/agent/` and one entry in its registry. The file knows where the agent keeps its session logs, how to read edits from them, and how to fork a session, continue a fork or start a new session through the agent's CLI. Nothing outside that file names the agent. Claude Code, Codex and OMP are the three today; CONTRIBUTING.md says how to add another.

## How origins are found

It is git blame over agent edit records.

| Agent | Log location | Edits read |
| --- | --- | --- |
| Claude Code | `~/.claude/projects/<cwd>/<session>.jsonl`, subagents in `<session>/subagents/` | Content of `Write`; the file before the edit and the replaced strings from the `Edit` result |
| Codex | `~/.codex/sessions/<date>/rollout-*.jsonl` | `FileChange` in `item_completed`: new file content or a unified diff |
| OMP | Session and subagent records in `~/.omp/agent/sessions/<cwd>/` | Content of `write`; the content before and after, or the diff, from the `edit` result |

1. Only edits from the earliest of these are used: the commit time of base, the author time of commits after base, and the time the branch was made. A rebase can move base after the work, but author times and the reflog remain. Only session files changed after that time and containing the repo path are read.
2. For each edit, build the file content before and after it. A Claude `Edit` replaces the string only when it appears exactly once. Codex and OMP diffs are applied to the previous state. If the line numbers do not match, the diff is applied where the changed lines and their surrounding lines match in exactly one place. OMP drops the before and after content of old edits as a session grows and keeps only the diff, so the diff is used. When even the `write` content was dropped, or Codex moved a file, dr does not follow it and those lines are unknown.
3. Starting from the base file, replay the edits in time order and remember, for every line, the last edit that changed it. Unchanged lines keep their previous origin. So an edit that rewrites the whole file only takes the lines it actually changed. Edits that delete base lines are remembered too. If a deleted base line is put back in the same place, it reverts to a base line, so the next edit that deletes it becomes its origin. Lines are matched with LCS. For large files the match is split at lines that appear exactly once on each side.
4. If the before content recorded in an edit differs from the replayed state so far, the file was changed outside the records in between. The differing lines become unknown and the replay continues. Edits that do not fit the previous state are skipped. Lines where the final replayed result differs from the actual file are also unknown.
5. The diff shown is a direct comparison of the base and the current file. The replay only decides who wrote the lines in that diff. When the same line appears several times and the replay and the diff pair them differently, the line is unknown.
6. A session fork carries the original edit records with the same timestamps. When several sessions have an edit with the same time and content, the one from the session that started first is kept. If they started at the same time, it is not known which one it was, so it is unknown. New edits made in a fork have different timestamps and belong to the fork. Codex forks do not carry file change records.
7. If a session ran in the file's worktree and its log has a branch, it must match the current branch. Claude has it in `gitBranch`, Codex in `session_meta`. Edits from other worktrees are used only when the logs of sessions that ran in that worktree record the current branch. OMP has no branch, so only edits to files inside this worktree are used.
8. For fixes dr applied, the dr records are the origin: the lines a fix wrote belong to the session that sent it, tagged `fix` in place of a request number, with the comment in place of the request. These edits do not count as the session's newer work, so `a` on a fixed line goes on in the fork that sent the fix. An undone fix leaves no trace.

Session log formats are not public APIs. When they cannot be read, those lines become unknown and a band over the diff names the agent whose logs failed. `dr blame` writes it to stderr. If this gets slow, a cache will be added then.

## Session forks

| Agent | Make a fork | Continue |
| --- | --- | --- |
| Claude Code | `claude -p --resume <id> --fork-session --safe-mode --tools Read,Grep,Glob --model <model> --output-format json` | the same with `--resume <fork>` and no `--fork-session` |
| Codex | `codex exec fork --json -o <file> --ignore-user-config --disable hooks -c sandbox_mode="read-only" -c approval_policy="never" -c model="<model>" <id> -` | `codex exec resume` with the same flags and `<fork> -` |
| OMP | `omp -p --fork <session log> --session-dir <git dir>/dr/sessions --tools read,grep,glob --approval-mode always-ask --no-extensions --no-skills --no-title --mode json --model <model> --thinking <level>` | the same with `--resume <fork>` instead of `--fork` |

- dr keeps one fork per writing session and continues it for later questions. When the original session has an edit newer than the fork, the next question makes a new fork, because the old fork does not know that work.
- The prompt goes on stdin. A question that takes more than 3 minutes is stopped, and quitting dr stops the questions in flight. The CLI runs in its own process group, which dr kills when the CLI ends, so nothing it started is left running.
- Forks only read. Claude gets `--tools Read,Grep,Glob`, Codex gets `-c sandbox_mode="read-only"`. Only dr writes files.
- Hooks and MCP are off. Claude gets `--safe-mode`, Codex gets `--ignore-user-config` and `--disable hooks`. A fork needs only the original conversation.
- The model is the one in the session log, and for Codex and OMP the reasoning effort too. If a different model answers, you are not asking the author. dr reads the answering model from Claude's JSON output and from the last turn of the Codex fork's own session log, because Codex's events do not name it. `^` at the start of a word in the `a` and `c` box opens a finder that picks another model for that one question or comment, from the models the CLI of the session's agent can run: any OMP model for an OMP fork, Claude models for a Claude Code fork, OpenAI models for a Codex fork. A picked model goes without the session's reasoning effort, and the answer shows it with a `^` instead of a warning. The fork stays the same, so the picked model still sees the whole conversation.
- The fork starts in the cwd from the session log.
- Every question sends the original conversation again. In testing, Codex used about 190,000 input tokens, 150,000 of them cached. Claude used about 8,000. So a question is sent only when a person presses the key.
- OMP forks a session log into dr's state directory with a new session id, so nothing is written next to your own sessions and dr never reads its forks as sessions. OMP subagent logs are full session logs, so a line an OMP subagent wrote is asked directly. In testing, a question to an OMP fork took 4 to 7 seconds. MCP does not delay it; OMP waits at most 250 ms for MCP servers to start.
- OMP's `--tools` does not remove MCP tools; they stay reachable as `xd://` devices, and some of them write to outside systems. MCP tools and the write tool are `write` tier, so `--approval-mode always-ask` makes them ask for approval, and in print mode a call that needs approval is refused. A test fork could not write a file or call an MCP tool, and could still read.

`ga` makes a fork to work in, not one to ask: `claude --resume <id> --fork-session --model <model>`, `codex fork -c model="<model>" -c model_reasoning_effort="<effort>" <id>` or `omp --fork <session log> --model <model> --thinking <level>`, in the session's cwd. It gets none of the read-only flags above. It is the user's own session, with their config, tools, hooks and approvals, and it is stored with their other sessions, where dr reads its edits.

A question is sent like this.

```text
[dr] Review question. Do not change any code, just answer.
ratelimit.go:17
>	initialized bool
Why did you add a separate initialized field instead of last.IsZero()? Start your answer with exactly one tag: [decided] if the reason was agreed with the user in this conversation, [chose] if you chose it yourself, or [guess] if you do not remember or cannot know. Then give the reason in one sentence of at most 25 words, in the language of this conversation, unless the question asks for more. No preamble, no restating the question or the code, no suggestions.
```

A selection quotes every line in order, `>` for lines in the current file and `-` for deleted lines, and the location line then adds `Lines marked - were deleted.`

A fix request carries all of one session's comments. dr decides the paths and ranges, and the agent returns only the changed lines for each comment. The request also says whether each earlier fix of the session was applied or dropped. The answer may come in a code fence or with words around it; dr takes the JSON object in it, and a comment missing from the answer gets no fix.

```text
[dr] Fix request. Do not change any files, return only the changed lines as JSON.
Earlier fixes in this conversation and what the person did with them:
* ratelimit.go:30-30, dropped: return an error instead of panicking
1. ratelimit.go:14-14
>	burst  float64
Request: add a one-line comment above it on why it is float64
2. ratelimit.go:52-52
>	return l.tokens >= 1
Request: compare with a named constant
Change only the lines of each comment. If something else needs to change, do not change it; write it in note.
Answer with this JSON only, with one entry per comment:
{"fixes":[{"comment":N,"new_lines":[...],"note":""}]}
new_lines is the complete replacement for the comment's lines, with their exact indentation; tabs stay tabs. [] deletes the lines. To leave the lines as they are, leave out new_lines and say why in note.
```

```json
{"fixes": [
  {"comment": 1, "new_lines": ["\t// float64 like tokens, so comparisons need no conversion.", "\tburst  float64"], "note": ""},
  {"comment": 2, "new_lines": ["\treturn l.tokens >= oneToken"], "note": "oneToken needs a declaration outside this comment."}
]}
```

## Applying

1. dr remembers the file's hash and the comment's lines when it sends a fix request.
2. On `y`, dr reads the file again. Its hash must be the one it remembered, or every change since must be a fix dr applied, each starting from the hash the one before it left. Those fixes move the comment's lines by exactly their size when they come before them, and refuse the apply when they overlap them, because dr knows those changes exactly. Then the lines at that place must be the comment's lines. If the file changed for any other reason, dr does not apply and asks you to submit again. It does not guess a new place.
3. It writes only regular files inside this worktree, with no symlink on the way. Every read and write goes through one handle on the worktree, Go's `os.Root`, so a directory swapped for a symlink after the check cannot send the write outside. It writes to a temporary file and renames it into place, keeping the file's mode, line endings and last newline.
4. The lines before and after the fix are recorded. `:undo` and origins use this record.

## State

- Kept in the worktree's git directory under `dr/` (`git rev-parse --git-path dr`) and written only by dr. Nothing appears in the working tree, nothing needs an ignore rule, and removing the worktree removes the state with it.
- `viewed.json` maps each viewed path to a hash of the diff dr showed for it. On the next open a file stays viewed only if the hash still matches. Otherwise it unfolds with `Changed since last view`, the same wording GitHub uses, so a file an agent changed again is not skipped by mistake.
- `log.jsonl` gets one line appended per fork, question and answer; per comment added, edited or removed, where the newest line of a comment wins; and per fix request, fix, dropped fix, applied fix and undo. It is read from the start on reopen, so pending fixes come back under their comments.
- A question record keeps the diff lines from the first asked line to the last, the ones between included, which of them were asked (a split selection can skip lines), and two diff lines of context before and after. It also keeps who was asked and how they relate to the lines: the author, the parent of a subagent author, a session that edited the file, or a new session. On reopen it is placed where that exact sequence appears once in the file's diff. Failing that, it is placed where its lines and the context after them appear once, then its lines and the context before them, then its lines alone, so a change on the next line does not lose it. If none appears exactly once, the record shows dim in the file header's origin box under `Earlier questions`. These records are for display only, so a wrong place never changes code. A comment record keeps its lines the same way and is placed the same way, on reopen and after every rebuild. A comment that cannot be placed shows dim under `Earlier comments` and is not sent. Applying never trusts the place: it checks the file itself.
- dr takes `lock` in the state directory when it opens. Another dr on the same worktree still opens and can read, ask and comment, but only the first applies and reverts fixes. If the lock cannot be taken for any other reason, that dr does not apply or revert fixes either, and says why.
- Before drawing, control characters and bidirectional control characters in file lines and session logs are replaced with visible text like `\x1b`. A reviewed file must not be able to control the terminal or hide lines.
