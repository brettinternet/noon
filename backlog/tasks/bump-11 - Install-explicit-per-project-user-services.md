---
id: BUMP-11
title: Install the scheduler as a per-user service
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - ops
dependencies:
  - BUMP-4
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
Outcome: one `bump service install` makes `bump serve` start at login and restart after failures or upgrades, with the user's PATH, on macOS (launchd) and Linux (systemd user units). `bump service` shows status, and `uninstall` reverses install.

Read first: decision-1, docs/design.md#scheduler-lifetime.

Scope:
- New internal/service. Pure rendering functions produce the unit text, and a small runner interface executes `launchctl`/`systemctl`, so tests can substitute fake executables on PATH.
- Executable path: use the PATH-resolved `bump` when it is the same file as `os.Executable()`, otherwise `os.Executable()`. Never resolve symlinks, so Homebrew upgrades keep working.
- State directory: the unit always passes `--state-dir STATE`, where STATE is the absolute state directory resolved at install time from the flag, environment, user config, or default. A relative flag or `BUMP_STATE_DIR` is therefore fixed against the installer's directory, not the service manager's.
- Environment captured at install: `PATH`, plus `XDG_CONFIG_HOME` when set. Nothing else, and never secrets.
- Install creates the state directory (0700) first, because service managers do not create log directories.
- macOS: `~/Library/LaunchAgents/io.github.brettinternet.bump.plist` with Label, ProgramArguments `[EXECUTABLE, --state-dir, STATE, serve]`, EnvironmentVariables, RunAtLoad true, KeepAlive true, ThrottleInterval 10, and StandardOutPath/StandardErrorPath `STATE/scheduler.log`. Load with `launchctl bootstrap gui/UID PLIST`; reload with `bootout` then `bootstrap`; status from `launchctl print gui/UID/io.github.brettinternet.bump`.
- Linux: `${XDG_CONFIG_HOME:-~/.config}/systemd/user/bump.service` with ExecStart `EXECUTABLE --state-dir STATE serve` (quoted per systemd rules), `Environment=` lines, Restart=always, RestartSec=5, KillMode=mixed, TimeoutStopSec=30, and WantedBy=default.target. Run `systemctl --user daemon-reload`, then `enable --now`, or `restart` when the unit changed. If `loginctl show-user` reports Linger=no, print a hint to run `loginctl enable-linger` (never run it).
- `bump service`: installed or not, unit path, manager state, scheduler heartbeat, and a warning with the hint `bump service install` when the unit's state directory or executable differs from what install would write now. `bump service install [--print]`: `--print` writes nothing and prints the unit. Install is idempotent: unchanged content triggers no reload. `bump service uninstall`: stop and remove the unit; exit 0 when not installed. Other OSes, or Linux without a systemd user manager: error `service_unsupported` with the hint to run `bump serve` under their own supervisor.
- Uninstall keeps state and history. Inspection commands never install or start anything.
- When the service is not installed, bare `bump` and `bump enable` suggest `bump service install` in place of `bump serve`.
- Add docs/service.md (install, logs, upgrade behavior via exit 75 and restart, uninstall, Linux lingering) and link it from README.

Non-goals: root or system services, per-project units, Windows, auto-install from any other command, log rotation beyond the state-dir file.

Tests:
- internal/service `TestServiceUnits`: rendered plist and unit contain the executable, the absolute `--state-dir` (a relative flag resolved against the installer's directory), `serve`, the captured variables only (never `BUMP_STATE_DIR` or `XDG_STATE_HOME`), and the restart policy. The plist passes `plutil -lint` on macOS (skipped elsewhere).
- internal/cli `TestServiceCommands`, with fake `launchctl` and `systemctl` scripts on PATH that record argv: install writes the unit and issues the documented commands once; repeating install with no change issues no reload; uninstall removes the unit; `install --print` writes nothing. The real service manager is never invoked.
- Never install a real service during autonomous work. Record in Implementation Notes the manual commands the owner can run to verify on a real host.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestServiceUnits|TestServiceCommands)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests without invoking a real service manager
- [ ] #2 `task cli:build && bin/bump service install --print; echo "exit=$?"` prints a unit for the current OS, `exit=0`, and creates no file under `~/Library/LaunchAgents` or `~/.config/systemd/user`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
