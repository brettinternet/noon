# bump

**Repo-local scheduling, without a platform.**

A Go CLI for recurring commands and agent work, designed for your machine first
and containers when needed. CLI now; optional TUI later. No required web UI,
Python, Node, cloud service, or agent provider.

**Status: foreground execution.** Working today: project discovery, layered settings,
non-destructive initialization, strict job validation, job listing with next-run
previews, foreground command execution, human/JSON output, and development tooling.
Scheduling, cron import, hooks, MCP, service installation, and TUI are designed but
**not implemented**. There is no background daemon yet.

## Try it

```sh
mise trust
mise exec -- task init
task cli:build

bin/bump                 # useful repo summary; starts nothing
bin/bump init            # create bump.yaml; preserve it if it exists
bin/bump list            # validated jobs and next-run previews; executes nothing
bin/bump run             # list runnable jobs; starts nothing
bin/bump run tests       # execute this job now, in the foreground
bin/bump config          # effective settings and source files
bin/bump config check    # validate without writing anything
bin/bump --json status
bin/bump --help
```

`bump init --local` creates a private `.bump.yaml` override instead. Ignore
`.bump.yaml` and `.bump/` in Git. Configuration layers are:
defaults → user config → `bump.yaml` → `.bump.yaml` → environment → CLI flags.

```yaml
version: 1
timezone: Local
jobs:
  tests:
    schedule: weekdays at 9am
    run: [go, test, ./...]
```

```sh
BUMP_TIMEZONE=UTC bin/bump config
bin/bump -C ../another-repo config
bin/bump --config ./custom.yaml config check
```

`--config` replaces automatic config files. No config is required for inspection.
No-argument commands do something useful without prompting or executing work.
Invalid commands and configuration fail instead of pretending to succeed.
Errors with `--json` produce one stdout object:
`{"schema_version":1,"error":{"code":"…","message":"…","hint":"…"}}` (`hint` is omitted
when empty). Human errors go to stderr. Exit codes are 0 for success, 2 for usage
errors, and 1 for other failures. Foreground runs instead return the child’s exit
code: 124 on timeout, 130 on cancellation, 128+N for signal N, and 125–127 on
start failure. Job output streams directly; status lines go to stderr.
`bump run NAME --json` sends both job streams to stderr and one run summary to
stdout. SIGINT, SIGTERM, or SIGHUP cancels the process group (TERM, then KILL
after 10s); a second signal kills it immediately. No run history is stored yet.

## Direction

Designed, not built yet ([design](docs/design.md), [decisions](backlog/decisions)):

- Command definitions, schedule parsing, and `bump run NAME` already work;
  agent prompts and unattended execution are still planned.
- One per-user `bump serve` runs every enabled repo's schedules;
  `bump service install` keeps it alive across reboots. Docker runs the same
  foreground process. Bump never backgrounds itself.
- Nothing from a repository runs unattended until you `bump enable` that
  checkout, and changed shared jobs wait for re-approval.
- After sleep, a missed job runs once, never in a burst.
- History, logs, hooks, Git and agent triggers, webhooks, MCP, and a TUI build
  on one store. Commands are the integration boundary: no plugin system.

## Docker bootstrap

```sh
task docker:smoke
docker run --rm bump:dev --help
```

The image currently supports the same inspection/config and foreground run commands as the host
binary. It does not run schedules. See [Docker](docs/docker.md).

## Documentation

- [Configuration](docs/configuration.md): implemented sources, paths, precedence.
- [Design](docs/design.md): proposed CLI, daemon decision, jobs, events, MCP, security.
- [Development](docs/development.md): tooling, tests, commits, contribution workflow.
- `task backlog`: ready work in the authoritative Backlog.md queue.

MIT licensed. Tooling and Go CLI structure follow the sibling hum/project repos.
