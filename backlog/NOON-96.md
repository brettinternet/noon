---
id: NOON-96
title: Show how to resume an agent run's session
status: done
type: feature
priority: low
labels: [cli, delight]
related: [NOON-82, NOON-59]
refs: [docs/agents.md, docs/configuration.md]
files:
  - internal/config/jobs.go
  - internal/config/config_test.go
  - internal/cli/run.go
  - internal/cli/history.go
  - internal/cli/run_test.go
  - internal/cli/history_test.go
  - docs/agents.md
  - docs/configuration.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T19:15:30Z
---

Why: the payoff of a scheduled agent run is often "pick up where it left off": the overnight review found something, and the user wants to continue that conversation. The `claude` and `pi` profiles already request a session ID per run, but the only way to use it is to read `runs[].agent_session_id` from `noon history JOB --json` and assemble the resume command from docs/agents.md by hand. Printing the exact command where people already look turns a recipe into one paste.

Outcome:

```console
$ noon logs review
...
Resume: cd ~/src/app && claude --resume 0b9c1e5e-...
```

Settled contract:
- Agent profiles gain an optional `resume_flag` (validated like `session_id_flag`). Built-ins: `claude` → `--resume`, `pi` → `--session-id`; others unset. It is excluded from the job digest, so adding or changing it never re-gates shared jobs.
- When a run has an `agent_session_id` and its profile has a `resume_flag`, foreground `noon run` (in its finish summary) and `noon logs` (next to `Continue:`) print one stderr line `Resume: [cd DIR && ]EXE FLAG ID`. EXE is the profile command's first element only; DIR is the job's working directory, with `cd` omitted when it equals the invocation directory. Values are shell-quoted. Nothing is printed without both, or with `--json`.
- docs/agents.md "Resume a prompt run" and docs/configuration.md profile fields describe it.

Scope: profile parsing and built-ins in internal/config/jobs.go, the two stderr lines in internal/cli/run.go and internal/cli/history.go, docs.

Tests (critical only):
- Authorization boundary: adding `resume_flag` to a profile used by an approved shared job leaves the job approved (digest unchanged).
- Parsing: `Resume:` is printed with a quoted directory containing a space, and not printed for a profile without `resume_flag`. Extend TestPromptJobs or the history tests.

Non-goals: launching or resuming the agent; new JSON, MCP, or HTTP fields (JSON already has `agent_session_id`); verifying that the agent honored the session ID; transcript storage.

Dependencies: none. Related: NOON-82, NOON-59.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)`, a stub `claude` executable first on PATH that reads stdin and exits 0, and a temp Git repo whose noon.yaml defines job `review` with `prompt: "Review"` and `agent: claude`, run from inside the repo: `noon run review` exits 0 and its stderr has exactly one line `Resume: claude --resume ID` where ID equals `runs[0].agent_session_id` from `noon history review --json`; `noon logs review` prints the same line on stderr; `noon run review --json` and `noon logs review --json` print no `Resume:` line (`noon` is the built bin/noon by absolute path).
  Evidence: AC#1 in Notes: passed as written.
- [x] In the same environment, from the repo's parent directory, `noon -C repo logs review` prints `Resume: cd <repo path> && claude --resume ID`; a job using a custom profile with `session_id_flag` but no `resume_flag` prints no `Resume:` line.
  Evidence: AC#2 in Notes: passed as written.
- [x] `task cli:test PKG=./internal/cli -- -run 'TestPromptJobs|TestEnableApproval' -count=1` and `task cli:test PKG=./internal/config -- -count=1` exit 0 and cover: adding `resume_flag` to a profile used by an approved shared job keeps it approved; the `Resume:` line quotes a directory containing a space; no line without `resume_flag`.
  Evidence: AC#3 in Notes: passed as written.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on 41259dd.
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Notes list AC#1-3 with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Contract plus approval_test.go, justified in Notes.
- [x] No test was deleted, skipped, or weakened
  Evidence: Only test additions.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/agents.md Resume a prompt run and docs/configuration.md profile tables describe implemented behavior; design.md has no settled contract to change.

## Notes

AC#1: /tmp/noon96-ac/ac.sh (task cli:build; isolated HOME/XDG/NOON_STATE_DIR; stub claude on PATH; repo job review prompt+agent claude) -> noon run review exit 0, exactly one stderr line 'Resume: claude --resume ID' with ID = history runs[0].agent_session_id; noon logs review same line; run --json and logs --json: 0 Resume lines.
AC#2: same script, from parent dir: noon -C repo logs review -> 'Resume: cd /private/var/.../repo && claude --resume ID' (canonical repo path, exact match); custom profile with session_id_flag but no resume_flag: session ID recorded, 0 Resume lines from run and logs.
AC#3: task cli:test PKG=./internal/cli -- -run 'TestPromptJobs|TestEnableApproval' -count=1 -> ok; task cli:test PKG=./internal/config -- -count=1 -> ok. TestEnableApproval: adding resume_flag to a shared --config profile keeps p approved. TestPromptJobs: Resume line quotes cwd 'has space' on run and logs; none for profile without resume_flag.
CI: task ci exit 0 on final commit 41259dd (rebased on cdfb5e9). One earlier run hit a pre-existing TestHooks flake (hook-child-pid file read before written, run_test.go:1250, unrelated to this change); passed -count=5 and the subsequent full runs.
Files: contract plus internal/cli/approval_test.go, which holds TestEnableApproval named by AC#3 (authorization-boundary test). history_test.go/config_test.go unchanged: coverage lives in TestPromptJobs/TestEnableApproval. noon config output (root.go) intentionally unchanged: non-goal of new JSON fields.
Design: run skips the line on start error (no session existed); logs prints it only when the run's project matches the loaded project and its job still exists, using the current profile. Commit 41259dd on main.

## Summary

Agent profiles gain resume_flag (claude --resume, pi --session-id), excluded from the approval digest. Foreground noon run and noon logs print 'Resume: [cd DIR && ]EXE FLAG ID' on stderr (shell-quoted, cd omitted when cwd matches, none with --json). Docs updated. Commit 41259dd on main; task ci, focused tests, and AC script pass.
