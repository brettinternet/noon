---
id: NOON-57
title: >-
  Give lifecycle hooks the consecutive failure count and ship a pause-after
  recipe
status: Done
assignee: []
created_date: '2026-10-18 16:24'
updated_date: '2026-10-18 16:48'
labels:
  - scheduler
  - hooks
  - agents
dependencies: []
documentation:
  - docs/hooks.md
  - docs/design.md
modified_files:
  - internal/store/store.go
  - internal/store/store_test.go
  - internal/app/hooks.go
  - internal/cli/run_test.go
  - docs/hooks.md
  - docs/design.md
  - examples/integrations/notifications/**
  - integration/examples_test.go
priority: medium
type: feature
ordinal: 17600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a failing agent or command job can stop running unattended after N consecutive failures without new scheduler policy. Hooks receive the consecutive failure count, and the notifications example ships a small wrapper that pauses the job at a threshold and then runs the next hook command.

Why: an agent job whose CLI login, token, or quota expires fails on every occurrence; an hourly prompt job keeps spending and alerting until someone notices. Prior art: Jobber error handlers (stop after consecutive failures); Claude Code routines turn off after 72 hours without a GitHub connection. decision-4 makes hooks the extension point, so the count goes to hooks and the policy stays in a wrapper.

Settled contract:
- `NOON_CONSECUTIVE_FAILURES` (decimal string) in the hook environment and top-level `consecutive_failures` (integer) in hook stdin JSON, beside `previous_status`. Additive; hook stdin `schema_version` stays 1.
- Count = the current run if `failed` or `timed_out`, plus the immediately preceding retained `failed`/`timed_out` results of the same project and job, ordered by `finished_at` then ID, stopping at the first `succeeded`. A succeeded current run yields 0.
- Only `succeeded`, `failed`, and `timed_out` are results, matching NOON_PREVIOUS_STATUS: noop, skipped, missed, expired, cancelled, and interrupted neither count nor reset. All triggers count, including foreground runs. Definition digest changes, pause, and resume do not reset; after resume, one more failure re-pauses until a success. The count is retained eligible history plus the current outcome, not a lifetime counter; because it is captured before FinishRun pruning, it can reach 51 with 50 retained failures plus the current failure. Missed, expired, cancelled, and interrupted rows consume the regular retention budget even though they do not count; docs state this limitation. Thresholds above the available history are not reliable.
- Taken in the same pre-FinishRun snapshot as the previous status. A lookup error keeps current behavior: the job result is recorded and the hook is skipped.
- Recipe: examples/integrations/notifications/bin/noon-pause-after reads a positive integer threshold (default 3, from `NOON_PAUSE_AFTER`; reject malformed or non-positive values before any pause), runs `noon -C "$NOON_PROJECT" pause "$NOON_JOB"` when the count reaches it, then execs its remaining arguments, so `on_failure: [noon-pause-after, noon-on-change]` both pauses and notifies. Document that a run dispatched before the hook finishes can still start, and that `noon resume NAME` re-enables background runs without resetting the count; a foreground run can still be used to obtain a success while paused. The recipe must explicitly set NOON_STATE_DIR to the same absolute state directory as the owning Noon invocation when using a non-default store: service install passes --state-dir, which is not automatically inherited by hook subprocesses. Do not infer a state path from the project or log file.

Non-goals: a core pause_after field, retries or backoff scheduling, a distinct auto-paused state or dashboard notice, classifying agent error causes, changing hook ordering, timeouts, or failure semantics.

Dependencies: none (builds on NOON-31, Done).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes first. Tests: extend TestStore, TestHooks, and TestExampleNotifications; never call desktop or network notifiers; isolate HOME, XDG, and state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test PKG=./internal/store -- -run "^TestStore$" -count=1` exits 0; a store-level case seeds results out of ID order and shows the count follows completion order, stops at the latest succeeded, includes timed_out, and ignores noop, skipped, cancelled, and interrupted rows. The same fixture verifies that retained-history exhaustion yields a bounded count, not a persisted lifetime streak.
- [x] #2 `task cli:test PKG=./internal/cli -- -run "^TestHooks$" -count=1` exits 0; through cli.Run, a job failing twice then succeeding gives on_failure hooks 1 then 2 in both NOON_CONSECUTIVE_FAILURES and stdin consecutive_failures, and the on_success hook 0; an inherited or job env NOON_CONSECUTIVE_FAILURES value never leaks through.
- [x] #3 `task cli:test PKG=./integration -- -run "^TestExampleNotifications$" -count=1 -v` exits 0 and prints PASS; through the built binary with an isolated state dir, a job using `on_failure: [noon-pause-after, <capture stub>]` with NOON_PAUSE_AFTER=2 is not paused after one failure, is paused after two (`noon list --json` reports paused true), and the capture stub ran both times. The fixture puts the built noon on PATH, sets NOON_STATE_DIR explicitly to its isolated non-default store, and verifies an invalid threshold exits nonzero without pausing. No real notifier is invoked.
- [x] #4 `rg -n "NOON_CONSECUTIVE_FAILURES" docs/hooks.md docs/design.md examples/integrations/notifications/README.md` matches in all three files, and the docs state the counting, retention, and no-reset rules above.
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
1. Extend the existing pre-finish result snapshot with consecutive failures and expose it to hooks.
2. Add the pause-after wrapper and document retained-history and state-directory rules.
3. Extend existing critical regression tests; run acceptance checks and task ci; review, commit, merge locally, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: PreviousStatus snapshots before FinishRun, so a retained-history count can include 50 prior rows plus the current outcome. internal/service/service.go passes --state-dir to serve, while JobEnvironment does not export that selected path; the wrapper recipe must explicitly preserve state selection. No new scheduler policy or core state environment field is added.

AC#1: task cli:test PKG=./internal/store -- -run "^TestStore$" -count=1 -> exit 0; completion-order/tie ordering, eligible streak, success boundary, and pruned-history exhaustion verified.
AC#2: task cli:test PKG=./internal/cli -- -run "^TestHooks$" -count=1 -> exit 0; hook env/stdin counts 1, 2, 0 and reserved metadata isolation verified.
AC#3: task cli:test PKG=./integration -- -run "^TestExampleNotifications$" -count=1 -v -> exit 0, PASS; real binary pauses only at two failures in explicit non-default store, chained stdin captured twice, invalid thresholds rejected without pause.
AC#4: rg -n "NOON_CONSECUTIVE_FAILURES" docs/hooks.md docs/design.md examples/integrations/notifications/README.md -> exit 0, matches all three; counting, retention, no-reset, dispatch race, and explicit state selection documented.
Initial task ci passed (security, vet, Staticcheck, all tests, race, smoke). Go LSP diagnostics clean for store.go and hooks.go. No files outside the contract; no tests removed or weakened. Independent scoped review pending.

Delivery: implementation 6d0c074 merged locally into main as 1bae883. task ci passed on merged implementation commit 1bae883 (security, checks, all tests, race, smoke). Independent reviewer reported no validated findings; no corrections required. Worktrunk removed the verified session-owned noon-57-hook-failures worktree and branch; associated Herdr workspace wGR confirmed absent. No retained task checkout, blocker, or next implementation step. No push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Hooks now expose retained consecutive failures in environment and JSON from the pre-finish snapshot. Shipped and documented a threshold pause-and-chain wrapper with explicit state-store guidance. All four acceptance checks and full CI passed; independent review found no defects. Committed, merged locally, and cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
