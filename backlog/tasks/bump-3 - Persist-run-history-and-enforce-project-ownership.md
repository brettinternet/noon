---
id: BUMP-3
title: Record runs in the per-user state store
status: Done
assignee:
  - '@pi'
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 03:55'
labels:
  - store
  - runner
dependencies:
  - BUMP-2
references:
  - 'https://github.com/brettinternet/hum/tree/main/internal/process'
  - 'https://pkg.go.dev/modernc.org/sqlite'
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-3 -
    Runtime-state-lives-per-user-in-SQLite-not-in-the-repository.md
modified_files:
  - internal/store/**
  - internal/runner/**
  - internal/config/**
  - internal/app/**
  - internal/cli/**
  - docs/configuration.md
  - docs/docker.md
  - README.md
  - scripts/smoke.sh
  - .gitignore
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every foreground run leaves a durable, bounded record and log in a per-user SQLite store. The same job cannot run twice at once, a crashed run is recorded honestly and never re-run, and no state lives in the repository.

Read first: docs/design.md#state and #execution, decision-3. Reference implementation to adapt (do not import): github.com/brettinternet/hum `internal/process/identity_darwin.go` and `identity_linux.go` (process start identity), `group_*.go` (group liveness).

Scope:
- State directory: `$XDG_STATE_HOME/bump` (must be absolute when set), else `~/.local/state/bump`. `BUMP_STATE_DIR`/`--state-dir` are now relative to the invocation directory. `state_dir` is allowed only in user config or an explicit `--config` file; in a project file it is an error naming the user config path. Update `bump config`, `bump init`, docs/configuration.md, docs/docker.md, README, smoke, and .gitignore so nothing mentions `PROJECT/.bump`.
- New internal/store on `modernc.org/sqlite`: directories 0700, files 0600, WAL, `busy_timeout=5000`, `foreign_keys=ON`. `Open` creates state; an open-existing call returns a sentinel when no database exists, so read-only commands can report "no runs yet" without creating anything.
- Migrations: an append-only slice of SQL strings applied in one transaction and tracked by `PRAGMA user_version`. A database newer than the binary fails with code `state_too_new`, an upgrade hint, and no modification. Never edit a merged migration.
- Migration 1:
```sql
CREATE TABLE projects (id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL);
CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  job TEXT NOT NULL, status TEXT NOT NULL, trigger TEXT NOT NULL, digest TEXT NOT NULL,
  scheduled_at TEXT, created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  exit_code INTEGER, signal TEXT, error TEXT, requested_by TEXT,
  owner_pid INTEGER, owner_start TEXT, pgid INTEGER, leader_start TEXT,
  log_path TEXT, log_bytes INTEGER NOT NULL DEFAULT 0, log_truncated INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX runs_one_running ON runs(project_id, job) WHERE status = 'running';
CREATE INDEX runs_by_job ON runs(project_id, job, id DESC);
```
- Times are UTC text `2006-01-02T15:04:05.000Z`.
- `bump run` opens the store (failing before the job starts if it cannot), registers the project by canonical path, recovers stale runs, inserts a `running` row, streams output to the terminal and the log, and records the outcome. Insert collisions on `runs_one_running` become `already_running` errors naming the run ID and its start time.
- Recovery, run by every writer before starting work: a `running` row whose owner (PID plus start identity) is gone becomes `interrupted` and is never re-run. Its process group is terminated (TERM, grace, KILL) only when the recorded PGID's leader start identity still matches. A live process with a mismatched identity is never signalled.
- Logs: `logs/PROJECT_ID/RUN_ID.log`, stdout and stderr in arrival order. Write the first 1 MiB live. Keep later output in an in-memory ring of the last 1 MiB, appended at the end after `\n[bump: N bytes omitted]\n`.
- Environment adds `BUMP_RUN_ID` and `BUMP_LAST_SUCCESS_AT` (RFC 3339 UTC start of the job's last succeeded run; unset when none).
- Retention, applied by the store whenever it records a terminal status (every status except `queued` and `running`): keep the newest 50 terminal runs per project and job, and log files for the newest 10 of those. Never delete a `queued` or `running` row or its files, however many newer records exist. Deleting a log clears `log_path`.

Non-goals: history or logs commands (next task), approvals, scheduler tables, queued runs, `--follow`, remote or shared state, an ORM.

Tests:
- internal/store `TestStore`: migrate from empty and reopen; a newer `user_version` refused with the file unchanged; one running run per job; retention at 50 runs and 10 logs; a `running` row and its log survive more than 50 newer terminal rows of the same job; 3 MiB of output stored as the first 1 MiB, the marker, and the last 1 MiB.
- internal/store `TestRecovery`: a row whose owner PID is dead becomes `interrupted` and is not re-run; a live `sleep` recorded with a wrong start identity is left running.
- internal/cli `TestRunRecordsHistory`: two runs create two records with exit codes and logs under `BUMP_STATE_DIR`; a concurrent second run of the same job exits 1 with `already_running`; the project directory gains no files.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestStore|TestRecovery)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task cli:test -- -run '^TestRunRecordsHistory$' -count=1 -v` exits 0 with sequential runs recorded, a concurrent same-job run refused as `already_running`, and no new files in the project
- [x] #3 `task cli:build && bump_ac_home=$(mktemp -d) && env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump --json config` exits 0 and reports `state_dir` as `$bump_ac_home/.local/state/bump`, and that directory does not exist afterwards
- [x] #4 `task cli:build && bump_ac_home=$(mktemp -d) && bump_ac_project=$(mktemp -d) && printf 'state_dir: /tmp/x\n' > "$bump_ac_project/bump.yaml" && env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump -C "$bump_ac_project" config check; echo "exit=$?"` names the user config as the allowed location and prints `exit=1`
- [x] #5 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/bump` exits 0
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
1. Implement per-user configuration and SQLite store, bounded logs, safe process identity recovery, and retention.
2. Integrate foreground runs and environment, update implemented-behavior documentation.
3. Add critical regression tests, run every acceptance command and task ci, obtain one independent risk-focused review.
4. Commit implementation, merge locally, finalize provider evidence, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:test -- -run '^(TestStore|TestRecovery)$' -count=1 -v -> exit 0, both PASS, all specified store/recovery cases plus cleanup retry and zombie owner regressions.
AC#2: task cli:test -- -run '^TestRunRecordsHistory$' -count=1 -v -> exit 0, PASS; records/logs, concurrent refusal, unchanged project.
AC#3: exact task command (task cli:build followed by isolated HOME bin/bump --json config) -> exit 0, HOME/.local/state/bump reported; extra assertion confirms directory absent.
AC#4: exact task command (task cli:build, temporary project state_dir configuration, isolated HOME config check) -> allowed user config path named and exit=1 printed.
AC#5: CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/bump -> exit 0; Linux cross-build also passed.
Independent review found terminal-failure log truncation, forgotten cleanup references, and zombie owners blocking recovery. All three corrected with focused regressions. task ci passed after corrections and again on implementation commit 05865711bd77c2b1cfc0836b680213fa15221dc9. Store LSP diagnostics clean. No tests deleted, skipped, or weakened.
Dependencies: task-approved modernc.org/sqlite is maintained pure-Go SQLite instead of CGO or an ORM; golang.org/x/sys exposes Darwin process identities. CGO_ENABLED=0 verified. All paths within modified-file contract; design already matches settled behavior and docs/help updated.
Delivery: 0586571 fast-forward merged into main. Owned worktree and branch bump-3-run-store removed with Worktrunk; exact Herdr workspace wBT removal verified. No push. No blocker or remaining implementation step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Durable per-user SQLite foreground history, bounded logs, exclusion, safe crash recovery and retention implemented in 0586571 and merged locally. All five acceptance checks and CI pass; three independent review findings fixed and regression-tested. Owned worktree and workspace removed.
<!-- SECTION:FINAL_SUMMARY:END -->
