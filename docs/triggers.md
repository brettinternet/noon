# Triggers

Git hooks, agent hooks, and scripts can queue a configured job with JSON context. Noon stores the
payload before a queued run becomes visible to `noon serve`; the job reads the resulting event-file
path from `NOON_EVENT_FILE`. Payload data never becomes a command or shell fragment.

A job may set a minimum one-second `cooldown`:

```yaml
jobs:
  tests:
    run: [go, test, ./...]
    cooldown: 5m
```

Cooldowns are included in the job digest and apply only to queued requests. Idempotency-key retries
are resolved first, then enablement/approval requirements, cooldown, and due-now coalescing are
checked in that order. Foreground and scheduled runs ignore cooldowns. CLI payloads may be a literal
JSON value, `@FILE`, or `-` for stdin, up to 256 KiB. For example:

```sh
noon run tests --detach --data '{"commit":"abc123"}'
noon run tests --detach --data @event.json
printf '%s\n' '{"sha":"abc123"}' | noon run tests --detach --data -
```

Every payload event file is JSON with `schema_version: 1`, `source`, `received_at`, and `data`.
CLI runs use `source: "cli"`, MCP `run_job` payloads use `source: "mcp"`; the job receives the
absolute file name in `NOON_EVENT_FILE`. Event files are private and retained with the newest ten
logs per job. MCP clients may pass the same JSON value with the optional `data` argument to
`run_job`.

## Git hooks

Create each hook file yourself; Noon never installs hooks. For a commit trigger, create
`.git/hooks/post-commit` and make that file executable:

```sh
#!/bin/sh
exec noon run tests --detach --key "$(git rev-parse HEAD)"
```

For a successful merge, create `.git/hooks/post-merge` and make that file executable:

```sh
#!/bin/sh
exec noon run tests --detach --key "merge-$(git rev-parse HEAD)" --data '{"event":"post-merge"}'
```

The commit or merge ID is used only as the idempotency key; it is not expanded into the job's
command. Git runs these hooks in the checkout. Queueing requires that checkout to be enabled with
`noon enable` and requires `noon serve` (or its explicitly installed service) to dispatch the run.

## Claude Code `Stop` hook

Create `.claude/settings.json` in the project; this example uses Claude Code's documented
`Stop` event and command-handler format. Claude Code sends the hook's JSON input to the command on
stdin, which `--data -` stores as the event's `data` value:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "noon run tests --detach --data -"
          }
        ]
      }
    ]
  }
}
```

## Codex `Stop` hook

Codex also supports command hooks in `.codex/hooks.json`. This current documented hook-file format
uses event names, optional matcher groups, and command handlers; `Stop` has no matcher here. Codex
sends each command hook a JSON object on stdin. Its `Stop` handler requires JSON on stdout, so
send Noon's status output to stderr and return an empty JSON object on success. Review and trust
this hook in Codex before using it:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "noon run tests --detach --data - >&2 && printf '{}\\n'"
          }
        ]
      }
    ]
  }
}
```

Recursion is bounded across detached runs: each run stores the requesting process's `NOON_DEPTH`,
and its job and lifecycle hooks receive that stored value plus one. A request at depth three or
greater, or a request for the same project and job as the calling process, fails with code
`recursion`. Keep hooks narrow and rely on this guard as a safety limit, not as a substitute for
reviewing their behavior.

Documentation verified on 2026-10-16 against the current vendor references:

- Claude Code hooks: <https://code.claude.com/docs/en/hooks> (project settings, `Stop` handlers,
  and JSON stdin input).
- Codex hooks: <https://developers.openai.com/codex/hooks> (`hooks.json`, `Stop`, command handler,
  and JSON stdin input).
