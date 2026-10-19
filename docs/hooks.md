# Lifecycle hooks

A hook is a command Noon runs after a job finishes.

```yaml
jobs:
  backup:
    run: [./backup.sh]
    on_success: [/home/me/.local/bin/noon-notify]
    on_failure: [/home/me/.local/bin/noon-notify]
```

| Job status | Hook |
| --- | --- |
| `succeeded` | `on_success` |
| `failed`, `timed_out` | `on_failure` |
| `noop` | none; it is neither success nor failure |
| anything else | none |

Like `run`, a hook is a string (`/bin/sh -c`) or an argv list. Set defaults for every job in user
config, and opt a job out with an empty list:

```yaml
# ~/.config/noon/config.yaml
on_failure: [/home/me/.local/bin/noon-notify]
```

```yaml
# noon.yaml
jobs:
  noisy:
    run: ./flaky.sh
    on_failure: []
```

Defaults are allowed only in user config or a `--config` file. A shared job's own hooks are part of
its approval digest; user-config defaults aren't, so changing them never revokes approval.

## What a hook gets

The hook runs in the job's directory with the job's environment, plus:

| Variable | Value |
| --- | --- |
| `NOON_STATUS` | `succeeded`, `failed`, or `timed_out` |
| `NOON_EXIT_CODE` | Job exit code |
| `NOON_LOG_FILE` | Absolute path to the run log; unset if there is none |
| `NOON_CONSECUTIVE_FAILURES` | Consecutive retained failures including this run; decimal integer, 0 on success |
| `NOON_PREVIOUS_STATUS` | This job's previous `succeeded`/`failed`/`timed_out` result; unset if none is retained |
| `NOON_JOB`, `NOON_RUN_ID`, … | Same as the job ([Execution](design.md#execution)) |

Stdin is one JSON object. `run` has the same shape as `noon history --json`:

```json
{
  "schema_version": 1,
  "previous_status": null,
  "consecutive_failures": 0,
  "project": {"path": "/home/me/app", "name": "app"},
  "run": {
    "id": 12,
    "project": "/home/me/app",
    "job": "backup",
    "status": "succeeded",
    "trigger": "manual",
    "digest": "sha256:…",
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

## Rules

- The hook starts after the job's final status is saved. A hook failure never changes the job's
  status or `noon run`'s exit code, and hooks never trigger other hooks.
- A hook gets 30 seconds, then TERM to its process group, then KILL five seconds later.
- The first 64 KiB of its output is appended to the raw run log after a marker at `log.hook_offset`.
  Captured hook bytes receive the same separate timing metadata as job output, rebased to their raw
  log offsets; the synthetic hook marker itself has no timestamp. Read one part with
  `noon logs RUN --section job` or `--section hook`, and opt into displayed receipt times with
  `--timestamps`. Times mean when Noon received hook output, not when the hook produced it.
- Hooks run outside the scheduler's concurrency limits.
- `previous_status` is the latest earlier `succeeded`, `failed`, or `timed_out` result of the same
  project and job, by finish time, regardless of definition changes; noops are excluded. It may come
  from a run with a higher ID that finished first. If Noon can't read history, it records the job's
  result, reports the error, and skips the hook.
- Hooks are not a sandbox. They run with your permissions and inherited secrets.

## Consecutive failures and pausing

`NOON_CONSECUTIVE_FAILURES` and top-level stdin `consecutive_failures` (integer) use the same
pre-finish snapshot as `previous_status`; stdin `schema_version` remains 1. A failed or timed-out
current run adds 1 to immediately preceding retained failures/timeouts, ordered by
`finished_at` then ID, stopping at the latest success. A current success yields 0.
Only `succeeded`, `failed`, and `timed_out` are results: noop, skipped, missed, expired,
cancelled, and interrupted neither count nor reset. All triggers count, including foreground
runs. Definition digest changes, pause, and resume do not reset the count.

This is retained history, not a lifetime counter. The snapshot precedes FinishRun pruning, so
50 retained failures plus the current failure can yield 51. Missed, expired, cancelled, and
interrupted rows consume the regular retention budget even though they do not count.
Thresholds above available history are not reliable.

Use the [pause-after recipe](../examples/integrations/notifications/README.md#pause-after-repeated-failures)
to pause at a threshold and then notify. A run dispatched before the hook finishes can still
start. `noon resume NAME` re-enables background runs without resetting the count; one more
failure can re-pause it. Use a foreground `noon run NAME` while paused to obtain a success.
With a non-default store, explicitly set `NOON_STATE_DIR` to the same absolute state directory
as the owning Noon invocation; service installation's `--state-dir` is not automatically
inherited by hook subprocesses. Never infer the state directory from the project or log path.

## Notify on first failure and recovery

This wrapper notifies when a job goes from healthy to failing or back, and stays quiet otherwise.
`failed` and `timed_out` both count as failing. Configure it for both hooks.

```sh
#!/bin/sh
# ~/.local/bin/noon-notify
case "${NOON_PREVIOUS_STATUS-}" in failed|timed_out) previous=unhealthy ;; *) previous=healthy ;; esac
case "$NOON_STATUS" in failed|timed_out) current=unhealthy ;; *) current=healthy ;; esac
[ "$previous" != "$current" ] || exit 0
exec notify-send "$NOON_JOB" "$NOON_STATUS"          # Linux
```

On macOS, replace the last line with an AppleScript call that receives values as arguments, not
as source code:

```sh
exec osascript "$HOME/.local/bin/noon-notify.applescript" "$NOON_JOB" "$NOON_STATUS"
```

```applescript
-- ~/.local/bin/noon-notify.applescript
on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run
```

Missing history counts as healthy, so a first success is quiet, and pruned history can make an
old outage look new. Failed hooks aren't retried.

## Post to chat

Forward the JSON from stdin. Keep the URL in the scheduler's environment, not in YAML:

```sh
#!/bin/sh
# ~/.local/bin/noon-chat
exec curl --fail --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @- "$CHAT_WEBHOOK_URL"
```

More: desktop, ntfy, and Slack hooks in [examples/integrations/notifications](../examples/integrations/notifications/).
