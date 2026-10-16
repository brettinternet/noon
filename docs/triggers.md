# Triggers

Anything that can run a command can queue a job:

```sh
noon run tests --detach --key "$(git rev-parse HEAD)"
noon run tests --detach --data '{"commit":"abc123"}'
noon run tests --detach --data @event.json
printf '%s\n' '{"sha":"abc123"}' | noon run tests --detach --data -
```

The project must be enabled, and `noon serve` (or the service) dispatches the run. Noon never
installs hooks for you.

## Event data

`--data` takes a JSON value, `@FILE`, or `-` for stdin, up to 256 KiB. Noon writes it to a private
file before the run is queued, and the job reads it from `$NOON_EVENT_FILE`:

```json
{"schema_version":1,"source":"cli","received_at":"2026-10-16T22:54:31.711Z","data":{"commit":"abc123"}}
```

`source` is `cli`, `mcp` (`run_job` with `data`), or `webhook`. Data never becomes part of a
command. Event files are kept with the newest 10 logs per job.

## Cooldown

```yaml
jobs:
  tests:
    run: [go, test, ./...]
    cooldown: 5m
```

If any run of the job was created within the `cooldown` (at least `1s`), a queued request returns
that run instead of adding one:

```console
$ noon run tests --detach
tests is cooling down until Fri 16:59:12 (run 4)
```

Foreground and scheduled runs ignore cooldown. It's part of the approval digest. Checks run in this
order: idempotency key, enablement and approval, cooldown, then merging into an already-due run.

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

## Recursion guard

A job that queues a job that queues a job is bounded. Each run stores the caller's `NOON_DEPTH`;
the job and its hooks get that value plus one. Noon refuses a request with code `recursion` when
the depth is 3 or more, or when a job requests itself. Treat this as a safety net, not a design
tool.
