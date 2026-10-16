# Noon design

Contracts and rationale. Backlog.md owns delivery order, and `backlog/decisions/` records why the
major choices were made. When a task's acceptance criteria disagree with this document, the task
wins and updates it. User-facing behavior is documented in the [other docs](../README.md#documentation);
this page records the rules they must keep.

## Product

Cron for repositories and coding agents. Jobs live beside the code, run on your machine or in a
container, leave inspectable history, and never run unattended without your say-so.

Jobs to be done:

- Recurring repo work: tests, dependency and security checks, reports, cleanup.
- Agent prompts on a schedule: PR review, triage, "summarize what changed since the last run".
- Event-driven runs: after a commit, when an agent stops, when CI calls a webhook.
- Later-once work: "in 2h, check CI and fix failures".

| Prior art | Borrow | Refuse |
| --- | --- | --- |
| cron, launchd, systemd timers | Five-field schedules; launchd's single catch-up run after sleep | Per-OS setup, silent failure, no history, minimal-PATH surprises |
| [Supercronic](https://github.com/aptible/supercronic) | Foreground container process, inherited environment, overlap awareness | Crontab as the only format |
| [schedx](https://github.com/Alireza29675/schedx) | Repo manifests, agent profiles, one-shot jobs, JSON everywhere | A per-minute OS timer tick as the execution model |
| [RunWisp](https://github.com/runwisp/runwisp) | Missed-run and overlap policies, bounded logs, honest interrupted runs, validate-first reload | Web dashboard, long-running services, notification-provider catalog |
| [Dagu](https://github.com/dagucloud/dagu) | Declarative jobs, run history, MCP | DAGs and a workflow platform |
| Claude Code scheduled tasks | One catch-up run after sleep, per-task permissions | Vendor lock-in; the app must stay open |
| [pueue](https://github.com/Nukesor/pueue) | Persisted queue, follow/wait ergonomics | General-purpose task queue |
| [herdr-sched](https://github.com/husniadil/herdr-sched), [mcp-cron](https://github.com/jolks/mcp-cron) | Named triggers, cooldowns, durable schedules over MCP | Agents granting themselves authority |
| hum (sibling) | Process groups, start-identity checks, strict YAML, JSON v1 envelopes | Supervising long-running services |

Non-goals: supervising long-running services (use hum), DAGs or job dependencies, a web UI,
multi-host scheduling, a secrets manager, provider SDKs or model credentials, LLM-parsed schedules,
a plugin ABI, a second live job format (crontabs are imported), a container per job, and native
Windows for now. No source code was copied from the references.

## Experience

The CLI is the product.

- Bare `noon` is a dashboard for the current project. It never starts a service or the TUI.
- Commands without a target inspect, list candidates, or print examples. A missing target never
  means "all". `noon serve` runs all enabled projects; `enable` and `disable` act on the current one.
- Human output is compact, uses the project timezone with relative hints ("in 14h"), and ends with
  at most one `Next:` line. Errors are one `noon: MESSAGE` line on stderr, optionally followed by
  `Hint: …`.
- Finishing commands accept `--json` and print exactly one object with `schema_version: 1`.
  Long-running commands (`serve`, `mcp`, `tui`, `logs --follow`) reject it. Errors are
  `{"schema_version":1,"error":{"code":"…","message":"…","hint":"…"}}`. Job output never enters
  JSON stdout; a JSON foreground run streams job output to stderr.
- Exit 0 for success (including empty results), 1 for operational errors, 2 for usage errors. A
  foreground `noon run` exits with the job's code (see [Execution](#execution)).
- No prompts. Noninteractive use never hangs; `noon mcp` on a TTY prints setup help and exits.
- No inference: one job is not "the" job, and a schedule string is never guessed.

Dashboard notices are prioritized: config errors, scheduled projects not enabled, pending
approvals, enabled projects with an offline scheduler, then queued work. It fits 80 columns and
shows at most three. The `Next:` step follows the same order: `noon config check`, `noon enable`,
`noon service install` (or `noon serve` if installed), then a runnable job or `noon add`.

`noon doctor` never executes jobs, repairs, migrates, or creates absent state. SQLite WAL/SHM
sidecars for an existing database are allowed; application data stays read-only.

## Jobs

Settings merge by key across files; jobs merge by name (decision-5). File placement, fields, and
digests are in [Configuration](configuration.md).

- `run` as a YAML string runs with `/bin/sh -c`; as a list it is exact argv. The YAML type decides,
  never punctuation.
- A `.noon.yaml` job replaces the shared job wholesale. `{enabled: false}` alone toggles it off.
  There is no field-level merge.
- Durations combine `s`, `m`, `h`, and `d` (24h), as in `1h30m`.
- Validation never executes anything or checks that commands exist; `noon doctor` does.
- An explicit `--config` file accepts every key; its jobs are shared and need approval.

### Agent prompts

Noon runs existing agent CLIs. It has no model SDK and stores no credentials. Profiles live in user
config because they are machine choices. Prompts are sent byte-for-byte (stdin, or appended as the
last argument) with no templating. Context comes from environment variables, so a prompt can say
"summarize commits since `$NOON_LAST_SUCCESS_AT`" or "read the event in `$NOON_EVENT_FILE`".

### Schedules

`internal/schedule.Parse` returns an immutable schedule with `String`, `Next(after, loc)`, and
`Interval`. `Next` is strictly after its argument. `ParseDuration` accepts nonnegative integer
`s`, `m`, `h`, `d` components and rejects overflow.

Accepted forms are case-insensitive and normalized for display:

| Form | Examples | Meaning |
| --- | --- | --- |
| Cron | `0 9 * * 1-5`, `*/15 * * * *` | Five fields: lists, ranges, steps, `jan`–`dec`, `sun`–`sat`, `0` or `7` = Sunday |
| Macro | `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`; bare `hourly`, `daily`, `weekly`, `monthly`, `yearly` | Cron equivalents |
| Calendar | `every day at 2am`, `daily at 14:30`, `weekdays at 9:30am`, `weekends at noon`, `mon,wed at 18:00`, `every friday at 5pm` | One cron expression. One time: `H[:MM]am/pm`, `H:MM`, `noon`, or `midnight` |
| Interval | `every 15m`, `every 6h`, `every 1d12h`, `every 2 hours` | Elapsed time, whole minutes, `1m` to `365d` |

Cron follows Vixie cron: when day-of-month and day-of-week are both restricted, either matching
is enough, and a field starting with `*` (including `*/2`) counts as unrestricted. `a/n` means
`a-max/n`. Wrap-around ranges (`fri-mon`) and expressions that never match (`0 0 30 2 *`) are
rejected.

Ambiguity is rejected with a suggestion, never guessed:

| Input | Error suggests |
| --- | --- |
| `every day`, `every hour` | `every day at 9am`, `daily` (midnight), or `every 24h` |
| `at 9` | `9am` or `9:00` |
| `every minute` | `* * * * *` or `every 1m` |
| `every 30s`, six-field cron | No seconds; `every 1m` |
| `@every 5m` | `every 5m` |
| `@reboot` | No boot trigger |

Display: cron fields lowercase; macros with `@`; calendar times as `HH:MM`; day lists as
three-letter names Monday-first, a single day as `every friday at 17:00`; intervals largest unit
first (`every 1d12h`).

Intervals keep their phase from when the job was first scheduled or its schedule last changed, and
continue across restarts. Use cron or calendar forms for clock-aligned times.

In the project timezone:

- A local time repeated when clocks fall back runs once, at its first occurrence.
- A local time skipped when clocks spring forward runs once, at the first instant after the gap.
- Intervals are elapsed time and ignore DST.
- `--at` accepts `HH:MM` (next occurrence), `YYYY-MM-DD HH:MM`, or RFC 3339; `--in` takes a duration.

### Missed runs, overlap, and concurrency

The pure `internal/scheduler.Due` and `Dispatch` planners decide; `noon serve` persists each tick's
schedule and dispatch changes atomically, then starts runs. Schedule state is keyed by normalized
schedule text plus timezone, so changing either resets it. Inactive, manual, and removed jobs lose
their state.

- First sight of a job, a schedule change, enabling, approving a changed job, re-adding, and
  resuming all start from now: no catch-up. These commands clear schedule state, so this holds
  even when the scheduler wasn't running.
- An occurrence noticed within 60s is on time. Later, it was missed: `missed: run_once` (default)
  runs once as `catch_up`, never a burst; `missed: skip` records one `missed` run. Both record the
  first missed occurrence. Cron advances to the first occurrence after now; intervals advance by
  whole periods, keeping phase.
- `overlap: skip` (default) records a `skipped` run when the job is already running.
  `overlap: queue` keeps at most one pending run.
- The scheduler starts a run only while fewer than one run is active in its project and fewer than
  four overall, counting foreground runs. The rest wait FIFO as `queued`, at most one due run per
  job. Foreground runs ignore the caps but never overlap the same job.
- A due-now request (`--detach`, MCP, triggers, webhooks) returns the job's existing due queued run
  instead of adding one. `--in` and `--at` one-shots stay independent.
- A queued run more than one hour overdue expires. Queued runs of inactive or removed jobs are
  cancelled instead. Dispatch orders by due time, then run ID. Future one-shots never suppress a
  scheduled occurrence; an already-due queued run does, recording the occurrence as `skipped`.

## Scheduler lifetime

One foreground `noon serve` per state directory runs every enabled project (decision-1). It never
self-daemonizes and is not one process per repo.

- It holds a non-blocking lock and records its PID, process identity, version, start time, and
  heartbeat. A second scheduler fails with `scheduler_running`, naming the owner. A database newer
  than the binary exits 75.
- It polls every second and reloads candidate config files (including absent ones) every two
  seconds. It validates before swapping; an invalid edit keeps the last valid jobs and records the
  error on the project. A heartbeat older than five seconds reads as offline.
- On SIGINT or SIGTERM it stops dispatch, terminates active process groups, and records their runs
  as `interrupted` before releasing the lock.
- Each project loads with the config selection recorded by `noon enable` plus user config.
  `--config`, `--timezone`, `--project`, `--json`, and their environment variables are usage errors.
- `noon service install` registers a launchd agent or systemd user unit. It records `PATH`,
  optional `XDG_CONFIG_HOME`, and the absolute state directory, because service managers otherwise
  run with a minimal `PATH` and a different working directory. Nothing else installs services.
- Docker runs the same process in the foreground. `noon run NAME` needs no scheduler, like `task NAME`.
- There is no IPC socket. Clients read SQLite for scheduler status and next times. Revisit polling
  only if latency or cost becomes a measured problem.
- Queueing (`--detach`, `--in`, `--at`, MCP `run_job`) is one immediate SQLite transaction that
  checks retained idempotency keys, enablement and approval, cooldown, and due-run coalescing
  before inserting.

### Enable and approve

Scheduling unattended commands from repository content is a supply-chain risk, especially when
`git pull` changes it (decision-2).

- `noon enable` registers the canonical checkout and its config selection, and approves the current
  definition of every shared job. It is idempotent, so a container can run
  `noon enable && exec noon serve`. Worktrees and clones are separate checkouts; none is enabled
  implicitly. `noon disable` stops background runs, cancels queued runs, and keeps history.
- `.noon.yaml` and user-config jobs need no approval. Jobs from `noon.yaml` or `--config` run
  unattended only while their digest matches the approved one; a changed job shows `needs approval`.
- The digest covers what runs and when (schedule, action, agent, cwd, env, timeout, overlap, missed,
  cooldown, hooks, webhook), not the description, and not referenced file contents.
- `noon add` enables the project when the new job has a schedule or queued run, and approves only
  that job. Foreground `noon run NAME` is explicit and needs no approval; anything the scheduler
  runs (scheduled, catch-up, queued, webhook) does.

## Execution

- Each run gets its own process group. Cancel and timeout send SIGTERM to the group, wait 10s,
  then SIGKILL. When the main process exits, remaining group members get the same treatment.
- `argv[0]` resolves against the job's `PATH`, or relative to `cwd` when it contains `/`.
- stdin is `/dev/null`, except for stdin-mode prompts. Jobs never get a TTY, so manual and scheduled
  runs behave the same.
- Foreground runs send job stdout to stdout and stderr to stderr; Noon's status lines go to stderr.
  SIGINT, SIGTERM, or SIGHUP cancels the job; a second signal kills the group. `noon cancel RUN`
  requests the same from another process. Owners poll for cancellation every second. If the owner
  is gone, cancellation checks process identity, terminates only a matching group, and records
  `cancelled`.
- A run that can't start is `failed` with `env`-style codes: 127 not found, 126 not executable, 125
  anything else (missing `cwd`, bad prompt file).
- The log interleaves stdout and stderr. The first 1 MiB is written live; after that the last 1 MiB
  is kept in memory and appended after an omission marker at exit, so `--follow` sees the head live
  and the tail at the end.
- Environment: inherited (the scheduler's or your shell's), then job `env`, then `NOON_PROJECT`,
  `NOON_JOB`, `NOON_RUN_ID`, `NOON_TRIGGER`, `NOON_SCHEDULED_AT`, `NOON_LAST_SUCCESS_AT` (start of the
  last successful run), `NOON_DEPTH` (stored request depth + 1), `NOON_AGENT` for prompt jobs, and
  `NOON_EVENT_FILE` when there's a payload. Depth comes from the stored run, not the scheduler's
  environment. Noon never sources shell rc files, creates worktrees, or changes branches.

Log reading ([Command reference](cli.md#logs)):

- Snapshots read one file state. Unflagged CLI reads include an active unterminated line; bounded
  CLI and MCP reads omit it so `next` stays on a line boundary.
- Order: section range ∩ `[after, size)`, then match and context, then `--tail`, then
  `--limit-bytes` (newest whole lines without a cursor, earliest with one; a single oversized line is
  cut on a UTF-8 boundary). Cursors are absolute file offsets.
- The separator newline before a hook marker belongs to the job section. Before a hook is appended,
  `job` is everything so far and `hook` is empty.
- `--follow` waits for a queued run, printing one stderr notice. It checks the owner's PID and start
  identity on each poll, re-reading status after a failed check to avoid racing a normal finish. If
  still active, it prints remaining bytes and exits 1 with `orphaned` and hint
  `Record it: noon cancel ID`. MCP `get_run` returns `orphaned` and ends `wait_seconds` early.
  Inspection never recovers runs, signals processes, or changes state.
- A queued snapshot fails with `not_found` and suggests `--follow`. A terminal run that never
  started says so. Only a missing log for a started run gets the retention hint.

## State

State is per user, not per repository (decision-3). It survives `git clean -fdx`, stays out of
worktrees, and lets one scheduler see every enabled project.

- Location: `$XDG_STATE_HOME/noon`, else `~/.local/state/noon`; override with `NOON_STATE_DIR`,
  `--state-dir`, or user `state_dir`. Directories 0700, files 0600.
- `noon.db` is SQLite (WAL, busy timeout) via `modernc.org/sqlite`. Beside it: `logs/PROJECT_ID/RUN_ID.log`,
  `events/RUN_ID.json`, `locks/`, `scheduler.lock`, and the service's `scheduler.log`.
- Migrations are an append-only list keyed by `PRAGMA user_version`, applied in one transaction. A
  newer database is refused with an upgrade hint. Read-only commands never create state.
- Run `status`: `queued`, `running`, `succeeded`, `failed`, `timed_out`, `cancelled`,
  `interrupted`, `skipped`, `missed`, `expired`. Run `trigger`: `manual`, `schedule`, `catch_up`,
  `queued`, `webhook`.
- `scheduled_at` is the occurrence a scheduled run stands for (the first missed one for `catch_up`
  and `missed`) or a queued run's due time. `idempotency_key` is unique per project and job.
  `cancel_requested_at` is a durable stop request. `depth` is the request's recursion depth
  (scheduled runs start at 0). `requested_by` is claimed identity, metadata only.
- Shared run JSON (CLI history/logs/run, MCP, `last_run`, hook stdin): `id`, `project`, `job`,
  `status`, `trigger`, `digest`, `scheduled_at`, `created_at`, `started_at`, `finished_at`,
  `duration_ms`, `exit_code`, `signal`, `error`, `requested_by`, `idempotency_key`, `due_at`,
  `cancel_requested_at`, `event_file`, `depth`, `log` (`path`, `bytes`, `truncated`, `available`,
  `hook_offset`), and `hook`. Paths are absolute; absent values are null.
- Times are UTC text, `2006-01-02T15:04:05.000Z`, so they sort as strings.
- Retention: newest 50 terminal runs per job; logs and event files for the newest 10. Queued and
  running runs are never pruned. Idempotency keys and delivery IDs deduplicate only against
  retained runs.
- Recovery: a `running` run whose owner is gone becomes `interrupted` and never re-runs. Its group is
  terminated only if the recorded PGID and leader start identity still match. Queued runs that never
  started are safe to dispatch after a restart.

## Integrations

No plugin system; commands are the integration boundary (decision-4).

- **Hooks** ([doc](hooks.md)): `on_success`/`on_failure` get a versioned JSON summary on stdin and
  `NOON_*` variables; 30s timeout; 64 KiB output appended after a marker; no hooks for hooks.
  `NOON_PREVIOUS_STATUS` / `previous_status` come from one snapshot taken before the terminal
  transition releases the running slot: the same project and job's latest retained `succeeded`,
  `failed`, or `timed_out` result by `finished_at` then ID, excluding only the current run. Digest
  changes don't reset it. On lookup error Noon still records the job result and skips the hook.
  Notification policy belongs in wrappers, not the core.
- **Triggers** ([doc](triggers.md)): `noon run NAME --detach --key ID [--data JSON]`. The event file
  is written before the queued run is committed, so dispatch can't race it. Payloads are files,
  never interpolated. Cooldown is checked after key deduplication and requirements, before due-run
  coalescing. Requests at depth 3 or more, or from the same project and job, fail with `recursion`.
  Nothing installs hooks for you.
- **Shell** ([doc](shell.md)): completion only reads config. `--on-enter` detects canonical project
  changes per shell, uses the checkout's recorded config and the normal queue with an atomic pause
  check, and stores directories as data. Nothing edits startup files or the parent environment.
- **Webhooks** ([doc](webhooks.md)): only with `serve --listen`. `POST /hooks/{project_id}/{job}`
  needs an HMAC-SHA256 signature; the secret comes from the scheduler's startup environment.
  Delivery IDs are idempotency keys. Payloads choose data, never commands.
- **Event files**: `schema_version`, `source` (`cli`, `mcp`, `webhook`), `received_at`, `data`, and
  allowlisted `headers` for webhooks. Signature and authorization headers are never stored.
- **MCP** ([doc](agents.md)): stdio, official Go SDK. Tools: `status`, `list_jobs`, `run_job`,
  `list_runs`, `get_run`, `cancel_run`. Agents can't approve, add, or edit unattended commands.
  `noon skill` prints the shell-only fallback unchanged; it's never auto-installed.
- **Crontab import** ([doc](crontab.md)): preview by default; unsupported features fail with line
  numbers.
- Outbound HTTP is a command (`run: [curl, -fsS, …]`), not a built-in action.
- Drafts, not commitments: a bearer-token remote API and MCP over Streamable HTTP on the same
  listener, attachable tmux/Herdr sessions, user-scope jobs, isolated worktree or container runs.

Revisit plugins only when two real integrations need something commands, hooks, and agent profiles
can't express. Even then, prefer `noon-NAME` subprocesses speaking versioned JSON over Go plugins.

## Security model

| Input | Trust | Consequence |
| --- | --- | --- |
| User config, `.noon.yaml` | Yours | Runs unattended once the project is enabled |
| `noon.yaml`, `--config` file | Repository or operator content | Unattended runs need the approved digest; manual runs are explicit |
| Scripts and prompt files | Not pinned | Approval covers what and when, not file contents |
| MCP clients | Inspect and queue approved jobs | Can't approve, add, or edit commands; identity is metadata |
| Webhooks | Authenticated data | Never select or build commands; payload is a file |
| Job and hook output | Untrusted bytes | Stored raw and bounded; never interpreted |
| Environment values | Secret by default | Never printed by list, status, JSON, or MCP |

Approval is not a sandbox. An agent with your shell can do anything you can; isolation is the
container's or OS's job.

## Architecture

```text
cmd/noon ─▶ internal/cli ─┐
internal/mcp ─────────────┼─▶ internal/app ─▶ internal/config     (files, jobs, digests; reads only)
internal/httpapi ─────────┤                 ├▶ internal/schedule   (pure grammar and next-time math)
internal/tui ─────────────┘                 ├▶ internal/store      (SQLite, logs, locks, recovery)
                                            ├▶ internal/runner     (processes, env, timeouts, capture)
                                            └▶ internal/scheduler  (pure planner + serve loop)
internal/service (launchd/systemd), internal/cronimport (crontab → jobs)
```

- `internal/app` holds operations every client shares and returns plain structs that CLI JSON and
  MCP both serialize. Adapters parse and render; they hold no rules.
- Only the serve loop and command entry points read the clock; everything else takes time as an
  argument.
- Dependencies: urfave/cli v3, `go.yaml.in/yaml/v3`, `modernc.org/sqlite`, `golang.org/x/sys`,
  `github.com/modelcontextprotocol/go-sdk` (MCP), `charm.land/bubbletea/v2` (TUI). No cron library:
  schedule semantics are ours to test. No fsnotify: polling works across editors and bind mounts.
  No HTTP framework: `net/http`.

## Docker

Same binary, same foreground model. The image is non-root with CA roots, a shell, and embedded
timezone data; it is not an agent sandbox. Mount the repository read-only and state on a volume,
enable explicitly, and run `noon serve` under an init process so orphans are reaped. No Docker
socket or host agent credentials by default. See [Docker](docker.md).
