# bump

**Repo-local scheduling, without a platform.**

A Go CLI for recurring commands and agent work, designed for your machine first
and containers when needed. CLI now; optional TUI later. No required web UI,
Python, Node, cloud service, or agent provider.

**Status: bootstrap.** Working today: project discovery, layered settings,
non-destructive initialization, validation, human/JSON output, and development
tooling. Job execution, scheduling, cron import, hooks, MCP, service installation,
and TUI are designed but **not implemented**. There is no background daemon yet.

## Try it

```sh
mise trust
mise exec -- task init
task cli:build

bin/bump                 # useful repo summary; starts nothing
bin/bump init            # create bump.yaml; preserve it if it exists
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
```

```sh
BUMP_TIMEZONE=UTC bin/bump config
bin/bump -C ../another-repo config
bin/bump --config ./custom.yaml config check
```

`--config` replaces automatic config files. No config is required for inspection.
No-argument commands do something useful without prompting or executing work.
Invalid commands and configuration fail instead of pretending to succeed.

## Direction

Designed, not built yet ([design](docs/design.md), [decisions](backlog/decisions)):

- Jobs in `bump.yaml` (shared) or `.bump.yaml` (private): shell strings or exact
  argv, or prompts for the agent CLI you already use. Schedules are cron or plain
  forms like `weekdays at 9am` and `every 6h`.
- `bump run NAME` runs a job in the foreground with no daemon.
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

The image currently supports the same inspection/config commands as the host
binary. It does not run schedules. See [Docker](docs/docker.md).

## Documentation

- [Configuration](docs/configuration.md): implemented sources, paths, precedence.
- [Design](docs/design.md): proposed CLI, daemon decision, jobs, events, MCP, security.
- [Development](docs/development.md): tooling, tests, commits, contribution workflow.
- `task backlog`: ready work in the authoritative Backlog.md queue.

MIT licensed. Tooling and Go CLI structure follow the sibling hum/project repos.
