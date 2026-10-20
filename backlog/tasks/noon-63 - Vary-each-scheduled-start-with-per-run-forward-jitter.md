---
id: NOON-63
title: Vary each scheduled start with per-run forward jitter
status: Done
assignee: []
created_date: '2026-10-20 19:15'
updated_date: '2026-10-20 20:01'
labels:
  - scheduler
  - schedule
dependencies:
  - NOON-62
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
  - internal/schedule/schedule.go
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
ordinal: 21200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: some jobs should not visibly fire at the same rigid time every run (exactly 09:00:00 every weekday). NOON-62 `splay` spreads jobs apart but pins each job to one fixed offset, so its starts still look mechanical. Promoted at the user's request. Prior art: systemd RandomizedDelaySec without FixedRandomDelay (redrawn per run, but not stable across restarts).

Outcome: a job may declare a forward jitter that varies on every scheduled run:

```yaml
jobs:
  digest:
    schedule: weekdays at 8:50am
    jitter: 20m       # starts from 08:50:00 to 09:09:59, different each run
    run: ./scripts/digest
```

```console
$ noon list
JOB     SCHEDULE                  NEXT
digest  weekdays at 08:50 ~20m    Mon 09:03 (in 2d)
```

Settled contract:
- `jitter` is an optional duration in `schedule.ParseDuration` form, at least `1s`, normalized like `splay`. It requires a `schedule` (config error on a manual job). It must not exceed half the schedule's shortest gap: for an interval, half the interval; for cron, macro, and calendar forms, half the smallest difference between consecutive times of day from the minute and hour fields, including the wrap from the last time of day to the first one the next day, ignoring day fields. A violation is a config error with file, line, YAML path, and the maximum allowed (for example `*/15 * * * *` allows at most `7m30s`).
- Each occurrence gets its own offset: whole seconds in [0, jitter) from the first 8 bytes of SHA-256(project path, NUL, job name, NUL, nominal occurrence Unix seconds in decimal), big-endian, modulo the jitter in seconds. The nominal occurrence is the instant `Next` resolves, so existing DST rules decide it: a repeated local time seeds once from its first occurrence, a skipped one from the instant after the gap. No clock reads, randomness, or stored offsets: restarts, `list`, and the scheduler always agree.
- Effective start = nominal + splay offset (NOON-62) + jitter offset. Forward only; the job never starts before the time the user wrote.
- Planner, using the persisted nominal next occurrence (NOON-62): a run is due when now reaches its effective start; the 60s on-time window and `missed` policy apply to the effective start. After an on-time run the next state is the following nominal occurrence; after a `catch_up` or `missed` record, and on first sight or reset, it is the first nominal occurrence whose effective start is after now (starting the search at `Next(now - splay offset - jitter)`). Intervals keep their nominal phase; jitter never accumulates into it. The half-gap bound keeps runs in order; where a DST spring-forward gap still reorders two occurrences, the later-due one is handled by the `missed` policy, never silently dropped or run twice.
- Persisted state key gains `~<jitter>` only when `jitter` is set, so other jobs keep their state; changing jitter resets state and starts from now.
- `scheduled_at` and `NOON_SCHEDULED_AT` stay nominal; `due_at` is the effective start (or now for `catch_up`).
- `jitter` is an execution field in the approval digest.
- Only schedule occurrences are jittered. `noon run`, `--detach`, `--in`, `--at`, MCP, triggers, shell `--on-enter`, hooks, and webhooks are unchanged.
- Display: `next_at` everywhere includes splay and jitter. `noon list` appends ` ~<jitter>` to the SCHEDULE cell after any `+<splay offset>`. `list --json` and MCP add `jitter` as a normalized duration string, omitted when unset.

Scope: parsing and validation including shortest-gap computation, digest, per-occurrence offset and the shared next-effective-start helper (generalizing NOON-62's), planner and persisted state key, scheduler job inputs, inspection fallback and persisted next time, `noon add --json` next_at, list text and JSON, schema, examples/jobs.yaml (uses every field), docs: configuration.md (field table, splay versus jitter, centering a window by writing an earlier time), design.md (schedules, missed runs, approval digest fields), agents.md (lateness checks against `NOON_SCHEDULED_AT` must allow for splay and jitter).

Tests (critical only): offsets are deterministic, within [0, jitter), and vary across occurrences (a seeding bug would pin or escape the window); shortest-gap bound including midnight wrap and intervals (parsing, the bound guarantees ordering); planner fires each jittered occurrence once at its effective start with nominal `scheduled_at`, keeps interval phase, catches up once after a missed window, and on a spring-forward fixture that reorders two occurrences records the late one under `missed` instead of dropping or doubling it (state transition, subtle regression); jitter changes the digest (authorization boundary). Extend existing internal/schedule, internal/scheduler, and internal/config tests.

Non-goals: offsets before the nominal time; non-uniform distributions; occasionally skipping runs or varying the day; true randomness or stored random state; jitter for manual or due-now requests; a user-level default; `noon add`/`noon import` flags for jitter.

Dependencies: NOON-62 (splay offset, shifted-next helper, persisted nominal state, display plumbing).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml defines `jittered` (schedule `every day at 9am`, jitter `20m`, run [true]) and `both` (same schedule, splay `10m`, jitter `20m`, run [true]): two consecutive `bin/noon list --json` calls report identical `next_at` values; `jittered` reports `jitter` "20m" and a `next_at` local clock time in [09:00:00, 09:20:00); `both` reports a `next_at` at or after 09:00:00 plus its `splay_offset` and before 09:00:00 plus `splay_offset` plus 20m; `bin/noon list` shows the `jittered` SCHEDULE cell ending in `~20m` and the `both` cell ending in `+<splay_offset> ~20m`.
- [x] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for each of: `jitter: 20m` on a job without `schedule`; `schedule: "*/15 * * * *"` with `jitter: 10m` (error names the 7m30s maximum); `schedule: every 1h` with `jitter: 31m`; `schedule: "0 0,23 * * *"` with `jitter: 31m` (midnight wrap gap is 1h); `jitter: 0s`; `jitter: 20` (no unit); `jitter: [20m]`. It exits 0 for `schedule: every 1h` with `jitter: 30m` and `schedule: "59 1,2 * * *"` with `jitter: 30m`. After `bin/noon enable`, changing only `jittered`'s jitter makes `bin/noon list --json` report it `needs_approval`, while editing its description does not.
- [x] #3 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: a jittered daily job is not due before nominal plus its offset and fires once then with nominal `scheduled_at`; consecutive occurrences fire once each at differing offsets; a jittered interval job keeps its nominal phase across several runs; after a missed window it records one `catch_up` with the first missed nominal `scheduled_at`; in a spring-forward fixture where `59 1,2 * * *` with `jitter: 30m` reorders two occurrences, each occurrence either runs once or is recorded under the `missed` policy, with none dropped or run twice; persisted state keys of jobs without jitter are unchanged.
- [x] #4 `task cli:test PKG=./internal/schedule -- -count=1` and `task cli:test PKG=./internal/config -- -count=1` exit 0 and cover per-occurrence offset determinism, range [0, jitter), and variation across occurrences; shortest-gap computation for cron (including midnight wrap), macros, calendar phrases, and intervals; jitter validation; and digest coverage of jitter.
- [x] #5 noon.schema.json accepts a valid `jitter` duration string and rejects a non-string; examples/jobs.yaml uses `jitter` and `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
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
Claimed with Worklease claim 98f9a58f4670adf913fbdbc3b576a434, session 01a15ad5-75f4-73da-8873-02e1f0977057. Implementation worktree: /Users/brett/dev/me/noon/.worktrees/noon-63-jitter, branch noon-63-jitter, base 975e015. Plan: implement contract and critical regressions, execute AC1–5 plus full CI, one independent scoped review, commit and merge locally, mark Done and remove owned worktree/release claim. No push requested.

Acceptance fixture correction: run: [true] is rejected by the existing strict argv parser because YAML true is boolean. AC1/2 execution uses run: ["true"] to invoke the intended executable; all jitter assertions and required commands are unchanged. AC#1: task cli:build; bun /tmp/noon-63-ac.AhwGTB/acceptance.ts -> PASS, isolated HOME/XDG_CONFIG_HOME/XDG_STATE_HOME/NOON_STATE_DIR and Git repo; identical repeated next_at, both bounds and list suffixes verified. AC#2: same acceptance script -> PASS, all seven invalid cases exit 1 with file/line/jobs.test.jitter, required maximum messages; both boundary-valid cases exit 0; description preserves approval and jitter change requires approval. AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> PASS, deterministic firing, interval phase, catch-up, DST reorder and state-key regressions. AC#4: task cli:test PKG=./internal/schedule -- -count=1 and task cli:test PKG=./internal/config -- -count=1 -> PASS, offsets, shortest gaps, validation and digest coverage. AC#5: mise exec go -- go run /tmp/noon-63-ac.AhwGTB/schema.go -> PASS, schema accepts 20m string and rejects numeric/sequence jitter; task cli:test PKG=./integration -- -run ^TestExamplesValidate$ -count=1 -> PASS; task ci -> PASS (security, vet/staticcheck, all tests, race, smoke). All 18 modified source/docs/test paths are in contract. No tests removed, skipped, or weakened. Independent review and final-commit CI remain before completion.

Final delivery: independent reviewer found no validated defects after tracing nominal/effective times, interval phase, DST reordering, persistence, validation and approval digest coverage. Implementation commit 0a924107b2228b5aa1b6b028f2b696948f51ad64 was fast-forward merged into main. task check:staged passed before commit; task ci passed again on that final implementation commit in main. Worktrunk removed the verified session-owned noon-63-jitter worktree and branch; Herdr workspace wJ8 is confirmed absent. No retained task worktree/workspace, no remaining blocker or resumable implementation step. Temporary acceptance evidence remains at /tmp/noon-63-ac.AhwGTB. No push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented deterministic per-occurrence forward jitter with shortest-gap validation, nominal scheduling and effective due times, digest approval protection, inspection/schema/docs, and critical regression coverage. All acceptance checks and full CI pass; independent review found no defects. Merged locally as 0a92410; owned worktree, branch and workspace removed.
<!-- SECTION:FINAL_SUMMARY:END -->
