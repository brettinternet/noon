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

Like `run`, a hook is a string (`/bin/sh -c`) or an argv list. Both hooks accept YAML `|`
for multiline scripts; use `set -e` to stop on a failed command:

```yaml
on_success: |
  set -e
  ./publish.sh
  ./notify.sh
on_failure: |
  echo "$NOON_JOB failed"
  ./notify.sh
```

Set defaults for every job in user config, and opt a job out with an empty list:

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

Defaults live only in user config or a `--config` file. A shared job's own hooks are in its
approval digest; user-config defaults aren't.

## What a hook gets

The hook runs in the job's directory with that run's environment, including `env_file` values
(without them if the env file itself failed), plus:

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

- The hook starts after the job's status is saved. A hook failure never changes the job's status
  or `noon run`'s exit code, and hooks never trigger hooks.
- A hook gets 30 seconds, then TERM to its process group, then KILL five seconds later.
- Its first 64 KiB of output is appended to the run log at `log.hook_offset`; read it with
  `noon logs RUN --section hook`.
- Hooks run outside the scheduler's concurrency limits.
- `previous_status` is the latest earlier result by finish time, ignoring noops and definition
  changes. If history can't be read, Noon records the job result and skips the hook.
- Hooks are not a sandbox. They run with your permissions and inherited secrets.

## Consecutive failures and pausing

`NOON_CONSECUTIVE_FAILURES` (stdin `consecutive_failures`) counts this run plus the retained
failures before it, back to the latest success:

| Retained results, oldest first | Current | Count |
| --- | --- | --- |
| `succeeded`, `failed`, `timed_out` | `failed` | 3 |
| `failed`, `cancelled`, `noop` | `timed_out` | 2 |
| `failed`, `failed` | `succeeded` | 0 |

Only `succeeded`, `failed`, and `timed_out` count or reset; other statuses are ignored. Every
trigger counts, including foreground runs. Pause, resume, and definition changes don't reset it.
It's bounded by [retention](agents.md#limits-and-safety) (at most 51), so don't set thresholds
near that.

The [pause-after recipe](../examples/integrations/notifications/README.md#pause-after-repeated-failures)
pauses at a threshold and notifies. A run dispatched before the hook finishes can still start.
`noon resume` keeps the count, so one more failure re-pauses; a foreground `noon run NAME`
success resets it. With a non-default state directory, set `NOON_STATE_DIR` explicitly in the
hook; it isn't inherited from the service's `--state-dir`.

## Notify on first failure and recovery

Configure this wrapper for both hooks. It notifies only when a job flips between healthy and
failing (`failed` or `timed_out`).

```sh
#!/bin/sh
# ~/.local/bin/noon-notify
case "${NOON_PREVIOUS_STATUS-}" in failed|timed_out) previous=unhealthy ;; *) previous=healthy ;; esac
case "$NOON_STATUS" in failed|timed_out) current=unhealthy ;; *) current=healthy ;; esac
[ "$previous" != "$current" ] || exit 0
exec notify-send "$NOON_JOB" "$NOON_STATUS"          # Linux
```

On macOS, pass values to AppleScript as arguments, never as source:

```sh
exec osascript "$HOME/.local/bin/noon-notify.applescript" "$NOON_JOB" "$NOON_STATUS"
```

```applescript
-- ~/.local/bin/noon-notify.applescript
on run argv
  display notification (item 2 of argv) with title (item 1 of argv)
end run
```

Missing history counts as healthy, so a first success is quiet. Failed hooks aren't retried.

## Post to chat

Forward stdin. Keep the URL in a gitignored `env_file`, not YAML:

```sh
#!/bin/sh
# ~/.local/bin/noon-chat
exec curl --fail --silent --show-error \
  -H 'Content-Type: application/json' \
  --data-binary @- "$CHAT_WEBHOOK_URL"
```

More: desktop, ntfy, and Slack hooks in [examples/integrations/notifications](../examples/integrations/notifications/).
