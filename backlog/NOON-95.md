---
id: NOON-95
title: Preview upcoming run times with noon next
status: done
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
updated: 2026-11-17T19:25:33Z
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

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo whose noon.yaml sets `timezone: America/Denver` and defines `tests` (schedule `every day at 2am`, run ["true"]) and `sync` (schedule `every 1h`, run ["true"]), after `bin/noon enable`: `bin/noon next --json` exits 0 with `schema_version` 1 and 10 occurrences in non-decreasing time order; after a 4s `bin/noon serve` persists schedule state, the first `tests` and first `sync` occurrences of `bin/noon next tests sync --json` equal their `next_at` in `bin/noon list --json`.
  Evidence: AC#1: /tmp/noon95-ac/ac.sh + ac1.sh on 6bb20f4: next --json exit 0, schema_version 1, 10 occurrences sorted; after 4s serve, next tests sync: tests 2026-11-18T02:00:00-07:00 = next_at, sync 2026-11-17T13:11:21.897-07:00 = next_at.
- [x] In the same environment, `bin/noon next tests -n 3 --json` exits 0 with three `tests` occurrences exactly one day apart at 02:00 local; `bin/noon next nope` exits 2; after `bin/noon pause sync`, `bin/noon next --json` contains no `sync` occurrence.
  Evidence: AC#2: /tmp/noon95-ac/ac.sh on 6bb20f4: next tests -n 3 --json exit 0 -> 2026-11-18/19/20T02:00:00-07:00; next nope exit 2; after pause sync, next --json has 0 sync occurrences.
- [x] In an empty temp directory, `bin/noon next --schedule "weekdays at 9am" -n 5 --json` exits 0 with five occurrences, all 09:00 on Monday–Friday; `bin/noon next --schedule "every blursday"` exits 2 with the same schedule error and hint `bin/noon add x "every blursday" -- true` prints inside a project; neither command creates a state database.
  Evidence: AC#3: ac.sh in empty dir: weekdays preview exit 0 -> 11-18,19,20,23,24 at 09:00 (Wed-Fri, Mon-Tue); blursday exit 2 with 'invalid duration "blursday": use s, m, h, or d units' and 'Hint: Try every day at 9am', identical to noon add except the --help command name; a fresh env -i HOME run created no files.
- [x] `task cli:test PKG=./internal/cli -- -count=1` exits 0 and covers: for a job with splay and a pending pause window, the first occurrence equals `next_at` and no occurrence falls inside the window; a `--schedule` preview across a DST change in America/Denver follows decision-5 rules.
  Evidence: AC#4: task cli:test PKG=./internal/cli -- -count=1 ok. TestNextOccurrences covers splay + pending pause (first = next_at, none inside window; fails when pause skipping is disabled) and America/Denver DST gap/repeat previews.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on final merged main commit a6815cc.
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC#1-4 evidence recorded under each criterion with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Diff within contract except internal/app/scheduler.go, justified in Notes.
- [x] No test was deleted, skipped, or weakened
  Evidence: Only TestNextOccurrences added; no test changed or removed.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: README table, docs/cli.md (Next run times, short flags), and next --help describe implemented behavior; design.md unaffected.

## Notes

Plan: implement noon next per contract in worktree .worktrees/feat-noon-95-next (branch feat/noon-95-next, created by session d7520777 via wt switch), merge to main, clean up.

Implemented on branch feat/noon-95-next commit 6bb20f4 (rebased on main 2d3f9c9); task ci exit 0. Contract deviation: internal/app/scheduler.go (4 lines) records the persisted nominal behind next_at so noon next chains later occurrences from the scheduler's own phase. internal/skill/SKILL.md unchanged: it lists task recipes, not commands. Temporary schedules are excluded per non-goals. AC1/AC3 as written fail independently of the code: AC1's default -n 10 merges hourly sync so the 02:00 tests run is absent before ~16:00 local, and an unpersisted interval is relative to now (ms drift between next and list); with a 4s serve persisting phase, next tests sync first occurrences equal list next_at exactly. AC3's 'every weekday at 9am' is rejected by the existing grammar (try weekdays at 9am); 'weekdays at 9am' passes all AC3 checks. Awaiting user decision on rewording.

User approved (2026-11-17) rewording AC1 to compare next tests sync after a 4s serve, and AC3 to use 'weekdays at 9am'.

## Summary

Added noon next [NAME...] [-n N] [--schedule EXPR]: app.Upcoming chains each job's occurrences from the nominal behind next_at (splay, jitter, pending pause window, until); app.PreviewSchedule previews an expression without config. Merged to main as a6815cc; task ci passes; TestNextOccurrences guards pause skipping and DST rules. AC1/AC3 reworded with user approval.
