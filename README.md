# noon

**Repo-local scheduling, without a platform.**

A Go CLI for recurring commands and agent work, designed for your machine first
and containers when needed. CLI now; optional TUI later. No required web UI,
Python, Node, cloud service, or agent provider.

**Status: foreground scheduling with explicit OS supervision.** Working today: project discovery,
layered settings, non-destructive initialization, strict job validation, byte-preserving job add/remove,
schedule parsing and previews, foreground command and agent-prompt execution, durable per-user run records and bounded
logs, success/failure lifecycle hooks, queued one-shot runs with idempotency and cancellation, Git and agent triggers with durable event payloads, signed webhooks, cooldowns, and recursion guards, explicit project opt-in and shared-job
approvals, pausing and resuming background jobs, `noon serve` for enabled schedules, `noon service install` for a per-user launchd or
systemd service, read-only run history and log inspection, a project dashboard, read-only setup diagnostics,
stdio MCP tools, and development tooling. Cron import is implemented as a preview-first conversion to
Noon YAML; the TUI remains designed but **not implemented**. Noon never backgrounds itself; keep
`noon serve` in the foreground or use the explicit service installer or an OS/container supervisor.

## Try it

```sh
mise trust
mise exec -- task init
task cli:build

bin/noon                 # project dashboard and next action; starts nothing
bin/noon doctor          # read-only setup checks; exit 1 if any check fails
bin/noon init            # create noon.yaml; preserve it if it exists
bin/noon list            # validated jobs and next-run previews; executes nothing
bin/noon add tests "every day at 2am" -- go test ./... # add a private scheduled job
bin/noon remove tests    # remove its definition; keep history
bin/noon import crontab old-crontab  # preview a user crontab as Noon YAML
crontab -l | bin/noon import crontab - --write # add imported jobs to .noon.yaml
bin/noon pause tests     # stop background dispatch
bin/noon resume tests    # resume from now, without catch-up
bin/noon enable          # explicitly approve this checkout for scheduled runs
bin/noon serve           # foreground scheduler; no listener; stop with Ctrl-C or SIGTERM
bin/noon serve --listen 127.0.0.1:8787 # opt in to signed webhook HTTP
bin/noon service install # explicitly install the per-user launchd/systemd service
bin/noon service         # inspect service and scheduler heartbeat
bin/noon run             # list runnable jobs; starts nothing
bin/noon run tests       # execute this job now, in the foreground
bin/noon run tests --detach # queue this job to run as soon as capacity is available
bin/noon run tests --detach --data '{"commit":"abc"}' # queue a run with JSON event data
bin/noon run tests --in 2h --key nightly-check # queue a one-shot with idempotency
bin/noon cancel          # list active runs without changing them
bin/noon cancel 12       # cancel run 12, including another terminal's foreground run
bin/noon history         # inspect recent runs
bin/noon history --status failed # inspect failures first
bin/noon logs            # print the latest started run log
bin/noon logs tests      # print this job's latest started log
bin/noon logs tests --status failed # print its latest failure
bin/noon logs 12 --follow # follow a running run
bin/noon logs 12 --section job # read only the job output
bin/noon logs 12 --section hook # read only hook output
bin/noon logs 12 --path   # print the absolute path of its retained log
bin/noon config          # effective settings and source files
bin/noon config check    # validate without writing anything
bin/noon --json status
bin/noon --help
```

`noon init --local` creates a private `.noon.yaml` override instead. Ignore
`.noon.yaml` in Git. Run state is stored per user under `$XDG_STATE_HOME/noon`,
or `~/.local/state/noon` when XDG is unset; `NOON_STATE_DIR` and `--state-dir`
can override it. `state_dir` belongs only in user config or an explicit `--config`
file, never a project file. Configuration layers are:
defaults → user config → `noon.yaml` → `.noon.yaml` → environment → CLI flags.

```yaml
version: 1
timezone: Local
jobs:
  tests:
    schedule: weekdays at 9am
    run: [go, test, ./...]
```

```sh
NOON_TIMEZONE=UTC bin/noon config
bin/noon -C ../another-repo config
bin/noon --config ./custom.yaml config check
```

`--config` replaces automatic config files. No config is required for inspection.
No-argument inspection commands do something useful without prompting or executing work;
`noon serve` and `noon service install` explicitly start the scheduler.
Invalid commands and configuration fail instead of pretending to succeed.
Errors with `--json` produce one stdout object:
`{"schema_version":1,"error":{"code":"…","message":"…","hint":"…"}}` (`hint` is omitted
when empty). Human errors go to stderr. Exit codes are 0 for success, 2 for usage
errors, and 1 for other failures (`noon serve` exits 75 when the database is newer than the
binary). Foreground runs instead return the child’s exit code: 124 on timeout, 130 on cancellation,
128+N for signal N, and 125–127 on start failure. Job output streams directly; status lines go to
stderr. `noon serve` rejects `--json`, `--config`, `--timezone`, `--project` and their environment
variables; projects use the selection recorded by `noon enable`.
`noon run NAME --json` sends both job streams to stderr and one run summary to
stdout. Each foreground run is recorded in the per-user SQLite database with a bounded log;
only one run of a given job can run at a time per project. Queued requests require `noon enable`
for that checkout and run only while `noon serve` is active. `--detach` requests due-now work;
`--in DURATION` and `--at TIME` queue independent one-shots. `--key KEY` deduplicates against the
same project's retained runs of that job; keys are retained with the newest 50 terminal runs per
job. `--data JSON`, `--data @FILE`, and `--data -` (stdin) accept valid JSON up to 256 KiB; the
payload is persisted as an event file before dispatch and exposed to the job as `NOON_EVENT_FILE`.
A configured `cooldown` applies only to queued requests, after idempotency and authorization checks;
foreground and scheduled runs ignore it. A due-now request coalesces with an already-due queued run.
When the scheduler is offline,
Noon says that queued work will wait until it starts. `noon cancel RUN` cancels queued work
immediately or asks the run owner to stop a running process group; `noon cancel NAME` requests
cancellation for every active run of that job. `noon disable` also cancels queued runs. SIGINT,
SIGTERM, or SIGHUP cancels a foreground run's process group (TERM, then KILL after 10s); a second
signal kills it immediately.
`noon history` shows the newest 20 runs (up to 50), with optional `--status` filters. Status filters
accept repeated or comma-separated values: `queued`, `running`, `succeeded`, `failed`, `timed_out`,
`cancelled`, `interrupted`, `skipped`, `missed`, and `expired`. `history NAME` and `logs NAME` can
read retained runs even after a job is removed from config. `noon list` and bare `noon` show each job's
latest run in the LAST column. Read-only commands
do not create the state directory. Bare `noon` and `noon status` show jobs, scheduler status,
queued work and up to three prioritized notices, with one suggested next action. Their JSON
includes `project`, `scheduler`, `service`, `jobs`, `queued`, `notices`, and `next`.
`noon doctor` checks configuration, state access/schema, opt-in, approvals, heartbeat, service
installation, job commands/cwd/prompt files, and Git ignoring `.noon.yaml`. It never fixes
anything or runs jobs. SQLite may create WAL/SHM coordination sidecars for an existing
database, but doctor never changes application data, migrates, or creates absent state.
Warnings exit 0; failing checks exit 1. JSON contains `ok` and `checks`
with `name`, `status` (`ok`, `warn`, `fail`), `message`, and `hint`.

Existing local users: stop all old/new processes and back up your data; move `~/.local/state/bump` to `~/.local/state/noon` and `~/.config/bump` to `~/.config/noon` only if the destinations do not exist, then rename `bump.db` to `noon.db` inside the moved state directory, along with any `bump.db-wal`/`bump.db-shm` sidecars to `noon.db-wal`/`noon.db-shm`; never overwrite existing destinations, and keep the `logs/` tree intact.

## Logs

`noon logs` prints the latest started run's stored bytes; `noon logs NAME` selects that job's latest
started run. NAME lookups use current jobs and retained history, including removed jobs.
`noon logs RUN --follow` waits for a queued run to start, then follows its output.
It prints one waiting notice on stderr; cancelling a queued run ends follow with a never-started
error. If the run or hook's owner exits without recording a result, follow prints remaining stored
output and exits 1 with code `orphaned` and a `noon cancel RUN` hint. Inspection never recovers or
cancels runs. `--status S` selects runs by one or more statuses,
using the same repeated or comma-separated values as `history`; only an explicit status filter can
select a queued or otherwise never-started run. Numeric RUN selectors cannot be combined with
`--status`. `--section all|job|hook` selects the whole log (default), job output, or hook output;
`--tail/-n N` keeps the last 1–10000 lines; `--after-cursor/-c BYTES` reads forward from an absolute
file byte offset; `--match/-m REGEX` searches lines with a case-sensitive Go RE2 expression, and
`--context N` adds up to 50 lines around matches. Context requires `--match`.
`--limit-bytes/-b N` caps output while keeping whole lines where possible. Unflagged CLI snapshots,
including JSON, include the full current log; bounded CLI and MCP snapshots omit an active unfinished
trailing line. JSON returns `log`, `next`, `size`, `truncated`, and `matched_lines`; pass `next` back
as `--after-cursor` for a forward page, keeping the selected `--section`. Run JSON includes nullable `log.hook_offset`, the absolute
byte offset where the hook marker starts. Any separator newline is part of the job section; before a
hook is appended, `job` includes all bytes written so far and `hook` is empty. Bounded human reads
keep selected text on stdout and print the resume command to stderr. Follow accepts only `--tail` and
`--after-cursor`, not `--section`; `--path` cannot be combined with selection flags, `--json`, or
`--follow`.

`noon logs [NAME|RUN] --path` prints the absolute path of the selected retained log. Run JSON (from
`history`, `logs`, `run --json`, MCP run tools, `last_run`, and hook stdin) includes the canonical
project path, retained `log.path`, optional event-file path, and stored request context. Foreground
run start lines include the run ID; queued run output suggests whether to follow or read its log.
Logs are retained for the newest 10 runs per job.

## Safety

`noon service install` explicitly registers the per-user scheduler at login and restarts it after
failures. Inspect it with `noon service`; remove the unit with `noon service uninstall` while
keeping state and history. See [Service](docs/service.md) for logs, upgrades, and Linux lingering.

`noon enable` opts in this canonical checkout and approves its current shared job
definitions. It starts nothing; schedules run only while `noon serve` is running, directly or
under the explicitly installed per-user service. `noon serve` starts no HTTP listener unless
`--listen ADDR` is given; see [Webhooks](docs/webhooks.md) for signed webhook setup.
`noon disable` revokes opt-in and approvals, cancels queued runs, and keeps run history.
Changed shared definitions show `needs approval`; rerun `noon enable` after review. When the
scheduler is offline, `noon enable` suggests `noon service install` when no service is installed.
Foreground `noon run NAME` needs no approval.

Digests cover job definitions, not the contents of referenced scripts or prompt
files. Only enable repositories you would run by hand. Keep `.noon.yaml` ignored
in Git: local jobs and whole-job local overrides bypass approval. Approval is not
a sandbox and cannot protect against an agent or user with local shell access.

## MCP

`noon mcp` serves six tools over stdio: `status`, `list_jobs`, `run_job`, `list_runs`,
`get_run`, and `cancel_run`. In Claude Code or Codex, add Noon with:

```sh
claude mcp add noon -- noon mcp
codex mcp add noon -- noon mcp
```

A generic client can use this configuration:

```json
{
  "mcpServers": {
    "noon": { "command": "noon", "args": ["mcp"] }
  }
}
```

MCP can inspect jobs and runs, queue only jobs permitted by the same project enablement and
shared-job approval rules as the CLI, pass an optional JSON `data` value to `run_job`, read bounded
log tails or pages, search `get_run` with `match` and `context`, and cancel runs. `list_runs` accepts
an optional status array. `get_run` accepts either a global `run_id` or a `job`; job selection uses
the newest started matching run, with optional absolute `project` and status array. Its `section` is
`all` (default), `job`, or `hook`; use `section: "job"` when diagnosing a failure so hook output
does not obscure the job's error. `get_run` returns `next`, `size`, `truncated`, nullable
`matched_lines`, and `orphaned` (a boolean). Its optional `wait_seconds` stops early if the run or
hook owner exits without recording a result; pass `next` as `after` to page forward.
It cannot add, edit, remove, enable, approve, pause, or resume jobs. The initialize client's name and version
are recorded as metadata on queued runs; identity does not grant authority. Optional `project`
paths on `status`, `list_jobs`, `run_job`, `list_runs`, and `get_run` job selection must be absolute.
`get_run` and `cancel_run` use globally unique run IDs when selecting by ID. MCP is a local stdio transport, not a
sandbox: approval is not isolation, and a client running with your account has your account's
local permissions.

## Direction

Implemented foundations and planned extensions ([design](docs/design.md), [decisions](backlog/decisions)):

- Command and agent-prompt jobs, schedule parsing, `noon add`/`remove`, pause/resume,
  preview-first `noon import crontab`, foreground `noon run NAME`, queued one-shot
  requests and cancellation, and durable scheduling through `noon serve` already work.
- One per-user `noon serve` runs every enabled repo's schedules. `noon service install` keeps
  it alive across reboots on macOS and Linux. Docker runs the same foreground process. Noon never
  backgrounds itself.
- Nothing from a repository runs unattended until you `noon enable` that
  checkout, and changed shared jobs wait for re-approval.
- After sleep, a missed job runs once, never in a burst.
- MCP's local stdio tools use the shared app operations. Lifecycle hooks, inbound Git/agent
  triggers, and signed webhooks run through the shared execution path; the TUI remains planned.
  Commands are the integration boundary: no plugin system.

## Docker

```sh
task docker:smoke
docker run --rm noon:dev --help
```

The image supports the host CLI, including `noon serve`; enable projects explicitly and keep the
scheduler in the foreground. The [Compose example](examples/compose.yaml) uses an init process,
a read-only project mount, and persistent state. `task docker:integration` verifies scheduling,
shutdown, child reaping, offline timezones, and history persistence. See [Docker](docs/docker.md).

## Documentation

- [Configuration](docs/configuration.md): implemented sources, paths, precedence.
- [Lifecycle hooks](docs/hooks.md): command examples, execution and safety contract.
- [Triggers](docs/triggers.md): Git and agent hook examples with durable JSON event payloads.
- [Webhooks](docs/webhooks.md): signed GitHub webhooks, listener setup, TLS proxying, and Docker publishing.
- [Service](docs/service.md): install, inspect, logs, upgrades, uninstall, and Linux lingering.
- [Design](docs/design.md): CLI, scheduler, jobs, events, MCP, security.
- [Development](docs/development.md): tooling, tests, commits, contribution workflow.
- `task backlog`: ready work in the authoritative Backlog.md queue.

MIT licensed. Tooling and Go CLI structure follow the sibling hum/project repos.
