# Scheduler service

`noon service install` runs `noon serve` as a per-user service: launchd on macOS, a systemd user
unit on Linux. It starts at login and restarts after failures. Nothing else installs a service, and
Noon never backgrounds itself.

```sh
noon service install          # install or update, then start
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

## Job environment

Scheduled runs inherit the scheduler's environment, not your shell's. Under the service that's the
`PATH` and `XDG_CONFIG_HOME` above plus whatever launchd or systemd adds; in a container it's the
container's environment. Noon never sources shell rc files, so `TERM`, `SHELL`, and your shell's
exports are usually unset. Stdin is `/dev/null` and output goes to the run log, not a terminal.
Set anything a job needs in its `env`:

```yaml
jobs:
  report:
    run: ./report.sh
    env:
      TERM: xterm-256color
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

Reinstalling reloads changed units: launchd units are bootstrapped again, systemd units are
reloaded and restarted. If an older binary meets a database from a newer Noon, `noon serve` exits
75 and the manager keeps retrying; install the newer binary.

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
