# Session log fixtures

Which agent version wrote each fixture. Claude Code logs its version in the `version` field of each record and Codex in `cli_version` of `session_meta`. OMP logs only the version of its session format, in the `session` record.

| Fixture | Agent | Version | What it holds |
| --- | --- | --- | --- |
| `claude-ec6e8b8a-34cf-47ad-b851-09985c776249.jsonl` | Claude Code | 2.1.295 | A session that wrote a rate limiter in two requests |
| `claude-fe5968c4-0984-42cd-ab47-059e1d663eef.jsonl` | Claude Code | 2.1.295 | A fork of that session |
| `claude-d3598b6f-f161-41e2-87a0-eeffe6014022.jsonl` | Claude Code | 2.1.295 | A session whose subagent wrote `set.go` |
| `claude-aefd1f72d41d72e65.jsonl` | Claude Code | 2.1.295 | The log of that subagent |
| `codex-01a1209b-a5af-7992-ba5a-c5fdd6a39255.jsonl` | Codex | 0.160.1 | A `codex exec` session that wrote a rate limiter in two requests |
| `codex-models_cache.json` | Codex | not recorded | The model list Codex keeps, written by hand |
| `omp-01a1f000-0000-7000-8000-000000000001.jsonl` | OMP | session format 3 | A session written by hand in the shape of a real one |
| `omp-01a1f000-0000-7000-8000-000000000002.jsonl` | OMP | session format 3 | A subagent of that session, written by hand |
| `omp-01a1f000-0000-7000-8000-000000000003.jsonl` | OMP | session format 3 | An `ast_edit` proposal applied through `xd://resolve` and one never applied, written by hand from the records of a real session |

When an agent's version changes, check it as [Checking a new agent version](../../../CONTRIBUTING.md#checking-a-new-agent-version) says, and update this table when you add or replace a fixture.
