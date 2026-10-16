---
id: BUMP-4
title: Serve schedules for enabled projects
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - scheduler
  - store
dependencies:
  - BUMP-18
  - BUMP-19
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
  - cmd/bump/main.go
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
Outcome: `bump serve` runs every enabled project's schedules in the foreground until stopped. It survives restarts without duplicates or bursts and reloads configuration safely.

Read first: decision-1, docs/design.md#scheduler-lifetime, #missed-runs-overlap-and-concurrency, #state, and #execution.

Scope:
- Migration 3: `job_state(project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL, schedule_key TEXT NOT NULL, next_at TEXT NOT NULL, PRIMARY KEY(project_id, job))`; `scheduler(id INTEGER PRIMARY KEY CHECK (id = 1), pid INTEGER NOT NULL, start_identity TEXT NOT NULL, version TEXT NOT NULL, started_at TEXT NOT NULL, heartbeat_at TEXT NOT NULL)`; add `runs.due_at TEXT` and `projects.config_error TEXT`.
- `bump serve` takes no arguments. `--json`, `--config`, `--timezone`, `--project`, and their environment variables are usage errors, because each project uses its enable-time selection.
- Ownership: take a non-blocking exclusive `flock` on `STATE/scheduler.lock` (golang.org/x/sys/unix) and write the `scheduler` row. A second instance exits 1 with `scheduler_running`, naming the PID and start time. A `state_too_new` database exits 75.
- Startup: recover stale runs (store recovery from the record task), then loop.
- Each tick (1s): heartbeat; every 2s, stat each enabled project's candidate config paths (`bump.yaml`, `.bump.yaml`, and the user config file, or the recorded explicit file), including paths that do not exist yet. A file appearing or disappearing, or a change in mtime, size, or inode, triggers a validate-first reload. Then build `JobInput`s, call `Due` and `Dispatch` (planner task), apply their results in one transaction, and start the chosen runs.
- Starting a run sets `running` with the scheduler as owner and executes it in a goroutine with the same runner, log, and recovery identity as `bump run`. The environment is the scheduler's own plus job env and `BUMP_*`, with `BUMP_TRIGGER` and `BUMP_SCHEDULED_AT` (RFC 3339 UTC).
- Reload: an invalid edit keeps the last valid jobs and sets `projects.config_error` to the `file:line` message. A valid edit swaps atomically and clears the error. A project that starts invalid has no jobs until fixed. A missing project directory is an error, not a crash. Active runs keep the snapshot they started with.
- Activation resets: in the same transaction, `bump enable` deletes the `job_state` rows of all the project's jobs when the project was not enabled, and of each job whose approval it creates or changes. An idempotent enable deletes nothing, so container restarts keep catch-up and interval phase.
- Shutdown on SIGINT/SIGTERM: stop dispatching; terminate running jobs (TERM, 10s, KILL) and record them `interrupted`; release the lock; exit 0. A second signal kills immediately.
- stderr log, one line per event with a local timestamp: start banner (state dir, project count), run started and finished, config errors, shutdown.
- Clients: bare `bump` and list show `scheduler running` when `heartbeat_at` is under 5s old, else `scheduler offline`. NEXT uses `job_state.next_at` when present, so interval phase is accurate. `bump enable` adds `Next: bump serve` while the scheduler is offline.
- Testability: the serve loop takes options for the clock (`Now`), the tick source (a channel the test drives), and kill grace. Tests step ticks deterministically; never sleep to wait for schedules.
- integration/ gains TestMain, which builds the binary once into a temp dir. Each test isolates HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and BUMP_STATE_DIR.

Non-goals: queued-run commands and cancel (next tasks), webhooks and `--listen`, service installation, hooks, an IPC socket, fsnotify, per-project concurrency settings.

Tests:
- internal/scheduler `TestServe` with injected clock and ticks: (a) a due job runs exactly once with trigger `schedule`; (b) after simulated downtime, one `catch_up`, not a burst; (c) an invalid config edit keeps the previous jobs and records `config_error`, and fixing it clears the error; (d) shutdown interrupts a running job, kills its group, and records `interrupted`; (e) a stale `running` row from a dead previous scheduler becomes `interrupted` at startup and is not re-run; (f) a project disabled while serving stops getting runs; (g) with the loop stopped, disabling and re-enabling a project after one of its jobs became overdue produces no catch-up when serving resumes; (h) a `.bump.yaml` created after startup is loaded and its job scheduled.
- integration `TestServeProcess`: with the real binary, a second `bump serve` exits 1 with `scheduler_running`, and SIGTERM to the first exits 0 within 15s.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestServe$' -count=1 -v` exits 0 and prints `--- PASS: TestServe`, with subtests (a) through (h) from the description
- [ ] #2 `task cli:test -- -run '^TestServeProcess$' -count=1 -v` exits 0 and prints `--- PASS: TestServeProcess`
- [ ] #3 `task race` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
