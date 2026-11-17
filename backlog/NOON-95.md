---
id: NOON-95
title: Preview upcoming run times with noon next
status: todo
type: feature
priority: medium
labels: [schedule, cli, delight]
refs:
  - docs/cli.md
  - docs/design.md
  - docs/decisions/decision-5 - Job-semantics-follow-cron-and-launchd-expectations.md
files:
  - internal/app/inspect.go
  - internal/cli/root.go
  - internal/cli/jobs.go
  - internal/cli/manage_test.go
  - README.md
  - docs/cli.md
  - internal/skill/SKILL.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T18:40:46Z
---

Why: a schedule is a promise about future times, and the only way to check it today is the single NEXT column in `noon list`. Users writing `every weekday at 9am`, a cron expression, or a job with `splay`/`jitter`, a pause window, or `until` cannot see the run after next, when two jobs will collide, or what a schedule string means before adding it. Previewing occurrences is the standard way schedulers build trust in a schedule, and noon already has a pure planner to compute them.

Outcome:

```console
$ noon next
Tue 2026-11-17 14:00  sync
Tue 2026-11-17 15:00  sync
Wed 2026-11-18 02:00  tests
$ noon next tests -n 3
Wed 2026-11-18 02:00  tests
Thu 2026-11-19 02:00  tests
Fri 2026-11-20 02:00  tests
$ noon next --schedule "every weekday at 9am" -n 2
Wed 2026-11-18 09:00
Thu 2026-11-19 09:00
```

Settled contract:
- `noon next [NAME...] [-n N] [--schedule EXPR]` is read-only and creates no state. `-n` is 1–100, default 10.
- Without names: the next N occurrences across all scheduled jobs, merged in time order (ties by job name). With names: the next N per named job, merged; an unknown or unscheduled name is a usage error (exit 2), and a missing target never means "all".
- Each job's first occurrence equals its `noon list --json` `next_at`. Later occurrences follow the same planner rules: `schedule.NextEffective` chained from the previous occurrence with the job's splay and jitter, occurrences inside a pending or active pause window skipped, nothing at or after `until`. Paused, ended, disabled, needs-approval, and platform-unsupported jobs contribute nothing, matching `next_at`.
- `--schedule EXPR` previews EXPR in the project timezone (or `--timezone`) without splay or jitter and excludes names. An invalid EXPR exits 2 with the same message and suggestions as `noon add`. It needs no config and works outside a project.
- Human output prints `Mon 2006-01-02 15:04` in the project timezone, then the job name. `--json` prints `{schema_version: 1, timezone, occurrences: [{job?, at}]}` with RFC 3339 times.
- README command table, docs/cli.md, and internal/skill/SKILL.md (if it lists commands) describe it.

Scope: an app function over the existing inspection and schedule planner, the CLI command, docs.

Tests (critical only):
- Nontrivial state: for a job with splay and a pending pause window, the first occurrence equals `next_at` and no listed occurrence falls inside the window; `--schedule` output for a DST-crossing cron in America/Denver follows decision-5 rules. Extend the list or manage tests.

Non-goals: temporary schedules and queued one-shots in the agenda; editing schedules; a calendar view in the TUI; MCP or HTTP endpoints.

Dependencies: none.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [ ] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo whose noon.yaml sets `timezone: America/Denver` and defines `tests` (schedule `every day at 2am`, run ["true"]) and `sync` (schedule `every 1h`, run ["true"]), after `bin/noon enable`: `bin/noon next --json` exits 0 with `schema_version` 1 and 10 occurrences in non-decreasing time order; the first `tests` and first `sync` occurrences equal their `next_at` in `bin/noon list --json`.
- [ ] In the same environment, `bin/noon next tests -n 3 --json` exits 0 with three `tests` occurrences exactly one day apart at 02:00 local; `bin/noon next nope` exits 2; after `bin/noon pause sync`, `bin/noon next --json` contains no `sync` occurrence.
- [ ] In an empty temp directory, `bin/noon next --schedule "every weekday at 9am" -n 5 --json` exits 0 with five occurrences, all 09:00 on Monday–Friday; `bin/noon next --schedule "every blursday"` exits 2 with the same schedule error and suggestions `bin/noon add x "every blursday" -- true` prints inside a project; neither command creates a state database.
- [ ] `task cli:test PKG=./internal/cli -- -count=1` exits 0 and covers: for a job with splay and a pending pause window, the first occurrence equals `next_at` and no occurrence falls inside the window; a `--schedule` preview across a DST change in America/Denver follows decision-5 rules.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
