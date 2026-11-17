---
id: NOON-96
title: Show how to resume an agent run's session
status: todo
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
updated: 2026-11-17T18:40:46Z
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

- [ ] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)`, a stub `claude` executable first on PATH that reads stdin and exits 0, and a temp Git repo whose noon.yaml defines job `review` with `prompt: "Review"` and `agent: claude`, run from inside the repo: `noon run review` exits 0 and its stderr has exactly one line `Resume: claude --resume ID` where ID equals `runs[0].agent_session_id` from `noon history review --json`; `noon logs review` prints the same line on stderr; `noon run review --json` and `noon logs review --json` print no `Resume:` line (`noon` is the built bin/noon by absolute path).
- [ ] In the same environment, from the repo's parent directory, `noon -C repo logs review` prints `Resume: cd <repo path> && claude --resume ID`; a job using a custom profile with `session_id_flag` but no `resume_flag` prints no `Resume:` line.
- [ ] `task cli:test PKG=./internal/cli -- -run 'TestPromptJobs|TestEnableApproval' -count=1` and `task cli:test PKG=./internal/config -- -count=1` exit 0 and cover: adding `resume_flag` to a profile used by an approved shared job keeps it approved; the `Resume:` line quotes a directory containing a space; no line without `resume_flag`.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
