# Security

## Reporting a vulnerability

Please report security problems privately through GitHub's private vulnerability reporting: open the [Security tab](https://github.com/JinmuGo/dr/security) of this repository and choose **Report a vulnerability**. Do not open a public issue for them.

Include what you ran, what happened, and the dr version or commit. You can expect a first reply within a week.

## Supported versions

dr has no releases yet. Security fixes land on `main`, so use the latest commit.

## What dr trusts

These are the places where a bug would matter most:

- **Files dr writes.** dr writes its state in the repository's git directory, and the fixes you apply with `y` or revert with `:undo`. It writes those fixes only to regular files inside the worktree, through one handle on the worktree (Go's `os.Root`), and only when the file still matches what the fix was made for. While Codex answers, its last message goes through a temporary file that dr removes right after.
- **Agent CLIs dr runs.** To ask (`a`) and to request fixes (`:submit`), dr runs `claude`, `codex`, `omp` or `pi` on a fork of a session, with flags that allow reading files only. The agents record those forks as sessions of their own. `ga` is different: it opens the agent's CLI for you to work in, with your own settings and approvals, so the agent can edit files and run tools as those settings allow.
- **Text dr shows.** File contents and session logs can hold control characters. dr replaces them with visible text before drawing, so a reviewed file cannot control the terminal or hide lines.
- **Text the agents send back.** Fixes come back as JSON and are applied only after you press `y`.
