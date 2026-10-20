---
id: NOON-62
title: Spread scheduled starts with a stable per-job offset
status: To Do
assignee: []
created_date: '2026-10-18 16:33'
updated_date: '2026-10-20 19:18'
labels:
  - scheduler
  - schedule
  - agents
dependencies: []
documentation:
  - docs/design.md
  - docs/configuration.md
  - docs/agents.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/schedule/next.go
  - internal/schedule/schedule_test.go
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/config/edit.go
  - internal/scheduler/plan.go
  - internal/scheduler/plan_test.go
  - internal/scheduler/serve.go
  - internal/app/inspect.go
  - internal/app/scheduler.go
  - internal/cli/jobs.go
  - internal/cli/manage.go
  - noon.schema.json
  - examples/jobs.yaml
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
priority: medium
type: feature
ordinal: 21100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: humans pick round times (`weekdays at 9am`, `0 * * * *`), so prompt jobs across projects become due on the same second. The scheduler serializes work beyond one run per project and four overall, but the first four still start together, and a burst against one agent provider can trip rate limits or usage windows. Promoted at the user's request, ahead of observed rate-limit evidence. Prior art: Claude Desktop scheduled tasks add a deterministic few-minute delay per task; systemd timers offer RandomizedDelaySec with FixedRandomDelay. Random per-run jitter for organic-looking starts is NOON-63, which builds on this task.

Outcome: a job may declare a stable start offset:

```yaml
jobs:
  triage:
    schedule: weekdays at 9am
    splay: 10m
    prompt: Triage new issues.
```

```console
$ noon list
JOB     SCHEDULE                    NEXT
triage  weekdays at 09:00 +4m12s    Mon 09:04 (in 2d)
```

Settled contract:
- `splay` is an optional duration in `schedule.ParseDuration` form, at least `1s` and at most `1h`, normalized like `timeout` and `cooldown`. It requires a cron, macro, or calendar `schedule`; on a manual job or an interval schedule (`every 6h`) it is a config error with file, line, and YAML path. Intervals already keep their own phase.
- The job's offset is whole seconds in [0, splay), derived only from the canonical project path Noon stores for the project, the job name, and the splay: the first 8 bytes of SHA-256(project path, NUL, job name), big-endian, modulo the splay in seconds. It never reads the clock or randomness, so it is identical across restarts, hosts sharing the path, `list`, and the scheduler. Moving the checkout or renaming the job changes it.
- Every occurrence is shifted by the same offset: the job's effective schedule is the nominal schedule plus the offset, computed as `Next(after - offset) + offset` after wall-clock resolution, so the offset is elapsed time and existing DST rules apply to the nominal time. One shared helper computes this for the scheduler, inspection, and `noon add` output.
- Persisted schedule state stores the nominal next occurrence; the planner and displays derive the effective time from it (NOON-63 needs the nominal time, which a per-run offset makes unrecoverable from the effective one). The state key gains the offset only when `splay` is set, so jobs without it keep their existing state across upgrade; changing the splay, project path, or job name resets state and starts from now like a schedule change.
- The 60s on-time window, missed-run handling, overlap, and catch-up apply to the effective time. A scheduled run's `due_at` is its effective time (or now for `catch_up`); its `scheduled_at` and `NOON_SCHEDULED_AT` remain the nominal occurrence, so scripts and history still identify "the 09:00 run".
- `splay` is an execution field in the approval digest; changing it requires re-approval for shared jobs.
- Only schedule occurrences are offset. `noon run`, `--detach`, `--in`, `--at`, MCP, triggers, shell `--on-enter`, hooks, and webhooks are unchanged.
- Display: `next_at` everywhere (`list`, `list --json`, MCP `list_jobs`, dashboard, TUI) includes the offset. `noon list` appends ` +<offset>` (Go duration form, e.g. `+4m12s`, `+0s`) to the SCHEDULE cell. `list --json` and MCP add `splay` and `splay_offset` as normalized duration strings, omitted when unset.

Scope: parsing and validation, digest, offset and shifted-next helper, planner and persisted state key, scheduler job inputs, inspection fallback and persisted next time, `noon add --json` next_at, list text and JSON, schema, examples/jobs.yaml (uses every field), docs: configuration.md (field table; note that splay shifts working-hours window edges too), design.md (schedules, missed runs, approval digest fields), agents.md (`NOON_SCHEDULED_AT` stays nominal, so lateness checks must allow for splay).

Tests (critical only): offset is stable and always within [0, splay) for representative paths, names, and splays (a modulo or seeding bug would fire jobs outside the window or move them on restart); planner fires at the effective time, records nominal `scheduled_at`, catches up once after a missed window, and leaves state keys of jobs without splay unchanged (state transition, upgrade regression); splay changes the digest (authorization boundary); validation rejections (parsing). Extend existing internal/schedule, internal/scheduler, and internal/config tests.

Non-goals: random or per-run jitter (NOON-63); offsets before the nominal time; splay for interval or manual schedules; a user-level default; load-aware scheduling, rate-limit detection, or retries; `noon add`/`noon import` flags for splay; showing the offset in TUI or dashboard schedule cells beyond NEXT.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml defines `splayed` (schedule `every day at 9am`, splay `10m`, run [true]) and `plain` (same schedule, no splay): two consecutive `bin/noon list --json` calls report identical `splayed` `splay` "10m" and `splay_offset` in [0s, 10m), and its `next_at` local clock time equals 09:00:00 plus that offset; `plain` omits both fields and its `next_at` is 09:00:00; `bin/noon list` shows the `splayed` SCHEDULE cell ending in `+<splay_offset>`.
- [ ] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for each of: `splay: 10m` on a job without `schedule`, `splay: 10m` with `schedule: every 6h`, `splay: 0s`, `splay: 2h`, `splay: 10` (no unit), `splay: [10m]`. After `bin/noon enable`, changing only `splayed`'s splay makes `bin/noon list --json` report it `needs_approval`, while editing its description does not.
- [ ] #3 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: a splayed job is not due before nominal+offset and fires then with `scheduled_at` nominal and `due_at` nominal+offset; after a missed window it records one `catch_up` with the first missed nominal `scheduled_at`; the persisted state key of a job without splay is unchanged from the pre-task format.
- [ ] #4 `task cli:test PKG=./internal/schedule -- -count=1` and `task cli:test PKG=./internal/config -- -count=1` exit 0 and cover offset stability and range [0, splay) across representative paths, names, and splays, splay validation, and digest coverage of splay.
- [ ] #5 noon.schema.json accepts a valid `splay` duration string and rejects a non-string; examples/jobs.yaml uses `splay` and `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
