---
id: NOON-69
title: Attach temporary schedules to approved jobs from the CLI and MCP
status: Done
assignee: []
created_date: '2026-10-22 02:56'
updated_date: '2026-10-22 03:39'
labels:
  - scheduler
  - cli
  - mcp
dependencies:
  - NOON-68
documentation:
  - docs/design.md
  - docs/cli.md
  - docs/agents.md
  - >-
    backlog/decisions/decision-1 -
    One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-3 -
    Runtime-state-lives-per-user-in-SQLite-not-in-the-repository.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/store/store.go
  - internal/store/temporary.go
  - internal/store/store_test.go
  - internal/store/scheduler.go
  - internal/store/approval.go
  - internal/store/cancel.go
  - internal/store/events.go
  - internal/scheduler/plan.go
  - internal/scheduler/plan_test.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/app/queue.go
  - internal/app/inspect.go
  - internal/app/scheduler.go
  - internal/app/dashboard.go
  - internal/cli/root.go
  - internal/cli/run.go
  - internal/cli/queue.go
  - internal/cli/queue_test.go
  - internal/cli/cancel.go
  - internal/cli/jobs.go
  - internal/cli/dashboard.go
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - internal/skill/skill.go
  - README.md
  - docs/cli.md
  - docs/agents.md
  - docs/design.md
priority: medium
type: feature
ordinal: 24400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: agents and users want bounded recurring work, such as checking CI every 10 minutes for the next two hours, without writing config. Agents cannot add or edit jobs (decision-2; docs/design.md trust table rejects mcp-cron and herdr-sched because agents would grant themselves authority). They can already queue approved jobs at any time, so a time-limited schedule that only re-queues an approved job grants no new authority. Promoted at the user request; the user chose this over MCP job creation.

Outcome: a temporary schedule is a schedule from the existing grammar, attached to one existing runnable job, stored in the state database, and required to end within 7 days:

```console
$ noon run watch-ci --schedule "*/10 * * * *" --for 2h
Scheduled watch-ci */10 * * * * until Mon 17:00 · next Mon 15:10 (in 9m)
$ noon run standup --schedule "weekdays at 9am" --until "2026-10-23 10:00"
$ noon cancel watch-ci        # ends the temporary schedule and cancels active runs
```

```json
{"name":"watch-ci","schedule":"every 10m","for":"2h","key":"watch-ci-pr-42","data":{"pr":42}}
```

Settled contract:
- One grammar: `--schedule` and MCP `schedule` go through `schedule.Parse` with the same errors as config (cron, macros, calendar phrases, intervals). An interval keeps the phase it started with; cron forms align to the clock, exactly as in config. No `--every` shorthand.
- An end is required: `--for DURATION` or `--until TIME` (CLI), `for` or `until` (MCP), never both, using the NOON-68 `noon add` resolution rules. The end must be after now and at most 7 days away, and at least one occurrence must start before it; each violation is a usage error. Combining `--schedule` with `--detach`, `--in`, or `--at` (MCP `in`, `at`) is a usage error. `noon run NAME... --schedule` gives each named job the same schedule and end.
- Eligibility at creation is exactly the queued-request check plus NOON-68: project enabled, job exists, enabled, on a supported platform, not paused, approved when shared, not ended, recursion check. Error codes match queued requests.
- At most one temporary schedule per job. A second request returns the existing one with `created` false and `reason` `duplicate` when it carries the same `key`; otherwise it fails with code `already_scheduled` and hint `noon cancel NAME`.
- Stored in a new table via an append-only migration: project, job, schedule text, end, the job digest at creation, requested_by (CLI `cli`, MCP client name and version, as metadata), optional key, optional data (validated like `--data`, up to 256 KiB), created_at, and persisted next occurrence. The project timezone applies.
- Planning reuses `scheduler.Due` rules with the job `missed` and `overlap` policies and DST handling. No splay or jitter. Overlap, due-queued suppression, and the per-job running limit are shared with the config schedule of the job: at most one new occurrence per job per tick, and a coinciding second occurrence records `skipped`. Runs keep trigger `schedule` or `catch_up`, carry the requested_by of the temporary schedule, and each run gets its own event file from the stored data. Cooldown and `requests: each` do not apply, as for config schedules. Occurrences start only before the end and before any NOON-68 `until` of the job.
- A temporary schedule ends, and its row is deleted, when: its end passes; `noon cancel NAME` or MCP `cancel_run` with `job` runs (which also cancel active runs, as `noon cancel NAME` does today); the job is paused, disabled, removed, ended, or moved to another platform; its digest no longer matches the digest recorded at creation; or the project is disabled. Resume, re-enable, and re-approval never restore it. Queued and running runs it already created follow existing rules.
- Visibility: `noon list --json` and MCP `list_jobs` add `temporary_schedule` (`schedule`, `until`, `next_at`, `requested_by`, `created_at`, optional `key`), omitted when none; the job `next_at` is the earlier of the config and temporary next occurrences. `noon list` NEXT reflects that time. Bare `noon` and MCP `status` add one notice line, such as `1 temporary schedule: watch-ci until Mon 17:00 (claude-code 2.1)`. CLI JSON for creation is `{"schema_version":1,"temporary_schedule":{...},"created":bool,"reason":"duplicate","scheduler":"running"|"offline"}`, with the same offline notice as queued runs.
- MCP: `run_job` gains `schedule`, `for`, and `until`; `cancel_run` gains `job` (with optional `project`) as an alternative to `run_id`, with `noon cancel NAME` semantics. Remote MCP over HTTP gets the same tools under the existing bearer token; the remote authority is unchanged because only approved jobs can be scheduled.

Scope: migration and store operations, scheduler snapshot and tick, planner inputs, the shared app operation used by CLI and MCP, CLI flags and output for run, cancel, list, and the dashboard, MCP tool schemas and descriptions, skill text, docs: cli.md (run --schedule, cancel, JSON), agents.md (temporary schedules, authority, ending), design.md (temporary schedules contract, trust table wording, state tables), README command table.

Tests (critical only): creation refuses ineligible jobs, missing or over-7-day ends, and conflicting flags (authorization boundary, parsing); one temporary schedule per job with key dedupe under concurrent requests (data integrity); occurrences follow the schedule, stop at the end, and share overlap with the config schedule without double runs (state transition); pause, disable, digest change, job removal, and project disable delete the schedule and it does not return on resume (authorization boundary, subtle regression); persisted next occurrence survives a scheduler restart (state transition). Extend existing internal/store, internal/scheduler, internal/cli, and internal/mcp tests.

Non-goals: agents creating or editing job definitions; temporary schedules for unapproved or ended jobs; more than one temporary schedule per job; ends beyond 7 days (use config `until` from NOON-68); run counts; splay or jitter; pausing a temporary schedule; HTTP JSON API `/v1/` routes for temporary schedules; TUI controls beyond the shared NEXT value; history of ended temporary schedules.

Dependencies: NOON-68 (`until` resolution rules, ended jobs, `ended` error code).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo enabled with `bin/noon enable` whose .git/noon.yaml defines manual job `ping` (run ["true"]): `bin/noon run ping --schedule "*/10 * * * *" --for 2h --json` exits 0 with `created` true and `temporary_schedule` holding that schedule, an `until` 2h out rounded up to a whole minute, a `next_at` on a 10-minute boundary, and `requested_by` "cli"; `bin/noon list --json` reports the same `temporary_schedule` for `ping` and a matching job `next_at`; bare `bin/noon` prints the temporary schedule notice.
- [x] #2 In the same environment: repeating the request without `--key` exits 1 with code `already_scheduled`; after `bin/noon cancel ping`, `bin/noon run ping --schedule "every 10m" --for 1h --key k1 --json` twice returns the same schedule with the second reporting `created` false and `reason` "duplicate"; each of `--schedule "every 10m"` alone, `--for 8d`, `--for 1h --until 17:00`, `--schedule "0 9 1 1 *" --for 1h` (no occurrence before the end), `--schedule "every 10m" --for 1h --detach`, and `--schedule "every day"` exits 2; after adding shared job `shared` (run ["true"]) to noon.yaml without re-running enable, `bin/noon run shared --schedule "every 10m" --for 1h` exits 1 with code `needs_approval`; after `bin/noon cancel ping` and `bin/noon pause ping`, a new request for `ping` exits 1 with code `paused`.
- [x] #3 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time and a real store: a temporary schedule creates runs at its occurrences with its requested_by and one event file per run holding its data; none at or after its end, after which its row is gone; a config schedule and a temporary schedule coinciding on one job produce one run and one `skipped` record; the job `missed` policy applies after downtime; pause, disable, digest change, job removal, and project disable each delete the schedule and resume or re-enable does not restore it; the persisted next occurrence survives a scheduler restart without a duplicate run.
- [x] #4 `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers migration of an existing database, and 10 concurrent creation requests for one job with distinct keys producing exactly one temporary schedule. `task cli:test PKG=./internal/mcp -- -count=1` exits 0 and covers `run_job` with `schedule` and `for` creating a schedule recorded with the client name, `run_job` rejecting `schedule` with `in`, and `cancel_run` with `job` ending the schedule.
- [x] #5 Docs in scope describe only implemented behavior, including the agents.md authority statement. `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation plan: append schema v13; serialize schedule creation with existing queue eligibility; persist an independent next occurrence and feed temporary occurrences through scheduler.Due after config occurrences so overlap is shared; expose shared app operation to CLI and MCP. No dependencies added. Modified-file contract expansion: internal/skill/SKILL.md is the actual go:embed source; update it rather than the unchanged embedding wrapper internal/skill/skill.go. Regression tests will cover authorization, concurrent deduplication, occurrence/restart integrity, cancellation, and permanent lifecycle removal.

Independent review found two concrete defects to fix before delivery: offline remove/re-add could retain and revive a temporary row, and mixed job/ID cancellation waited on the first job before requesting later targets. Contract expansion: internal/cli/manage.go must invalidate temporary schedules when removing a job, including while the scheduler is offline. Add regressions in existing queue tests. The first task ci attempt hit an unrelated existing runner subprocess PID timing flake; its focused rerun and a complete task ci rerun passed without changing or weakening that test.

Delivery: implementation commit 761349c0729486bbd57118ce8e7dd65df84cfb29 fast-forward merged into main. One independent reviewer pass found offline remove/re-add resurrection and delayed mixed-selector cancellation; both fixed and regression-tested. No further review pass; no tests deleted, skipped, or weakened. AC#1: task cli:build && bun /tmp/noon-69-acceptance.mjs -> PASS, isolated temporary Git repo with private ping, all HOME/XDG/state variables under mktemp; validates created metadata, minute-rounded 2h end, 10-minute NEXT, list identity and dashboard notice. AC#2: same acceptance script -> PASS, unkeyed conflict, keyed duplicate identity, cancel, every specified invalid form exit 2, needs_approval and paused exit 1. AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> PASS; real-store/injected-clock occurrences, distinct event files/data/identity, exclusive expiry/deletion, shared overlap with config, missed policies, permanent lifecycle deletion, persisted phase and no restart duplicate. AC#4: task cli:test PKG=./internal/store -- -count=1 and task cli:test PKG=./internal/mcp -- -count=1 -> PASS; schema v12 migration, ten competing writers, key duplicate, MCP client identity, conflicting timing and job cancellation. AC#5: task ci -> PASS after review fixes, including unit/integration/race, vet/Staticcheck, vulnerability/secret scans and smoke; docs/help describe implemented authority and behavior. Focused CLI regression checks passed; LSP diagnostics clean. Worktree creation receipt: checkout /Users/brett/dev/me/noon/.worktrees/feat-noon-69-temporary-schedules; branch feat/noon-69-temporary-schedules; creation SHA 71e81db96e77c6b1c5d1f7f1640eb777df399f87; owner session 01a1619e-7b71-7735-9cbc-f05fd8c52fa3; loop fc6f397e-090b-47d1-9f96-11d9e27ee2e1. Verified clean and integrated using wt list and Git-local agent-creation.json; associated Herdr workspace wHW has only its idle zsh pane wHW:p1. Next step: remove this verified owned worktree through Worktrunk, finalize task bookkeeping, and run task ci on final main commit. No remaining implementation blocker.

Cleanup complete: wt remove feat/noon-69-temporary-schedules --foreground --format=json --yes returned branch_outcome deleted; git worktree list now shows only primary main. Verified associated Herdr workspace wHW disappeared. No retained task worktrees/workspaces or running children. All acceptance and delivery obligations are satisfied; final bookkeeping commit is gated by task ci immediately after commit. No push or PR requested or performed.

AC#1: task cli:build && bun /tmp/noon-69-acceptance.mjs -> PASS (creation, rounded end, NEXT, list and dashboard).
AC#2: bun /tmp/noon-69-acceptance.mjs -> PASS (duplicate/key/cancel, invalid inputs, approval and pause errors).
AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> PASS (real-store injected-clock temporary scheduling regressions).
AC#4: task cli:test PKG=./internal/store -- -count=1; task cli:test PKG=./internal/mcp -- -count=1 -> both PASS (migration/concurrency and MCP lifecycle).
AC#5: task ci -> PASS after fixes (docs/help checked against implemented behavior; full compiler, test, race, security and smoke gate).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented bounded temporary schedules for approved existing jobs across CLI and MCP, schema v13 persistence, shared planner policies, lifecycle deletion, NEXT/dashboard visibility, cancellation, docs and skill. Commit 761349c fast-forward integrated into main. Acceptance checks and full CI/race passed; one independent review completed and both concrete findings fixed with regressions. Owned worktree, branch and Herdr workspace removed; no remaining blocker or resumable work.
<!-- SECTION:FINAL_SUMMARY:END -->
