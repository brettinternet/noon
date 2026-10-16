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
limits. They use the job's working directory and environment, plus `NOON_STATUS`,
`NOON_EXIT_CODE`, and `NOON_LOG_FILE` (an absolute path, omitted when no run log exists). Stdin is
one JSON object:

```json
{
  "schema_version": 1,
  "project": {"path": "/path/to/project", "name": "project"},
  "run": {"id": 12, "job": "backup", "status": "succeeded", "hook": {"event": "on_success", "status": "running", "exit_code": null}}
}
```

`run` uses the same JSON shape as `noon history --json`. A hook gets 30 seconds; Noon then sends
TERM to its process group, waits five seconds, and sends KILL if needed. The first 64 KiB of
combined stdout and stderr are appended to the run log after a status marker. The run JSON records
the hook event, status, and exit code. `noon logs RUN --follow` continues following while a hook
is running. Hook output is subject to the existing per-job log retention.

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
into a shell command string. A shared job's hook commands participate in its approval digest.
Hooks are commands, not a sandbox: they run with the same local permissions and inherited
secrets as Noon.
