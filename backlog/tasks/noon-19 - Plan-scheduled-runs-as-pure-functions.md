---
id: NOON-19
title: Plan scheduled runs as pure functions
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:03'
labels:
  - scheduler
dependencies:
  - NOON-1
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/scheduler/**
  - docs/design.md
priority: high
type: feature
ordinal: 900
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
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
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestPlanDue|TestPlanDispatch)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `rg -n 'time\.Now\(|"os"|database/sql|go func' internal/scheduler/plan.go` prints nothing
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
