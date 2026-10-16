---
id: BUMP-17
title: Inspect run history and logs
status: Done
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 04:20'
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
- [x] #1 `task cli:test -- -run '^(TestHistoryAndLogs|TestLogsFollow)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task cli:build && bump_ac_state=$(mktemp -d) && BUMP_STATE_DIR="$bump_ac_state/state" bin/bump --config examples/jobs.yaml history; echo "exit=$?"; test ! -e "$bump_ac_state/state" && echo clean` prints a no-runs message, `exit=0`, and `clean`
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
1. Inspect existing store, app, CLI, and design contracts. 2. Implement read-only history/log inspection and last-run rendering. 3. Add critical history/follow regressions and run acceptance commands plus CI. 4. Review, commit, merge locally, finalize task evidence, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation complete within allowed paths; executor reports focused tests and task ci passing. AC#1: task cli:test -- -run '^(TestHistoryAndLogs|TestLogsFollow)$' -count=1 -v -> exit 0, both tests PASS (executor). AC#2: task cli:build && bump_ac_state=$(mktemp -d) && BUMP_STATE_DIR="$bump_ac_state/state" bin/bump --config examples/jobs.yaml history; echo "exit=$?"; test ! -e "$bump_ac_state/state" && echo clean -> exact command executed via /tmp/bump-17-acceptance.sh after inline-shell parser rejected dynamic syntax; No runs yet., exit=0, clean. Independent review pending; next: resolve concrete findings, commit, final CI, merge and cleanup.

Single independent review found three concrete defects: OpenExisting migrated v1 inspection state, fast overflow could print terminal tail before the deferral notice, and follow swallowed unavailable logs. Parent confirmed paths; executor is correcting all three with regression checks. No additional general review planned.

All three review findings corrected with regressions: read-only v1 queries without migration, bounded live output with notice-before-tail ordering, terminal missing-log errors. Focused acceptance tests and task ci passed after corrections. AC#2 exact script rerun passed (No runs yet., exit=0, clean). Final implementation commit 006dc26 passed task check:staged and task ci; merged locally as a956bd3, preserving concurrent backlog changes. Diff stays in contract; tests were strengthened, not weakened. README/help match implemented behavior. Owned worktree/branch bump-17-history removed through Worktrunk; matching Herdr workspace wBV removed. No blocker or remaining step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented history, logs/follow, nullable run JSON, LAST summaries and follow hints. Acceptance commands and CI pass; independent review findings fixed with regressions. Committed 006dc26, merged a956bd3, owned worktree cleaned; nothing pushed.
<!-- SECTION:FINAL_SUMMARY:END -->
