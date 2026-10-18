# External heartbeats

**Level:** intermediate · **Needs:** POSIX sh, awk, curl, an external dead man's switch monitor

Use two separate monitors: one for the result of real work and one for scheduler liveness.
The example uses Healthchecks-style base GET pings and appends `/fail` for failed or
timed-out jobs. No HTTP action or monitoring service is built into Noon.

## Private setup

Create two monitors with a 5-minute period. Keep each secret ping URL in its own protected
file, not YAML, Git, a service unit, or shell history:

```sh
umask 077
mkdir -p ~/.config/noon/bin
chmod 700 ~/.config/noon
cp examples/integrations/heartbeat/bin/noon-heartbeat ~/.config/noon/bin/
$EDITOR ~/.config/noon/job-heartbeat.url
$EDITOR ~/.config/noon/scheduler-heartbeat.url
chmod 600 ~/.config/noon/*-heartbeat.url
```

Each file must contain exactly one non-empty HTTP(S) base URL (one final newline is fine).
The example accepts DNS/IPv4 hosts, an optional port 1–65535, and paths made from letters,
digits, `/._~-`, or percent-encoded bytes. It deliberately rejects query strings, fragments,
userinfo, IPv6 literals, whitespace, and multiple lines before invoking curl.
Treat the containing directory and files as trusted private user-owned configuration.
The URL is passed to curl as an argument, so local process inspection is not a secret boundary.

## Per-job results

Copy this directory into your project. In `noon.yaml`, replace both URL-file placeholders
with the absolute path to your private job URL file (YAML does not expand `~` or `$HOME`).
Replace the demonstration `run` with your real check, then:

```sh
noon run check
touch simulate-failure
noon run check
noon logs check --section hook
noon enable
```

Foreground runs also ping. Remove the demonstration marker when finished testing.
Success pings the base URL; failure and timeout ping its `/fail` endpoint.
Noop, cancelled, skipped, and interrupted runs do not fire these hooks.
A failed ping is recorded in hook logs but never changes the job result.

## Scheduler liveness

For a fresh user project, copy `user-config.yaml` to `~/.config/noon/config.yaml`.
If you already have user config, merge the job into its `jobs` map instead of overwriting it.
If using XDG_CONFIG_HOME, use that directory rather than `~/.config` for all setup paths.
The script and scheduler URL file above live relative to this user project:

```sh
noon --user run scheduler-heartbeat
noon --user enable
noon service install
```

The service schedules both enabled projects. Do not pass `--user` to service installation.
A stopped or crashed scheduler can send no failure hook: only the external monitor's
**missing ping** alert reveals that outage. A liveness ping proves this job ran, not that every
other job succeeded. Manual runs reset the monitor too.

## Period, grace, and delivery limits

Match each monitor's period to its job schedule, then allow grace for run duration,
queueing, network delay, and normal laptop sleep. For an always-on machine, 5 minutes plus
2 minutes of grace is a starting point; sleeping laptops need a longer grace or intentionally
receive offline alerts. A long grace also delays real outage detection.

`missed: skip` avoids a burst of stale liveness pings after wake. Catch-up work can run once
after downtime but cannot retroactively prove the scheduler was alive during sleep.
Set the real work monitor's grace to account for its missed-run policy.

Requests have a 2s connection limit, 5s per attempt, one retry with a 1s delay, and a 6s retry
window (at most about 11s including the final attempt), inside the 30s hook timeout.
The script ignores .curlrc, never follows redirects, discards response bodies, and emits only
a generic error. Only 2xx responses count as delivered.

Other monitors accepting a plain base GET can use the same URL-file mechanism for success
and scheduler pings. Configure `on_failure` only if that provider also supports the `/fail`
convention. Providers requiring query parameters or other HTTP methods need a different
wrapper; this example deliberately does not implement those protocols.
