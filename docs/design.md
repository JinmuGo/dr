# dr

A terminal review tool that helps a person understand code that agents wrote and fix it line by line. The name comes from diff review.

## Where things stand

| PR | What it does | Status |
| --- | --- | --- |
| PR 1 | Design doc | Rewritten to be simpler, one cross review applied |
| PR 2 | Finding origins and `dr blame` | Built, two cross reviews applied |
| PR 3 | Diff screen and origin panel | Built, one cross review applied |
| PR 3a | Showing edit notes | Built, passed one cross review |
| PR 3b | A diff screen like GitHub's | Built, one cross review applied |
| PR 3c | Origin box and `h` `l` file moves | Built, one cross review applied |
| PR 3d | Split view and tree resizing | Built, one cross review applied |
| PR 3e | Structure refactor and open source files | Built, passed one cross review |
| PR 3f | English text and docs | Built, passed one cross review |
| PR 3g | A file header that stands out | Built, passed one cross review |
| PR 3h | Viewed marks and a GitHub-like file tree | Built, passed one cross review |
| PR 4 | `K`: ask the writing session | Built, one cross review applied |
| PR 4a | Any agent can be asked, and so can lines with no author | Built, passed one cross review |
| PR 4b | Read Claude Code subagent edits that have no tool result details | Built, passed one cross review |
| PR 4c | A question thread like GitHub's, and short answers | Built, passed one cross review |
| PR 4d | `}` `{` move between changes | Built, passed one cross review |
| PR 5a | `e`: fix lines by hand in an editor | Built, one cross review applied |
| PR 5b | `c`: change comments on lines | Built, one cross review applied (the fixes landed with PR 5c) |
| PR 5c | `:submit`, fixes with `y` `x`, `:undo`, one writer per worktree, origins of applied fixes | Built, one cross review applied |
| PR 5d | `ga`: open a fork of the session in its agent, in this terminal | Built, one cross review applied |
| PR 5e | `K` and `c` type in a box under the line, like tuicr's | Built, one cross review applied |
| PR 5f | `^` in the `K` and `c` box picks the model, like OMP | Built, passed one cross review |
| PR 5g | `J` `K` move half a page like `d` `u`; asking moves from `K` to `a` | Built, passed one cross review |

## Principles

Keep it simple, the way Go is.

- dr does three things only. It shows the origin of every line, asks the session that wrote a line, and fixes lines.
- Keys are the ones you know from vim. dr adds its own only for its work: `a`, `c`, `e`, `ga` for the three things, `s` and `v` for split and Viewed the way GitHub has them, and `:submit` to send comments.
- What is not known is shown as unknown. Lines with no origin found are not filled in by guessing.
- Almost no settings. The defaults work as they are.
- Dependencies start with Bubble Tea and Lip Gloss. Every new one comes with a written reason.
- Go code follows the Google Go Style Guide and Effective Go. Errors are wrapped with the standard `%w` instead of an error library, to avoid a dependency.
- When screen behavior is unclear, look at how [hunk](https://github.com/modem-dev/hunk) and [tuicr](https://github.com/agavra/tuicr) do it.
- Each PR gets one cross review, and what it finds gets fixed.
- Add things when they are needed. Commits, PRs and PR review come after these three are done.

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

- `a` opens an input box right under the line, like tuicr's comment box, with "Why did you write this line this way?" as a placeholder ("Why did you delete this line?" on a deleted line). Type your own, or press `Enter` to send the placeholder. `Shift-Enter`, `Alt-Enter` or `Ctrl-J` starts a new line. Select several lines with `V` to ask about the range; the changed lines in it must come from one session. The box is drawn with heavy lines in the accent color, so it stands apart from the light-lined origin and comment boxes, and still does without colors. It comes before the line's comment boxes, so the box and its cursor stay on the screen however long those are.
- The question goes to a fork of the session that wrote the line. The original session is not touched.
- The answer starts with a tag that says where the reason comes from: `[decided]` when it was agreed with the user, `[chose]` when the agent chose it, `[guess]` when the agent does not remember or cannot know. Then comes one sentence of at most 25 words, in the language of the conversation, unless the question asks for more. dr shows the tag as a colored badge and the sentence as the answer.
- Questions show as a thread at the top of the origin box, the way GitHub shows review comments: `You` with the question, then who answered with the badge, the model and how long it took, then the answer. A model other than the session's is shown in the warning color. While an answer is on its way, the elapsed seconds count up. If the box is closed when the answer comes, the status line says where it is. `a` inside the box asks a follow-up about the same lines.
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

## Keys

| Key | Action |
| --- | --- |
| `j` `k` `gg` `G` | Move. Takes a count |
| `d` `u` `J` `K` | Half a page down, up |
| `}` `{` | Next, previous change |
| `h` `l` | Previous, next file |
| `s` | Switch between unified and split views |
| `v` | Mark the file viewed or not. Viewed files fold to their header |
| `/` `n` `N` | Search |
| `Enter` | Open the origin box below the line. `q` or `Esc` closes it |
| `V` | Select several lines |
| `a` | Ask the session that wrote the line, in a box under the line |
| `c` | Leave a change comment on the line or range, in a box under the line |
| `Enter` `Shift-Enter` `Esc` | In that box: send or save, start a new line (`Alt-Enter` and `Ctrl-J` too), cancel (`Ctrl-C` too) |
| `:submit` | Send all pending comments to the sessions that wrote their lines |
| `y` `x` | Apply, drop a fix |
| `e` | Fix by hand in an editor |
| `ga` | Open a fork of the session that wrote the line in its agent, in this terminal |
| `:undo` | Revert the last applied fix |
| `q` `?` | Quit, help |
| Mouse | Drag the line between tree and diff to resize or fold the tree. The wheel moves three lines. Click a file in the tree to go to it, or the Viewed box on a file header to mark it |

## Layout

| Path | What it does |
| --- | --- |
| `main.go` | The `dr` and `dr blame` commands. Only parses arguments and wires things together |
| `blame.go` | The `dr blame` output |
| `internal/git/` | Base, branch, worktrees, file contents |
| `internal/diff/` | Line matching between two versions and word diff inside a line |
| `internal/origin/` | Replays session edits to find the origin of each line |
| `internal/review/` | Data shared by `dr blame` and the screen: changed files, origin per line, hunks |
| `internal/agent/` | Reads each agent's session logs and asks forks |
| `internal/tui/` | Screen and keys. File tree, syntax colors, colors for dark and light terminals |
| `internal/fix/` | Fix requests and their answers, and writing fixes to files only when it is safe |
| `internal/state/` | State dr keeps between runs: viewed files, the log of questions, comments and fixes, and the lock |

All packages live under `internal/`. Nothing outside can import them, so their insides can change freely after the code is public. There is one command, so there is no `cmd/`; `main.go` at the root is the command.

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

Session log formats are not public APIs. When they cannot be read, those lines become unknown and the status line names the agent whose logs failed. `dr blame` writes it to stderr. If this gets slow, a cache will be added then.

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
- The model is the one in the session log, and for Codex and OMP the reasoning effort too. If a different model answers, you are not asking the author. dr reads the answering model from Claude's JSON output and from the last turn of the Codex fork's own session log, because Codex's events do not name it. `^` in the `a` and `c` box picks another model for that one question or comment, from the models the CLI of the session's agent can run: any OMP model for an OMP fork, Claude models for a Claude Code fork, OpenAI models for a Codex fork. A picked model goes without the session's reasoning effort, and the answer shows it with a `^` instead of a warning. The fork stays the same, so the picked model still sees the whole conversation.
- The fork starts in the cwd from the session log.
- Every question sends the original conversation again. In testing, Codex used about 190,000 input tokens, 150,000 of them cached. Claude used about 8,000. So a question is sent only when a person presses the key.
- OMP forks a session log into dr's state directory with a new session id, so nothing is written next to your own sessions and dr never reads its forks as sessions. OMP subagent logs are full session logs, so a line an OMP subagent wrote is asked directly. In testing, a question to an OMP fork took 4 to 7 seconds. MCP no longer delays it; OMP waits at most 250 ms for MCP servers to start.
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

## 2026-10-09 test

In a throwaway repo, Claude Code and Codex were asked to write a rate limiter over two requests, and dr's method was done by hand, without dr.

- Origins: 73 lines from Claude, 70 from Codex, and 253 lines of this document written with OMP were all matched down to the request number. The remaining lines were one line each from fixes applied the dr way, so unknown was correct. Edits that rewrote whole files took only the lines they actually changed. Searching the session files took 30 to 80 ms.
- Asking: Claude answered in 7 seconds, Codex in 6 to 8. The original session files were unchanged. Both agents separated reasons agreed with the user from reasons they chose.
- Fixes: arrived in 7 to 8 seconds, correct down to the tabs. `go vet` passed after applying. At that time the format also returned `old_lines`. The current format, which returns only changed lines, is to be checked in PR 5.
- Matching origins by identical line content was also tried and dropped. Claude fork records left almost everything undecided, and the cross review found a counterexample where lines attached to the wrong session.
- Repeated with the PR 2 implementation. In a file where Claude added and then removed a method over two requests, and a file Codex wrote over two requests, every line, deleted lines included, got the right request. Lines changed with sed and lines a person appended were unknown. It took 0.3 seconds.
- For all 365 OMP edits, applying the diff to the before content gave the after content. Of 596 file changes in Codex logs, 507 applied at their line numbers and 42 applied at a moved position. 47 were not applied because lines changed outside the records or the diff matched in more than one place.
- In the last 30 days of logs, only 17% of 1,990 Claude edits had text written right before the edit. OMP attaches a one-line intent to 92% of edits, but it only says what the edit does. Codex wrote a progress message before every file change. So leaving reasons is left to an agent skill.

## Tests

- PR 2: fixtures are trimmed Claude Code and Codex test session logs and a hand-written OMP log. They cover replay, whole-file rewrites, unknown for lines changed by shell, deleted lines, sessions that switched branches, and filtering out forks. They also cover reading a fork before its original session, and new changes made in a fork. A full `dr blame` run also happens once, with the three logs moved into a temporary repo.
- PR 3: Bubble Tea `teatest` sends keys and checks moves, search and the origin panel. The whole screen is not pinned as golden; the cursor and panel contents are checked. Hunk splitting and the changed file list are tested separately.
- PR 3a: checks that the text right before an edit is read from all three agents' logs, that edits without it stay empty, and that it shows in the `dr blame` legend.
- PR 3b: key behavior tests stay as they are; word highlight pairing and the file tree are tested separately. File status A, M, D is decided by whether the file exists in base and in the working tree, not guessed from line counts, and is checked with empty and binary files. The screen is checked by opening it in dark and light terminals and with `NO_COLOR` and looking at captures.
- PR 3c: opens the origin box on a changed line, an unknown line and a file header row, and checks scrolling inside the box and closing it. `h` `l` file moves are checked with count prefixes too.
- PR 3d: checks split row pairing, that the cursor stays on the same line when switching views, the origin box on a row whose two sides have different origins, and dragging, folding and the wheel by sending mouse messages through teatest.
- PR 3e: pins `dr blame` output byte for byte in `testdata/blame/*.golden`. To regenerate, run `go test -run TestBlame -update .` and review the diff. The binaries from before and after the move were also run on the same repository to check that stdout, stderr and exit codes match.
- PR 3g: checks the blank line above each file header except at the top, and that `j` `k` cross it as one row in unified and split views.
- PR 3h: checks saving and loading viewed state, that a changed diff clears the mark, that cursor moves, change jumps and search skip folded files, the Viewed box and tree clicks, and that the tree shows no line counts.
- PR 4: fake `claude` and `codex` executables check the command lines, stdin, cwd, output parsing, errors and cancelling. In a throwaway repo, real Claude Code (claude-opus-5-5) and Codex (gpt-6.1-sol) sessions were asked through the screen. Codex answered in 6 seconds and said it chose `container/list` itself. Claude answered in 5 and 4 seconds, said the head index was the user's decision, and marked its reason for it as a guess. The second Claude question continued the same fork. The original session files had the same sha256 before and after. The Codex fork ran with a read-only sandbox and never-approve, the same model answered both, the questions came back on the same lines after reopening, and `dr blame` still named the original sessions.
- PR 4b: a subagent log trimmed from a real Claude Code 2.1.295 session, whose tool results have no `toolUseResult`. Its Write must become the subagent's edit, with the parent session as Parent.
- PR 4c: tag reading at the start or end of an answer, each relation's prompt, the thread order, the pending timer and follow-up questions from inside the box. The thread look is checked on the real screen.
- PR 5a: the editor command for added and deleted lines, headers and the split view, `$VISUAL` over `$EDITOR`, and the rebuild: the cursor stays on its line when lines above it change, questions find their place again and a pending answer still lands, a file that leaves the diff goes, a rebuild with no files left shows an empty screen where no key breaks, a newer session edit makes the next question start a new fork, and a failed rebuild keeps the old diff. On the real screen, `e` with a stand-in editor opened the right line, and the cursor stayed on it after the rebuild.
- PR 5b: the comment rule (author, parent, no origin, mixed sessions, no changed lines, deleted lines only, the 200-line limit), adding, editing and removing through keys, a selection that overlaps a comment, box lines that the cursor, search and `row x/y` skip in both views and at narrow widths, the counts in the tree and the header, records written and read back, and placing comments after a reopen and after a rebuild, including ones that cannot be placed. The box and the counts were checked on the real screen.
- PR 5c: the engine applies a fix, two fixes to the same file in turn, a file with the same block twice, CRLF and no final newline, and refuses a file changed outside after a request, an overlapping earlier fix, symlinks and paths that leave the worktree; it parses answers in fences or with prose around them. The screen submits comments for two sessions at once, skips a busy session, shows fixes and errors, applies, drops, undoes, keeps a comment on the next line placed after a fix, refuses without the lock and brings pending fixes back on reopen. Fixes become origins in the replay and in `dr blame`. On the real screen, four sessions answered one request each in about 20 seconds: Codex with two fixes, Claude, the parent of a Claude subagent, and OMP. Codex kept the tab indentation, adjacent fixes applied one after the other, `x` and `:undo` brought their comments back, `K` on a fixed line went on in the fork that sent the fix, a second dr could not apply, and the original session logs kept their sha256.
- PR 5c, after its cross review: a parent directory swapped for a symlink between the check and the write leaves the outside file alone, `:undo` restores the exact bytes after a fix that emptied the last line, a lock that fails for any reason blocks `y` and `:undo` (checked on the real screen with an unreadable lock file), dr opens onto the empty screen to undo a fix that brought the diff back to base, an older rebuild that arrives late is dropped, and a fix that leaves one empty line keeps it as the fix's line.
- PR 5e: the text box's keys (new lines from `Shift-Enter`, `Alt-Enter` and `Ctrl-J`, moves by displayed row, word moves, `Ctrl-U`, `Ctrl-W`, pastes with CRLF and tabs), the title for asking, commenting and editing, the heavy outline with and without colors, wrapping wide characters, the cursor cell and the terminal cursor at the insertion point, the 10-line cap, split view and narrow widths, `K` from the origin box with `Enter` and `Esc`, a rebuild closing the box, and multi-line text in the thread, the comment box and the fix request. On the real screen: the box under the line in dark, light and `NO_COLOR`, Korean text with the terminal cursor right after it, a two-line comment through `Ctrl-J`, editing a comment, and a real two-line question to Claude whose thread shows both lines.
- PR 5e, after its cross review: the input box comes right under its line, before the comment boxes of that line, so a comment taller than the screen cannot push the box or its cursor out of view, and a growing box keeps its cursor row on the screen.
- PR 5f: each agent's model list (Claude Code's aliases; Codex's list by priority with hidden models left out and `CODEX_HOME` first; `omp models --json` through the fake CLI, chat models only, newest first by the numbers in the id, and its failures), and the picker: when `^` opens it, the order of its rows, the loading and error rows, filtering by substring and subsequence, the `as typed` row, the keys, picking and clearing a pick, a picked model sent without the session's effort, the `^` note, a comment's pick through save, reopen and edit, and `:submit` holding back a session's second model. On the real screen, a question went to Claude Code with `^sonnet`, to Codex with `^gpt-6-luna` and to an OMP session written by Claude Opus with `^openai-codex/gpt-6-astra`, each answered by the picked model in 5 to 7 seconds; a comment with `^gpt-6-luna` and one without went to the same Codex session in two `:submit`s; and the original session logs kept their sha256. Claude Code reports every model a run used, so with an alias dr takes the model whose name holds it.
- PR 5g: `J` and `K` leave the cursor and the screen where `d` and `u` do, a count included, and scroll the open origin box as they do; `K` and `ga` open no input box, in the diff or in the origin box; `a`, `J` and `K` do nothing on the empty screen. On the real screen, `J` `J` `K` went to the same rows as `d` and `u`, `a` asked on a line and inside the origin box, whose border reads `a ask · Esc close`, and `aJK` typed in the box stayed text.
- PR 5d: each agent's command to open a fork for work, with and without a model and effort, and its errors; `ga` on an author line, a Claude Code subagent's line (the parent opens), a line with no origin (the session that last edited the file opens), a new-session target (refused), from the origin box and from a selection, an agent that exits with an error or is interrupted, and the empty screen; `Ctrl-C` doing nothing while the terminal is handed over, even when it arrives before dr hears the terminal is back, and for a second after. On the real screen, in a throwaway repo, `ga` on a line Claude Code wrote opened a fork in Claude Code; the fork changed the line, and after `/exit` dr was back on the same line, which now named the fork and its new request as its origin. `ga` on a Codex line opened Codex on the original conversation with the session's model and effort, and on an OMP line OMP on the session's model; both came back to dr on exit, and a quick double `Ctrl-C` that closed Codex left dr open.

## PR plan

PR 1 is this document.

PR 2 is finding origins. It adds reading the three agents' logs, the replay, and `dr blame <path>`. It prints the origin of every line without a TUI, which is useful on its own. At the top it lists, for each request that changed the file, the session id, the full request text and the start of the last reply. Below that it prints the diff from base with the origin of every line. The diff compares base and the current file directly, so lines changed and then reverted along the way do not appear. Unchanged lines have an empty origin column, and deleted lines have `-` in place of a line number.

PR 3 is the diff screen. It adds the file list, unified diff, vim movement, search and the origin panel. Read only. The diff is in the middle, the origin panel on the right, the status line at the bottom, and the file list on the left when there is room. Dependencies are Bubble Tea v2 and Lip Gloss v2, with teatest for tests only.

PR 3a is edit notes. The text an agent wrote right before an edit is read as that edit's note and shown in `dr blame`. The origin panel gets it in PR 3b, which redraws the screen. An agent skill that asks agents to leave that sentence is kept outside this repository for now. Notes go only on edits that involve a decision, not on every line.

PR 3b redraws the screen like GitHub's diff view: file tree, file header bar with diffstat, two line number columns, backgrounds for added and deleted lines, word highlights inside lines, syntax colors, and a note section in the origin panel. It follows tuicr's implementation. `chroma` is added for syntax colors, because coloring per language like GitHub and tuicr is not possible with the standard library alone. The colors are chosen by asking the terminal whether its background is dark or light, and with `NO_COLOR` the bars are drawn in reverse video.

PR 3c removes the origin panel on the right. A panel that is always open takes width from the diff. Instead, `Enter` inserts an origin box right below the line, the way tuicr puts a line comment in a box under the line. While the box is closed, the status line still shows the agent and request number for the cursor line. File moves use `h` `l` instead of `]f` `[f`.

PR 3d adds a split view like GitHub's. Base is on the left, the current file on the right, and deleted and added lines in a change block are paired in order. When the diff is narrower than 100 columns it is drawn unified. The tree width changes by dragging with the mouse, and dragging it very narrow folds it down to a one-column handle. While dr holds the mouse, selecting terminal text needs Option or Shift held down. tuicr and hunk hold the mouse too.

PR 3e cleans up the structure before going open source. Packages move under `internal/`, the `dr blame` output code moves to `blame.go`, and golangci-lint reports zero issues. It adds an MIT LICENSE and GitHub Actions CI (gofmt, vet, golangci-lint, `go test -race`, govulncheck, `go mod tidy -diff`). The toolchain in `go.mod` goes up to go1.26.9 because of standard library vulnerabilities. One behavior changes: when writing to stdout fails, dr now prints the error and exits with 1.

PR 3f switches everything to English: code comments, screen text, CLI output, and docs. It adds README.md and CONTRIBUTING.md and translates this document.

PR 3g makes file headers stand out the way GitHub's file cards do. A blank line separates each file from the one before. The header band is a step stronger than code lines and hunk headers, starts with `▾`, and shows the file name in the brightest color with the directory quieter. The blank line is drawn only: cursor moves, counts, search and `row x/y` do not see it.

PR 3h adds GitHub's Viewed mark. `v` marks the file under the cursor and folds it to its header, and the header shows `☐ Viewed` or `☑ Viewed` on the right next to the diffstat. The mark is saved per worktree and cleared when the file's diff changes, like GitHub and tuicr do. The file tree drops line counts and shows a status icon (`+` added, `−` deleted, `±` modified), the name, and `✓` for viewed files, with `3 / 54 files viewed` on top. Folding folders and filtering files by name are left for later.

PR 4 is `K`. It adds `V` selections, making and continuing forks, asking, and keeping questions and answers in `log.jsonl`.

PR 4a makes agents pluggable and lets every changed line be asked. Each agent is one file and one registry entry. OMP sessions and OMP subagents can be asked through OMP's own fork. Lines a Claude Code subagent wrote go to its parent session. Lines with no author go to the session that last edited the file, or to a new session of the agent used most in the review.

PR 4b fixes reading Claude Code subagent edits. Claude Code 2.1.295 stopped writing `toolUseResult` in subagent logs, so a file a Claude subagent wrote showed as unknown. When a tool result has no `toolUseResult`, dr now builds the edit from the `Write` or `Edit` call it answers.

PR 4c makes questions read like a GitHub review thread and asks for short answers. Answers start with a `[decided]`, `[chose]` or `[guess]` tag, which dr shows as a badge, and keep to one sentence unless the question asks for more.

PR 4d moves between changes with `}` `{` instead of `]c` `[c`. One key instead of two, so `]` and `[` no longer wait for a second key.

PR 5 is fixing. Questions go out right away because reading needs their answers. Change requests are collected as comments and sent together, like a GitHub review, so each session gets all of its requests in one go and sees them side by side. It comes in five parts, each merged after the checks, one cross review and a look at the real screen.

- PR 5a adds `e`. It opens the editor at the line and rebuilds the diff when the editor closes, keeping the cursor on the same line.
- PR 5b adds change comments with `c`: one per line or range, kept in `log.jsonl`, drawn under their lines and counted in the file tree and the file header. Nothing is sent yet.
- PR 5c sends the comments with `:submit`, one request per session, shows the fixes under their comments, and adds `y`, `x` and `:undo`. dr takes a lock so only one dr per worktree writes files, and the fixes it applied become the origin of their lines.
- PR 5d adds `ga`. The plan was a herdr pane beside dr. It opens in the current terminal instead, the way `e` opens the editor, so it needs nothing but the agent's CLI, and dr rebuilds the diff when the agent exits instead of watching for changes.
- PR 5e moves the `K` and `c` inputs from the status line into a box right under the line, like tuicr's comment box. Its heavy lines in the accent color set the box being typed in apart from the boxes around it, and its text can span lines. The terminal cursor follows the insertion point, so an input method composes in place.
- PR 5f adds `^` to the `K` and `c` box, as OMP's composer has it: a list of the models the answering agent's CLI can run, filtered by typing. Each agent lists its own: OMP through `omp models --json`, Codex from the model list it keeps, Claude Code from the aliases its `--model` help documents, since it has no list. The session's model and the models the review's sessions used come first. A pick holds for one question or comment, and comments for one session that picked different models go one request at a time.
- PR 5g makes `J` and `K` move half a page, as `d` and `u` do, so paging needs no reach for a key that is not next to `j` and `k`. Asking moves from `K` to `a`, which sits on the home row and reads as ask, next to `c` for comment and `e` for edit. The earlier PRs above say `K` because that was the key then.

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
