# noon

**Repo-local scheduling, without a platform.**

A Go CLI for recurring commands and agent work, designed for your machine first
and containers when needed. CLI now; optional TUI later. No required web UI,
Python, Node, cloud service, or agent provider.

**Status: foreground scheduling with explicit OS supervision.** Working today: project discovery,
layered settings, non-destructive initialization, strict job validation, schedule parsing and
previews, foreground command and agent-prompt execution, durable per-user run records and bounded
logs, queued one-shot runs with idempotency and cancellation, explicit project opt-in and shared-job
approvals, `noon serve` for enabled schedules, `noon service install` for a per-user launchd or
systemd service, read-only run history and log inspection, and development tooling. Cron import,
hooks, MCP, and TUI are designed but **not implemented**. Noon never backgrounds itself; keep
`noon serve` in the foreground or use the explicit service installer or an OS/container supervisor.

## Try it

```sh
mise trust
mise exec -- task init
task cli:build

bin/noon                 # useful repo summary; starts nothing
bin/noon init            # create noon.yaml; preserve it if it exists
bin/noon list            # validated jobs and next-run previews; executes nothing
bin/noon enable          # explicitly approve this checkout for scheduled runs
bin/noon serve           # foreground scheduler; stop with Ctrl-C or SIGTERM
bin/noon service install # explicitly install the per-user launchd/systemd service
bin/noon service         # inspect service and scheduler heartbeat
bin/noon run             # list runnable jobs; starts nothing
bin/noon run tests       # execute this job now, in the foreground
bin/noon run tests --detach # queue this job to run as soon as capacity is available
bin/noon run tests --in 2h --key nightly-check # queue a one-shot with idempotency
bin/noon cancel          # list active runs without changing them
bin/noon cancel 12       # cancel run 12, including another terminal's foreground run
bin/noon history         # inspect recent runs
bin/noon logs            # print the latest run log
bin/noon logs tests      # print this job's latest log
bin/noon logs 12 --follow # follow a running run
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
job. A due-now request coalesces with an already-due queued run. When the scheduler is offline,
Noon says that queued work will wait until it starts. `noon cancel RUN` cancels queued work
immediately or asks the run owner to stop a running process group; `noon cancel NAME` requests
cancellation for every active run of that job. `noon disable` also cancels queued runs. SIGINT,
SIGTERM, or SIGHUP cancels a foreground run's process group (TERM, then KILL after 10s); a second
signal kills it immediately.
`noon history` shows the newest 20 runs (up to 50); `noon logs` prints stored bytes, and
`noon logs --follow` waits for a running job. Logs are retained for the newest 10 runs per job.
`noon list` and bare `noon` show each job's latest run in the LAST column. Read-only commands
do not create the state directory.

Existing local users: stop all old/new processes and back up your data; move `~/.local/state/bump` to `~/.local/state/noon` and `~/.config/bump` to `~/.config/noon` only if the destinations do not exist, then rename `bump.db` to `noon.db` inside the moved state directory, along with any `bump.db-wal`/`bump.db-shm` sidecars to `noon.db-wal`/`noon.db-shm`; never overwrite existing destinations, and keep the `logs/` tree intact.

## Safety

`noon service install` explicitly registers the per-user scheduler at login and restarts it after
failures. Inspect it with `noon service`; remove the unit with `noon service uninstall` while
keeping state and history. See [Service](docs/service.md) for logs, upgrades, and Linux lingering.

`noon enable` opts in this canonical checkout and approves its current shared job
 definitions. It starts nothing; schedules run only while `noon serve` is running, directly or
 under the explicitly installed per-user service.
`noon disable` revokes opt-in and approvals, cancels queued runs, and keeps run history.
Changed shared definitions show `needs approval`; rerun `noon enable` after review. When the
scheduler is offline, `noon enable` suggests `noon service install` when no service is installed.
Foreground `noon run NAME` needs no approval.

Digests cover job definitions, not the contents of referenced scripts or prompt
files. Only enable repositories you would run by hand. Keep `.noon.yaml` ignored
in Git: local jobs and whole-job local overrides bypass approval. Approval is not
a sandbox and cannot protect against an agent or user with local shell access.

## Direction

Implemented foundations and planned extensions ([design](docs/design.md), [decisions](backlog/decisions)):

- Command and agent-prompt jobs, schedule parsing, foreground `noon run NAME`, queued one-shot
  requests and cancellation, and durable scheduling through `noon serve` already work.
- One per-user `noon serve` runs every enabled repo's schedules. `noon service install` keeps
  it alive across reboots on macOS and Linux. Docker runs the same foreground process. Noon never
  backgrounds itself.
- Nothing from a repository runs unattended until you `noon enable` that
  checkout, and changed shared jobs wait for re-approval.
- After sleep, a missed job runs once, never in a burst.
- Hooks, Git and agent triggers, webhooks, MCP, and a TUI build on the store. Commands are the
  integration boundary: no plugin system.

## Docker bootstrap

```sh
task docker:smoke
docker run --rm noon:dev --help
```

The image supports the host CLI, including `noon serve`; enable projects explicitly and keep the
scheduler in the foreground. See [Docker](docs/docker.md).

## Documentation

- [Configuration](docs/configuration.md): implemented sources, paths, precedence.
- [Service](docs/service.md): install, inspect, logs, upgrades, uninstall, and Linux lingering.
- [Design](docs/design.md): CLI, scheduler, jobs, events, MCP, security.
- [Development](docs/development.md): tooling, tests, commits, contribution workflow.
- `task backlog`: ready work in the authoritative Backlog.md queue.

MIT licensed. Tooling and Go CLI structure follow the sibling hum/project repos.
