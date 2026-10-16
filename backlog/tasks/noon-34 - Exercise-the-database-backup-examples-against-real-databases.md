---
id: NOON-34
title: Make database backup and restore examples safe and verify them
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
updated_date: '2026-10-16 22:21'
labels:
  - ops
  - examples
dependencies: []
documentation:
  - examples/local-dev/db-backups/README.md
modified_files:
  - examples/local-dev/db-backups/**
  - scripts/db-backups-smoke.sh
  - Taskfile.dist.yaml
priority: high
type: task
ordinal: 3100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the Postgres/SQLite examples preserve unrelated data, publish only successful backups, and prove a real restore on macOS and Debian tools.

Confirmed risks:
- prune.sh interpolates prefix/suffix into a regular expression without escaping. Its .dump pattern also matches a file ending Xdump. KEEP is not validated, and directories/symlinks are not excluded.
- pg-restore-check.sh unconditionally drops a fixed <db>_restore_check name before creating it and in its exit trap. It can destroy a pre-existing database it did not create.
- Backup names have one-second resolution: Postgres can replace a same-second backup; SQLite can collide. SQLite destination paths are inserted into SQL without quote escaping.
- A failed dump can leave partial output, and psql query errors need explicit failure handling. Integrity and restore success must not be inferred from a command that merely ran.

Scope:
- Fix the shipped scripts/README without adding a backup framework. Retention matches literal prefix/suffix and the documented timestamp name shape, validates KEEP as a positive integer before any pruning, and touches only matching regular backup files. Preserve unrelated names, partials, directories, and symlinks.
- Publish only completed/validated output, never overwrite an existing backup, handle destination paths safely, and leave prior good backups intact on failure. Use a documented bounded wait or clear failure for SQLite write contention; do not promise every concurrent write permits immediate VACUUM.
- Restore into a fresh uniquely owned scratch database, or fail safely if the chosen name already exists. Never pre-drop another database; cleanup only one successfully created by this invocation. Restore/query errors must fail the drill, and all owned scratch databases must be cleaned on success and failure. A successful CHECK_SQL result is reported; do not invent a business-specific expected row count.
- Add a repeatable opt-in smoke harness using scripts/Taskfile conventions: build Noon, copy examples, replace hard-coded job env with isolated fixture paths, seed real Postgres/SQLite data, and run jobs through the built binary. Inherited environment alone cannot override job.env.
- Use temporary HOME/XDG/state and canonical temporary backup paths with ownership markers, plus a uniquely named disposable Postgres container/database. Poll readiness with deadlines; cleanup must verify ownership and preserve unrelated resources. Test both host macOS tools and Debian tools in a container; record actual versions, not assumed portability.

Non-goals: new backup products/features, remote/encrypted storage, new engines, scheduler changes, automatic external-integration checks in task ci.

Dependencies: none. Matching-major Postgres server/clients, SQLite, Docker Linux engine, and access to macOS for its verification are explicit prerequisites. Report unavailable environments as blockers; do not claim either platform from the other one's results.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task examples:db-backups` (the opt-in host target implemented here) exits 0 and prints passing Postgres backup/retention checks: KEEP+2 successful runs leave exactly KEEP valid dumps, same-second invocations never overwrite an existing dump, and unrelated/regex-lookalike files survive. Invalid KEEP fails without deleting backups.
- [ ] #2 The same host command proves restore contents from seeded rows, removes only owned scratch databases, preserves a pre-existing scratch-name sentinel, and fails on empty backup directories, corrupt dumps, and invalid CHECK_SQL without leaking its scratch database.
- [ ] #3 The same host command injects a dump failure after output begins and verifies no new published dump, no leftover owned partial, unchanged prior backups, and no retention of failed output. It verifies that retention ignores partials, symlinks, and directories and treats prefix/suffix literally.
- [ ] #4 The same host command exercises SQLite with a real concurrent writer, obtains successful snapshots that pass integrity_check, keeps exactly KEEP successful snapshots, handles a destination containing spaces/apostrophes, and preserves existing backups on contention/failure or name collision.
- [ ] #5 `task examples:db-backups` on macOS and `task examples:db-backups:linux` in a Debian-based container both pass all checks above with real databases. Implement both targets and record exact commands, OS/client/server versions, output, and owned-resource cleanup in Implementation Notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement evidence: a read-only grep probe of prune.sh's pattern matched both app-20261016T120000.dump and app-20261016T120000Xdump. The restore script pre-drops a fixed database and registers its drop trap before createdb succeeds. Raised priority and moved this ahead of feature work because the risks are data loss, not merely unverified examples. No destructive example was executed.
<!-- SECTION:NOTES:END -->
