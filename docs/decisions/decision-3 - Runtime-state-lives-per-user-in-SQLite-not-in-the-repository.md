---
id: decision-3
title: 'Runtime state lives per user in SQLite, not in the repository'
date: '2026-10-16 01:14'
status: accepted
---
## Context

The bootstrap defaulted `state_dir` to `PROJECT/.noon`. Repo-local state is deleted by
`git clean -fdx`, multiplies across worktrees, needs ignore rules, cannot serve a cross-project
scheduler or registry, and fails on read-only repository mounts. Run history, queues, approvals,
and occurrence watermarks need transactions and concurrent readers while the scheduler writes.

## Decision

1. Default state is `$XDG_STATE_HOME/noon`, else `~/.local/state/noon`. Overrides are
   `NOON_STATE_DIR`, `--state-dir`, and user-config `state_dir`; project files may not set it.
2. One SQLite database (`modernc.org/sqlite`, pure Go, WAL, busy timeout) holds projects,
   approvals, job state, pauses, runs, and scheduler heartbeat. Logs and event payloads are files
   beside it. Directories are 0700 and files 0600.
3. Migrations are append-only and keyed by `PRAGMA user_version`. Never edit a migration after it
   merges. A database newer than the binary is refused; the scheduler exits 75 so its service
   manager restarts it with the upgraded binary.
4. Read-only commands never create state. Retention is fixed: 50 runs per job, logs for 10.

## Consequences

- `CGO_ENABLED=0` static builds keep working on Linux, macOS, and containers.
- Moving a checkout makes it a new project; the old entry is reported missing.
- Containers persist state on a volume; Docker Desktop bind mounts are discouraged for the database.
- ncruces/go-sqlite3 is an acceptable future swap if modernc proves problematic; no ORM.
