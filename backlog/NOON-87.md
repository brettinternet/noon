---
id: NOON-87
title: Pause jobs for a bounded or scheduled window
status: todo
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
updated: 2026-10-27T16:22:38Z
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

- [ ] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml sets `timezone: America/Denver` and defines `tests` and `lint` (schedule `every 1h`, run ["true"]), after `bin/noon enable`: `bin/noon pause tests --for 2h --json` exits 0 with `changed` 1 and an `ends_at` at a whole minute no earlier than 2h and less than 2h1m after the command started; `bin/noon list --json` reports `tests` with `paused` true, `pause_ends_at`, and no `next_at`; human `bin/noon list` shows NEXT `paused until` for `tests`; `bin/noon resume tests` then reports `paused` false and no `pause_ends_at`.
- [ ] In the same environment, `bin/noon pause tests --for 30s`, `--until "2000-01-01 00:00"`, `--for 2h --until 17:00`, `--from "2099-01-01 00:00"` alone, and `--from "2099-01-01 08:00" --until "2099-01-01 00:00"` each exit 2, and `bin/noon list --json` is unchanged afterwards.
- [ ] In the same environment, `bin/noon pause lint --from "2099-01-01 00:00" --until "2099-01-01 08:00" --json` exits 0; `bin/noon list --json` reports `lint` with `paused` false, `pause_starts_at` "2099-01-01T00:00:00-07:00", `pause_ends_at` "2099-01-01T08:00:00-07:00", and a `next_at`; `bin/noon run lint --schedule "every 1h" --for 2h --json` exits 1 with code `paused`; foreground `bin/noon run lint` exits 0.
- [ ] In the same environment, after `bin/noon pause tests` (open-ended), `bin/noon pause tests --for 1h --json` exits 0 with `changed` 0 and `bin/noon list --json` still reports `tests` paused with no `pause_ends_at`.
- [ ] `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: an active window dispatches nothing and cancels a queued run; a pending window cancels nothing before it starts; after expiry no occurrence inside the window runs or catches up; with the scheduler stopped across expiry, `missed: run_once` yields one catch-up only when an occurrence fell after `resumes_at`, and none when every missed occurrence was inside the window; open-ended pause behavior is unchanged.
- [ ] `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers: a pause row written before the migration stays an open-ended active pause after upgrade; queue eligibility and temporary-schedule creation refuse during active and pending windows and accept after expiry without a scheduler. Docs in scope describe only implemented behavior. `task ci` exits 0.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
