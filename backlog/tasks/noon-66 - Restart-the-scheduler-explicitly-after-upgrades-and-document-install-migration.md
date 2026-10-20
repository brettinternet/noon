---
id: NOON-66
title: Restart the scheduler explicitly after upgrades and document install migration
status: In Progress
assignee: []
created_date: '2026-10-20 22:54'
updated_date: '2026-10-20 23:13'
labels: []
dependencies:
  - NOON-11
documentation:
  - docs/service.md
  - docs/configuration.md
modified_files:
  - internal/service/**
  - internal/cli/**
  - README.md
  - docs/service.md
  - docs/cli.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 23200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can explicitly restart the installed scheduler after replacing its binary, and can switch Noon installation methods without losing existing state or history.

Context: service install reconciles the unit and restarts when its contents change, but intentionally leaves an unchanged running unit alone. Upgrading the executable behind a stable Homebrew symlink therefore can leave the old scheduler process alive. Ordinary job configuration is already reloaded every two seconds; env_file is read at run start. Shutdown currently interrupts active jobs rather than draining them, so automatic restarts would be disruptive.

Scope: add noon service restart for per-user launchd and systemd services, using the installed definition rather than silently capturing a new shell environment or rewriting the unit. Preserve idempotent install semantics. Restart must not install a missing service; return actionable install guidance. Propagate manager failures and retain the existing unsupported-manager contract. Follow existing human/JSON CLI output conventions. Document explicit restart interruption behavior, same-path binary upgrades, changed unit settings requiring service install, and switching installation methods by invoking service install with the new executable while retaining the same state directory and config location. Config edits that reload automatically must not trigger a restart; shared job changes still need approval.

Non-goals: automatic restarts, graceful job draining, binary download/update commands, data-directory relocation or copying, database migration redesign, running-version telemetry, root/system services, or changing scheduler semantics. Running-versus-installed version reporting is a possible follow-up, not required here.

Modified-file contract: listed paths plus this Backlog record; justify any expansion in Implementation Notes before changing files. Dependencies: NOON-11 is complete; no unresolved prerequisites. Never invoke real service managers during autonomous verification; extend existing fake-manager tests after implementation to catch unchanged-unit upgrades failing to restart, accidental installation/environment rewrites, and swallowed manager failures.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 task cli:test PKG="./internal/service ./internal/cli" -- -run TestService -count=1 -v exits 0. Existing service tests extended through public interfaces cover explicit restart on both platforms even when the unit is unchanged, preservation of installed unit contents, missing-service guidance without installation, unsupported managers, and failed restart returning an error; only fake managers are invoked.
- [x] #2 task cli:test PKG="./internal/service ./internal/cli" -- -run TestService -count=1 -v exits 0 with existing install/preview/status regressions intact: unchanged install does not restart, changed install still reconciles/restarts, and read-only inspection never starts services.
- [x] #3 task cli:build exits 0; env -i PATH="$PATH" HOME="$(mktemp -d)" bin/noon service restart --help exits 0 and describes the explicit restart and active-job interruption behavior without contacting a service manager.
- [x] #4 git diff --check exits 0. Review README.md, docs/service.md, and docs/cli.md against command help: document binary upgrade then explicit restart, service install for changed executable/environment settings, preservation of state/config when switching installations, automatic config/env-file reload behavior, shared-job reapproval, and interruption of active runs; no unimplemented drain or version-detection promises.
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
Pre-implementation decision conflict: accepted decision-1 states that no command other than service install registers or starts a service. NOON-66 explicitly requires service restart, which starts a replacement scheduler and may start an installed but stopped service. No implementation files changed. Need authorization to amend decision-1 to permit explicit restart of an already installed service (never installing), or a decision that this is already within its intended contract. Worktree: .worktrees/noon-66-service-restart; creation receipt: .git/worktrees/noon-66-service-restart/agent-creation.json. Next: resolve decision wording, then implement and verify all four ACs and task ci.

User resolved decision conflict: explicitly authorized clarifying decision-1 to permit restart of an already installed service without installation or unit rewriting. Scope expansion: decision-1 wording and internal/app/service.go adapter wrapper, preserving the existing CLI-to-app service pattern.

AC#1: task cli:test PKG="./internal/service ./internal/cli" -- -run TestService -count=1 -v -> exit 0. Fake Runner exercises launchd and systemd restart, unchanged/preserved units, missing-service guidance, manager failures, unloaded launchd, and unsupported platform/manager; CLI fake executables verify human/JSON behavior.
AC#2: same TestService command -> exit 0 with unchanged install, changed install repair/rollback, preview, and status regressions intact.
AC#3: task cli:build -> exit 0; env -i PATH="$PATH" HOME=<fresh mktemp directory> bin/noon service restart --help -> exit 0, explicit restart and active-job interruption documented. Shell safety guard required creating the temporary directory in a separate command rather than inline substitution.
AC#4: git diff --check -> exit 0; README.md, docs/service.md, docs/cli.md, docs/design.md and help document explicit upgrades, changed-unit install, same-state/config migration, config/env-file reload and shared approval, with no drain or version detection promises.
Validation: task ci -> exit 0 (security, formatting, vet, staticcheck, all tests, race tests, smoke); all modified Go LSP diagnostics clean. First CI invocation hit tool timeout during race tests; rerun completed successfully. No tests deleted, skipped, or weakened. Independent scoped review pending; next commit/integrate and run CI on final commit.
<!-- SECTION:NOTES:END -->
