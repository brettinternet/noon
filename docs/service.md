# Scheduler service

`noon service install` runs `noon serve` as a per-user service: launchd on macOS, a systemd user
unit on Linux. It starts at login and restarts after failures. Nothing else installs a service, and
Noon never backgrounds itself.

```sh
noon service install          # install or update, then start
noon service restart          # explicitly restart; interrupts active jobs
noon service                  # unit, manager state, scheduler heartbeat and build
noon service install --print  # show the unit; writes and starts nothing
noon service uninstall        # stop and remove the unit; keep state and history
```

## What the unit contains

| Captured | From |
| --- | --- |
| Executable | `noon` on `PATH` if it's the running binary (keeps Homebrew's stable symlink), else the running binary |
| `--state-dir` | `--state-dir`, `NOON_STATE_DIR`, user `state_dir`, or the default; made absolute and created 0700 |
| `PATH`, `XDG_CONFIG_HOME` | Your current shell; nothing else, so secrets aren't copied |

Re-run `noon service install` after changing any of these. An unchanged unit isn't rewritten.

## Job environment

Scheduled runs inherit the scheduler's environment, not your shell's: the captured `PATH` and
`XDG_CONFIG_HOME` plus whatever launchd or systemd adds. Noon never sources shell rc files, so
`TERM`, `SHELL`, and your exports are usually unset. Stdin is `/dev/null`. Set what a job needs:

```yaml
jobs:
  report:
    run: ./report.sh
    env_file: .env          # gitignored secrets, read at each run
    env:
      TERM: xterm-256color  # beats .env
```

When a job works in your shell but fails on schedule, run it inside the scheduler:

```sh
noon run report --detach
noon logs <run-id> --follow
env -i PATH="$PATH" HOME="$HOME" noon run report   # rough local approximation
```

## Logs

```sh
# macOS
tail -f ~/.local/state/noon/scheduler.log
launchctl print "gui/$(id -u)/io.github.brettinternet.noon"

# Linux
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

Replacing the binary doesn't restart the scheduler. Reconcile explicitly:

```sh
brew upgrade noon
noon service install
```

**Restarts interrupt active jobs; they don't drain.** Pick a quiet time.

| Situation | `service install` does |
| --- | --- |
| Unit settings changed (executable path, `PATH`, `XDG_CONFIG_HOME`) | Rewrites the unit and restarts |
| Same unit, running scheduler reports a different version or commit | Restarts it |
| Same unit, same build | Nothing |
| Same unit, build metadata unknown (e.g. dev builds) | Tells you to run `noon service restart` |

Install only restarts a scheduler the service manager owns, with a verified process identity and a
fresh heartbeat. `noon doctor` reports `scheduler_version_mismatch`; `noon status` and
`noon service` show the running build. Rebuilt binaries with the same version and commit look
identical.

Config edits (including `vars_file`) reload every two seconds, and `env_file` is read at each run.
Neither needs a restart. Restarting never approves changed shared jobs; run `noon enable`.

An older binary facing a newer database exits 75, and the manager keeps retrying until you install
the newer binary.

### Switching installation methods

Run the new executable's `service install` with the same state directory, `HOME`, and
`XDG_CONFIG_HOME`:

```sh
/path/to/new/noon --state-dir /existing/noon-state service install
```

Projects, approvals, queued work, and history stay in the state directory; nothing is copied.
Remove the old installation only after the installed unit points to the new executable.

## Linux lingering

A systemd user service runs only while your user manager is active. If `loginctl` reports
`Linger=no`, install prints a hint. To run without a login session:

```sh
loginctl enable-linger
```

Noon never changes this for you. Without launchd or systemd, supervise `noon serve` yourself, for
example as a container's foreground process.

## Testing

Automated tests use fake `launchctl` and `systemctl`. To check real integration, use a disposable
user account: install, inspect with the commands above, then uninstall.
