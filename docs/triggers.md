# Triggers

Anything that can run a command can queue a job:

```sh
noon run tests --detach --key "$(git rev-parse HEAD)"
noon run tests --detach --data '{"commit":"abc123"}'
noon run tests --detach --data @event.json
printf '%s\n' '{"sha":"abc123"}' | noon run tests --detach --data -
```

The project must be enabled and a scheduler running. Noon never installs hooks for you.

## Event data

`--data` (JSON, `@FILE`, or `-` for stdin; ≤ 256 KiB) is written to a private file before the run
is queued. The job reads `$NOON_EVENT_FILE`:

```json
{"schema_version":1,"source":"cli","received_at":"2026-10-16T22:54:31.711Z","data":{"commit":"abc123"}}
```

`source` is `cli`, `mcp`, `api`, or `webhook`. Data never becomes part of a command. Event files
are kept with the newest 10 logs per job.

## Cooldown

```yaml
jobs:
  tests:
    run: [go, test, ./...]
    cooldown: 5m
```

A queued request within `cooldown` (≥ `1s`) of the job's last run returns that run:

```console
$ noon run tests --detach
tests is cooling down until Fri 16:59:12 (run 4)
```

Foreground and scheduled runs ignore cooldown. It's in the approval digest and can't be combined
with `requests: each`. Checks run in order: idempotency key, enablement and approval, cooldown,
then due-run merging or the `each` cap.

## Merge vs each event requests

| | `requests: merge` (default) | `requests: each` |
| --- | --- | --- |
| Due-now request while a run is already due | Returns that run; new payload dropped | New run with its own payload |
| Use when | Requests are wake-ups and the job reconciles state | Every event matters |
| Limit | — | 20 queued runs per job, including `--in`/`--at` |

```yaml
jobs:
  review:
    run: [./review-event]
    requests: each
```

At the `each` cap, a due-now request fails with `queue_full` (webhooks get 429) and creates nothing.
Idempotency keys still dedupe, so webhook redelivery stays safe. `--in`/`--at` one-shots never
merge.

Queued runs more than an hour overdue become `expired`. If an event must never be missed, have the
job reconcile outstanding work rather than rely on event files.

## Git hooks

Create the hook and make it executable. Git runs it in the checkout.

```sh
#!/bin/sh
# .git/hooks/post-commit
exec noon run tests --detach --key "$(git rev-parse HEAD)"
```

```sh
#!/bin/sh
# .git/hooks/post-merge
exec noon run tests --detach --key "merge-$(git rev-parse HEAD)" --data '{"event":"post-merge"}'
```

The commit ID is only an idempotency key; it never reaches the job's command.
[examples/local-dev/git-hooks](../examples/local-dev/git-hooks/) also covers `post-checkout`.

## Claude Code `Stop` hook

Claude Code sends hook input as JSON on stdin; `--data -` stores it as the event's `data`. In
`.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "noon run tests --detach --data -" }] }
    ]
  }
}
```

## Codex `Stop` hook

Codex expects JSON on stdout from a `Stop` hook, so send Noon's output to stderr and print `{}`.
Review and trust the hook in Codex first. In `.codex/hooks.json`:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "noon run tests --detach --data - >&2 && printf '{}\\n'" }] }
    ]
  }
}
```

Vendor formats checked 2026-10-16 against the
[Claude Code](https://code.claude.com/docs/en/hooks) and
[Codex](https://developers.openai.com/codex/hooks) hook docs.

## Shell entry

`noon shell init bash --on-enter JOB` queues a job when you `cd` into the project. See
[Shell setup](shell.md).

## File changes

Noon doesn't watch files. Run a watcher yourself and let it call Noon:

```sh
watchexec -n --postpone --exts go --debounce 1s --on-busy-update queue -- noon run tests
```

[examples/local-dev/file-watch](../examples/local-dev/file-watch/) compares this foreground form
with `--detach` and covers trigger loops.

## Recursion guard

A job that queues a job that queues a job is bounded. Each run stores the caller's `NOON_DEPTH`;
the job and its hooks get that value plus one. Noon refuses a request with code `recursion` when
the depth is 3 or more, or when a job requests itself. Treat this as a safety net, not a design
tool.
