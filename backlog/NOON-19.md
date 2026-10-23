---
id: NOON-19
title: Plan scheduled runs as pure functions
status: done
type: feature
priority: high
labels: [scheduler, reviewed]
blocked_by: [NOON-1]
rank: a1dc47711dc477114
refs: [docs/design.md, docs/development.md, backlog/decisions/decision-5 - Job-semantics-follow-cron-and-launchd-expectations.md]
files: [internal/scheduler/**, docs/design.md]
created: 2026-10-16T01:41:00Z
updated: 2026-10-16T14:21:00Z
---

Outcome: every scheduling decision comes from two pure functions with an injected clock, exhaustively table-tested: what is due, caught up, missed, skipped, queued, expired, cancelled, or started.

Read first: docs/design.md#missed-runs-overlap-and-concurrency and #schedules, decision-5.

Contract (internal/scheduler/plan.go; plain structs; no clock, database, filesystem, or goroutines):
```go
type JobKey struct{ Project int64; Job string }
type JobInput struct {
    Key      JobKey
    Schedule *schedule.Schedule // nil means manual
    Location *time.Location
    Active   bool   // project enabled, job enabled, approved or not requiring approval, not paused
    Missed   string // run_once | skip
    Overlap  string // skip | queue
}
type JobState struct { Key JobKey; ScheduleKey string; NextAt time.Time } // persisted per scheduled job
type Run struct { ID int64; Key JobKey; Status string; DueAt time.Time }  // existing queued or running runs
type NewRun struct { Key JobKey; Status, Trigger string; ScheduledAt, DueAt time.Time }
func Due(now time.Time, jobs []JobInput, states []JobState, runs []Run) (upsert []JobState, remove []JobKey, create []NewRun)
func Dispatch(now time.Time, jobs []JobInput, runs []Run) (start, expire, cancel []int64)
```
Due rules:
- ScheduleKey is `schedule.String() + "|" + location name`. A job that is inactive, manual, or gone gets its state removed. Missing state or a changed key sets `NextAt` to the first occurrence after now and creates no run, so first sight and schedule changes never catch up. Commands that reactivate a job (enable, approval, resume, add) delete its state, so reactivation is first sight too (serve and add tasks).
- Before `NextAt`, nothing happens. Otherwise: within 60s of `NextAt` it is on time (trigger `schedule`, ScheduledAt = DueAt = NextAt). Later than that, `missed: run_once` creates one run with trigger `catch_up` (ScheduledAt = NextAt, DueAt = now), and `missed: skip` creates one record with status `missed`, trigger `schedule`, ScheduledAt = NextAt, and DueAt = now.
- New `NextAt`: for cron, the first occurrence after now. For intervals, advance `NextAt` by whole intervals until it is after now, preserving phase.
- A run to execute becomes `queued`, unless the job already has a queued run whose DueAt has passed, or it is running with `overlap: skip`; then it becomes a `skipped` record with the same trigger and times. `overlap: queue` permits one queued run while running. A future one-shot (DueAt after now) never suppresses an occurrence.

Dispatch rules:
- Cancel queued runs of jobs that are inactive or gone. Expire queued runs whose `DueAt` is more than 1h before now.
- Consider the rest in (DueAt, ID) order. Start one when DueAt <= now, its job is not running, its project has no running run, and fewer than 4 runs are running in total. Count runs started earlier in the same call, and count foreground (`manual`) runs.

Tests (table-driven, fixed times):
- `TestPlanDue`: first sight; on time at +0s and +59s; catch-up at +61s; `missed: skip`; three days asleep with an hourly cron yields exactly one catch-up and the next hour after now; an interval keeps its phase across downtime (`every 6h` from 01:00, now 14:30, gives a catch-up and NextAt 19:00); schedule and timezone changes reset without a run; inactive removes state and reactivation is first sight; overlap skip and queue; at most one due queued run per job; a future one-shot does not suppress a scheduled occurrence; every field of `missed` and `skipped` records.
- `TestPlanDispatch`: FIFO by due time; one run per project; four total across five projects; a running foreground run blocks its project; same-job queued run waits for the running one; expiry after 1h; cancel for inactive and removed jobs; not-yet-due runs stay queued.

Non-goals: SQL, the serve loop, CLI changes, persistence formats.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.

## Acceptance criteria

- [x] `task cli:test -- -run '^(TestPlanDue|TestPlanDispatch)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] `rg -n 'time\.Now\(|"os"|database/sql|go func' internal/scheduler/plan.go` prints nothing

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Implement pure Due and Dispatch using validated job snapshots and persisted watermarks; preserve interval phase and enforce queue/concurrency limits. 2. Add table-driven public-interface regression tests for the specified timing, reactivation, overlap, cancellation, expiry and capacity cases. 3. Update design implementation status, run acceptance commands and CI, obtain one focused independent review, commit and merge to main, finalize task and clean owned worktree.

## Notes

AC#1: task cli:test -- -run '^(TestPlanDue|TestPlanDispatch)$' -count=1 -v -> exit 0; both named tests PASS, with all specified cases plus inclusive timing boundaries, input immutability and replay suppression. AC#2: rg -n 'time\.Now\(|"os"|database/sql|go func' internal/scheduler/plan.go -> no output (exit 1, expected no matches). Initial task ci passed: security, formatting, vet, Staticcheck, full tests, race and smoke. LSP diagnostics clean for both Go files. No existing tests modified; changes limited to internal/scheduler/** and docs/design.md. Independent review pending; next step review result, implementation commit, main integration, final-commit CI and cleanup.

Implementation commit 4b42af9 (feat(scheduler): plan due runs and dispatch) fast-forward merged into main. task check:staged passed before commit; task ci passed on committed implementation 4b42af9. Independent reviewer reported no validated findings and confirmed every specified case is covered. Design documents implemented pure planners while leaving persistence/serve explicitly unimplemented. No blockers or remaining implementation steps; final delivery is task-record commit and owned-worktree cleanup.

Review (post-merge): Due and Dispatch implement every rule. Checked the 60s on-time boundary, catch-up and `missed: skip` record fields, the interval phase arithmetic (`now + interval - (now-NextAt) mod interval`, which is strictly after now even on exact multiples), overlap and pending suppression, future one-shots, cancel-before-expire, FIFO with ID tie-break, and project/global capacity including running runs of removed jobs. No defects. Low-severity notes: (1) ScheduleKey uses `Location.String()`, so a job on `timezone: Local` keeps its state when the host zone changes and catches up from the old zone instead of resetting; (2) changing only the timezone resets interval jobs too, which matches the spec even though intervals ignore the zone. ACs 1-2 rerun and pass.

## Summary

Implemented pure Due and Dispatch planners with phase-preserving catch-up, overlap suppression, inactive cancellation, expiry, FIFO and project/global capacity. All acceptance checks, full CI and independent review passed; implementation 4b42af9 merged into main.
