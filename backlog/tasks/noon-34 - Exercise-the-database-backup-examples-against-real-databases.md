---
id: NOON-34
title: Make database backup and restore examples safe and verify them
status: Done
assignee: []
created_date: '2026-10-16 22:10'
updated_date: '2026-10-16 22:35'
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
- [x] #1 `task examples:db-backups` (the opt-in host target implemented here) exits 0 and prints passing Postgres backup/retention checks: KEEP+2 successful runs leave exactly KEEP valid dumps, same-second invocations never overwrite an existing dump, and unrelated/regex-lookalike files survive. Invalid KEEP fails without deleting backups.
- [x] #2 The same host command proves restore contents from seeded rows, removes only owned scratch databases, preserves a pre-existing scratch-name sentinel, and fails on empty backup directories, corrupt dumps, and invalid CHECK_SQL without leaking its scratch database.
- [x] #3 The same host command injects a dump failure after output begins and verifies no new published dump, no leftover owned partial, unchanged prior backups, and no retention of failed output. It verifies that retention ignores partials, symlinks, and directories and treats prefix/suffix literally.
- [x] #4 The same host command exercises SQLite with a real concurrent writer, obtains successful snapshots that pass integrity_check, keeps exactly KEEP successful snapshots, handles a destination containing spaces/apostrophes, and preserves existing backups on contention/failure or name collision.
- [x] #5 `task examples:db-backups` on macOS and `task examples:db-backups:linux` in a Debian-based container both pass all checks above with real databases. Implement both targets and record exact commands, OS/client/server versions, output, and owned-resource cleanup in Implementation Notes.
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
1. Harden backup publication, literal retention, and owned restore cleanup. 2. Add opt-in real-database smoke targets covering failures and contention on macOS and Debian. 3. Run acceptance and CI, review data-loss boundaries, commit and merge, finalize task and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement evidence: a read-only grep probe of prune.sh's pattern matched both app-20261016T120000.dump and app-20261016T120000Xdump. The restore script pre-drops a fixed database and registers its drop trap before createdb succeeds. Raised priority and moved this ahead of feature work because the risks are data loss, not merely unverified examples. No destructive example was executed.

Implemented private staging directories, UTC timestamp/random-token names, atomic no-overwrite hard-link publication, literal regular-file retention, fixed scratch ownership via successful createdb only, strict restore/query failures, SQLite SQL quoting and 5-second busy timeout. Added opt-in isolated real-database harness and both Task targets. No scope expansion; no tests removed or weakened; example README documents only implemented behavior (no scheduler/design contract changes).
AC#1: mise exec postgres@17.11 -- task examples:db-backups -> exit 0; PASS Postgres KEEP+2 real dumps, same-second unique publication, KEEP retained; invalid KEEP preserves backups; explicit collision preserves bytes.
AC#2: same host command -> PASS restore seeded rows (CHECK_SQL returned 3), sentinel preservation, empty/corrupt/query failures and scratch cleanup.
AC#3: same host command -> PASS failed dump leaves prior backups unchanged and no partial or published output; retention preserves literal lookalikes, unrelated files, partials, directories and symlinks.
AC#4: same host command -> PASS SQLite concurrent writer, bounded contention failure, valid snapshots, quoted paths, retention and publication failure preservation; actual name collision leaves prior bytes unchanged.
AC#5: mise exec postgres@17.11 -- task examples:db-backups and mise exec -- task examples:db-backups:linux -> both exit 0, PASS all database backup checks. Host macOS 27.0.1 build 26A434 arm64, pg_dump/pg_restore/psql 17.11, SQLite 3.54.0 (Apple build 8fa8248e303219400c646a885e36dfc52eae33d83f4412e3f369b2be5373aapl). Container Debian GNU/Linux 12 bookworm aarch64, clients/server PostgreSQL 17.11 (Debian 17.11-1.pgdg12+2), SQLite 3.40.1 (df5c253c0b3dd24916e4ec7cf77d3db5294cc9fd45ae7b9c5e82ad8197f3alt1); both used this matching-major disposable server. Image digest sha256:3645570cccdfa447589da9f57dd740faa29b30938e861289a5574b6ca6b03826. Logs: /tmp/noon-db-host-check.log and /tmp/noon-db-linux-check.log. Final runs verified label ownership and removed containers a6be9a89ff8e33d316710573ac836e902a41f06895d87df88e916e4c5dae776d and 4c0fca54bacb3630a55f480423e4cce304440f8622e3a78efc6c43fdcde65d14, including anonymous volumes. Early failed-run container ee29dffc1b1f0bbc97174457c5f85cd7c239fb87b02fcae5a0e407138d53dca4 was separately label-verified and removed after fixing Docker-context preservation. Other failed-run containers cleaned by trap. Canonical host fixtures deliberately retained with ownership markers for diagnosis under /private/var/folders/jp/1mwx72h172955139pth4h8800000gn/T/noon-db-backups.* (final R5oGTJ and UrxiJE).
Verification: mise exec -- task ci -> exit 0 (security, formatting/static checks, tests, race, binary smoke), log /tmp/noon-db-ci.log. Shell LSP diagnostics clean. Independent reviewer completed one data-loss/ownership review: no remaining validated findings; host PG environment isolation corrected with env -i. No remaining blocker. Next: source commit, integrate main, finalize and commit provider record, rerun CI on final commit, remove owned worktree and release Worklease claim.

Delivery: source commit 7badc367ceab47a1e40496f660523b7118087731 integrated into main by fast-forward after rebasing over an unrelated backlog-only commit; no unrelated changes altered. mise exec -- task ci passed on this final source commit in main (log /tmp/noon-db-final-ci.log). Owned worktree/branch fix-db-backups removed through Worktrunk after receipt and listing verification; its sole Herdr pane was an idle zsh. Worktrunk post-remove hook closed associated workspace wE1. No Docker containers with the smoke ownership label remain. Existing unrelated feat/tui-design checkout intentionally untouched. All acceptance and delivery requirements satisfied; no blocker or resumable implementation step remains. Provider completion commit and Worklease release are the only remaining administrative actions.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Made PostgreSQL/SQLite backup publication, retention and restore cleanup safe; added opt-in real-database smoke targets. All five criteria passed on macOS and Debian with real PostgreSQL 17.11 and SQLite, independent review found no remaining defects, and task ci passed on integrated source commit 7badc36. Owned worktree/branch removed; diagnostic temporary fixtures retained as documented.
<!-- SECTION:FINAL_SUMMARY:END -->
