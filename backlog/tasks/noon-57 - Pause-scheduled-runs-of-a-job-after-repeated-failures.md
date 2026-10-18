---
id: NOON-57
title: >-
  Give lifecycle hooks the consecutive failure count and ship a pause-after
  recipe
status: To Do
assignee: []
created_date: '2026-10-18 16:24'
updated_date: '2026-10-18 16:31'
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
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a failing agent or command job can stop running unattended after N consecutive failures without new scheduler policy. Hooks receive the consecutive failure count, and the notifications example ships a small wrapper that pauses the job at a threshold and then runs the next hook command.

Why: an agent job whose CLI login, token, or quota expires fails on every occurrence; an hourly prompt job keeps spending and alerting until someone notices. Prior art: Jobber error handlers (stop after consecutive failures); Claude Code routines turn off after 72 hours without a GitHub connection. decision-4 makes hooks the extension point, so the count goes to hooks and the policy stays in a wrapper.

Settled contract:
- `NOON_CONSECUTIVE_FAILURES` (decimal string) in the hook environment and top-level `consecutive_failures` (integer) in hook stdin JSON, beside `previous_status`. Additive; hook stdin `schema_version` stays 1.
- Count = the current run if `failed` or `timed_out`, plus the immediately preceding retained `failed`/`timed_out` results of the same project and job, ordered by `finished_at` then ID, stopping at the first `succeeded`. A succeeded current run yields 0.
- Only `succeeded`, `failed`, and `timed_out` are results, matching NOON_PREVIOUS_STATUS: noop, skipped, missed, expired, cancelled, and interrupted neither count nor reset. All triggers count, including foreground runs. Definition digest changes, pause, and resume do not reset; after resume, one more failure re-pauses until a success. The count is bounded by retained history (the regular 50-run budget) and the docs say so.
- Taken in the same pre-FinishRun snapshot as the previous status. A lookup error keeps current behavior: the job result is recorded and the hook is skipped.
- Recipe: examples/integrations/notifications/bin/noon-pause-after reads a threshold (default 3, from `NOON_PAUSE_AFTER`), runs `noon -C "$NOON_PROJECT" pause "$NOON_JOB"` when the count reaches it, then execs its remaining arguments, so `on_failure: [noon-pause-after, noon-on-change]` both pauses and notifies. Document that a run dispatched before the hook finishes can still start, and that `noon resume NAME` re-enables the job.

Non-goals: a core pause_after field, retries or backoff scheduling, a distinct auto-paused state or dashboard notice, classifying agent error causes, changing hook ordering, timeouts, or failure semantics.

Dependencies: none (builds on NOON-31, Done).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes first. Tests: extend TestStore, TestHooks, and TestExampleNotifications; never call desktop or network notifiers; isolate HOME, XDG, and state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run "^TestStore$" -count=1` exits 0; a store-level case seeds results out of ID order and shows the count follows completion order, stops at the latest succeeded, includes timed_out, and ignores noop, skipped, cancelled, and interrupted rows.
- [ ] #2 `task cli:test -- -run "^TestHooks$" -count=1` exits 0; through cli.Run, a job failing twice then succeeding gives on_failure hooks 1 then 2 in both NOON_CONSECUTIVE_FAILURES and stdin consecutive_failures, and the on_success hook 0; an inherited or job env NOON_CONSECUTIVE_FAILURES value never leaks through.
- [ ] #3 `task cli:test -- -run "^TestExampleNotifications$" -count=1 -v` exits 0 and prints PASS; through the built binary with an isolated state dir, a job using `on_failure: [noon-pause-after, <capture stub>]` with NOON_PAUSE_AFTER=2 is not paused after one failure, is paused after two (`noon list --json` reports paused true), and the capture stub ran both times.
- [ ] #4 `rg -n "NOON_CONSECUTIVE_FAILURES" docs/hooks.md docs/design.md examples/integrations/notifications/README.md` matches in all three files, and the docs state the counting, retention, and no-reset rules above.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
