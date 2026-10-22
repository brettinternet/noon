---
id: NOON-70
title: Create temporary jobs kept in Noon state from the CLI
status: Done
assignee: []
created_date: '2026-10-22 21:28'
updated_date: '2026-10-22 22:56'
labels:
  - scheduler
  - cli
  - store
  - tui
  - mcp
dependencies: []
documentation:
  - docs/design.md
  - docs/cli.md
  - docs/configuration.md
  - docs/agents.md
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
  - internal/store/cancel.go
  - internal/config/jobs.go
  - internal/app/manage.go
  - internal/app/inspect.go
  - internal/app/queue.go
  - internal/app/run.go
  - internal/app/history.go
  - internal/app/dashboard.go
  - internal/app/scheduler.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/cli/root.go
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/jobs.go
  - internal/cli/cancel.go
  - internal/cli/queue.go
  - internal/cli/dashboard.go
  - internal/tui/view.go
  - internal/tui/tui_test.go
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - internal/skill/SKILL.md
  - README.md
  - docs/cli.md
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
priority: medium
type: feature
ordinal: 25400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: agents are often asked to follow up on something on a schedule for a few days, such as checking a PR every two hours until Friday. Today every job definition must live in a config file. That fails when the only private config is a shared template symlinked into `.git/noon.yaml` (Noon now refuses to edit symlinked config), and expired definitions linger in YAML that someone maintains. Temporary schedules (NOON-69) only re-queue jobs that already exist. A temporary job is a complete, short-lived job definition that Noon keeps in its state database, runs like a private job, ends on its own, and cleans up after a short grace period.

Outcome:

```console
$ noon add watch-pr-42 "every 2h" --temporary --for 3d --agent claude --prompt "Check PR 42 CI; fix failures"
Added temporary job watch-pr-42 (every 2h until Fri 17:00) · next Tue 15:00 (in 1h)
$ noon list
JOB          SCHEDULE                                      NEXT                LAST   COMMAND
tests        weekdays at 09:00                             Wed 09:00 (in 18h)  ok     go test ./...
watch-pr-42  every 2h · temporary until 2026-10-23 17:00   Tue 15:00 (in 1h)   never  claude: Check PR 42 CI; fix failures
old-watch    every 1h · temporary, ended 2026-10-20 12:00  ended               ok     sh: ./check
$ noon cancel watch-pr-42   # end it now; it stays visible for 24h
$ noon remove watch-pr-42   # delete it now; run history stays
```

Settled contract:
- Creation is CLI only: `noon add NAME [SCHEDULE] --temporary` with the existing `noon add` action flags (argv after `--`, `--sh`, `--agent`, `--prompt`, `--prompt-file`, `--description`, `--timeout`) plus `--in`/`--at` to queue a first run and a new `--key K` (only valid with `--temporary`). `--user` targets the user project. It writes no config file, so it works in a checkout with no config, only shared `noon.yaml`, or a symlinked `.git/noon.yaml`.
- An end is required: exactly one of `--for` or `--until`, resolved by the existing `noon add` rules (NOON-68), after now and at most 7 days away including minute rounding (the same limit as temporary schedules; reuse that constant). At least one of a schedule, `--in`, or `--at` is required, and some occurrence or the queued run must fall before the end. Violations, `--temporary` with `--shared`, and `--key` without `--temporary` are usage errors (exit 2). `--config` files are refused as `noon add` already refuses them.
- Names: creation fails with code `exists` (exit 1) if any config file of the project defines the name, or an active temporary job has it with a different or missing key. The same nonempty key returns the existing job with `created` false and `reason` `duplicate`. An ended temporary job still in its grace period is replaced. `noon add NAME` without `--temporary` fails with `exists` while a temporary job of that name is retained (hint `noon remove NAME`). If a hand edit later adds a config job with the same name, the config job wins and the temporary job is deleted.
- Trust: the user created it through their own shell, so it is private: `shared` false, approval `not_required`. Creating one enables the project, as a scheduled `noon add` does. requested_by is `cli`.
- Storage: a new `temporary_jobs` table added by an append-only migration (do not overload `temporary_schedules`): project, name (unique per project), the one-job definition in the config job schema, digest, until, created_at, requested_by, optional key, and ended_at. The definition is turned into a job through config's existing job parser (`config.JobFromSource`), so validation, defaults, project hook defaults, and digests match an equivalent private job. One function in internal/app merges temporary jobs into a project's job set, and every path that resolves jobs uses it: list, status and dashboard, run, queue, cancel, remove, logs and history name resolution, MCP, TUI, and the scheduler's project reload. The scheduler must see created or removed temporary jobs within its 1-second state poll even when no config file changed.
- Running: identical to a private job with the given `until` (NOON-68 semantics). Starts happen only before the end; at the end, queued runs are cancelled and running work finishes, bounded by its timeout. Pause and resume work as for other jobs and never extend the end. Project disable stops runs while the end keeps counting. `noon run NAME --schedule` on a temporary job is a usage error (it already has a schedule). Foreground `noon run NAME` works while the job is retained. Queued requests after the end fail with code `ended`.
- Ending and grace period: a temporary job ends when its `until` passes or `noon cancel NAME` / MCP `cancel_run` with `job` runs. Cancelling also cancels active runs and records ended_at as now. An ended definition stays visible for 24 hours after it ends (a fixed constant, not configurable), then is deleted. Reads exclude rows past that time even while the scheduler is offline; the scheduler deletes them. `noon remove NAME` deletes it at once. Runs and logs follow normal per-job retention and stay readable through `noon history NAME` and `noon logs NAME` after the definition is gone.
- Visibility: list JSON and MCP `list_jobs` report temporary jobs with `source` `"temporary"`, the existing `until` and `ended` fields, and a new `temporary` object `{created_at, requested_by, key?, ended_at?, removes_at}`; ended_at is set once ended, and removes_at is the end (ended_at, else until) plus 24h. Human `noon list` appends `· temporary until YYYY-MM-DD HH:MM` to SCHEDULE while active and `· temporary, ended YYYY-MM-DD HH:MM` after, with NEXT `ended`. Bare `noon` and MCP `status` add one notice listing active temporary jobs, matching the temporary schedule notice. In the TUI, the SCHEDULE cell marks the job temporary, ended temporary rows render entirely faint with NEXT `ended`, and the selected job's detail rows show when it ends or ended and when it will be removed.
- Creation JSON: `{"schema_version":1,"job":{...list job object...},"created":bool,"reason":"duplicate"?,"scheduler":"running"|"offline"}`, with the same offline notice as queued runs.
- The symlinked-config refusal hint from `noon add` mentions `--temporary`.
- Docs: configuration.md "Temporary jobs" (about config `until`) is renamed "Ending jobs" and cli.md "Adding temporary jobs" is renamed to match. Update every link to the old anchors. "Temporary jobs" then names only this feature.

Scope: migration and store operations, the app merge function and creation/cancel/remove operations, scheduler reload and purge, CLI add/list/cancel/run/dashboard, TUI cells and detail rows, MCP list/status/cancel visibility, skill text telling shell agents to use `noon add --temporary` for bounded follow-ups, docs: README command table, cli.md, configuration.md, design.md (temporary jobs contract, state tables, trust table row stating definitions normally live in config and this is the only state-backed exception), agents.md (visibility, cancel, CLI-only creation).

Tests (critical only): creation rejects a missing or over-7-day end, conflicting flags, and name collisions, and keyed duplicates return the existing job (parsing, data integrity); no config file is written or replaced, including a symlinked `.git/noon.yaml` (data integrity regression); a temporary job runs at its occurrences, stops at its end, cancels queued work, stays visible as ended for 24h, then disappears while its runs remain in history (state transition); a hand-added config job with the same name shadows and deletes it (subtle regression); 10 concurrent creations of one name with distinct keys leave exactly one row (data integrity); migration of an existing database. Extend existing internal/store, internal/scheduler, internal/cli, internal/mcp, and internal/tui tests.

Non-goals: creating temporary jobs over MCP or the HTTP API (a separate authority decision); editing a temporary job in place (remove and add again); lifetimes over 7 days (use config `until`); fields `noon add` cannot express (webhooks, triggers, env, os/arch, per-job hooks); promoting a temporary job into config; configurable grace period; temporary schedules attached to temporary jobs; keeping definitions after the grace period.

Dependencies: none (builds on completed NOON-68 and NOON-69).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d`, and a temp Git repo whose `.git/noon.yaml` is a symlink to a file outside the repo defining manual job `ping` (run ["true"]): `bin/noon add watch "every 10m" --temporary --for 2h --sh true --key k1 --json` exits 0 with `created` true; the symlink, its target bytes, and the absence of `noon.yaml` are unchanged; repeating the command exits 0 with `created` false and `reason` "duplicate"; `bin/noon list --json` reports `watch` with `source` "temporary", `shared` false, `approval` "not_required", `until` 2h out rounded up to a whole minute, `ended` false, `next_at` following elapsed ten-minute interval phase (not wall-clock alignment, per decision-5), and `temporary.requested_by` "cli"; the project is enabled; human `bin/noon list` shows `temporary until` in the SCHEDULE of `watch`; bare `bin/noon` prints the temporary job notice.
- [x] #2 In the same environment each exits 2: `--temporary` with neither `--for` nor `--until`; `--for 8d`; `--for 1h --until 17:00`; `--temporary --shared`; `--temporary --for 1h` with no schedule, `--in`, or `--at`; `--key k` without `--temporary`; `bin/noon run watch --schedule "every 5m" --for 1h`. Each exits 1 with code `exists`: `bin/noon add ping "every 10m" --temporary --for 1h --sh true`; `bin/noon add watch "every 5m" --temporary --for 1h --sh true --key k2`; `bin/noon add watch --sh true` (config add).
- [x] #3 In the same environment: `bin/noon cancel watch` exits 0; `bin/noon list --json` then reports `watch` with `ended` true, `temporary.ended_at` set, and `temporary.removes_at` exactly 24h after it; human list shows NEXT `ended` and `temporary, ended` in SCHEDULE; `bin/noon run watch --detach` exits 1 with code `ended`; `bin/noon add watch "every 10m" --temporary --for 1h --sh true` exits 0 (replaces the ended job); `bin/noon remove watch` exits 0, after which list omits `watch`, `bin/noon history watch` exits 0, and the symlink target bytes are still unchanged.
- [x] #4 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time and a real store: a temporary job created while the scheduler runs is picked up without any config file change and runs at its occurrences with requested_by `cli`; no start at or after its end, and queued runs are cancelled at the end; the ended definition is still listed as ended until 24h after the end, then its row is gone while its runs remain in history; a hand-added config job with the same name replaces it and deletes the row; a scheduler restart keeps the job without a duplicate run.
- [x] #5 `task cli:test PKG=./internal/store -- -count=1` exits 0 and covers migration of an existing database, 10 concurrent creations of one name with distinct keys leaving exactly one row, and reads excluding rows past their removal time with no scheduler running. `task cli:test PKG=./internal/mcp -- -count=1` exits 0 and covers `list_jobs` returning a temporary job with its `temporary` object, `cancel_run` with `job` ending it, `run_job` with `schedule` refusing a temporary job, and no tool creating one. `task cli:test PKG=./internal/tui -- -count=1` exits 0 and covers an ended temporary job row rendering faint with NEXT `ended` and a temporary marker in SCHEDULE.
- [x] #6 Docs in scope describe only implemented behavior, including the renamed "Ending jobs" sections with no stale links to the old anchors, and the design.md trust table row. `task ci` exits 0.
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
1. Add temporary job storage and shared app resolution, creation, and lifecycle operations.
2. Integrate CLI, scheduler, MCP, TUI, and documentation with existing job semantics.
3. Verify all six acceptance criteria, run task ci, perform one focused independent review, and fix concrete defects.
4. Commit implementation, merge into main, finalize task evidence, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract expansion approved: internal/cli/run.go and internal/cli/history.go resolve foreground/queued run and logs targets directly; internal/tui/tui.go resolves queue/pause targets; internal/httpapi/api.go may resolve run targets directly. These existing resolution callsites must use the single app merge function so temporary jobs work consistently across all adapters, without adding remote creation authority.

AC3 compatibility decision: history watch must succeed after removal even when watch never ran, but existing history tests require never-seen names to remain not_found. Preserve that public behavior with minimal project/name-only evidence for former temporary jobs in a separate table in the append-only migration, recorded transactionally and consulted by history resolution. No job definitions remain after removal/grace. This storage extension avoids weakening existing tests, inventing run history, or accepting all typo names.

Modified-file contract expansion approved: internal/store/queue.go must enforce temporary-job existence and active deadline inside the queue transaction, not only against the app snapshot, so cancellation/removal racing a queue request cannot admit new work after the job ends or disappears.

Acceptance conflict found: AC1 requires every 10m next_at on a 10-minute wall boundary, while accepted decision-5 section 4 requires elapsed intervals to keep their starting phase and reserves clock alignment for cron. Existing schedule.Next and temporary schedules use elapsed phase. No alignment exception will be implemented without user authorization. Requested decision: correct AC1 to elapsed interval semantics (recommended), or explicitly authorize and document a temporary-job exception. Remaining item work continues without changing settled schedule behavior.

User resolved interval conflict in-session: preserve elapsed intervals (recommended). AC1 corrected to accepted decision-5 elapsed phase semantics; all other criteria unchanged.

Modified-file contract expansion approved: internal/app/shell.go prechecks entry-hook targets before shared queueing, internal/cli/shell.go supplies job-name completion, and internal/app/doctor.go inspects executable/prompt targets. Route these existing effective-job resolution paths through the shared loader so state-backed jobs are not silently omitted; config import/write validation remains config-only.

Default reload decision: retain config.JobFromSource semantics matching equivalent private jobs, including current project defaults and hooks. Transactional stale-row guards must use stored creation identity separately from effective execution digest, so changing default_agent does not silently disable temporary jobs and a replaced definition cannot receive stale queued work.

Parent verification: built binary acceptance script ruby /tmp/noon-70-acceptance.rb passed AC1-3; uncached scheduler/store/MCP/TUI package checks passed; task ci passed (initial 120s harness timeout, rerun with 300s completed). Single independent review found four concrete lifecycle concerns: stale cached-config shadow deletion; effective-vs-creation digest cancellation on shadow; cache/snapshot creation race cancelling first runs; missing retention after direct cancellation SQL. Bounded correction/validation underway; no second general review planned.

AC#1: task cli:build; ruby /tmp/noon-70-acceptance.rb -> PASS on committed implementation 3eaf743: isolated real Git symlink fixture, immutable target/config absence, create/keyed duplicate, private approval-free JSON metadata, rounded end, elapsed interval next_at, enabled project, human schedule marker and dashboard notice.
AC#2: ruby /tmp/noon-70-acceptance.rb -> PASS: all seven invalid flag/lifetime/schedule cases exit 2 with usage; all three config/active-temporary name collisions exit 1 with exists.
AC#3: ruby /tmp/noon-70-acceptance.rb -> PASS: cancel records end and exact 24h grace, human NEXT ended, queued run refuses ended, ended replacement succeeds, remove hides definition, never-run history succeeds, symlink unchanged.
AC#4: task cli:test PKG=./internal/scheduler -- -count=1 -> exit 0 on merged 3eaf743. TestServeTemporaryJobs covers state polling without config edits, occurrences/provenance, restart phase, end cancellation, grace/purge/history, config shadow deletion, defaults reload; additional regressions cover stale cached collisions and cache/snapshot interleavings.
AC#5: task cli:test PKG=./internal/store -- -count=1; task cli:test PKG=./internal/mcp -- -count=1; task cli:test PKG=./internal/tui -- -count=1 -> all exit 0 on merged 3eaf743. Covers migration/history preservation, ten competing creators, offline grace reads, identity races, normal cancellation retention, MCP visibility/cancel/creation authority and schedule refusal, faint ended TUI row with marker/deadlines.
AC#6: task ci -> exit 0 on committed implementation 3eaf743 (security, formatting, vet, Staticcheck, all tests, race, build, smoke). Verified scoped docs/help/skill and renamed Ending jobs links; design trust table states sole state-backed definition exception. Final bookkeeping-only commit will also run task ci.
Review: one independent general pass; all four confirmed findings fixed with targeted regression coverage (stale collision reload, changed-digest shadow cancellation, unseen live identity deferral, per-job run/payload retention). No further general pass. No test removed/skipped/weakened; modified tests additions only. All 38 implementation paths within original contract or prior approved notes. LSP diagnostics clean for app/manage.go and scheduler/serve.go; compiler/test gates passed.
Delivery: implementation commit 3eaf7436313d659237325fe5e43348b46af2e240 fast-forward merged to main. task check:staged passed before commit. Verified session receipt matched checkout path/branch and session 01a165a8-5542-70fd-80c9-785887156b50; checked exact Herdr workspace wKX (one shell pane), removed owned worktree and branch together with Worktrunk foreground removal, and verified workspace wKX disappeared and only main worktree remains. No retained task worktree/workspace; no push. Remaining blockers: none; next resumable step: none.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented CLI-only state-backed temporary jobs with bounded lifetime, keyed creation, approval-free private execution, cancellation/grace/purge, retained history, shared adapter visibility, scheduler reload and race-safe identity checks. All acceptance commands and task ci passed; four independent-review findings fixed with regressions. Implementation 3eaf743 merged into main; owned branch/worktree/Herdr workspace removed; no push.
<!-- SECTION:FINAL_SUMMARY:END -->
