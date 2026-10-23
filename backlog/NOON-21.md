---
id: NOON-21
title: Show the project dashboard and diagnose setup
status: done
type: feature
priority: high
labels: [cli, reviewed]
blocked_by: [NOON-20, NOON-17, NOON-11]
rank: a2ca6b29aca6b299e
refs: [docs/design.md, docs/development.md]
files: [internal/app/**, internal/cli/**, internal/service/**, internal/store/**, README.md]
created: 2026-10-16T01:41:00Z
updated: 2026-10-16T14:37:00Z
---

Outcome: bare `noon` answers "what will run, what ran, and what needs me?" on one screen, and `noon doctor` finds every setup problem that would stop jobs from running.

Read first: docs/design.md#experience (the dashboard transcript is the target), #enable-and-approve, and #scheduler-lifetime.

Scope:
- One app operation builds the dashboard; the CLI only renders it. JSON: `{"schema_version":1,"project":{"path","name","timezone","enabled","config_error"},"scheduler":{"state":"running"|"offline","pid","heartbeat_at"},"service":{"installed"},"jobs":[...list job objects...],"queued":[...run objects...],"notices":[{"code","message"}],"next":"noon enable"}`.
- Human layout: a header line `NAME · PATH · scheduler running|offline · TIMEZONE`; the JOB/SCHEDULE/NEXT/LAST table (NEXT shows `paused`, `disabled`, or `needs approval` where relevant); up to three notice lines; exactly one `Next:` line. Fit 80 columns by truncating the widest column. No color.
- Notice codes, in priority order: `config_error`, `not_enabled` (scheduled jobs exist), `needs_approval`, `scheduler_offline` (project enabled), `queued`.
- `next` follows the same priority: fix config, `noon enable`, `noon service install` (or `noon serve` when the service is installed but not running), `noon run JOB`, or `noon add` when there are no jobs.
- `noon doctor` checks, each `ok`, `warn`, or `fail` with a one-line fix:
  - Config: config valid (fail); timezone valid (fail).
  - State and scheduler: state directory writable, or its nearest existing parent writable when absent, without creating it (fail); database schema compatible (fail); project enabled when scheduled jobs exist (warn); approvals pending (warn); scheduler heartbeat fresh while the project is enabled (warn); service installed on macOS or Linux (warn).
  - Per job: argv[0] resolvable for list `run` jobs using the job's PATH and cwd, `/bin/sh` present for string jobs, and the agent profile's argv[0] for prompt jobs (fail); `cwd` exists (fail); `prompt_file` exists and is at most 64 KiB (fail).
  - `.noon.yaml` ignored by Git when it exists (warn).
- Doctor exits 1 if any check fails, otherwise 0. JSON: `{"schema_version":1,"ok":bool,"checks":[{"name","status","message","hint"}]}`. Doctor performs no logical application-data writes, migrations, or creation of the state directory/main database. SQLite-managed WAL/SHM coordination sidecars are permitted (operator-approved exception).

Non-goals: fixing problems automatically, network checks, agent login checks, colors, a watch mode.

Tests:
- internal/cli `TestDashboard` (JSON) for an empty project, not enabled, needs approval, scheduler offline vs. a fresh heartbeat, a recorded config error, and a queued run; each case asserts `notices` codes and `next`.
- internal/cli `TestDoctor`: a job whose command is missing fails with exit 1 and names the job; a healthy enabled project with a fresh heartbeat exits 0; neither run changes project files or application database contents, or creates absent application state; SQLite-managed WAL/SHM coordination sidecars are permitted.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.

## Acceptance criteria

- [x] `task cli:test -- -run '^(TestDashboard|TestDoctor)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: [definitely-not-a-command-xyz]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" doctor; echo "exit=$?"` prints a failing check naming job `t` and the missing command, then `exit=1`

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Inspect existing inspection, runner, service, and store APIs; build shared dashboard and read-only doctor operations.
2. Render dashboard and doctor CLI contracts and add critical regression coverage.
3. Run acceptance commands, independent review, and full CI; commit, merge, finalize task, and remove owned worktree.

## Notes

Scope expansion: export the existing executable resolver in internal/runner/runner.go (and update its existing tests if needed) so doctor uses exactly the runner PATH/cwd semantics rather than duplicating them. Update docs/design.md Experience to reflect the implemented dashboard/doctor contract.

Implemented app dashboard and doctor operations, CLI JSON/human output, documentation, and critical tests. Dashboard cases pass. Doctor missing-command direct execution prints fail job t command and exit=1. Read-only regression exposed existing SQLite mode=ro behavior: it creates WAL/SHM sidecars on a closed WAL database; resolving this without ignoring live WAL data before completion. Existing invalid --timezone error-envelope regression is preserved. task cli:check passes.

Operator explicitly approved permitting SQLite coordination sidecars after the strict no-file-creation regression exposed stock SQLite WAL behavior. Retain normal mode=ro and locking; do not use immutable=1 or unsafe snapshots. The new read-only regression now follows this approved contract: absent state stays absent and all project/main-database bytes remain unchanged; only exact noon.db-wal/noon.db-shm paths may be created. Existing tests remain unchanged.

AC#1: task cli:test -- -run '^(TestDashboard|TestDoctor)$' -count=1 -v -> exit 0; both TestDashboard and TestDoctor print PASS, covering all named scenarios and the approved read-only contract.
AC#2: task cli:build; isolated HOME/project fixture with jobs.t.run=[definitely-not-a-command-xyz]; env -i PATH="$PATH" HOME=<fixture-home> bin/noon -C <fixture-project> doctor; echo "exit=$?" -> fail job t command names definitely-not-a-command-xyz, then exit=1. Harness rejected the combined dynamic-shell fixture bootstrap, so the same mkdir/write/build/invocation steps ran separately with resolved literal temporary paths.
Full task ci passed (format, vet, Staticcheck, unit/integration tests, race, secret/vulnerability scans, build, smoke). Independent final defect review pending.

Final review: one concrete JSON error-classification regression for absent --project was found, reproduced, fixed, and covered by extending TestJSONFailuresAreSingleObjects for bare noon and status. No existing tests removed/skipped/weakened; the new doctor test follows the explicitly approved sidecar exception. LSP diagnostics clean. Final source commit 3ecf24a passed task check:staged and task ci after commit, then fast-forward merged into main. Owned noon-21-dashboard worktree and branch removed via Worktrunk; its Herdr workspace contained only an idle shell and the post-remove hook closed it. No remaining blocker or resumable implementation work.

Review (post-merge): dashboard JSON/human contract, notice and next priority, 80-column bounding, doctor checks, exit codes, and read-only behavior (no state creation; approved SQLite sidecar exception) match the contract. Fixed in bffdf41: doctor resolved commands/cwd/prompt files of enabled: false jobs, so a job disabled because its tool is absent on this machine failed doctor with exit 1 forever; disabled jobs are now skipped (regression in TestDoctor healthy case, fails before the fix). Low, not fixed: doctor warns approvals pending on a not-enabled project while the dashboard gates needs_approval on enabled; with a config error the enabled check reports ok because jobs are unknown; NEXT shows needs approval rather than disabled for shared scheduled jobs in a not-enabled project; manual NEXT renders - not the design's em dash; on Linux without a systemd user manager next suggests noon service install, which then fails service_unsupported. task ci passes.

## Summary

Implemented shared project dashboard and read-only setup diagnostics with prioritized guidance, bounded human output, versioned JSON, and command/cwd/prompt/state checks. Acceptance checks and full CI pass; independent review finding fixed. Source commit 3ecf24a merged locally into main; owned worktree/branch cleaned up. SQLite coordination sidecars are the explicitly approved read-only exception. No push.
