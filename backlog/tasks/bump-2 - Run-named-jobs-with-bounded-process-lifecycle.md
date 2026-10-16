---
id: BUMP-2
title: Run a named job in the foreground
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 01:41'
labels:
  - runner
  - cli
dependencies:
  - BUMP-1
references:
  - 'https://github.com/brettinternet/hum/tree/main/internal/process'
documentation:
  - docs/design.md
  - docs/development.md
modified_files:
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - cmd/bump/main.go
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
Outcome: `bump run NAME` executes a job now, in the foreground, with the documented process, environment, timeout, signal, and exit-code behavior. Bare `bump run` lists runnable jobs and starts nothing.

Read first: docs/design.md#execution and #experience. Reference implementation to adapt (do not import): github.com/brettinternet/hum `internal/process` — `process.go` (Start, Signal, capture draining), `group_darwin.go`/`group_linux.go` (group liveness), `identity_*.go` (start identity, needed by the next task), `ResolveExecutable`.

Scope:
- New internal/runner. One call runs one process and returns its result. Its spec holds argv, dir, env, stdin (nil means /dev/null), stdout and stderr writers, timeout, and kill grace (default 10s; tests pass a shorter value). Its result holds exit code, signal name, timed-out and cancelled flags, start error, start and finish times, PID, and PGID.
- A string `run` becomes `[/bin/sh, -c, STRING]`; a list is exact argv. Resolve argv[0] against the job environment's PATH (not Bump's), relative to `cwd` when it contains `/`.
- Start the child in its own process group. On timeout or context cancellation, send SIGTERM to the group, wait the grace period, then SIGKILL. After the leader exits, terminate any remaining group members the same way. Never wait forever on output pipes held open by grandchildren.
- Exit codes: the child's code; 124 timeout; 130 cancelled; 128+N killed by signal N; 127 not found, 126 not executable, 125 other start failures (missing `cwd`). Start failures never start a process.
- Environment: Bump's environment, then job `env`, then `BUMP_PROJECT` (absolute path), `BUMP_JOB`, and `BUMP_TRIGGER=manual`. The next task adds run IDs.
- `bump run NAME`: before the job, print `▶ NAME · COMMAND` on stderr. Stream the child's stdout to stdout and stderr to stderr. After it ends, print one stderr line, such as `✓ NAME succeeded in 3.2s`, `✗ NAME failed: exit 1 after 3.2s`, or `✗ NAME timed out after 1h`. Exit with the mapped code.
- Signals: first SIGINT, SIGTERM, or SIGHUP cancels the job (TERM, grace, KILL); a second kills the group immediately. Add SIGHUP to cmd/bump/main.go; the run command handles the second signal itself.
- `bump run NAME --json`: child stdout and stderr both go to stderr; stdout gets one object `{"schema_version":1,"run":{"job","status","exit_code","signal","started_at","finished_at","duration_ms"}}`; exit code as above. Status values: `succeeded`, `failed`, `timed_out`, `cancelled`.
- Bare `bump run` lists runnable jobs with descriptions and one `Next: bump run NAME` example, then exits 0. An unknown name is a `not_found` error with the closest job name (edit distance at most 2) in the hint. A disabled job is refused with a hint naming the file that disables it. Extra arguments after NAME are a usage error.
- README and help describe `bump run`.

Non-goals: run records and logs (next task), prompts and agents, queued or detached runs, retries, TTY allocation, passing extra arguments to jobs, Windows.

Tests:
- internal/runner `TestRunner` with real `sh` children in temp dirs. Cover exact argv (spaces, quotes, `$HOME` passed literally), the `/bin/sh -c` string form, cwd, env precedence and `BUMP_*`, `/dev/null` stdin (a `cat` child exits immediately), each exit-code mapping, and timeout and cancel that leave no surviving child or grandchild (`sh -c 'sleep 30 & sleep 30'`, checked by PID liveness). Also cover a leader that exits while a grandchild keeps stdout open, which must not hang.
- internal/cli `TestRunCommand`: bare guidance, `not_found` with suggestion, disabled job refused, JSON stdout purity, and propagated exit code.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestRunner$' -count=1 -v` exits 0 on macOS and Linux and prints `--- PASS: TestRunner`, with subtests for every behavior listed under Tests
- [ ] #2 `task cli:test -- -run '^TestRunCommand$' -count=1 -v` exits 0: bare `bump run` exits 0 and lists jobs; `bump run tset` exits 1 with `error.code` `not_found` and a hint naming `tests`; `bump run --json NAME` writes exactly one JSON object to stdout while job output appears on stderr, and exits with the job's code
- [ ] #3 `task cli:build && bin/bump --config examples/jobs.yaml run; echo "exit=$?"` lists runnable jobs, starts nothing, and prints `exit=0`
- [ ] #4 `task race` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
