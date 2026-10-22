---
id: NOON-68
title: End jobs at a configured until time
status: To Do
assignee: []
created_date: '2026-10-22 02:43'
updated_date: '2026-10-22 02:55'
labels:
  - scheduler
  - config
dependencies: []
documentation:
  - docs/design.md
  - docs/configuration.md
  - docs/cli.md
  - docs/agents.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/config/edit.go
  - internal/scheduler/plan.go
  - internal/scheduler/plan_test.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/store/queue.go
  - internal/app/queue.go
  - internal/app/inspect.go
  - internal/app/scheduler.go
  - internal/cli/root.go
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/queue_test.go
  - internal/cli/jobs.go
  - internal/mcp/server.go
  - internal/httpapi/api.go
  - internal/httpapi/webhooks.go
  - internal/tui/view.go
  - noon.schema.json
  - examples/jobs.yaml
  - docs/configuration.md
  - docs/cli.md
  - docs/design.md
  - docs/agents.md
priority: medium
type: feature
ordinal: 24300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: a recurring job cannot end on its own. A script guard with `noop_exit_code` still fires every occurrence, and hooks act only after a run finishes, so only the scheduler can stop planning future occurrences. Promoted at the user request, with NOON-69 (temporary schedules for agents) depending on it. Prior art: systemd and launchd have no end date; Kubernetes CronJob uses suspend; Claude Code routines expire.

Outcome: a job may declare when it stops running unattended:

```yaml
jobs:
  watch-ci:
    schedule: every 10m
    until: 2026-10-20 17:00
    agent: claude
    prompt: Check CI and fix failures.
```

```console
$ noon add watch-ci "every 10m" --for 2h --agent claude --prompt "Check CI and fix failures"
$ noon list
JOB       SCHEDULE                          NEXT
watch-ci  every 10m until 2026-10-20 17:00  Mon 15:10 (in 9m)
freeze    weekdays at 09:00 until 2026-10-01 09:00  ended
```

Settled contract:
- `until` is an optional job field: `YYYY-MM-DD HH:MM` in the project timezone or RFC 3339. `HH:MM` and durations are config errors: config never stores a time relative to now, because it has no anchor and approval or edits would silently restart the clock. A repeated local time resolves to its first occurrence and a skipped local time to the first instant after the gap (decision-5 wall-clock rules).
- Allowed on any job, scheduled or manual. A past `until` is valid config, not an error, so `config check` never changes verdict with the clock; the job is simply ended.
- Ended means now >= `until`. An ended job is inactive like a disabled one: the planner plans nothing and drops its schedule state, so there is no catch-up for occurrences missed before the end. A scheduled occurrence runs only when its effective start (after splay and jitter) is before `until`.
- Queued requests (`--detach`, `--in`, `--at`, MCP, API, webhooks, triggers, shell entry) are refused with code `ended` when the job has ended or the requested due time is at or after `until`. Message `job "NAME" ended at TIME` or `job "NAME" ends at TIME`, hint naming the source file. Exit 1 (operational), HTTP 409 like `disabled`.
- Already queued runs are cancelled once `until` passes, through the existing inactive-job dispatch rule. A run already in progress finishes, bounded by `timeout`.
- Foreground `noon run NAME` still works after the end, matching paused jobs.
- The definition stays; the scheduler never edits config files. `noon remove` cleans up.
- `until` is an execution field in the approval digest; changing it re-requires approval for shared jobs. `noon enable` approves ended jobs like any other.
- `noon add` gains mutually exclusive `--for DURATION` (`schedule.ParseDuration`, at least 1m) and `--until TIME` (the `--at` forms: `HH:MM` as its next occurrence, `YYYY-MM-DD HH:MM`, RFC 3339). Both resolve at write time; `--for` rounds up to the next whole minute. `noon add` always writes RFC 3339 with the project-timezone offset, which is unambiguous across DST. A resolved time not after now is a usage error.
- Display: `noon list` appends ` until YYYY-MM-DD HH:MM` (project timezone) to the SCHEDULE cell after any splay and jitter suffix, and shows NEXT `ended` after the existing other platform, paused, needs approval, and disabled states. When no occurrence starts before `until`, NEXT is `-`. `list --json` and MCP `list_jobs` add `until` (RFC 3339 in the project timezone, omitted when unset) and `ended` (bool), and omit `next_at` when the next occurrence is at or after `until`. The TUI job status shows `ended`.

Scope: config parsing and validation, digest, `noon add` flags and edit encoding, planner and scheduler job inputs, queue eligibility and error mapping (CLI, MCP, HTTP API, webhooks), inspection and next time, list text and JSON, TUI status, schema, examples/jobs.yaml (uses every field), docs: configuration.md (field table, a temporary-job section), cli.md (add flags, queue error codes, HTTP 409 list), design.md (job semantics, approval digest fields, queued-run rules), agents.md (ended jobs refuse run_job).

Tests (critical only): `until` parsing including DST repeated and skipped local times (parsing); planner plans occurrences strictly before `until`, none at or after, and no catch-up after the end, while a run in progress is unaffected (state transition); queue refuses after the end and for due times at or after `until` (authorization boundary); `until` changes the digest (authorization boundary). Extend existing internal/config, internal/scheduler, and internal/cli tests.

Non-goals: run counts (`max_runs`); auto-deleting ended jobs or any scheduler writes to config; a pause that resumes later; `until` in crontab import; a user-level default; temporary schedules in state (NOON-69); time windows (DRAFT-10).

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml sets `timezone: America/Denver` and defines `future` (schedule `every 1h`, until `2099-01-01 00:00`, run ["true"]) and `past` (schedule `every 1h`, until `2000-01-01 00:00`, run ["true"]): `bin/noon list --json` reports `future` with `until` "2099-01-01T00:00:00-07:00", `ended` false, and a `next_at`; and `past` with `ended` true and no `next_at`. `bin/noon list` shows the `future` SCHEDULE cell ending in `until 2099-01-01 00:00` and NEXT `ended` for `past`.
- [ ] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for each of `until: tomorrow`, `until: "17:00"`, `until: 2h`, `until: 2026-13-01 00:00`, and `until: [x]`, and exits 0 for both valid values above. After `bin/noon enable`, changing only the `until` of `future` makes `bin/noon list --json` report it `needs_approval`, while editing its description does not.
- [ ] #3 In the same environment after `bin/noon enable`: `bin/noon run past --detach --json` exits 1 with error code `ended`; `bin/noon run future --at "2099-01-01 00:00" --json` exits 1 with code `ended`; `bin/noon run future --detach --json` exits 0; foreground `bin/noon run past` exits 0 and records a succeeded run.
- [ ] #4 In the same environment, `bin/noon add tmp "every 1h" --for 2h -- true` exits 0 and writes `until` to .git/noon.yaml as an RFC 3339 time with a -06:00 or -07:00 offset, at a whole minute, no earlier than 2h and less than 2h1m after the command started; `bin/noon add tmp2 "every 1h" --until "2000-01-01 00:00" -- true`, `--for 30s`, and `--for 2h --until 17:00` each exit 2 and write nothing.
- [ ] #5 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: an hourly job with `until` runs each occurrence before `until` and none at or after it; a scheduler offline across `until` records no catch-up afterwards; a queued due run is cancelled once `until` passes while a run in progress finishes normally; persisted schedule state of jobs without `until` is unchanged.
- [ ] #6 `task cli:test PKG=./internal/config -- -count=1` exits 0 and covers `until` parsing for both forms, DST repeated and skipped local times, rejected forms, and digest coverage. noon.schema.json accepts a string `until` and rejects a non-string; examples/jobs.yaml uses `until` and `task cli:test PKG=./integration -- -run ^TestExamplesValidate$ -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
