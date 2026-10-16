---
id: NOON-4
title: Serve schedules for enabled projects
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 14:33'
labels:
  - scheduler
  - store
  - reviewed
dependencies:
  - NOON-18
  - NOON-19
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-1 -
    One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/scheduler/**
  - internal/store/**
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - integration/**
  - cmd/noon/main.go
  - README.md
  - docs/design.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `noon serve` runs every enabled project's schedules in the foreground until stopped. It survives restarts without duplicates or bursts and reloads configuration safely.

Read first: decision-1, docs/design.md#scheduler-lifetime, #missed-runs-overlap-and-concurrency, #state, and #execution.

Scope:
- Migration 3: `job_state(project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL, schedule_key TEXT NOT NULL, next_at TEXT NOT NULL, PRIMARY KEY(project_id, job))`; `scheduler(id INTEGER PRIMARY KEY CHECK (id = 1), pid INTEGER NOT NULL, start_identity TEXT NOT NULL, version TEXT NOT NULL, started_at TEXT NOT NULL, heartbeat_at TEXT NOT NULL)`; add `runs.due_at TEXT` and `projects.config_error TEXT`.
- `noon serve` takes no arguments. `--json`, `--config`, `--timezone`, `--project`, and their environment variables are usage errors, because each project uses its enable-time selection.
- Ownership: take a non-blocking exclusive `flock` on `STATE/scheduler.lock` (golang.org/x/sys/unix) and write the `scheduler` row. A second instance exits 1 with `scheduler_running`, naming the PID and start time. A `state_too_new` database exits 75.
- Startup: recover stale runs (store recovery from the record task), then loop.
- Each tick (1s): heartbeat; every 2s, stat each enabled project's candidate config paths (`noon.yaml`, `.noon.yaml`, and the user config file, or the recorded explicit file), including paths that do not exist yet. A file appearing or disappearing, or a change in mtime, size, or inode, triggers a validate-first reload. Then build `JobInput`s, call `Due` and `Dispatch` (planner task), apply their results in one transaction, and start the chosen runs.
- Starting a run sets `running` with the scheduler as owner and executes it in a goroutine with the same runner, log, and recovery identity as `noon run`. The environment is the scheduler's own plus job env and `NOON_*`, with `NOON_TRIGGER` and `NOON_SCHEDULED_AT` (RFC 3339 UTC).
- Reload: an invalid edit keeps the last valid jobs and sets `projects.config_error` to the `file:line` message. A valid edit swaps atomically and clears the error. A project that starts invalid has no jobs until fixed. A missing project directory is an error, not a crash. Active runs keep the snapshot they started with.
- Activation resets: in the same transaction, `noon enable` deletes the `job_state` rows of all the project's jobs when the project was not enabled, and of each job whose approval it creates or changes. An idempotent enable deletes nothing, so container restarts keep catch-up and interval phase.
- Shutdown on SIGINT/SIGTERM: stop dispatching; terminate running jobs (TERM, 10s, KILL) and record them `interrupted`; release the lock; exit 0. A second signal kills immediately.
- stderr log, one line per event with a local timestamp: start banner (state dir, project count), run started and finished, config errors, shutdown.
- Clients: bare `noon` and list show `scheduler running` when `heartbeat_at` is under 5s old, else `scheduler offline`. NEXT uses `job_state.next_at` when present, so interval phase is accurate. `noon enable` adds `Next: noon serve` while the scheduler is offline.
- Testability: the serve loop takes options for the clock (`Now`), the tick source (a channel the test drives), and kill grace. Tests step ticks deterministically; never sleep to wait for schedules.
- integration/ gains TestMain, which builds the binary once into a temp dir. Each test isolates HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR.

Non-goals: queued-run commands and cancel (next tasks), webhooks and `--listen`, service installation, hooks, an IPC socket, fsnotify, per-project concurrency settings.

Tests:
- internal/scheduler `TestServe` with injected clock and ticks: (a) a due job runs exactly once with trigger `schedule`; (b) after simulated downtime, one `catch_up`, not a burst; (c) an invalid config edit keeps the previous jobs and records `config_error`, and fixing it clears the error; (d) shutdown interrupts a running job, kills its group, and records `interrupted`; (e) a stale `running` row from a dead previous scheduler becomes `interrupted` at startup and is not re-run; (f) a project disabled while serving stops getting runs; (g) with the loop stopped, disabling and re-enabling a project after one of its jobs became overdue produces no catch-up when serving resumes; (h) a `.noon.yaml` created after startup is loaded and its job scheduled.
- integration `TestServeProcess`: with the real binary, a second `noon serve` exits 1 with `scheduler_running`, and SIGTERM to the first exits 0 within 15s.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^TestServe$' -count=1 -v` exits 0 and prints `--- PASS: TestServe`, with subtests (a) through (h) from the description
- [x] #2 `task cli:test -- -run '^TestServeProcess$' -count=1 -v` exits 0 and prints `--- PASS: TestServeProcess`
- [x] #3 `task race` exits 0
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
1. Add migration 3, atomic planner persistence, scheduler ownership/heartbeat, and activation watermark resets using existing store and recovery patterns.
2. Implement the foreground serve loop with validate-first polling reloads, existing Due/Dispatch planners and runner, snapshot execution, and graceful shutdown.
3. Wire CLI restrictions, scheduler status/next-run inspection, and implemented-behavior documentation.
4. Add the specified deterministic TestServe scenarios and real-binary TestServeProcess regression; run all acceptance commands and task ci.
5. Perform one risk-focused review, fix concrete scoped defects, commit, merge into main, finalize this record, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract deviation justified: allow narrowly scoped internal/config/state.go, internal/config/config.go, and relevant existing config tests. Serve must resolve per-user state without discovering or validating the launch directory as a project; reuse config-layer loading rather than duplicate it in CLI/scheduler. Validate-first reload diagnostics also require safe numeric source lines while preserving the existing suppression of raw YAML decoder text/secrets. Executor created state.go before reporting this deviation; parent inspected it and records authorization before further config edits. No broader configuration redesign is authorized.

Additional modified-file deviation justified: docs/configuration.md and docs/docker.md currently explicitly deny scheduling execution and must be updated to meet DoD #5 (implemented behavior only); no container feature scope is added. Migration numbering clarification: existing main already has versions 1 (runs), 2 (log overflow), and 3 (approvals). The requested scheduler schema is appended as version 4, preserving the settled append-only migration contract rather than rewriting shipped version 3.

AC#1: task cli:test -- -run '^TestServe$' -count=1 -v -> exit 0, PASS TestServe and all specified subtests (a)-(h), rerun after review fixes.
AC#2: task cli:test -- -run '^TestServeProcess$' -count=1 -v -> exit 0, PASS TestServeProcess (second owner rejected, SIGTERM exits 0).
AC#3: task race -> exit 0 across all packages; rerun in task ci after final fixes.
Delivery: implementation commit 3229c4c4d691dc19d63e4b3e092ec3ab36d00c69 fast-forward merged locally into main. task ci passed on that commit in main (security, vet, Staticcheck, unit/integration, race, build, smoke). task check:staged passed before commit; no push requested or performed.
One independent risk-focused review completed PASS after concrete fixes: nullable never-started history, scheduler-only terminal retention, config-selection authorization race, persisted NEXT timezone, and absent/restored explicit-config project reload. Added targeted regressions; no test deleted, skipped, or weakened. A test marker read race found by full CI was corrected to await complete output, then TestServe passed 20 repetitions and full CI. LSP diagnostics clean on changed core files. CLI rejects explicit --json=false as well as other forbidden flags/env. Documentation updated to distinguish implemented scheduling from future services/integrations.
Cleanup: verified session creation receipt, clean owned worktree and idle Herdr shell; Worktrunk removed feat/noon-4-serve worktree and branch. Herdr workspace wC7 disappeared; only primary checkout remains. Executor timeout did not lose changes; parent finished directly. No remaining blocker or resumable implementation step.

Review (post-merge): serve loop, ownership lock, heartbeat, validate-first reload, activation resets, and shutdown match the contract. Fixed in f7d106c: (1) an enabled project with no valid config snapshot (invalid at scheduler start, or after a selection change to a broken file) produced no JobInputs, so Dispatch cancelled all its queued runs, including future --at one-shots, and Due dropped its catch-up watermarks; the planners now skip such projects until a valid load (regression: TestServeQueued "config invalid at startup keeps queued runs and watermarks", fails before the fix). (2) scheduler.lock owner line was written with a literal backslash-n. Low, not fixed: after a selection change to an invalid file, reload is forced every tick, so the config error is re-recorded and logged every second until fixed. AC1-AC3 and task ci rerun and pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented durable foreground scheduling for enabled projects with safe reloads, approval/activation resets, catch-up/queue persistence, graceful shutdown and live inspection. All acceptance checks and full CI pass; reviewed, committed as 3229c4c, merged to main, and owned worktree/workspace removed.
<!-- SECTION:FINAL_SUMMARY:END -->
