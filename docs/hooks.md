# Lifecycle hooks

Noon runs optional commands after a job finishes. `on_success` runs only after `succeeded`;
`on_failure` runs after `failed` or `timed_out`. Cancelled, skipped, expired, and interrupted runs
have no hook. A hook never triggers another hook, and hook failures do not change the job's status
or foreground exit code.

A hook command is a non-empty string (run as `/bin/sh -c`) or a non-empty argv list. Job-level
hooks override user-configured defaults. An empty job-level list disables that inherited default.
Top-level defaults are accepted only from user config or an explicit `--config` file, never from
`noon.yaml` or `.noon.yaml`:

```yaml
# ~/.config/noon/config.yaml
on_success: [/Users/me/.local/bin/noon-notify]
on_failure: [/Users/me/.local/bin/noon-notify]
```

Hooks run after Noon durably records the terminal run status and outside scheduler concurrency
limits. They use the job's working directory and environment, plus `NOON_STATUS`, `NOON_EXIT_CODE`, and
`NOON_LOG_FILE` (an absolute path, omitted when no run log exists). The JSON `run.log.path` is the
same path and null when unavailable.

`NOON_PREVIOUS_STATUS` is the raw previous eligible result (`succeeded`, `failed`, or
`timed_out`), unset when none remains in retained history. Inherited and job-configured values
are removed even when it is unset. History uses the same canonical project and job name,
regardless of definition digest or whether the earlier run had a hook; all other statuses are
ignored. Noon takes one snapshot immediately before the current run's terminal transition,
while it still owns its running slot, and reuses it for environment and stdin. Results are
ordered by `finished_at` descending, then ID descending; only the current run is excluded.
A delayed queued run can therefore see a higher-ID manual run that completed before it.
Lookup happens only for a matching hook. A state lookup error still attempts to record the
job's actual result, reports the error, and skips the hook rather than inventing missing history.

Stdin is one JSON object; top-level `previous_status` is a string or explicit null under
schema version 1, not a field in shared CLI/MCP run JSON:

```json
{
  "schema_version": 1,
  "previous_status": null,
  "project": {"path": "/path/to/project", "name": "project"},
  "run": {
    "id": 12,
    "project": "/path/to/project",
    "job": "backup",
    "status": "succeeded",
    "trigger": "manual",
    "digest": "…",
    "requested_by": null,
    "idempotency_key": null,
    "due_at": null,
    "cancel_requested_at": null,
    "event_file": null,
    "depth": 0,
    "log": {
      "path": "/home/me/.local/state/noon/logs/1/12.log",
      "bytes": 42,
      "truncated": false,
      "available": true,
      "hook_offset": null
    },
    "hook": {"event": "on_success", "status": "running", "exit_code": null}
  }
}
```

`run` uses the same JSON shape as `noon history --json`: it includes the canonical absolute `project`
path, `log.path` (null if unavailable), `event_file` (null without a payload), and the stored `digest`,
`requested_by`, `idempotency_key`, `due_at`, `cancel_requested_at`, and `depth` request context. A
hook gets 30 seconds; Noon then sends TERM to its process group, waits five seconds, and sends KILL if
needed. The first 64 KiB of combined stdout and stderr are appended to the run log after a status
marker. Run JSON records `log.hook_offset`, the absolute byte offset where that marker begins, or null
when no hook was appended. If job output lacks a trailing newline, Noon inserts a separator newline
before the marker; that separator remains part of the job section. Read just one part with
`noon logs RUN --section job` or `noon logs RUN --section hook`. The run JSON also records the hook
event, status, and exit code. `noon logs RUN --follow` continues following while a hook is running.
Hook output is subject to the existing per-job log retention.

## First failure and recovery notifications

The two desktop wrappers below implement notification policy, not core Noon behavior.
Configure both hooks: notify on the first unhealthy result and on recovery to healthy.
They group `failed` and `timed_out` as one outage; switching between them does not notify.
A first success stays quiet. Unknown/pruned history is treated as healthy, so pruning can
make an old outage appear new. This is not guaranteed delivery: failed hooks are not retried.
Notifying on every raw-status change would be a different optional policy.

## macOS notification

Save an AppleScript handler and an executable wrapper. The job and status are passed as argv, not
inserted into AppleScript source:

```applescript
-- ~/.local/bin/noon-notify.applescript
on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run
```

```sh
#!/bin/sh
# ~/.local/bin/noon-notify
case "${NOON_PREVIOUS_STATUS-}" in failed|timed_out) previous=unhealthy ;; *) previous=healthy ;; esac
case "$NOON_STATUS" in failed|timed_out) current=unhealthy ;; *) current=healthy ;; esac
[ "$previous" != "$current" ] || exit 0
exec osascript "$HOME/.local/bin/noon-notify.applescript" "$NOON_JOB" "$NOON_STATUS"
```

```yaml
on_success: [/Users/me/.local/bin/noon-notify]
on_failure: [/Users/me/.local/bin/noon-notify]
```

## Linux notification

Install `notify-send` and use a wrapper that passes values as quoted arguments:

```sh
#!/bin/sh
# ~/.local/bin/noon-notify
case "${NOON_PREVIOUS_STATUS-}" in failed|timed_out) previous=unhealthy ;; *) previous=healthy ;; esac
case "$NOON_STATUS" in failed|timed_out) current=unhealthy ;; *) current=healthy ;; esac
[ "$previous" != "$current" ] || exit 0
exec notify-send "$NOON_JOB" "$NOON_STATUS"
```

```yaml
on_success: [/home/me/.local/bin/noon-notify]
on_failure: [/home/me/.local/bin/noon-notify]
```

## Chat webhook

Set `CHAT_WEBHOOK_URL` in the environment of Noon, then install a wrapper that forwards the
versioned JSON summary from stdin directly to curl. Keep the URL out of project config and logs:

```sh
#!/bin/sh
# ~/.local/bin/noon-chat
exec curl --fail --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @- "$CHAT_WEBHOOK_URL"
```

```yaml
jobs:
  backup:
    run: [./backup.sh]
    on_success: [/home/me/.local/bin/noon-chat]
    on_failure: [/home/me/.local/bin/noon-chat]
```

The examples pass environment values as argv or send JSON on stdin; none interpolates run output
into a shell command string. A shared job's own hook commands, and defaults from an explicit
`--config` file, participate in its approval digest; user-config defaults do not.
Hooks are commands, not a sandbox: they run with the same local permissions and inherited
secrets as Noon.
