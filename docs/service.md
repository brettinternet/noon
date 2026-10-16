# Per-user scheduler service

`noon service install` explicitly registers the foreground `noon serve` scheduler with the current
user's service manager. It is available on macOS (launchd) and Linux with a systemd user manager.
Nothing else installs or starts a service. Noon never forks or backgrounds itself.

```sh
noon service install
noon service                 # inspect unit, manager state, and scheduler heartbeat
noon service install --print  # render the unit without writing or starting anything
noon service uninstall       # stop and remove the unit; state and history remain
```

Installation creates the selected state directory with mode `0700` before registering the unit.
The service always passes its absolute `--state-dir`, resolved at install time from `--state-dir` /
`NOON_STATE_DIR`, user `state_dir`, or the default. A relative explicit state path is anchored to
the installer's current directory. The state choice does not change when the manager's working
directory differs.

The executable is the PATH-resolved `noon` when it is the running executable (preserving a stable
Homebrew or other symlink); otherwise Noon uses its running executable path. The unit captures
only `PATH` and, when set, `XDG_CONFIG_HOME`. Other environment variables and secrets are not
copied into the service definition.

## Status and drift

`noon service` reports whether a unit is installed, its path, manager state, and the scheduler's
persisted heartbeat. Inspection does not install or start anything. When the unit differs from
current install settings, status recommends `noon service install` to update it. `install --print`
prints the current launchd plist or systemd user unit to stdout and has no filesystem or manager
side effects.

## Logs

On macOS, scheduler stdout and stderr are written to `scheduler.log` inside the state directory:

```sh
tail -f "$(noon config --json | jq -r '.config.state_dir')/scheduler.log"
launchctl print "gui/$(id -u)/io.github.brettinternet.noon"
```

On Linux, inspect the user journal:

```sh
systemctl --user status noon.service
journalctl --user -u noon.service -f
```

Noon does not rotate the scheduler log. Job output is stored separately in each run's bounded log.

## Upgrades and restarts

Install again after changing the executable path, state directory, `PATH`, or `XDG_CONFIG_HOME`;
unchanged unit content is left alone. Changed launchd units are unloaded and bootstrapped again;
changed systemd units are reloaded and restarted. The managers also restart the scheduler after
failures. When a newer binary encounters a database schema created by a newer version, `noon serve`
exits with status 75 and the manager retries it; install the matching newer Noon binary to recover.

## Uninstall and Linux lingering

`noon service uninstall` stops the service and removes only its unit. It keeps the state directory,
run history, and logs. It succeeds when no unit is installed.

A systemd user service normally runs while the user manager is active. If Linux reports `Linger=no`,
installation prints a hint to enable lingering so schedules can run without an interactive login.
Noon never changes this setting. An owner who wants that behavior can run this command manually:

```sh
loginctl enable-linger
```

If the platform has no supported service manager, supervise `noon serve` directly (for example,
as a foreground container process).

## Manual host verification

`install --print` is safe for preview. To verify actual manager integration on a disposable user
account, install explicitly, inspect manager status/logs using the commands above, then run
`noon service uninstall`. Never run these commands in automated tests; service lifecycle tests use
fake `launchctl` / `systemctl` executables.
