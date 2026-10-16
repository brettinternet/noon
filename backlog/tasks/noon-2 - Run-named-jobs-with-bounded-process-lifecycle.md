---
id: NOON-2
title: Run a named job in the foreground
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 14:21'
labels:
  - runner
  - cli
  - reviewed
dependencies:
  - NOON-1
references:
  - 'https://github.com/brettinternet/hum/tree/main/internal/process'
documentation:
  - docs/design.md
  - docs/development.md
modified_files:
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - cmd/noon/main.go
  - examples/**
  - README.md
  - docs/configuration.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `noon run NAME` executes a job now, in the foreground, with the documented process, environment, timeout, signal, and exit-code behavior. Bare `noon run` lists runnable jobs and starts nothing.

Read first: docs/design.md#execution and #experience. Reference implementation to adapt (do not import): github.com/brettinternet/hum `internal/process` — `process.go` (Start, Signal, capture draining), `group_darwin.go`/`group_linux.go` (group liveness), `identity_*.go` (start identity, needed by the next task), `ResolveExecutable`.

Scope:
- New internal/runner. One call runs one process and returns its result. Its spec holds argv, dir, env, stdin (nil means /dev/null), stdout and stderr writers, timeout, and kill grace (default 10s; tests pass a shorter value). Its result holds exit code, signal name, timed-out and cancelled flags, start error, start and finish times, PID, and PGID.
- A string `run` becomes `[/bin/sh, -c, STRING]`; a list is exact argv. Resolve argv[0] against the job environment's PATH (not Noon's), relative to `cwd` when it contains `/`.
- Start the child in its own process group. On timeout or context cancellation, send SIGTERM to the group, wait the grace period, then SIGKILL. After the leader exits, terminate any remaining group members the same way. Never wait forever on output pipes held open by grandchildren.
- Exit codes: the child's code; 124 timeout; 130 cancelled; 128+N killed by signal N; 127 not found, 126 not executable, 125 other start failures (missing `cwd`). Start failures never start a process.
- Environment: Noon's environment, then job `env`, then `NOON_PROJECT` (absolute path), `NOON_JOB`, and `NOON_TRIGGER=manual`. The next task adds run IDs.
- `noon run NAME`: before the job, print `▶ NAME · COMMAND` on stderr. Stream the child's stdout to stdout and stderr to stderr. After it ends, print one stderr line, such as `✓ NAME succeeded in 3.2s`, `✗ NAME failed: exit 1 after 3.2s`, or `✗ NAME timed out after 1h`. Exit with the mapped code.
- Signals: first SIGINT, SIGTERM, or SIGHUP cancels the job (TERM, grace, KILL); a second kills the group immediately. Add SIGHUP to cmd/noon/main.go; the run command handles the second signal itself.
- `noon run NAME --json`: child stdout and stderr both go to stderr; stdout gets one object `{"schema_version":1,"run":{"job","status","exit_code","signal","started_at","finished_at","duration_ms"}}`; exit code as above. Status values: `succeeded`, `failed`, `timed_out`, `cancelled`.
- Bare `noon run` lists runnable jobs with descriptions and one `Next: noon run NAME` example, then exits 0. An unknown name is a `not_found` error with the closest job name (edit distance at most 2) in the hint. A disabled job is refused with a hint naming the file that disables it. Extra arguments after NAME are a usage error.
- README and help describe `noon run`.

Non-goals: run records and logs (next task), prompts and agents, queued or detached runs, retries, TTY allocation, passing extra arguments to jobs, Windows.

Tests:
- internal/runner `TestRunner` with real `sh` children in temp dirs. Cover exact argv (spaces, quotes, `$HOME` passed literally), the `/bin/sh -c` string form, cwd, env precedence and `NOON_*`, `/dev/null` stdin (a `cat` child exits immediately), each exit-code mapping, and timeout and cancel that leave no surviving child or grandchild (`sh -c 'sleep 30 & sleep 30'`, checked by PID liveness). Also cover a leader that exits while a grandchild keeps stdout open, which must not hang.
- internal/cli `TestRunCommand`: bare guidance, `not_found` with suggestion, disabled job refused, JSON stdout purity, and propagated exit code.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^TestRunner$' -count=1 -v` exits 0 on macOS and Linux and prints `--- PASS: TestRunner`, with subtests for every behavior listed under Tests
- [x] #2 `task cli:test -- -run '^TestRunCommand$' -count=1 -v` exits 0: bare `noon run` exits 0 and lists jobs; `noon run tset` exits 1 with `error.code` `not_found` and a hint naming `tests`; `noon run --json NAME` writes exactly one JSON object to stdout while job output appears on stderr, and exits with the job's code
- [x] #3 `task cli:build && bin/noon --config examples/jobs.yaml run; echo "exit=$?"` lists runnable jobs, starts nothing, and prints `exit=0`
- [x] #4 `task race` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect existing job/CLI contracts and hum process implementation. 2. Implement foreground runner and run command with bounded group cleanup and signal handling. 3. Add critical process/CLI regression tests; run acceptance commands and CI; review lifecycle risks. 4. Commit, merge to main, record completion, and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the process-group runner, shared job-to-spec adaptation, foreground run CLI, docs and critical regression tests. No new modules or modified-file contract deviations. Adapted explicit pipe ownership and job-PATH resolution from hum; identity persistence remains for NOON-3. Adjacent transpositions count as one suggestion edit, so tset suggests tests as required.
AC#1: task cli:test -- -run '^TestRunner$' -count=1 -v -> exit 0, PASS: TestRunner on macOS arm64 and Linux arm64 (dedicated Colima container, pinned Go 1.27.2 and Task 3.53.1). Covers argv/shell, cwd/env/metadata, stdin, PATH, all exit classes, timeout/cancel/forced cleanup, and leader exit with grandchild-held pipes.
AC#2: task cli:test -- -run '^TestRunCommand$' -count=1 -v -> exit 0 on macOS and Linux; guidance, suggestion, disabled/extra-argument refusal, stdout JSON purity and propagated exit all pass.
AC#3: task cli:build && bin/noon --config examples/jobs.yaml run; echo "exit=$?" -> report/tests listed, Next: noon run report, exit=0; nothing executes.
AC#4: task race -> exit 0 on macOS; go test -race ./... also passes in Linux Go 1.27.2 container.
Additional verification: task ci passed; LSP diagnostics clean for runner and CLI. Built-binary isolated signal check sent SIGHUP then SIGINT after observing the child TERM trap: exit 130, cancelled JSON, no hang. Independent reviewer (read-only, process lifecycle/exit mapping/JSON risk focus) reported no validated findings. No tests removed, skipped or weakened. README/configuration/help updated; design execution contract unchanged. Dedicated Linux test VM/profile was deleted; default stopped profile untouched. Next: commit, run CI on commit, merge and clean up owned worktree.

Delivery: implementation commit c7dd8b36d8c32b51441f6de14eab5fec319fc44a passed task ci after committing, then fast-forward merged to main. Verified session-owned worktree receipt and idle shell pane; Worktrunk removed noon-2-run checkout and branch, and the matching Herdr workspace wBQ disappeared. No retained worktree/workspace or external blocker. Remaining step: commit this completion record, recheck final main CI, and release the Worklease claim.

Review (post-merge): runner and `noon run` match the contract. Probed the built binary: exact argv and shell strings, 127 not found, 126 not executable, 125 missing cwd, and a backgrounded `sleep 30` grandchild that is reaped once the leader exits with no survivor; signal handling, edit-distance suggestion, extra-argument refusal, and JSON purity checked in code. Low-severity note for NOON-12: the cleanup loop checks liveness with `kill(-pgid, 0)`, which still succeeds for zombie group members. If noon runs as PID 1 with no reaper (the current Dockerfile ENTRYPOINT), an orphaned grandchild becomes an unreaped zombie, so every such run waits the full 10s grace and the zombies pile up. `processGroupAlive` already skips zombies; run under an init (`--init`/tini) or use that check. ACs 1-3 rerun and pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added noon run NAME with exact argv or shell commands, job environment/PATH, streamed output, timeout/cancellation and bounded process-group cleanup. Bare run lists runnable jobs; JSON remains one stdout object and child exit codes propagate. macOS/Linux acceptance tests, race checks, full CI, built-binary signal check, and independent review passed. Merged c7dd8b3 to main and removed the owned worktree/branch/workspace.
<!-- SECTION:FINAL_SUMMARY:END -->
