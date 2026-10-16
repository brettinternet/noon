---
id: NOON-11
title: Install the scheduler as a per-user service
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 05:48'
labels:
  - ops
dependencies:
  - NOON-4
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-1 -
    One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md
modified_files:
  - internal/service/**
  - internal/app/**
  - internal/cli/**
  - docs/service.md
  - README.md
priority: high
type: feature
ordinal: 1100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one `noon service install` makes `noon serve` start at login and restart after failures or upgrades, with the user's PATH, on macOS (launchd) and Linux (systemd user units). `noon service` shows status, and `uninstall` reverses install.

Read first: decision-1, docs/design.md#scheduler-lifetime.

Scope:
- New internal/service. Pure rendering functions produce the unit text, and a small runner interface executes `launchctl`/`systemctl`, so tests can substitute fake executables on PATH.
- Executable path: use the PATH-resolved `noon` when it is the same file as `os.Executable()`, otherwise `os.Executable()`. Never resolve symlinks, so Homebrew upgrades keep working.
- State directory: the unit always passes `--state-dir STATE`, where STATE is the absolute state directory resolved at install time from the flag, environment, user config, or default. A relative flag or `NOON_STATE_DIR` is therefore fixed against the installer's directory, not the service manager's.
- Environment captured at install: `PATH`, plus `XDG_CONFIG_HOME` when set. Nothing else, and never secrets.
- Install creates the state directory (0700) first, because service managers do not create log directories.
- macOS: `~/Library/LaunchAgents/io.github.brettinternet.noon.plist` with Label, ProgramArguments `[EXECUTABLE, --state-dir, STATE, serve]`, EnvironmentVariables, RunAtLoad true, KeepAlive true, ThrottleInterval 10, and StandardOutPath/StandardErrorPath `STATE/scheduler.log`. Load with `launchctl bootstrap gui/UID PLIST`; reload with `bootout` then `bootstrap`; status from `launchctl print gui/UID/io.github.brettinternet.noon`.
- Linux: `${XDG_CONFIG_HOME:-~/.config}/systemd/user/noon.service` with ExecStart `EXECUTABLE --state-dir STATE serve` (quoted per systemd rules), `Environment=` lines, Restart=always, RestartSec=5, KillMode=mixed, TimeoutStopSec=30, and WantedBy=default.target. Run `systemctl --user daemon-reload`, then `enable --now`, or `restart` when the unit changed. If `loginctl show-user` reports Linger=no, print a hint to run `loginctl enable-linger` (never run it).
- `noon service`: installed or not, unit path, manager state, scheduler heartbeat, and a warning with the hint `noon service install` when the unit's state directory or executable differs from what install would write now. `noon service install [--print]`: `--print` writes nothing and prints the unit. Install is idempotent: unchanged content triggers no reload. `noon service uninstall`: stop and remove the unit; exit 0 when not installed. Other OSes, or Linux without a systemd user manager: error `service_unsupported` with the hint to run `noon serve` under their own supervisor.
- Uninstall keeps state and history. Inspection commands never install or start anything.
- When the service is not installed, bare `noon` and `noon enable` suggest `noon service install` in place of `noon serve`.
- Add docs/service.md (install, logs, upgrade behavior via exit 75 and restart, uninstall, Linux lingering) and link it from README.

Non-goals: root or system services, per-project units, Windows, auto-install from any other command, log rotation beyond the state-dir file.

Tests:
- internal/service `TestServiceUnits`: rendered plist and unit contain the executable, the absolute `--state-dir` (a relative flag resolved against the installer's directory), `serve`, the captured variables only (never `NOON_STATE_DIR` or `XDG_STATE_HOME`), and the restart policy. The plist passes `plutil -lint` on macOS (skipped elsewhere).
- internal/cli `TestServiceCommands`, with fake `launchctl` and `systemctl` scripts on PATH that record argv: install writes the unit and issues the documented commands once; repeating install with no change issues no reload; uninstall removes the unit; `install --print` writes nothing. The real service manager is never invoked.
- Never install a real service during autonomous work. Record in Implementation Notes the manual commands the owner can run to verify on a real host.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestServiceUnits|TestServiceCommands)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests without invoking a real service manager
- [x] #2 `task cli:build && bin/noon service install --print; echo "exit=$?"` prints a unit for the current OS, `exit=0`, and creates no file under `~/Library/LaunchAgents` or `~/.config/systemd/user`
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
1. Inspect scheduler/configuration and CLI patterns; implement pure unit rendering and explicit service lifecycle operations. 2. Wire service commands and offline hints, add critical fake-manager regression tests and service documentation. 3. Run acceptance commands and task ci, perform one independent review, commit implementation, merge main, finalize backlog evidence, and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed under Worklease loop 53909a3c-e1ac-40d5-a491-b52b62d6687d, claim 2b5be51223ad3348b9c8b4f5c449625f. Implementation worktree /Users/brett/dev/me/bump/.worktrees/noon-11-service, branch noon-11-service; creation receipt .git/worktrees/noon-11-service/agent-creation.json. Modified-file contract expansion: docs/design.md scheduler-lifetime paragraph must change planned service installation to implemented behavior to satisfy DoD #5; no architectural change. Executor b635e0d1-f931-488c-b81e-cb609572cf59 owns worktree edits while parent manages provider state. Existing offline-enable assertion will be updated to the required noon service install hint, retaining its behavioral coverage.

AC#1: task cli:test -- -run '^(TestServiceUnits|TestServiceCommands)$' -count=1 -v -> exit 0, both named tests printed PASS; launchd plist passed plutil -lint. Tests use fake launchctl/systemctl/loginctl only. AC#2: task cli:build && bin/noon service install --print; echo "exit=$?" -> macOS plist and exit=0. Before/after recursive listings of ~/Library/LaunchAgents and ~/.config/systemd/user were identical; neither Noon unit exists on host. Full task ci passed on implementation commit 4d00f1e1785666cf7a02d59950ee890f47c9fcc9 (security, formatting/vet/Staticcheck, tests, race, build/smoke). LSP diagnostics clean. Independent single reviewer pass found failed-update retry masking, systemd dollar expansion, and fake systemctl --user dispatch; all fixed with regression coverage and focused checks rerun. Linux TestServiceCommands also passed in network-disabled golang:1.27.2 Docker using a cross-compiled linux/arm64 test binary, including failed-update rollback/retry and unavailable-manager errors. No real manager was invoked. No existing tests deleted/skipped/weakened; offline guidance assertion updated for required behavior. Source/docs stayed within the contract plus previously justified docs/design.md. No new dependencies. Manual owner-only host verification: noon service install; noon service; on macOS launchctl print gui/$(id -u)/io.github.brettinternet.noon; on Linux systemctl --user status noon.service and journalctl --user -u noon.service; noon service uninstall. These commands intentionally were not run against a real manager. Delivery: implementation 4d00f1e fast-forward merged into main. Verified receipt-owned worktree and branch noon-11-service removed via Worktrunk; Herdr workspace wC8 was an idle zsh and its disappearance was verified. No retained checkout/workspace. Remaining blockers: none; no next resumable step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented per-user launchd/systemd scheduler service install, preview, status/drift/heartbeat, and uninstall with safe environment capture, stable executable paths, idempotency, failure retry, offline guidance, and operator docs. Acceptance checks and full CI pass; macOS and Linux fake-manager lifecycle tests pass. Merged 4d00f1e into main and cleaned up owned worktree/branch/workspace; no real service installed.
<!-- SECTION:FINAL_SUMMARY:END -->
