---
id: NOON-15
title: Parse schedules and compute next occurrences
status: Done
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:40'
labels:
  - schedule
dependencies: []
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/schedule/**
  - docs/design.md
priority: high
type: feature
ordinal: 200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a pure `internal/schedule` package turns every schedule form in the design into a validated value with a normalized display string and exact next-occurrence math, including DST, without reading the clock.

Read first: docs/design.md#schedules, decision-5.

Contract (keep the exported API this small):
```go
func Parse(text string) (Schedule, error)                // errors carry the suggestion the design lists
func (s Schedule) String() string                        // normalized display form
func (s Schedule) Next(after time.Time, loc *time.Location) time.Time // first occurrence strictly after `after`
func (s Schedule) Interval() (time.Duration, bool)       // true only for interval schedules
func ParseDuration(text string) (time.Duration, error)   // s, m, h, d units; "90s", "1h30m", "1d12h"
```
- Interval `Next(after)` returns `after + d`; keeping phase is the planner's job.
- Cron: five fields; `*`, `a`, `a-b`, `*/n`, `a-b/n`, `a/n` (= `a-max/n`), comma lists; `jan`-`dec` and `sun`-`sat` names, case-insensitive; day-of-week `0` and `7` are Sunday. Reject wrap-around ranges, steps of 0, out-of-range values, six fields ("Noon uses five-field cron, without seconds"), and expressions that can never match. Vixie day rule: if day-of-month and day-of-week are both restricted, either may match; a field that starts with `*` counts as unrestricted.
- Never-matching check: under Vixie OR semantics any expression with a valid month matches. Otherwise it matches only if some allowed month has an allowed day-of-month that fits it (February has 29 days).
- Macros: `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`, and bare `hourly`, `daily`, `weekly`, `monthly`, `yearly`. Reject `@reboot` (hint: Noon has no boot trigger) and `@every 5m` (hint: `every 5m`).
- Calendar: `every day at T`, `daily at T`, `weekdays at T`, `weekends at T`, `every DAY at T`, `DAY[,DAY...] at T`. Days are full or three-letter names. T is `9am`, `9:30pm`, `12am` (00:00), `12pm` (12:00), 24-hour `H:MM` or `HH:MM`, `noon`, or `midnight`. One time only. Reject a bare `at 9` (suggest `9am` or `9:00`).
- Interval: `every 15m`, `every 1d12h`, `every 90 minutes`, `every 2 hours`, `every 3 days`. Units m/min/mins/minute/minutes, h/hr/hrs/hour/hours, d/day/days. Whole minutes from 1m to 365d.
- Ambiguity is an error with a suggestion: `every day`, `every hour`, `every minute`, `every 30s`.
- Normalized `String()`: cron as five space-separated fields in lowercase; macros as `@daily` and so on; calendar as `every day at 09:00`, `weekdays at 09:30`, `weekends at 12:00`, `every friday at 17:00` (one day), `mon,wed,fri at 18:00` (several days, week order); intervals compact, largest units first (`every 1h30m`, `every 1d12h`).
- Wall clock in `loc`: a matching local time that occurs twice (fall back) fires only at its first instant; matching local times inside a spring-forward gap fire once, at the first instant after the gap.

Suggested approach for Next: walk candidate wall times as naive values using `time.Date(..., time.UTC)` arithmetic, advancing by the first non-matching field (month, then day, then hour, then minute) instead of minute by minute, bounded to 8 years. Resolve each naive wall time W in `loc`: `u := time.Date(W..., loc)`. If `u` does not show W, W is in a gap, and Go may have normalized it either way: with `start, end := u.ZoneBounds()`, the gap ends at `end` when `u` shows a wall time before W (Denver 02:30 becomes 01:30 MST) and at `start` when after (Lord Howe 02:15 becomes 02:45). If W also exists in the previous zone (fall back), prefer that earlier instant. Skip any result that is not after `after`.

Tests (one table each, plus a fuzz target):
- `TestScheduleParse`: accepted inputs with their normalized `String()`; rejected inputs with a substring of the expected suggestion.
- `TestScheduleNext`: cron lists, ranges, steps, names, the Vixie day rule, `0 0 29 2 *`, macros, calendar forms, intervals, and these DST fixtures, asserting exact instants (for example, `30 2 * * *` fires at 03:00 MDT on 2026-03-08): America/Denver 2026-03-08 (02:00 to 03:00) and 2026-11-01 (02:00 MDT back to 01:00 MST); Australia/Lord_Howe 2026-10-04 (02:00 to 02:30) and 2026-04-05 (02:00 back to 01:30); America/Santiago 2026-09-06, where `daily at midnight` fires at 01:00.
- `FuzzScheduleParse`: never panics; when Parse succeeds, parsing `String()` succeeds and yields the same `String()`, and `Next(t, time.UTC)` is after `t` for a fixed `t`.

Non-goals: persistence, planning, CLI wiring, `noon list`, seconds fields, free text, LLM parsing, any cron library.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestScheduleParse|TestScheduleNext)$' -count=1 -v` exits 0 and prints `--- PASS: TestScheduleParse` and `--- PASS: TestScheduleNext`, with Next cases for every DST fixture named in the description
- [x] #2 `mise exec go -- go test ./internal/schedule -run '^$' -fuzz '^FuzzScheduleParse$' -fuzztime 30s` exits 0
- [x] #3 `rg -n 'time\.Now\(' internal/schedule` prints nothing
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
1. Implement the pure schedule API, strict grammar, normalization, and wall-clock DST resolution. 2. Add parsing, next-occurrence regression tables and round-trip fuzzing. 3. Run all acceptance commands, independent scoped review, and CI; commit, merge to main, finalize task, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fuzzing found that the suggested 8-year Next search bound violates the required next-occurrence contract: 0 1 1 1 */7 has no occurrence within 8 years after 2026-01-01 (next is Sunday 2034-01-01). Use the complete 400-year Gregorian cycle as the search bound, still advancing by unmatched fields. Retain the minimized fuzz regression. No settled decision or exported API changes.

AC#1: task cli:test -- -run '^(TestScheduleParse|TestScheduleNext)$' -count=1 -v -> exit 0, both named tests PASS, including Denver spring/fall, Lord Howe spring/fall, Santiago midnight gap and repeated-hour suppression. AC#2: mise exec go -- go test ./internal/schedule -run '^$' -fuzz '^FuzzScheduleParse$' -fuzztime 30s -> exit 0, 26,331,707 executions after fixing the discovered search-bound regression. AC#3: rg -n 'time\.Now\(' internal/schedule -> no output (exit 1, expected no matches). Full task ci passed before commit: security, formatting, vet, Staticcheck, unit tests, race, smoke. LSP diagnostics clean. Diff confined to internal/schedule/** and docs/design.md plus this task record; no existing tests removed or weakened. CLI remains unchanged and design explicitly distinguishes the implemented library from future CLI scheduling. Independent scoped review pending; final-commit CI and delivery remain.

Delivery: dd260cd feat(schedule): parse schedules and compute occurrences fast-forward merged into main. task ci passed on this final implementation commit. The single independent reviewer found no validated defects (static review; execution was performed by the parent). Worktrunk removed the verified session-owned feat/noon-15-schedules worktree and branch; its Herdr workspace wBK was removed. All acceptance and delivery requirements satisfied; no blocker or resumable implementation work remains.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented the pure schedule API with cron, macros, calendar phrases, intervals, normalized display, overflow-safe durations, and DST gap/fold semantics. Verified all acceptance commands, 30-second fuzzing, full CI on dd260cd, and independent review. Merged to main and cleaned up the owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
