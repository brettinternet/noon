---
id: NOON-87
title: Pause jobs for a bounded or scheduled window
status: done
type: feature
priority: medium
labels: [scheduler, store, cli]
related: [NOON-77, NOON-68, NOON-20]
refs:
  - docs/design.md
  - docs/cli.md
  - docs/configuration.md
  - docs/decisions/decision-5 - Job-semantics-follow-cron-and-launchd-expectations.md
files:
  - internal/store/store.go
  - internal/store/approval.go
  - internal/store/approval_test.go
  - internal/store/scheduler.go
  - internal/store/queue.go
  - internal/store/temporary.go
  - internal/store/store_test.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/app/manage.go
  - internal/app/approval.go
  - internal/app/inspect.go
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/jobs.go
  - internal/cli/dashboard.go
  - internal/tui/view.go
  - README.md
  - docs/cli.md
  - docs/configuration.md
  - docs/design.md
created: 2026-10-27T16:22:30Z
updated: 2026-10-27T16:43:12Z
---

Why: there is no way to say "nothing runs Saturday 22:00 to Sunday 06:00" for a maintenance window, a release freeze, or a trip. `noon pause` is open-ended, and a script guard with `noop_exit_code` only covers recurring windows (NOON-77). Chaining two temporary jobs that run `noon pause -a` and `noon resume -a` is unsafe: a resume one-shot that is more than 1h overdue (laptop asleep, scheduler down) expires, so the jobs stay paused indefinitely and silently; in the same project, `pause -a` cancels the resume run itself; and `resume -a` also resumes jobs that were paused individually before the window. NOON-68 listed "a pause that resumes later" as a non-goal; this task is that follow-up.

Outcome: a pause can have an end and, optionally, a future start:

```console
$ noon pause -a --for 8h
Paused 3 jobs until Sun 06:00. Queued runs cancelled; running work continues.
$ noon pause deploy --from "2026-11-07 22:00" --until "2026-11-08 06:00"
Pauses deploy Sat 22:00 – Sun 06:00.
$ noon list
JOB     SCHEDULE            NEXT
deploy  every day at 23:00  Sun 23:00
tests   every day at 02:00  paused until Sun 06:00
```

Settled contract:
- `noon pause` gains `--for DURATION` (`schedule.ParseDuration`, at least 1m, rounded up to a whole minute) and `--until TIME` (mutually exclusive), plus `--from TIME`, which requires one of them. TIME uses the `noon add --until` resolution (`HH:MM` as its next occurrence, `YYYY-MM-DD HH:MM`, RFC 3339) in the project timezone with decision-5 DST rules. With `--from`, `--for` counts from the start. The end must be after now and after the start; `--from` must be after now. Anything else is a usage error (exit 2) that changes nothing. There is no maximum length: open-ended pauses already exist.
- Storage: the next state migration adds nullable `starts_at` and `resumes_at` to `pauses`. NULL `starts_at` means active since `paused_at`; NULL `resumes_at` means open-ended. Rows from older stores stay open-ended pauses. Each job has at most one pause row.
- Active means `starts_at <= now < resumes_at`, with NULL bounds unbounded. Every reader uses this one predicate: scheduler snapshot, `PausedJobs`, queue eligibility (`RequireUnpaused`/webhook), and temporary-schedule creation. Readers without a scheduler ignore expired rows. A pending window has no effect before it starts.
- While active, pause semantics are unchanged: the job is inactive for the planner, queued runs are cancelled (including future one-shots, through the inactive-job dispatch rule), running work finishes, queued requests that require an unpaused job are refused with `paused`, and foreground `noon run` works.
- Creating any pause, pending or active, deletes the job's temporary schedule, as pause does today, and temporary-schedule creation is refused while the job has an unexpired pause row (pending or active). An immediate pause cancels queued runs at command time, as today. A pending window cancels them when it starts.
- Expiry has resume semantics: no occurrence whose effective start falls inside the window runs or catches up. The scheduler deletes the expired row and resets the job's schedule state in its tick transaction before planning, so that occurrences after `resumes_at` that were missed while the scheduler was offline follow the job's `missed` policy. Occurrences inside the window never count as missed.
- Combining: an open-ended pause is never shortened, so a timed `pause` on an open-ended paused job changes nothing (`changed: 0`). A timed pause replaces an existing timed or pending row. A plain `pause` turns any row into an open-ended active pause. `resume` deletes any row (pending, active, or open-ended) and resets schedule state as today. `-a` covers the jobs that exist now, as today.
- Display: list JSON and MCP `list_jobs` keep `paused` (true only while active) and add `pause_starts_at` and `pause_ends_at` (RFC 3339 in the project timezone, omitted when unset or expired). Human NEXT shows `paused until DAY HH:MM` for an active timed pause and `paused` for an open-ended one. `next_at` and NEXT skip occurrences whose effective start falls inside a pending or active window. Pause JSON keeps `jobs`, `paused`, and `changed` and adds `starts_at`/`ends_at` when set. The TUI shows the same status text.

Scope: store migration and the pause predicate, pause/resume store and app operations, scheduler snapshot and expiry handling, queue and temporary-schedule eligibility, inspection (`paused`, new fields, `next_at` skip), `noon pause` flags and output, list/dashboard text, TUI status text, and docs: README command table, docs/cli.md "Pausing jobs" and JSON shapes, docs/configuration.md pause recipe, and docs/design.md pause and missed-run rules.

Tests (critical only):
- State transitions: an active window blocks dispatch and cancels queued runs; a pending window cancels nothing before it starts; expiry runs no occurrence inside the window; a scheduler offline across expiry applies `missed` only to post-window occurrences.
- Data integrity: a pre-migration pause row stays open-ended.
- Authorization boundary: queue and temporary-schedule refusal while active or pending.
- Extend TestBulkPauseAtomicity/TestStore, the serve tests with injected time, and TestPauseResume.

Non-goals: recurring windows (NOON-77); project-wide pauses that cover jobs added later; pause or resume from MCP, the HTTP API, or the TUI key; preserving queued work across a window; stopping running work at window start; more than one window per job; config-file pause fields.

Dependencies: none (builds on NOON-20 and NOON-68, Done).

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml sets `timezone: America/Denver` and defines `tests` and `lint` (schedule `every 1h`, run ["true"]), after `bin/noon enable`: `bin/noon pause tests --for 2h --json` exits 0 with `changed` 1 and an `ends_at` at a whole minute no earlier than 2h and less than 2h1m after the command started; `bin/noon list --json` reports `tests` with `paused` true, `pause_ends_at`, and no `next_at`; human `bin/noon list` shows NEXT `paused until` for `tests`; `bin/noon resume tests` then reports `paused` false and no `pause_ends_at`.
  Evidence: task cli:build; /tmp/noon87-ac/ac.sh: pause tests --for 2h --json exit 0, ends_at - start = 7208s on a whole minute; list --json paused true with pause_ends_at; human NEXT 'paused until Tue 12:39'; resume clears paused/pause_ends_at and restores next_at.
- [x] In the same environment, `bin/noon pause tests --for 30s`, `--until "2000-01-01 00:00"`, `--for 2h --until 17:00`, `--from "2099-01-01 00:00"` alone, and `--from "2099-01-01 08:00" --until "2099-01-01 00:00"` each exit 2, and `bin/noon list --json` is unchanged afterwards.
  Evidence: ac.sh AC2: --for 30s, --until past, --for+--until, --from alone, --from after --until each exit 2 with usage message (last: 'until must be after from'); list --json (minus wall-clock next_at preview) unchanged.
- [x] In the same environment, `bin/noon pause lint --from "2099-01-01 00:00" --until "2099-01-01 08:00" --json` exits 0; `bin/noon list --json` reports `lint` with `paused` false, `pause_starts_at` "2099-01-01T00:00:00-07:00", `pause_ends_at` "2099-01-01T08:00:00-07:00", and a `next_at`; `bin/noon run lint --schedule "every 1h" --for 2h --json` exits 1 with code `paused`; foreground `bin/noon run lint` exits 0.
  Evidence: ac.sh AC3: pause lint --from 2099-01-01 00:00 --until 08:00 exit 0 with starts_at/ends_at; list shows paused false, pause_starts_at/pause_ends_at, next_at present; run lint --schedule exits 1 code paused; foreground run lint exits 0.
- [x] In the same environment, after `bin/noon pause tests` (open-ended), `bin/noon pause tests --for 1h --json` exits 0 with `changed` 0 and `bin/noon list --json` still reports `tests` paused with no `pause_ends_at`.
  Evidence: ac.sh AC4: after open-ended pause, pause tests --for 1h --json -> changed 0, paused true, no pause_ends_at; human NEXT 'paused'.
- [x] `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: an active window dispatches nothing and cancels a queued run; a pending window cancels nothing before it starts; after expiry no occurrence inside the window runs or catches up; with the scheduler stopped across expiry, `missed: run_once` yields one catch-up only when an occurrence fell after `resumes_at`, and none when every missed occurrence was inside the window; open-ended pause behavior is unchanged.
  Evidence: task cli:test PKG=./internal/scheduler -- -count=1 -> ok. TestServePauseWindows: pending window keeps queued run at +1m, active window cancels it and dispatches nothing through +30m, first run after expiry is the +40m schedule occurrence; scheduler stopped across expiry with missed run_once: restart +35m no catch-up then +40m schedule run, restart +45m one catch_up. Open-ended pause covered by existing TestServeTemporarySchedules 'permanent removal pause'.
- [x] `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers: a pause row written before the migration stays an open-ended active pause after upgrade; queue eligibility and temporary-schedule creation refuse during active and pending windows and accept after expiry without a scheduler. Docs in scope describe only implemented behavior. `task ci` exits 0.
  Evidence: task cli:test PKG=./internal/store -- -count=1 -> ok. TestPauseWindows: v14 pause row upgraded to open-ended active (nil bounds), timed pause does not shorten it (changed 0); QueueRun RequireUnpaused refuses ErrJobPaused in active window, accepts before pending window starts; CreateTemporarySchedule refuses in active and pending windows; both accept after expiry and Pauses omits the expired row, no scheduler. Docs (README, cli.md Pausing jobs + JSON table, configuration.md recipe + list fields, design.md pause/missed rules + v15) describe implemented behavior. task ci exit 0.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: bl heartbeat -- task ci exit 0 (lint, tests, gitleaks, govulncheck, gofmt, smoke).
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC 1-6 each checked with command and observed result.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Out-of-contract files justified in notes: schedule/next.go, scheduler/plan.go, app/scheduler.go, cli/root.go, cli/serve_test.go, tui/tui_test.go.
- [x] No test was deleted, skipped, or weakened
  Evidence: No test weakened; only signature updates in existing tests plus TestPauseResume chaining WithScheduler; new TestServePauseWindows and TestPauseWindows.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: README, docs/cli.md, docs/configuration.md, docs/design.md updated to the settled contract.

## Notes

Implemented in d248c88 (feat: pause jobs for a bounded or scheduled window), fast-forwarded onto main; worktree .worktrees/noon-87-pause-window and branch removed via wt remove. Not pushed.
Deviations outside the modified-file contract: internal/schedule/next.go adds Schedule.AfterPause, the shared first-occurrence-after-window helper used by both app and scheduler (lives in schedule to avoid an app<->scheduler import cycle); internal/scheduler/plan.go seeds state from AfterPause when a pause expired this tick (JobInput.Resumed) so in-window occurrences never catch up while later offline-missed ones follow missed; internal/app/scheduler.go moves pause enrichment into WithScheduler because it needs now (pending windows, next_at skipping); internal/cli/root.go defines the --for/--until/--from flags; internal/cli/serve_test.go and internal/tui/tui_test.go only follow the ReadSchedulerSnapshot(now)/Pauses signature changes.
Interpretation of AC6: queue eligibility refuses only during an active window (a pending window has no effect until it starts, per contract); temporary-schedule creation refuses during pending and active windows. Both accept after expiry without a scheduler.
Review: one pass over store/scheduler/app changes; no defects found. Calendar occurrences exactly at resumes_at run; intervals restart one period after resumes_at.

## Summary

noon pause gains --for/--until/--from timed windows backed by nullable pauses.starts_at/resumes_at (migration 15) and one active predicate shared by all readers. The scheduler expires windows in the tick like resume and restarts from the first occurrence after the window; list/MCP/TUI show pause bounds and 'paused until DAY HH:MM'. Commit d248c88 on main (not pushed); task ci passes.
