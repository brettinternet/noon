---
id: BUMP-17
title: Inspect run history and logs
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:03'
labels:
  - store
  - cli
dependencies:
  - BUMP-3
documentation:
  - docs/design.md
  - docs/development.md
modified_files:
  - internal/store/**
  - internal/app/**
  - internal/cli/**
  - README.md
priority: high
type: feature
ordinal: 700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users see what ran, when, and how it ended, and can read or follow any retained log. Inspection never creates state.

Read first: docs/design.md#experience, #execution, and #state.

Scope:
- Shared run JSON object, used by these commands and every later client (MCP, TUI, hooks): `{"id","job","status","trigger","scheduled_at","created_at","started_at","finished_at","duration_ms","exit_code","signal","error","log":{"bytes","truncated","available"}}`. Absent values are null.
- `bump history [NAME] [--limit N]`: newest first; default 20, maximum 50. Columns RUN, JOB, STATUS, TRIGGER, STARTED (relative), DURATION, EXIT. Bare shows recent runs in the project. An unknown NAME is `not_found` with a suggestion. A job with no runs prints `No runs of NAME yet. Next: bump run NAME` and exits 0. JSON: `{"schema_version":1,"runs":[...]}`.
- `bump logs [NAME|RUN] [--follow]`: bare means the latest run in the project, NAME the latest run of that job, and a number that run (run IDs are global; job names start with a letter, so a number is never a job). Print the stored bytes unmodified to stdout. A pruned log explains that logs are kept for the newest 10 runs per job. JSON (no follow): `{"schema_version":1,"run":{...},"log":"..."}`, where invalid UTF-8 is replaced.
- `--follow` streams the log while the run is `running`: poll file growth every 250ms and the run status every second. When output passes 1 MiB, print `[bump: output past 1 MiB is shown when the run ends]` and wait. When the run ends, print the remainder and exit 0. `--follow --json` is a usage error.
- The LAST column on `bump list` and bare `bump`: `✓ 2h ago (3m12s)`, `✗ exit 1 · 22h ago`, `running 3m`, `never`. List JSON adds `last_run` (run object or null).
- The `already_running` hint becomes `Follow it: bump logs RUN --follow`.
- With no database, every inspection command reports no runs and exits 0 without creating the state directory or database.

Non-goals: queued runs, cancel, pagination beyond `--limit`, log search, colors, mutating retention from read commands.

Tests:
- internal/cli `TestHistoryAndLogs`: newest-first history with status and exit code; bare, NAME, and RUN targets for logs; the pruned-log message; with no state directory, history, logs, and list exit 0 and create nothing.
- internal/cli `TestLogsFollow`: a running job (a real child writing a line, then sleeping, then writing again) is followed; output written after the follow began is printed, and the command returns within 2s of the run finishing. `logs --follow --json` exits 2.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestHistoryAndLogs|TestLogsFollow)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `task cli:build && bump_ac_state=$(mktemp -d) && BUMP_STATE_DIR="$bump_ac_state/state" bin/bump --config examples/jobs.yaml history; echo "exit=$?"; test ! -e "$bump_ac_state/state" && echo clean` prints a no-runs message, `exit=0`, and `clean`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
