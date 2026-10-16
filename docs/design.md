# Bump design

Status: contracts for work that is mostly **not implemented**. README.md lists what works today.
Backlog.md owns delivery order; `backlog/decisions/` records why the major choices were made. When a
task's acceptance criteria and this document disagree, the task wins and updates this document.

## Product

Bump is cron for repositories and coding agents. Jobs live beside the code, run on your machine or
in a container, leave inspectable history, and never run unattended without your say-so.

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

Non-goals: long-running service supervision (use hum), DAGs or job dependencies, a web UI,
distributed or multi-host scheduling, a secrets manager, provider SDKs or model credentials in the
core, LLM-parsed schedules, a plugin ABI or marketplace, a live second job format (crontabs are
imported), launching a container per job, and native Windows for now. References informed the
design; no source code was copied.

## Experience

The CLI is the product. Rules:

- Bare `bump` is a dashboard for the current project, never a service start or a TUI.
- Every command is valid without arguments: it inspects, lists candidates, or shows copy-paste
  examples. A missing target never means "all", and never mutates.
- Human output is compact, uses the project timezone with relative hints ("in 14h"), and ends with
  at most one "Next:" line. An error is one `bump: MESSAGE` line on stderr, optionally followed by
  one `Hint: ...` line with the fix.
- `--json` works on every command that finishes: exactly one object on stdout,
  `schema_version: 1`. Long-running commands (`serve`, `mcp`, `tui`, `logs --follow`) reject
  `--json` as a usage error. Errors are
  `{"schema_version":1,"error":{"code":"…","message":"…","hint":"…"}}`. Job output never mixes into
  JSON stdout; in JSON mode a foreground run streams job output to stderr.
- Exit codes: 0 success (including empty results and guidance), 1 operational error, 2 usage error.
  A foreground `bump run` exits with the job's code instead: 124 timeout, 130 cancelled, 128+N
  killed by signal N, and 125-127 when it could not start (see Execution).
- No prompts. Noninteractive use never hangs; `bump mcp` on a TTY prints setup help and exits.
- No surprising inference: one job is not "the" job, and a schedule string is never guessed at.

First run, the experience every task protects:

```console
$ bump add review "weekdays at 9am" --agent claude --prompt "Review open PRs and list risks"
Added review to .bump.yaml (private) · weekdays at 09:00 · next Fri 09:00 (in 14h)
Scheduling enabled for ~/code/app. The scheduler isn't running, so nothing runs in the background.
Next: bump service install   (once per machine)  ·  bump run review   (run it now)

$ bump
app · ~/code/app · scheduler running · America/Denver
JOB      SCHEDULE            NEXT          LAST
review   weekdays at 09:00   Fri 09:00     ✓ 2h ago (3m12s)
tests    every day at 02:00  tomorrow      ✗ exit 1 · 22h ago
deploy   manual              —             never
1 shared job changed since you enabled this project. Next: bump enable
```

| Command | Bare behavior | With arguments |
| --- | --- | --- |
| `bump`, `bump status` | Dashboard | — |
| `bump list`, `ls` | All jobs, next runs, state | — |
| `bump run` | Runnable jobs + examples | `NAME` foreground; `--detach`, `--in`, `--at` queue |
| `bump add` | Examples | `NAME [SCHEDULE] -- CMD…`, `--sh`, `--prompt`, `--prompt-file` |
| `bump remove`, `rm` | Candidates | Remove a job from the file that defines it |
| `bump pause`, `resume` | Candidates | Pause or resume background runs of a job |
| `bump cancel` | Active runs | Cancel a run, or a job's active and queued runs |
| `bump logs` | Latest run in project | `NAME` or `RUN`; `--follow` |
| `bump history` | Recent runs | `NAME`; `--limit` |
| `bump enable`, `disable` | Acts on current project | — |
| `bump serve` | Foreground scheduler for enabled projects | `--listen ADDR` for webhooks |
| `bump service` | Service status | `install`, `uninstall` |
| `bump doctor` | Diagnose setup | — |
| `bump init`, `config` | Implemented today | — |
| `bump import crontab` | Usage and example | `FILE` or `-`; `--write` |
| `bump mcp` | stdio MCP server | — |
| `bump tui` | Terminal UI | — |

Global flags work before or after subcommands: `-C/--project`, `--config`, `--timezone`,
`--state-dir`, `--json`.

## Jobs and configuration

Semantics are decided in decision-5. Files merge by setting (see [configuration](configuration.md));
jobs merge by name. Where keys are allowed:

| Key | User config | `bump.yaml` | `.bump.yaml` |
| --- | --- | --- | --- |
| `version`, `timezone` | yes | yes | yes |
| `state_dir` | yes | no | no |
| `jobs` | no (draft: user-scope jobs) | yes | yes |
| `agents` | yes | no | no |
| `default_agent` | yes | no | yes |
| `on_success`, `on_failure` defaults | yes | no | no |

A key outside its allowed files is a validation error naming the file and the allowed location. An
explicit `--config` file replaces all three and accepts every key, so one file can describe a
container deployment; its jobs are shared jobs that need approval. `bump add`, `remove`, and
`import --write` refuse to edit an explicit file; edit it directly.

Job paths (`cwd`, `prompt_file`) are relative to the project root. Setting paths (`state_dir`) are
relative to the file that sets them.

```yaml
version: 1
timezone: America/Denver
jobs:
  tests:
    description: Nightly full test suite
    schedule: every day at 2am
    run: task test                     # string: /bin/sh -c
  deps:
    schedule: "0 6 * * 1"
    run: [./scripts/check-deps, --md]  # list: exact argv, no shell
    timeout: 10m
  review:
    schedule: weekdays at 9am
    prompt_file: prompts/review.md
    agent: claude
  release-notes:                       # no schedule: manual only
    run: ./scripts/release-notes
```

| Field | Type | Default | Contract |
| --- | --- | --- | --- |
| `description` | string | — | Shown by list, TUI, MCP |
| `schedule` | string | manual only | Grammar below |
| `run` | string or list | — | String runs via `/bin/sh -c`; list is exact argv. Decided by YAML type, never by punctuation |
| `prompt` | string | — | Inline agent prompt, ≤ 64 KiB |
| `prompt_file` | path | — | Relative to project root; read at run time, ≤ 64 KiB |
| `agent` | string | `default_agent` | Agent profile for prompt jobs; one of the two is required |
| `cwd` | path | project root | Relative to project root |
| `env` | map | `{}` | Literal strings; names `[A-Za-z_][A-Za-z0-9_]*`; `BUMP_*` reserved; values never printed |
| `timeout` | duration or `none` | `1h` | Whole run, at least `1s`; then terminate |
| `overlap` | `skip`, `queue` | `skip` | Queue holds at most one pending run |
| `missed` | `run_once`, `skip` | `run_once` | Behavior after sleep or downtime |
| `enabled` | bool | `true` | Declarative disable |
| `cooldown` | duration | none | Minimum gap between event-triggered runs (see Integrations) |
| `on_success`, `on_failure` | string or list | user defaults | Lifecycle hooks; an empty list disables an inherited default |
| `webhook` | mapping | — | Inbound endpoint opt-in (see Integrations) |

Exactly one action field: `run`, `prompt`, or `prompt_file`. Durations use `s`, `m`, `h`, and `d`
(24h) units, combined as in `1h30m`. Job names match `^[a-z][a-z0-9_-]{0,63}$`; because they
start with a letter, a bare number on the command line always means a run ID. Unknown fields,
duplicate keys, and wrong types fail with file, YAML path, and line. Validation never executes
anything or checks that commands exist; `bump doctor` does.

Local overrides replace a whole named job: `.bump.yaml` may redefine `tests` completely, or
disable it with only `tests: {enabled: false}`. There is no field-level merge of execution settings.
List output records each job's source file and whether it overrides a shared definition.

### Agent prompts

Bump runs existing agent CLIs; it has no model SDK and stores no provider credentials. Profiles
live in user config because they are machine choices:

```yaml
# ~/.config/bump/config.yaml
default_agent: claude
agents:
  claude:
    command: [claude, -p, --permission-prompts, none, --permission-mode, acceptEdits]
    prompt: stdin        # stdin (default) or argument (appended as the last argv entry)
  gemini:
    command: [gemini, -p]
    prompt: argument
```

Built-in profiles, overridable by name: `claude` → `claude -p --permission-prompts none` and
`codex` → `codex exec -`, both with the prompt on stdin. Unattended runs must never wait for
permission prompts. The prompt is passed verbatim, with no templating. Context comes from
environment variables (below), so a prompt can say "summarize commits since
`$BUMP_LAST_SUCCESS_AT`" or "read the event in `$BUMP_EVENT_FILE`".

### Schedules

The pure `internal/schedule` parser, job configuration validation, and CLI next-run
previews are implemented; actual scheduling and execution are not. `Parse` returns an immutable
schedule with normalized `String`, `Next(after, loc)`, and `Interval` methods. `Next` is strictly
after the supplied instant; interval phase remains the planner's responsibility. `ParseDuration`
accepts nonnegative integer components in `s`, `m`, `h`, and `d`, rejecting overflow.

Accepted forms, case-insensitive, normalized for display:

| Form | Examples | Meaning |
| --- | --- | --- |
| Cron | `0 9 * * 1-5`, `*/15 * * * *` | Five fields: lists, ranges, steps, `jan`–`dec`, `sun`–`sat`, `0` or `7` = Sunday |
| Macro | `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`, also bare `hourly`, `daily`, `weekly`, `monthly`, `yearly` | Cron equivalents; `@reboot` is rejected |
| Calendar | `every day at 2am`, `daily at 14:30`, `weekdays at 9:30am`, `weekends at noon`, `mon,wed at 18:00`, `every friday at 5pm` | Compiles to one cron expression. One time per schedule: `H[:MM]am`/`pm`, 24-hour `H:MM`, `noon`, or `midnight` |
| Interval | `every 15m`, `every 6h`, `every 1d12h`, `every 2 hours` | Elapsed time in whole minutes, from `1m` to `365d` |

Cron matching follows Vixie cron: when day-of-month and day-of-week are both restricted, a day
matches if either matches; a field starting with `*` (including `*/2`) counts as unrestricted for
that rule. `a/n` means `a-max/n`. Wrap-around ranges such as `fri-mon` are rejected. Expressions that
can never match, such as `0 0 30 2 *`, are rejected. Ambiguous phrases are rejected with a
suggestion rather than guessed: `every day` and `every hour` (clock-aligned or elapsed?), and a
bare `at 9` (`9am` or `9:00`?). For example: "`every day` needs a time: try `every day at 9am`,
`daily` (midnight), or `every 24h`". `every minute` suggests `* * * * *` or `every 1m`;
`every 30s` suggests `every 1m`. Six-field cron is rejected because Bump has no seconds field;
`@every 5m` suggests `every 5m`, and `@reboot` explains that Bump has no boot trigger.

Display keeps five cron fields lowercase and macro names prefixed with `@`. Calendar times
use `HH:MM`; day lists use three-letter names in Monday-to-Sunday order, with a single day
shown as `every friday at 17:00`. Intervals use largest units first (`every 1d12h`).

Intervals keep their phase from when the job was first scheduled, or its schedule last changed, and
continue across restarts. Use cron or calendar forms for clock-aligned times.

Wall-clock rules in the project timezone:

- A local time that occurs twice when clocks fall back runs once, at its first occurrence.
- A local time skipped when clocks spring forward runs once, at the first instant after the gap.
- Intervals are elapsed durations and ignore DST.
- One-shot times (`--at`) accept `HH:MM` (next occurrence), `YYYY-MM-DD HH:MM` (project timezone),
  or RFC 3339; `--in` takes an interval duration.

### Missed runs, overlap, and concurrency

- First sight of a job, a schedule change, enabling, approving a changed job, re-adding, or
  resuming starts from "now": no catch-up. Commands that reactivate a job clear its schedule
  state, so this holds even when the scheduler was not running at the time.
- An occurrence noticed within 60s of its time is on time. Later than that, it was missed (sleep,
  downtime): `missed: run_once` (default) runs once as `catch_up`, never a burst, and
  `missed: skip` records one `missed` entry. Either way the record carries the first missed
  occurrence, and the next occurrence is computed from now.
- `overlap: skip` (default) records a `skipped` run when the job is already running.
  `overlap: queue` keeps at most one pending run.
- The scheduler starts a run only while fewer than one run is active in its project and fewer than
  four in total, counting foreground runs; the rest wait in FIFO order as `queued`, at most one due
  run per job. Foreground manual runs ignore these caps but never overlap the same job.
- A due-now request (`--detach`, MCP, triggers, webhooks) for a job that already has a due queued
  run returns that run instead of adding another. One-shots for later (`--in`, `--at`) each keep
  their own run.
- Queued runs expire one hour after they were due if they never start.

## Scheduler lifetime

Decided in decision-1. Bump needs a long-running scheduler for overlap, catch-up, queues, events,
webhooks, and live status. It does **not** self-daemonize, and it is **not** one process per repo.

- `bump serve` is one foreground scheduler for every enabled project in its state directory. A
  second scheduler on the same state refuses, naming the owner's PID and start time.
- `bump service install` (once per machine) registers a launchd agent or systemd user unit that
  runs `bump serve`. It records the installer's `PATH` and resolved state directory explicitly,
  because service managers otherwise run jobs with a minimal `PATH` and a different working
  directory. Nothing else installs or starts services.
- Docker runs the same `bump serve` as the container's foreground process.
- `bump run NAME` executes in the foreground without any scheduler, like `task NAME`.
- The scheduler loads each enabled project with the configuration selection recorded by
  `bump enable` (automatic files, or the explicit `--config` file) plus user config. It rejects
  `--config`, `--timezone`, `--project`, and their environment variables as usage errors.
- An invalid config edit keeps the project's last valid jobs running and records the error, which
  status and doctor show until the file is fixed.
- There is no IPC socket. CLI, MCP, HTTP, and TUI clients read and write the SQLite store; the
  scheduler polls it every second for queued runs and cancellations, and polls config files every
  two seconds, including files that do not exist yet. Revisit only if latency or polling cost
  becomes a measured problem.
- Commands that need the scheduler (queued runs, MCP `run_job`) still succeed while it is offline,
  record durable intent, and say plainly that nothing will run until a scheduler serves the project.

### Enable and approve

Decided in decision-2. Scheduling unattended commands from repository content is a supply-chain
risk, especially when `git pull` changes it.

- `bump enable` registers this checkout (canonical path) and its configuration selection, and
  approves the current definition of every job that needs approval. It is idempotent, so a
  container entrypoint can run `bump enable && exec bump serve` to trust what it deploys.
  Worktrees and clones are separate checkouts; none is enabled implicitly. `bump disable` stops
  background runs, cancels queued runs, and keeps history.
- Jobs defined in `.bump.yaml` and user config are yours and need no approval. Jobs from
  `bump.yaml` or an explicit `--config` file run unattended only while their definition digest
  matches the approved digest; a changed job waits with state `needs approval`.
- A digest covers every field that affects what runs or when (schedule, action, agent, cwd, env,
  timeout, overlap, missed, cooldown, hooks, webhook), not description, and not the contents
  of scripts or prompt files it references. Say so in docs: enable only repositories whose contents
  you would also run by hand.
- `bump add` enables the project when the new job has a schedule or a queued run, and approves only
  the job it writes. Foreground `bump run NAME` is an explicit act and needs no approval; anything
  the scheduler executes (scheduled, catch-up, queued, webhook) does.

## Execution

- Each run gets its own process group. Cancel and timeout send SIGTERM to the group, wait 10s, then
  SIGKILL. When the main process exits, remaining group members are terminated the same way.
- `argv[0]` resolves against the job environment's `PATH`, relative to `cwd` when it contains `/`.
- stdin is `/dev/null`, except a stdin-mode prompt. Jobs never get a TTY, so manual and scheduled
  runs behave the same.
- The log combines stdout and stderr in arrival order. The first 1 MiB is written live; output past
  that is kept in memory as the last 1 MiB and appended after an omission marker when the run ends.
  `logs --follow` therefore streams the first 1 MiB live and the tail at the end.
- A foreground run streams the job's stdout to stdout and stderr to stderr, so
  `bump run report > report.md` works; Bump's own status lines go to stderr. SIGINT, SIGTERM, or
  SIGHUP to `bump run` cancels the job as above; a second signal kills the group immediately.
- A run that cannot start records a `failed` run and a message, using `env`-style exit codes: 127
  command not found, 126 not executable, 125 any other start failure (missing `cwd`, unreadable
  prompt file).
- Environment: inherited (the scheduler's for background runs, your shell's for manual runs), then
  job `env`, then `BUMP_PROJECT`, `BUMP_JOB`, `BUMP_RUN_ID`, `BUMP_TRIGGER`, `BUMP_SCHEDULED_AT`,
  `BUMP_LAST_SUCCESS_AT` (start of the last successful run), and `BUMP_EVENT_FILE` when a run has a
  payload. Bump never sources shell rc files, creates worktrees, or changes branches.

## State

Decided in decision-3. State is per user, not per repository: it survives `git clean -fdx`, never
pollutes worktrees, and lets one scheduler see every enabled project.

- Location: `$XDG_STATE_HOME/bump`, else `~/.local/state/bump`; override with `BUMP_STATE_DIR`,
  `--state-dir`, or user-config `state_dir`. Directories are 0700, files 0600.
- `bump.db` is SQLite (WAL, busy timeout) via `modernc.org/sqlite`. `logs/PROJECT_ID/RUN_ID.log`,
  `events/RUN_ID.json`, `locks/`, `scheduler.lock`, and the service's `scheduler.log` sit beside it.
- Migrations are an append-only list keyed by `PRAGMA user_version`, applied in one transaction. A
  database newer than the binary is refused with an upgrade hint; the scheduler exits 75 so its
  service manager restarts it with the new binary. Read-only commands never create state.
- Projects and runs have integer IDs. Run `status`: `queued`, `running`, `succeeded`, `failed`,
  `timed_out`, `cancelled`, `interrupted`, `skipped`, `missed`, `expired`. Run `trigger`: `manual`,
  `schedule`, `catch_up`, `queued`, `webhook`. `scheduled_at` is the occurrence a scheduled run
  stands for (the first missed one for `catch_up` and `missed`) or the due time of a queued run.
  `requested_by` records claimed client identity as metadata only.
- Times are stored as UTC text, `2006-01-02T15:04:05.000Z`, so they sort as strings.
- Retention: the newest 50 terminal runs per job; logs and payloads for the newest 10 of them.
  Queued and running runs are never pruned. Idempotency keys and webhook delivery IDs deduplicate
  only against retained runs.
- Recovery: a `running` run whose owner process is gone becomes `interrupted` and is never re-run.
  Its process group is terminated only when the recorded PGID and leader start identity still match.
  Queued runs that never started are safe to dispatch after a restart.

## Integrations

Decided in decision-4: no plugin system. Commands are the integration boundary.

- **Hooks**: `on_success` and `on_failure` commands get a versioned JSON run summary on
  stdin plus `BUMP_*` variables. 30s timeout, 64 KiB captured output, recorded on the run, no hooks
  for hook failures. Desktop notifications and chat posts are examples, not built-ins.
- **Triggers**: Git hooks and agent hooks call `bump run NAME --detach --key ID [--data JSON]`.
  Payloads are files, never interpolated. Cooldowns, deduplication, coalescing, and a recursion
  guard (depth carried through queued runs) bound noisy sources. Nothing installs hooks for you.
- **Webhooks**: `bump serve --listen ADDR` opt-in, loopback by default, per-job `webhook:` opt-in,
  HMAC signatures, delivery deduplication, body limits. Payloads choose data, never commands.
- **Event files**: one JSON object for every payload with keys `schema_version` (1), `source`
  (`cli` or `webhook`), `received_at`, `data`, and, for webhooks only, `headers`. `data` is the
  JSON payload, or a string when a webhook body is not JSON.
- **MCP**: `bump mcp` over stdio with the official Go SDK. Inspect, queue approved jobs, read logs,
  cancel. Agents cannot approve, add, or edit unattended commands.
- **Crontab import**: preview YAML from a user crontab; `--write` adds the jobs to `.bump.yaml`.
  Unsupported cron features fail with line numbers.
- Outbound HTTP calls are commands (`run: [curl, -fsS, ...]`), not a built-in action.
- Drafts, not commitments: a bearer-token remote API and MCP over Streamable HTTP on the same
  listener, attachable tmux/Herdr sessions, user-scope jobs, and isolated worktree or container
  runs for agents.

Revisit the plugin decision only when two real integrations need something commands, hooks, and
agent profiles cannot express. Even then, prefer `bump-NAME` subprocess executables speaking
versioned JSON over Go plugins.

## Security model

| Input | Trust | Consequence |
| --- | --- | --- |
| User config, `.bump.yaml` | Yours | Runs unattended once the project is enabled |
| `bump.yaml`, `--config` file | Repository or operator content | Unattended runs need the approved digest; manual runs are explicit |
| Scripts and prompt files | Not pinned | Approval covers what and when, not file contents |
| MCP clients | Inspect and queue approved jobs | Cannot approve, add, or edit commands; identity is metadata |
| Webhooks | Authenticated data | Never select or build commands; payload is a file |
| Job and hook output | Untrusted bytes | Stored raw and bounded; never interpreted |
| Environment values | Secret by default | Never printed by list, status, JSON, or MCP |

Approval is not a sandbox. An agent with your shell can do anything you can; isolation is the
container's or OS's job.

## Architecture

```text
cmd/bump ─▶ internal/cli ─┐
internal/mcp ─────────────┼─▶ internal/app ─▶ internal/config     (files, jobs, digests; no I/O beyond reads)
internal/httpapi ─────────┤                 ├▶ internal/schedule   (pure grammar and next-time math)
internal/tui ─────────────┘                 ├▶ internal/store      (SQLite, logs, locks, recovery)
                                            ├▶ internal/runner     (processes, env, timeouts, capture)
                                            └▶ internal/scheduler  (pure planner + serve loop)
internal/service (launchd/systemd), internal/cronimport (crontab → jobs)
```

- `internal/app` holds operations every client shares and returns plain structs that CLI JSON and
  MCP structured content both serialize. Adapters parse input and render; they hold no rules.
- `internal/schedule` and the planner are pure and take time as an argument; nothing reads the
  clock except the serve loop and command entry points.
- Decided dependencies: urfave/cli v3, `go.yaml.in/yaml/v3`, `modernc.org/sqlite`,
  `golang.org/x/sys`, `github.com/modelcontextprotocol/go-sdk` (MCP only),
  `charm.land/bubbletea/v2` (TUI only). No cron library: schedule semantics are ours to test. No
  fsnotify: polling is simpler across editors and bind mounts. No HTTP framework: `net/http`.

## Docker

The same binary and foreground model. The image is non-root with CA roots, a shell, and embedded
timezone data; it is not an agent sandbox. Mount the repository read-only and state on a volume,
enable the project explicitly, and run `bump serve` with an init process (`init: true`) so orphaned
grandchildren are reaped. No Docker socket and no host agent credentials by default. Extend the image
with the tools your jobs need. See [Docker](docker.md).
