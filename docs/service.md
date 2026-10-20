# Scheduler service

`noon service install` runs `noon serve` as a per-user service: launchd on macOS, a systemd user
unit on Linux. It starts at login and restarts after failures. Nothing else installs a service, and
Noon never backgrounds itself.

```sh
noon service install          # install or update, then start
noon service restart          # explicitly restart; interrupts active jobs
noon service                  # unit, manager state, scheduler heartbeat
noon service install --print  # show the unit; writes and starts nothing
noon service uninstall        # stop and remove the unit; keep state and history
```

## What the unit contains

- The `noon` found on `PATH` if it's the running binary (keeping a stable Homebrew symlink),
  otherwise the running binary's path.
- An absolute `--state-dir`, resolved at install time from `--state-dir`, `NOON_STATE_DIR`, user
  `state_dir`, or the default. The directory is created with mode 0700.
- Your current `PATH`, and `XDG_CONFIG_HOME` if set. No other environment, so secrets aren't copied.

Jobs run with that `PATH`. Re-run `noon service install` after changing any of these; an unchanged
unit is left alone. `noon service` says when the installed unit is out of date.

`${...}` in schedules and paths resolves only from a manifest's
[`vars_file`](configuration.md#config-time-variables), never the process environment, so the
service and your shell see the same jobs. Edits to it need no restart.

## Job environment

Scheduled runs inherit the scheduler's environment, not your shell's. Under the service that's the
`PATH` and `XDG_CONFIG_HOME` above plus whatever launchd or systemd adds; in a container it's the
container's environment. Noon never sources shell rc files, so `TERM`, `SHELL`, and your shell's
exports are usually unset. Stdin is `/dev/null` and output goes to the run log, not a terminal.
Set what a job needs in `env`, or in an [`env_file`](configuration.md#env-files) read at each run
start (rotation needs no restart):

```yaml
jobs:
  report:
    run: ./report.sh
    env_file: .env          # gitignored secrets
    env:
      TERM: xterm-256color  # beats .env
```

When a job works in your shell but fails on schedule, run it inside the scheduler to get the exact
environment:

```sh
noon run report --detach
noon logs <run-id> --follow
```

For a rough local check without a scheduler, strip your shell's environment:
`env -i PATH="$PATH" HOME="$HOME" noon run report`.

## Logs

macOS writes scheduler output to `scheduler.log` in the state directory:

```sh
tail -f ~/.local/state/noon/scheduler.log
launchctl print "gui/$(id -u)/io.github.brettinternet.noon"
```

Linux uses the user journal:

```sh
systemctl --user status noon.service
journalctl --user -u noon.service -f
```

```text
2026-10-16 16:54:17 scheduler started state_dir="/home/me/.local/state/noon" projects=1
2026-10-16 16:54:18 run started project="/home/me/app" job="tests" run=1 trigger=queued
2026-10-16 16:54:18 run finished project="/home/me/app" job="tests" run=1 status=succeeded
```

`scheduler.log` isn't rotated. Job output goes to each run's own log.

## Upgrades

Replacing the binary does not restart the running scheduler. After upgrading at the same path
(for example, behind Homebrew's stable symlink), explicitly restart:

```sh
brew upgrade noon
noon service restart
```

Restart uses the installed definition, without rewriting its executable, state directory, or
captured environment. It refuses a missing service with `noon service install` guidance.
**Restart interrupts active jobs; it does not wait for them to finish.** Choose a quiet time.
Noon never restarts automatically on upgrade.

For a changed executable path, `PATH`, `XDG_CONFIG_HOME`, or other unit settings, run
`noon service install` instead. It reconciles and restarts changed units; an unchanged running
unit is left alone, even if the binary behind its path has changed.

### Switching installation methods

Install the new binary, then invoke that executable's `service install` (use its full path if
both installations remain on `PATH`). Keep the same absolute state directory and config location:

```sh
/path/to/new/noon --state-dir /existing/noon-state service install
```

Use the same `HOME` and `XDG_CONFIG_HOME` as before and preserve the existing configuration.
The state directory retains registered projects, approvals, queued work, and run history; no
copying or relocation is needed. A changed unit restarts the scheduler and interrupts active jobs.
If its contents are unchanged, use `service restart` explicitly. Remove the old installation only
after the installed unit points to the new executable.

Job configuration reloads every two seconds; `vars_file` edits reload too. `env_file` is read at
each run start. These edits need no service restart. Changed shared job definitions still need
`noon enable` approval; restarting does not approve them.

If an older binary meets a database from a newer Noon, `noon serve` exits 75 and the manager keeps
retrying; install the newer binary.

## Linux lingering

A systemd user service runs only while your user manager is active. If `loginctl` reports
`Linger=no`, install prints a hint. To run schedules without a login session:

```sh
loginctl enable-linger
```

Noon never changes this for you. Without launchd or systemd, supervise `noon serve` yourself, for
example as a container's foreground process.

## Testing

Automated tests use fake `launchctl` and `systemctl`. To check real integration, use a disposable
user account: install, inspect with the commands above, then uninstall.
