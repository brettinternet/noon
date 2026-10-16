---
id: NOON-34
title: Exercise the database backup examples against real databases
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
labels:
  - ops
  - examples
dependencies: []
documentation:
  - examples/local-dev/db-backups/README.md
modified_files:
  - examples/local-dev/db-backups/**
priority: medium
type: task
ordinal: 3500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the Postgres and SQLite backup, retention, and restore-drill scripts in examples/local-dev/db-backups are proven against real databases on macOS and Linux tools, with any bugs fixed.

Why: these scripts were never run. A backup example that silently keeps the wrong files, counts a failed dump, or reports a restore drill as passing without restoring is worse than no example, because users trust it with data. Specific risks: `prune.sh` matching (it must delete only `PREFIX-<timestamp>SUFFIX` files beyond KEEP and never anything else in the directory); `pg_restore --exit-on-error` with a `--no-owner` custom dump into a fresh database; the EXIT trap dropping the scratch database even when `createdb` or the restore fails; `VACUUM INTO` while another connection writes; BSD versus GNU behavior of `ls -t`, `du`, `date`, and `grep -E`.

Scope: run each job through `noon run` against a disposable Postgres (for example the official postgres container) and a local SQLite file, on macOS and inside a Debian-based container; fix the scripts and README.

Non-goals: new backup features, remote or encrypted storage, other database engines, scheduling changes.

Dependencies: requires `pg_dump`/`pg_restore`/`psql` client tools matching the server major version, `sqlite3`, and Docker for the disposable server and the Linux run.

Modified-file contract: the modified_files list plus this task record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Against a disposable Postgres seeded with a `users` table, running `noon run pg-backup` KEEP+2 times with BACKUP_DIR in a temporary directory leaves exactly KEEP dumps, and an unrelated file placed in BACKUP_DIR beforehand is still present.
- [ ] #2 `noon run pg-restore-check` exits 0 and prints the `CHECK_SQL` result; afterwards the `<db>_restore_check` database does not exist. With BACKUP_DIR empty it exits 1 with "no dumps".
- [ ] #3 With the server stopped, `noon run pg-backup` exits non-zero and leaves no new `.dump` file and no `.partial` file that a later run would count toward retention.
- [ ] #4 `noon run sqlite-backup` while another process writes to the database produces snapshots that pass `PRAGMA integrity_check`, and retention keeps exactly KEEP snapshots.
- [ ] #5 The criteria above pass both on macOS and inside a Debian-based container; commands and results for both are recorded in Implementation Notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
