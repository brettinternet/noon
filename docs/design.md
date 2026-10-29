# Noon design

Contracts and rationale. `bl` owns delivery order, and `docs/decisions/` records why the
major choices were made. When a task's acceptance criteria disagree with this document, the task
wins and updates it. User-facing behavior is documented in the [other docs](../README.md#documentation);
this page records the rules they must keep.

## Product

Cron for repositories and coding agents. Jobs live beside the code, run on your machine or in a
container, leave inspectable history, and never run unattended without your say-so.

Jobs to be done:

- Recurring repo work: tests, dependency and security checks, reports, cleanup.
- Personal work with no repository: machine chores, cross-repository reports, and imported crontabs.
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
  at most one `Suggested action:` line. Errors are one `noon: MESSAGE` line on stderr, optionally followed by
  `Hint: …`.
- Finishing commands accept `--json` and print exactly one object with `schema_version: 1`.
  Long-running commands (`serve`, `mcp`, `tui`, `logs --follow`) reject it. Errors are
  `{"schema_version":1,"error":{"code":"…","message":"…","hint":"…"}}`. Job output never enters
  JSON stdout. Help, version, and completion stay text. Unknown or extra group arguments are
  usage errors; a unique single-typo match may be suggested, never executed.
- Exit 0 for success (including empty results), 1 for operational errors, 2 for usage errors. A
  foreground `noon run` exits with the job's code (see [Execution](#execution)).
- No prompts. Noninteractive use never hangs; `noon mcp` on a TTY prints setup help and exits.
- No inference: one job is not "the" job, and a schedule string is never guessed.

Dashboard priority notices (at most three, 80 columns) and the `Suggested action:` share one
priority. A temporary-schedule summary is also shown when present:

| Notice | Suggested action |
| --- | --- |
| Config error | `noon config check` |
| Scheduled project not enabled; pending approvals | `noon enable` |
| Enabled project, scheduler offline | `noon service install` (`noon serve` if installed) |
| Orphaned runs, each with `noon cancel ID`; they block the project | `noon cancel` lowest orphaned ID |
| Queued work | An unpaused runnable job, or `noon add` |

`noon doctor` never executes jobs, repairs, migrates, or creates absent state. SQLite WAL/SHM
sidecars for an existing database are allowed; application data stays read-only.

## Distribution

Stable release archives target macOS and Linux on amd64 and arm64, are named
`noon-v<VERSION>-<macos|linux>-<x64|arm64>.tar.gz`, and contain only `bin/noon`. Each release
includes a SHA-256 `checksums.txt`; version, full commit SHA, and UTC build time are embedded in the
binary. Homebrew formula updates consume the checksums from the published release, and installing
or upgrading the binary never installs or starts the scheduler service.

## Jobs

Settings merge by key across files; jobs merge by name (decision-5). File placement, fields, and
digests are in [Configuration](configuration.md).

- `run` as a YAML string runs with `/bin/sh -c`; as a list it is exact argv. The YAML type decides,
  never punctuation.
- `noop_exit_code` is an optional integer from 1–124 on `run` jobs only, and is included in the
  execution digest. Without it, every nonzero exit fails as usual.
- A private `.git/noon.yaml` or `.git/noon.d/*.yaml` job replaces the shared job wholesale.
  `{enabled: false}` alone toggles a shared or fragment job off. There is no field-level merge.
  Private job names are unique across private files, so a name collision is an error rather than a
  silent override that depends on file order.
- Optional `os` and `arch` lists constrain execution to supported Go platforms (`darwin`/`linux`,
  `amd64`/`arm64`). Both fields participate in approval digests. A nonmatching job stays visible but
  is inert: it has no schedule or dispatch eligibility, and foreground and queued requests fail
  with `unsupported_platform`.
- The user project is the canonical `$XDG_CONFIG_HOME/noon` directory (else `~/.config/noon`). Its jobs come only from
  its `config.yaml`; repository manifests, local Git config, and files above it are never loaded. `--user` and `-C` to
  that directory select the same project. Normal repository loads validate user config but never include its jobs.
- Optional top-level `vars_file` values are document-local and resolve only the allowlisted job fields before validation.
  Missing files provide no values for literal `:-` defaults. The process environment, job `env`, and runtime `env_file`
  are never interpolation sources; vars-file values are not exported. See [Configuration](configuration.md#config-time-variables)
  for the grammar and security rules.
- User jobs are private (`shared: false`, no approval), and their root/default `cwd` plus relative `cwd` and prompt paths
  use the user project directory.
- Durations combine `s`, `m`, `h`, and `d` (24h), as in `1h30m`.
- Validation never executes anything or checks that commands exist; `noon doctor` does.
- An explicit `--config` file accepts every key; its jobs are shared and need approval.

### Agent prompts

Noon runs existing agent CLIs. It has no model SDK and stores no credentials. Profiles live in user
config because they are machine choices. Prompts are sent byte-for-byte (stdin, or appended as the
last argument) with no templating. A profile may provide `session_id_flag`; Noon then creates and
records a random v4 UUID when execution starts, passes it after the profile command, and sets
`NOON_AGENT_SESSION_ID`. This is a requested ID, not proof that the agent honored it.

A prompt job may set `agent_args` to a non-empty list of literal argv strings; it must name `agent`
explicitly rather than inherit the machine's `default_agent`. Effective argv order is profile
`command`, requested session flag and ID, job `agent_args`, profile `prompt_args`, then an argument
delivered prompt. `prompt_args` holds profile-specific trailing flags or separators; built-ins use
it to keep their existing effective argv. Changes to `agent_args` change the shared job's execution
digest and require approval again. Approval is not a sandbox: arguments can widen permissions just
as `run` can, and arguments are visible configuration, not a secret channel. Context comes from
environment variables, so a prompt can say
"summarize commits since `$NOON_LAST_SUCCESS_AT`" or "read the event in `$NOON_EVENT_FILE`".

### Schedules

`internal/schedule.Parse` returns an immutable schedule with `String`, `Next(after, loc)`, and
`Interval`. `Next` is strictly after its argument. `ParseDuration` accepts nonnegative integer
`s`, `m`, `h`, `d` components and rejects overflow.

Accepted forms are case-insensitive and normalized for display:

| Form | Examples | Meaning |
| --- | --- | --- |
| Cron | `0 9 * * 1-5`, `*/15`, `*/15 1-5` | One to five fields; omitted trailing fields default to `*`. Lists, ranges, steps, `jan`–`dec`, `sun`–`sat`, `0` or `7` = Sunday |
| Macro | `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`; bare `hourly`, `daily`, `weekly`, `monthly`, `yearly` | Cron equivalents |
| Calendar | `every day at 2am`, `daily at 14:30`, `weekdays at 9:30am`, `weekends at noon`, `mon,wed at 18:00`, `every friday at 5pm` | One cron expression. One time: `H[:MM]am/pm`, `H:MM`, `noon`, or `midnight` |
| Interval | `every 15m`, `every 6h`, `every 1d12h`, `every 2 hours` | Elapsed time, whole minutes, `1m` to `365d` |

Cron fields are minute, hour, day-of-month, month, and day-of-week, in that order.
`*/15` expands to `*/15 * * * *`; `*/15 1-5` expands to `*/15 1-5 * * *` (01:00–05:45).
A bare `5` means minute 5 of every hour, not every five minutes. Empty schedules and more than
five fields are rejected. Crontab import still requires all five fields before the command.

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

Display: cron expanded to five lowercase fields; macros with `@`; calendar times as `HH:MM`; day lists as
three-letter names Monday-first, a single day as `every friday at 17:00`; intervals largest unit
first (`every 1d12h`).

Intervals keep their phase from when the job was first scheduled or its schedule last changed, and
continue across restarts. Use cron or calendar forms for clock-aligned times.

In the project timezone:

- A local time repeated when clocks fall back runs once, at its first occurrence.
- A local time skipped when clocks spring forward runs once, at the first instant after the gap.
- Intervals are elapsed time and ignore DST.
- `--at` accepts `HH:MM` (next occurrence), `YYYY-MM-DD HH:MM`, or RFC 3339; `--in` takes a duration.
  Local times resolve like `until` and schedules: a repeated time is its first occurrence, a
  skipped time the first instant after the gap.

Optional `splay` (1s–1h) shifts each cron, macro, or calendar occurrence forward by a
stable whole-second offset; manual and interval jobs reject it. The offset is the first eight
bytes of SHA-256(canonical project path, NUL, job name), big-endian, modulo splay in seconds.

Optional `jitter` shifts each scheduled occurrence forward by its own deterministic whole-second
offset in [0, jitter). It requires a schedule and is bounded by half the shortest gap: half the
interval for elapsed schedules, or half the smallest gap between clock times for cron, macro, and
calendar schedules. Clock gaps include the wrap from the last time of day to the first and ignore
day fields. For example, `*/15 * * * *` allows at most `7m30s`. Each jitter offset is the first
eight bytes of SHA-256(canonical project path, NUL, job name, NUL, nominal occurrence Unix seconds
in decimal), big-endian, modulo jitter in seconds. Effective start is nominal time plus the stable
splay offset plus this occurrence's jitter offset. The shared next-effective helper resolves nominal
wall time first, then adds elapsed offsets, preserving the DST rules above; it searches from now
minus the maximum forward offset. Intervals retain their nominal phase, and jitter never accumulates
into it. Offsets use no clock reads or randomness and are not stored; inspection, scheduler, and add
output use the same helper. All `next_at` views include configured offsets; list text appends
`+<splay offset>` then `~<jitter>`, and JSON/MCP expose normalized `splay`, `splay_offset`, and
`jitter` only when set. To center a 20-minute window around 9am, use `weekdays at 8:50am`. Explicit
runs and queued/triggered work are not shifted.

### Missed runs, overlap, and concurrency

An optional absolute `until` ends unattended execution at `now >= until`. It accepts local
`YYYY-MM-DD HH:MM` or RFC 3339, using the same DST wall-clock rules as schedules. Relative config
values are invalid; past values stay valid. Ended jobs drop schedule state without catch-up.
Effective starts, including splay and jitter, must be strictly before the end. Queued requests
refuse with `ended` when now or their due time reaches it; queued runs are cancelled after expiry
by the inactive-job dispatch rule, while running work finishes under its timeout. Foreground
runs remain explicit and allowed. Config definitions stay in config, which the scheduler never writes.
CLI-created [temporary jobs](#temporary-jobs) are the only state-backed definition exception.

The pure `internal/scheduler.Due` and `Dispatch` planners decide; `noon serve` persists each tick's
schedule and dispatch changes atomically, then starts runs. Schedule state is keyed by normalized
schedule text plus timezone, so changing either resets it. For splayed jobs only, the key also
includes the splay, offset, canonical project path, and job name; changing any resets state. Jittered
jobs append `~<normalized jitter>` to the key, so changing jitter resets state while jobs without
jitter keep their existing keys. Persisted next occurrences are always nominal; the planner and
displays derive effective times by adding splay and occurrence jitter. Inactive, manual, and removed
jobs lose their state.

- First sight of a job, a schedule change, enabling, approving a changed job, re-adding, and
  resuming all start from now: no catch-up. These commands clear schedule state, so this holds
  even when the scheduler wasn't running.
- A pause row has optional `starts_at` and `resumes_at`; every reader treats it as active while
  `starts_at <= now < resumes_at`, with a NULL bound unbounded. Pending windows change nothing
  until they start, except that temporary-schedule creation refuses any unexpired pause. Readers
  ignore expired rows without a scheduler. The tick transaction deletes them and their schedule
  state like resume, then seeds state from the first occurrence after `resumes_at` (calendar
  occurrences at exactly `resumes_at` run; intervals restart one period later). Occurrences inside
  the window never run or count as missed; a later one missed while the scheduler was offline
  follows the `missed` policy.
- An occurrence noticed within 60s of its effective start is on time. Later, it was missed:
  `missed: run_once` (default) runs once as `catch_up`, never a burst; `missed: skip` records one
  `missed` run. Both record the first missed occurrence. After an on-time occurrence, state advances
  to the following nominal occurrence; after a catch-up or missed record, it advances to the first
  nominal occurrence whose effective start is after now. Intervals advance by whole periods and keep
  phase. If DST reorders occurrences despite the jitter bound, the later-due occurrence is handled
  by the `missed` policy rather than silently dropped or run twice. Splayed and jittered runs retain
  nominal `scheduled_at` and `NOON_SCHEDULED_AT`; `due_at` is the effective start (now for catch-up).
  Overlap also applies at the effective start.
- `overlap: skip` (default) records a `skipped` run when the job is already running.
  `overlap: queue` keeps at most one pending run.
- The scheduler starts a run only while fewer than one run is active in its project and fewer than
  four overall, counting foreground runs. The rest wait FIFO as `queued`. Foreground runs ignore
  the caps but never overlap the same job.
- A due-now request (`--detach`, MCP, triggers, webhooks) defaults to merging into the job's
  existing due queued run; the new payload and event file are discarded. `requests: each` instead
  creates a separate run up to a per-job cap of 20 queued runs, counting due and future runs. The
  cap is checked in the queue transaction before any event file is written. `--in` and `--at`
  one-shots stay independent.
- A queued run more than one hour overdue expires as `expired`. Queued runs of inactive or removed
  jobs are cancelled instead. Dispatch orders by due time, then run ID. Future one-shots never
  suppress a scheduled occurrence; an already-due queued run does, recording the occurrence as
  `skipped`.

## Scheduler lifetime

One foreground `noon serve` per state directory runs every enabled project (decision-1). It never
self-daemonizes and is not one process per repo.

- It holds a non-blocking lock and records its PID, process identity, version, start time, and
  heartbeat. A second scheduler fails with `scheduler_running`, naming the owner. A database newer
  than the binary exits 75.
- It polls every second and reloads candidate config files (including absent ones) every two
  seconds. It stamps each configured `vars_file`, including paths absent or broken on the previous
  load. It validates before swapping; an invalid edit keeps the last valid jobs and records the error
  on the project. A heartbeat older than five seconds reads as offline.
- On SIGINT or SIGTERM it stops dispatch, terminates active process groups, and records their runs
  as `interrupted` before releasing the lock.
- Each project loads with the config selection recorded by `noon enable` plus user settings. Selecting the user project
  loads only its `config.yaml`; it never reads repository manifests. `--config`, `--timezone`, `--project`, `--json`, and
  their environment variables are usage errors.
- `noon service install` registers a launchd agent or systemd user unit. It records `PATH`,
  optional `XDG_CONFIG_HOME`, and the absolute state directory, because service managers otherwise
  run with a minimal `PATH` and a different working directory. Nothing else installs services.
- `noon service restart` restarts an installed service from its existing definition; a missing
  service needs `service install`. Restarts interrupt active jobs, never drain. An unchanged
  `install` restarts only a live, identity-verified, manager-owned scheduler whose known version or
  commit differs; unknown metadata yields restart guidance instead. Config reloads never restart.
  See [service upgrades](service.md#upgrades).
- Docker runs the same process in the foreground. `noon run NAME` needs no scheduler, like `task NAME`.
- There is no local IPC socket. Local clients read SQLite for scheduler status and next times; the optional HTTP API is an adapter over shared app operations, not a second scheduler. The scheduler polls SQLite every second and config files every two seconds. Revisit polling only if latency or cost becomes a measured problem.
- Queueing (`--detach`, `--in`, `--at`, MCP, API, webhooks) is one immediate SQLite transaction:
  idempotency key, enablement and approval, cooldown, then due-run merging or the `each` cap.

### Temporary jobs

Definitions normally live in config. The only state-backed exception is CLI
`add NAME [SCHEDULE] --temporary`, using the existing add action flags plus `--in`/`--at` and
optional `--key`; `--user` selects the user project. It writes no config and supports absent,
shared-only, or symlinked private config. Explicit `--config` and `--shared` are refused.
Creation requires exactly one end (`--for`/`--until`), after now and within the shared 7-day limit
including minute rounding, and a schedule or queued run before it. Config `until` alone has no
7-day limit. Intervals retain elapsed phase, not wall-clock alignment.

The definition uses config's one-job parser, including validation, hook defaults, and digest.
It is private (`shared: false`, approval `not_required`); creation enables the project and records
`requested_by: cli`. Only CLI creation has this authority; neither MCP nor HTTP creates definitions.
Name ownership is transactional: config names refuse with `exists`; an active temporary name
requires the same nonempty key for a duplicate (`created: false`, `reason: duplicate`), otherwise
`exists`. Ended retained names can be replaced. Config add refuses a retained temporary name;
a hand-added config job wins and deletes the state definition. There is no in-place edit or
promotion: remove and add again.

One app merge operation resolves these jobs for inspection, execution, queue/cancel/remove,
history/logs, MCP, TUI, and scheduler reload. The scheduler refreshes state-backed definitions on
its 1-second state poll independently of config stamps. They use normal private-job scheduling,
missed/overlap rules, timeout, and exclusive `until`: no start at/after the end, queued work is
cancelled, running work finishes. Pause/resume and disable never extend the end. Foreground runs
remain available while retained; queued requests after the end refuse `ended`. Temporary
schedules cannot attach to temporary jobs.

Cancel by job ends the definition at now and cancels active work; cancel by run ID does not end
it. Natural expiry records `ended_at: until`. The fixed grace is 24h from `ended_at`, else `until`;
reads exclude elapsed grace even offline, and the scheduler purges definitions. Remove deletes
immediately. Normal run/log retention remains, including history/log lookup after removal.
A name-only record lets never-run removed jobs return empty history without accepting unknown
names or retaining expired definitions.

List JSON/MCP use `source: temporary`, existing `until`/`ended`, and `temporary` containing
`created_at`, `requested_by`, optional `key`/`ended_at`, and `removes_at`. Human list marks
`temporary until` or `temporary, ended` and NEXT `ended`. Dashboard/MCP status list active jobs
in one notice. TUI marks them temporary, renders ended rows faint, and shows end/removal times.
Creation JSON is `{schema_version:1, job:<list job>, created:bool, reason?:duplicate,
scheduler:running|offline}`; human creation uses the queued-run offline notice.

### Temporary schedules

CLI `run --schedule S --for D` (or `--until T`) and MCP `run_job` with `schedule` and
`for`/`until` attach recurring requests to one existing runnable job. They never change job
definitions or approval authority. Schedule parsing and timezone/DST rules are the config
grammar. Intervals retain their creation phase. Exactly one end is required, after now and
within 7 days including whole-minute upward rounding for durations, with an occurrence before it.
Schedule mode rejects detach/in/at. Several CLI names share the schedule and end.

Creation uses an immediate transaction and the queue eligibility checks, including pause, job end,
and recursion; a first occurrence at or after the job's `until` is refused as `ended`. `temporary_schedules` has one row per project/job: schedule text, exclusive end,
creation digest, requested_by, optional key/data (valid JSON up to 256 KiB), event source,
created_at, and persisted next_at. Same nonempty key returns the existing row as a duplicate;
otherwise `already_scheduled` hints to cancel by job.

Planning feeds the persisted phase through `Due` with the job's missed/overlap rules, zero splay
and jitter, and the earlier temporary/job end. Config occurrences are planned first, and their
queues suppress temporary occurrences in the same tick; coinciding occurrences record one skipped
row, never double execution. Cooldown and requests-each do not apply. Runs use schedule/catch_up,
carry requested_by, and receive separate event files from stored data before the transaction commits.

Expiry, cancel by job, pause, project disable, job removal, and job end delete the row; the
running scheduler also deletes it when it observes the job disabled, changed, or unsupported. Resume, re-enable, and re-approval never restore it.
Already-created runs follow existing dispatch/cancellation rules. Cancel by run ID leaves it intact.
Inspection omits absent/ineligible schedules, exposes temporary_schedule metadata, and uses the
earlier next_at. Dashboard and MCP status add a notice. HTTP MCP shares this authority; no JSON
API /v1/ routes create temporary schedules.

### Enable and approve

Scheduling unattended commands from repository content is a supply-chain risk, especially when
`git pull` changes it (decision-2).

- `noon enable` registers the canonical project and its config selection, and approves the current
  definition of every shared job. It is idempotent, so a container can run
  `noon enable && exec noon serve`. Worktrees and clones are separate checkouts; none is enabled
  implicitly. `noon disable` stops background runs, cancels queued runs, and keeps history.
- `noon --user enable` opts in the personal project. Its jobs are the user's own and need no approval;
  a scheduled or queued `noon --user add` enables it automatically. The ordinary `noon serve` runs it
  with other enabled projects.
- Private `.git/noon.yaml` and `.git/noon.d/*.yaml` jobs need no approval. Jobs from `noon.yaml` or `--config` run
  unattended only while their digest matches the approved one; a changed job shows `needs approval`.
- Privacy needs a file repository content can't supply. Git refuses paths inside its directory, so
  a branch can't add or replace `.git/noon.yaml` or `.git/noon.d`. A filename in the tree is never
  private: a commit can add `.noon.yaml` despite ignore rules, so Noon never loads it. For the same
  reason, a private file or `noon.d` that resolves through a link into the working tree is an error.
- The digest covers what runs and when (resolved schedule, splay, jitter, until (canonical absolute instant), action, agent, cwd, env, ordered env_file paths, timeout, overlap, missed,
  cooldown, hooks, webhook, `os`, `arch`, and `requests` when explicitly set), not the description or vars-file contents. A vars-file edit needs approval only when it changes an effective allowlisted value; expression rewrites with the same result and unused variables do not. Omitting `requests` preserves existing job digests. A prompt job's
  digest also covers its agent profile when that profile comes from the same `--config` file;
  built-in and user-config profiles are machine choices and stay out.
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
  anything else (missing `cwd`, bad prompt or env file). With a configured `noop_exit_code`, a process that
  started and exited normally with exactly that code is `noop`; timeout, cancellation, and start
  failure are never noops. Noops retain their log, duration, and actual exit code. Foreground `noon
  run` treats a noop as a successful command exit (0), while its summary says `noop` and JSON reports
  the actual exit code.
- The log interleaves stdout and stderr. The first 1 MiB is written live; after that the last 1 MiB
  is kept in memory and appended after an omission marker at exit, so `--follow` sees the head live
  and the tail at the end.
- Environment: inherited (the scheduler's or your shell's), then runtime `env_file` values in list order
  (later files win), then job `env`, then `NOON_PROJECT`,
  `NOON_JOB`, `NOON_RUN_ID`, `NOON_TRIGGER`, `NOON_SCHEDULED_AT`, `NOON_LAST_SUCCESS_AT` (start of the
  last successful run), `NOON_DEPTH` (stored request depth + 1), `NOON_AGENT` for prompt jobs,
  `NOON_AGENT_SESSION_ID` only for prompt profiles with `session_id_flag`, and
  `NOON_EVENT_FILE` when there's a payload. Inherited run and hook metadata (`NOON_RUN_ID`,
  `NOON_STATUS`, `NOON_EXIT_CODE`, `NOON_SCHEDULED_AT`, and the rest of the names above and in
  hooks) is stripped, so a job started from a hook never sees its parent's values. Depth comes from the stored run,
  not the scheduler's environment. Noon never sources shell rc files, creates worktrees, or changes
  branches. Env files are required regular files of at most 64 KiB each, opened nonblocking and
  parsed at each run start; errors name the configured path and syntax line, never values.
  Paths are absolute or project-relative even in private Git config; file contents are not digested.
  Hooks reuse the run environment without rereading files, or fall back without file values on
  pre-environment startup failures. See [Env files](configuration.md#env-files) for the strict format.

Log reading ([Command reference](cli.md#logs)):

- Raw run logs remain combined byte files. A sibling `.log.timing` sidecar stores fixed-width UTC
  receipt-time records by raw offsets; it is created with the log and pruned with it. A timestamp is
  when Noon received bytes, not when the child produced them, so child-side buffering can delay it.
  Each line takes the receipt time covering its first byte. Missing, torn, or historical metadata is
  unavailable rather than inferred from run start, a neighboring line, or file modification time.
- CLI `--timestamps` / `-t` and structured CLI JSON, MCP `get_run`, and HTTP API reads opt into
  timing. Human prefixes use local `2006-01-02T15:04:05.000-07:00` with explicit offset; structured
  `line_times` use UTC `2006-01-02T15:04:05.000Z` and `{offset,time}` entries keyed to each returned
  raw line's first-byte file offset (`time` is null if unavailable). The `log` value and cursors stay
  raw and unchanged. TUI logs show timestamps by default; `t` toggles them without resetting search
  or follow position. Follow emits partial lines immediately and a continued line keeps its original
  prefix/time.
- Snapshots read one file state. Unflagged CLI reads include an active unterminated line; bounded
  CLI and MCP reads omit it so `next` stays on a line boundary.
- CLI copyable guidance preserves explicit invocation selectors (including environment overrides)
  with absolute shell-quoted paths. Service suggestions use only state selection and clear
  unsupported selector environment variables. Log resume commands retain section/filter/byte-limit
  selection (and `--timestamps` when requested), pin the run and cursor, and drop tail/status
  selection; JSON keeps numeric `next`
  and adds a copyable `resume` command.
- Order: section range ∩ `[after, size)`, then match and context, then `--tail`, then
  `--limit-bytes` (newest whole lines without a cursor, earliest with one; a single oversized line is
  cut on a UTF-8 boundary). Cursors are absolute file offsets.
- The separator newline before a hook marker belongs to the job section. Before a hook is appended,
  `job` is everything so far and `hook` is empty.
- Every shared run object includes `orphaned`. It is true when a run or its hook is active and the
  recorded owner PID/start identity no longer matches a live, non-zombie process. Inspection uses
  the same read-only owner check as `logs --follow` and re-reads after a failed identity check so a
  normal finish is not called orphaned. Queued and fully terminal runs are false; hook stdin is
  false because its owner is alive. Stored status is unchanged (`running` for a run-level orphan,
  possibly terminal for a hook-only orphan); human output keeps a hook-only run's status label.
  Inspection never recovers, signals, or writes. `get_run`'s top-level flag agrees with `run.orphaned`.
- `--follow` waits for a queued run, printing one stderr notice. It checks the owner's PID and start
  identity on each poll, re-reading status after a failed check to avoid racing a normal finish. If
  still active, it prints remaining bytes and exits 1 with `orphaned` and hint
  `Record it: noon cancel ID`. MCP `get_run` returns `orphaned` and ends `wait_seconds` early.
  Dashboard, list, history, cancel, and TUI inspection show orphaned runs and point to
  `noon cancel ID`; that command records the run cancelled and only signals an identity-matched
  process group.
- A queued snapshot fails with `not_found` and suggests `--follow`. A terminal run that never
  started says so. Only a missing log for a started run gets the retention hint.

## State

State is per user, not per repository (decision-3). It survives `git clean -fdx`, stays out of
worktrees, and lets one scheduler see every enabled project.

- Location: `$XDG_STATE_HOME/noon`, else `~/.local/state/noon`; override with `NOON_STATE_DIR`,
  `--state-dir`, or user `state_dir`. Directories 0700, files 0600.
- `noon.db` is SQLite (WAL, busy timeout) via `modernc.org/sqlite`. Beside it: raw
  `logs/PROJECT_ID/RUN_ID.log` files and optional `RUN_ID.log.timing` sidecars, `events/RUN_ID.json`,
  `locks/`, `scheduler.lock`, and the service's `scheduler.log`. Sidecars use 0600 permissions,
  contain little-endian start/end offsets and Unix milliseconds, and are removed with retained logs.
- Migrations are an append-only list keyed by `PRAGMA user_version`, applied in one transaction. A
  newer database is refused with an upgrade hint. Read-only commands never create state. State schema
  version 10 marks the incompatible noop status and retention semantics; version 11 adds the nullable
  `agent_session_id` run field. Version 12 adds scheduler build_commit; version 13 adds the
  `temporary_schedules` table and its independent durable phase. Version 14 adds `temporary_jobs`
  (project/name primary key, one-job config definition, digest, until, created_at, requested_by,
  optional key, ended_at) and `temporary_job_names` (project/name only, retained for empty history
  resolution after removal). Temporary schedules and job definitions are separate tables.
  Version 15 adds nullable pause `starts_at` and `resumes_at`; existing rows stay open-ended.
  Older binaries refuse the newer database.
- Run `status`: `queued`, `running`, `succeeded`, `noop`, `failed`, `timed_out`, `cancelled`,
  `interrupted`, `skipped`, `missed`, `expired`. `noop` is a terminal run status, distinct from
  `skipped`, which means an occurrence never started because of overlap. Noops count as terminal
  for scheduling and idempotency; they do not count as job results, fire hooks, update
  `NOON_LAST_SUCCESS_AT`, or become a hook's `previous_status`. Run `trigger`: `manual`, `schedule`,
  `catch_up`, `queued`, `webhook`.
- `scheduled_at` is the occurrence a scheduled run stands for (the first missed one for `catch_up`
  and `missed`) or a queued run's due time. `idempotency_key` is unique per project and job.
  `cancel_requested_at` is a durable stop request. `depth` is the request's recursion depth
  (scheduled runs start at 0). `requested_by` is claimed identity, metadata only.
- Shared run JSON (CLI history/logs/run, MCP, `last_run`, hook stdin): `id`, `project`, `job`,
  `status`, `orphaned`, `trigger`, `digest`, `agent_session_id`, `scheduled_at`, `created_at`,
  `started_at`, `finished_at`,
  `duration_ms`, `exit_code`, `signal`, `error`, `requested_by`, `idempotency_key`, `due_at`,
  `cancel_requested_at`, `event_file`, `depth`, `log` (`path`, `bytes`, `truncated`, `available`,
  `hook_offset`), and `hook`. Paths are absolute; absent values are null.
- Run and structured line times are UTC text, `2006-01-02T15:04:05.000Z`, so they sort as strings.
- Retention: newest 50 terminal runs per job excluding `noop` and `skipped`, with logs and event
  files for the newest 10. Noops have an independent budget of the newest 10 runs with their logs
  and event files; skipped runs have another independent budget of the newest 10 rows (no logs,
  event files, or idempotency keys). These three budgets never count against one another. `missed`
  and `expired` runs count toward the regular budget. Queued and running runs are never pruned.
  Idempotency keys and delivery IDs deduplicate only against retained runs.
- Recovery: a `running` run whose owner is gone becomes `interrupted` and never re-runs. Its group is
  terminated only if the recorded PGID and leader start identity still match. Queued runs that never
  started are safe to dispatch after a restart.

## Integrations

No plugin system; commands are the integration boundary (decision-4).

- **Hooks** ([doc](hooks.md)): `on_success`/`on_failure` get a versioned JSON summary on stdin and
  `NOON_*` variables; 30s timeout; 64 KiB output appended after a marker; no hooks for hooks.
  `previous_status` and `consecutive_failures` come from one snapshot taken before the run's
  terminal transition, over retained `succeeded`/`failed`/`timed_out` results of the same project
  and job ordered by `finished_at` then ID. Other statuses neither count nor reset; digest changes,
  pause, and resume don't reset. Noops run no hooks. On lookup error Noon records the job result
  and skips the hook. Pause and notification policy belong in wrappers, not the core.
- **Triggers** ([doc](triggers.md)): `noon run NAME --detach --key ID [--data JSON]`. The event file
  is written before the queued run is committed, so dispatch can't race it. Payloads are files,
  never interpolated. Cooldown is checked after key deduplication and requirements, before due-run
  merging or the each-mode cap. `requests: merge` drops a new payload when a due run exists;
  `requests: each` preserves each due-now payload up to 20 queued runs per job. Requests at depth 3
  or more, or from the same project and job, fail with `recursion`. Nothing installs hooks for you.
- **Shell** ([doc](shell.md)): completion only reads config. `--on-enter` detects canonical project
  changes per shell, uses the checkout's recorded config and the normal queue with an atomic pause
  check, and stores directories as data. Nothing edits startup files or the parent environment.
- **Webhooks** ([doc](webhooks.md)): only with `serve --listen`. `POST /hooks/{project_id}/{job}`
  needs an HMAC-SHA256 signature; the secret comes from the scheduler's startup environment.
  Delivery IDs are idempotency keys. Payloads choose data, never commands.
- **Event files**: `schema_version`, `source` (`cli`, `mcp`, `api`, `webhook`), `received_at`, `data`, and
  allowlisted `headers` for webhooks. Signature and authorization headers are never stored.
- **MCP** ([doc](agents.md)): stdio and stateless Streamable HTTP, official Go SDK. Tools: `status`,
  `list_projects`, `list_jobs`, `run_job`, `list_runs`, `get_run`, `cancel_run`. HTTP MCP has no
  default project and only reaches enabled projects. Agents can't approve, add, or edit unattended
  commands. `noon skill` prints the shell-only fallback unchanged; it's never auto-installed.
- **Remote JSON API** ([reference](cli.md#remote-api-and-mcp-over-http)): with
  `serve --listen --api-token-file`, routes under `/v1/` expose only the inspect, queue, read, and
  cancel operations shared with MCP. The shared bearer token is read once from a file, checked
  before routing, and never inherited by jobs or hooks. Only enabled projects and their runs are
  reachable; `/healthz` remains unauthenticated, and webhooks retain HMAC authentication.
- **Crontab import** ([doc](crontab.md)): preview by default; unsupported features fail with line
  numbers.
- Outbound HTTP is a command (`run: [curl, -fsS, …]`), not a built-in action.
- Not implemented: attachable tmux/Herdr sessions are a draft. File watching,
  worktree lifecycles, and container-isolated agents compose existing tools instead (see
  `examples/local-dev/file-watch`, `examples/integrations/worktrunk`, `examples/ai/container-agent`).

Revisit plugins only when two real integrations need something commands, hooks, and agent profiles
can't express. Even then, prefer `noon-NAME` subprocesses speaking versioned JSON over Go plugins.

## Security model

| Input | Trust | Consequence |
| --- | --- | --- |
| User project `config.yaml`, `.git/noon.yaml`, `.git/noon.d/*.yaml` | Yours: user config is outside the repository; Git cannot commit `.git` paths | Runs unattended once the project is enabled |
| Temporary jobs (`noon add --temporary`) | Created through the user's shell, private and approval-free; creation enables the project | Definitions normally live in config; this is the only state-backed exception, CLI-only, ending within 7 days and removed after 24h grace |
| In-tree `.noon.yaml` | Repository content: commits can add it | Never loaded |
| `noon.yaml`, `--config` file | Repository or operator content | Unattended runs need the approved digest; manual runs are explicit |
| Scripts, prompt files, and env files | Not pinned | Approval covers what and when, not file contents |
| MCP clients and authenticated HTTP API clients | Inspect, queue approved jobs, read logs, cancel runs; MCP also attaches bounded temporary schedules to approved jobs | Can't add, edit, enable, disable, approve, pause, or resume jobs; identity is metadata |
| HTTP API bearer token | One shared token read from `--api-token-file` | Required before `/v1/` and `/mcp` routing; never logged, persisted, echoed, or passed to jobs/hooks |
| Webhooks | HMAC-authenticated data | Never select or build commands; payload is a file |
| Job and hook output | Untrusted bytes | Stored raw and bounded; never interpreted |
| Environment values | Secret by default | Never printed by list, status, JSON, or MCP |

Runtime env files may appear in every job source; approval pins paths and order, not secret contents.
Config-time vars files are a separate, non-secret source for allowlisted interpolation; their resolved
values are visible in inspection and are never exported to jobs or hooks. Noon does not redact child output.

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
