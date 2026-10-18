---
id: NOON-55
title: Give skipped runs their own retention budget
status: To Do
assignee: []
created_date: '2026-10-18 06:43'
updated_date: '2026-10-18 06:43'
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
- [ ] #1 `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers: two real results (one `succeeded`, one `failed`, each with a log) followed by 60 `skipped` runs for the same job leave both results with their logs and exactly the 10 newest skipped runs; `PreviousStatus` still returns the older result.
- [ ] #2 `task cli:test PKG=./internal/store -- -count=1 -run 'TestScheduledHistoryRetention|TestNoopRetentionPreservesResultsAndState'` exits 0 with both tests unchanged (missed and noop retention unaffected).
- [ ] #3 docs/design.md and docs/agents.md state the skipped budget exactly as implemented, and `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
