---
id: NOON-55
title: Give skipped runs their own retention budget
status: In Progress
assignee: []
created_date: '2026-10-18 06:43'
updated_date: '2026-10-18 06:52'
labels:
  - store
  - retention
dependencies: []
documentation:
  - docs/design.md
  - docs/agents.md
modified_files:
  - internal/store/store.go
  - internal/store/store_test.go
  - docs/design.md
  - docs/agents.md
priority: medium
type: bug
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: `pruneRuns` (internal/store/store.go) counts `skipped` runs against the regular per-job budget: logs and event files for the newest 10, rows for the newest 50. A job with `overlap: skip` (the default) on a frequent schedule records one `skipped` run per tick while a long run is active. A `*/15` job whose run lasts all day evicts that real run log after about 2.5 hours and its row within about 2 days, and the hook `previous_status` and `NOON_LAST_SUCCESS_AT` lose the results they read. Observed with a scheduled Herdr agent job (see DRAFT-1).

Outcome: overlap skips can never evict a real result.

Settled contract:
- `skipped` runs get an independent per-job budget of the newest 10, mirroring the `noop` budget. They no longer count toward the regular 50-row or 10-log budgets, and regular runs no longer count toward theirs.
- Skipped runs are created only by the scheduler (internal/store/scheduler.go): no log, event file, or idempotency key. The change therefore cannot shorten log, event, or idempotency retention for any other run.
- `missed` and `expired` runs keep counting toward the regular budget (TestScheduledHistoryRetention relies on `missed`). Noop retention is unchanged.
- No schema migration: retention is applied at write time, and an older binary pruning the same database only deletes more skipped rows.

Scope: the skipped branch in `pruneRuns`; retention text in docs/design.md (State) and docs/agents.md (Limits and safety).

Tests (critical only): a retention regression silently drops real history. Extend internal/store/store_test.go next to TestNoopRetentionPreservesResultsAndState: older real results followed by 60 skipped runs leave every real result (with its log within the newest 10 results) and exactly the newest 10 skipped runs. No new test files.

Non-goals: recording fewer skips or coalescing them; a budget for `missed`, `expired`, or `cancelled`; configurable retention; hiding skips from history or the dashboard.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers: two real results (one `succeeded`, one `failed`, each with a log) followed by 60 `skipped` runs for the same job leave both results with their logs and exactly the 10 newest skipped runs; `PreviousStatus` still returns the older result.
- [x] #2 `task cli:test PKG=./internal/store -- -count=1 -run 'TestScheduledHistoryRetention|TestNoopRetentionPreservesResultsAndState'` exits 0 with both tests unchanged (missed and noop retention unaffected).
- [x] #3 docs/design.md and docs/agents.md state the skipped budget exactly as implemented, and `task ci` exits 0.
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
1. Give skipped rows an independent counter in pruneRuns, preserving all other budgets.
2. Add one public-store regression for real results/logs and the newest ten of sixty scheduler skips; update the two retention docs.
3. Run both acceptance test commands and task ci; review the bounded diff, commit, merge, clean up, push, and publish v0.4.1 after CI passes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:test PKG=./internal/store -- -count=1 -> exit 0; two logged results survive sixty scheduler skips, exactly the latest ten skips remain, PreviousStatus and LastSuccess are preserved.
AC#2: task cli:test PKG=./internal/store -- -count=1 -run 'TestScheduledHistoryRetention|TestNoopRetentionPreservesResultsAndState' -> exit 0; both existing tests unchanged.
Full task ci initially failed at the existing scheduler cancellation test (queue_test.go:113, EOF). Inspection shows it waits for PID file existence, then parses immediately: shell redirection creates the file before echo writes the PID. Focused race execution passed 20 repetitions, consistent with a timing flake. Modified-file contract expansion: internal/scheduler/queue_test.go only, to poll for complete PID content before proceeding, preserving every cancellation assertion. This is required to stabilize the requested CI delivery; no scheduler production behavior changes.
One general review of the retention diff found no item-scoped defects; the fix preserves regular/noop counters and all file cleanup paths.

AC#3: task ci -> exit 0 in the implementation worktree and again on merged main at 5f38a2c; docs/design.md and docs/agents.md describe independent 10-row skipped retention and unchanged regular/noop budgets. Store/scheduler LSP diagnostics clean; git diff --check passes.
Scheduler test fix reuses the existing shutdown-test newline readiness predicate. task cli:test PKG=./internal/scheduler -- -race -count=20 -run '^TestServeQueued/running_cancellation' -> exit 0 after correction. No tests deleted, skipped, or weakened.
Committed 4b749d8 (retention fix, regression, docs) and 5f38a2c (PID readiness flake fix), fast-forward merged into main. task check:staged passed before each commit. Owned worktree .worktrees/fix-skipped-retention and branch removed through Worktrunk after receipt verification; associated Herdr workspace wGH removed and verified absent.
Remaining delivery: push main, verify exact-SHA GitHub CI, publish v0.4.1 via tag workflow, then record release evidence and release the Worklease claim.
<!-- SECTION:NOTES:END -->
