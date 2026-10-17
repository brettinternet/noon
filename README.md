# noon

Cron for repositories and coding agents. Jobs live in the repo, run on your machine or in a
container, leave history you can inspect, and never run unattended until you say so.

```yaml
# noon.yaml
version: 1
timezone: America/Denver
jobs:
  tests:
    schedule: every day at 2am
    run: [go, test, ./...]        # list: exact argv
  deps:
    schedule: "0 6 * * 1"
    run: ./scripts/check-deps     # string: /bin/sh -c
    timeout: 10m
  review:
    schedule: weekdays at 9am
    agent: claude
    prompt: Review open PRs and list risks.
```

## Install

Install on macOS or Linux (Intel/amd64 or arm64) with Homebrew:

```sh
brew install brettinternet/tap/noon
brew upgrade noon
```

Or use mise directly without custom asset matching:

```sh
mise exec github:brettinternet/noon@latest -- noon --version
mise exec github:brettinternet/noon@0.1.0 -- noon --version  # pinned version
# To add the latest version to your global mise config:
mise use -g github:brettinternet/noon@latest
```

If mise's minimum-release-age policy hides a new release from `latest`, use the explicit version
or wait until it becomes eligible. No custom asset matching is needed.

To build from source with [mise](https://mise.jdx.dev):

```sh
mise trust && mise exec -- task init
task cli:build        # writes bin/noon
```

Installing a binary never installs or starts a scheduler service. Set up background scheduling
explicitly with `noon service install` after installation; use `noon serve` to run it in the
foreground instead.

## Quick start

`noon add` writes a job to the private `.git/noon.yaml`, which Git can't commit. Add `--shared` to
write `noon.yaml` instead.

```console
$ noon add tests "every day at 2am" -- go test ./...
Added tests to .git/noon.yaml (private) · every day at 02:00 · next Sat 02:00
Scheduling enabled for /home/me/app.
The scheduler isn't running, so nothing runs in the background.
Next: noon service install · noon run tests

$ noon run tests
▶ tests · run 1 · "go" "test" "./..."
ok      example.com/app    0.412s
✓ tests succeeded in 1.2s

$ noon
app · /home/me/app · scheduler offline · America/Denver
JOB    SCHEDULE            NEXT       LAST
tests  every day at 02:00  Sat 02:00  ✓ now (1s)
Scheduler offline; background work will wait.
Next: noon service install
```

Nothing has run in the background yet. Scheduled runs need a scheduler:

```sh
noon service install   # once per machine: launchd or systemd user service
noon serve             # or run the scheduler in the foreground (Ctrl-C to stop)
```

Bare `noon` always shows the current project and one suggested next step. It never starts anything.

## Background runs are opt-in

A repository can't schedule commands on your machine by being cloned or pulled.

```console
$ git clone https://example.com/team/app && cd app
$ noon
app · /home/me/app · scheduler offline · Local
JOB    SCHEDULE  NEXT            LAST
lint   every 6h  needs approval  never
Not enabled for background runs.
Next: noon enable

$ noon enable
Enabled /home/me/app · 1 jobs · approved 1 shared jobs
```

`noon enable` approves the current definition of each job in `noon.yaml`. If a pull changes a
job, that job waits as `needs approval` until you run `noon enable` again. Jobs in your private
`.git/noon.yaml` don't need approval. `noon run NAME` runs in the foreground and never needs approval.
`noon disable` revokes everything and keeps history.

Approval covers job definitions, not the scripts or prompt files they reference. Only enable
repositories you would run by hand. Noon ignores an in-tree `.noon.yaml`, since a commit could
supply it.

## Commands

| Command | What it does |
| --- | --- |
| `noon` / `noon status` | Dashboard: jobs, scheduler, queued work, next step |
| `noon list` | Jobs and next run times |
| `noon add NAME [SCHEDULE] -- CMD…` | Add a job (`--sh`, `--prompt`, `--prompt-file`, `--shared`) |
| `noon remove NAME` | Remove a job definition; history stays |
| `noon run NAME` | Run now in the foreground; exits with the job's code |
| `noon run NAME --detach` | Queue for the scheduler (`--in 2h`, `--at 17:00`, `--key`, `--data`) |
| `noon cancel RUN\|NAME` | Cancel queued or running work |
| `noon pause NAME` / `resume NAME` | Stop or restart background runs of one job |
| `noon history` | Recent runs (`--status failed`) |
| `noon logs [NAME\|RUN]` | Read, search, page, or `--follow` a run log |
| `noon enable` / `disable` | Opt this checkout in or out of background runs |
| `noon serve` | Foreground scheduler for every enabled project |
| `noon service install` | Run `noon serve` as a per-user launchd/systemd service |
| `noon doctor` | Read-only setup checks |
| `noon tui` | Keyboard-driven jobs, runs, and logs |
| `noon mcp` | MCP tools over stdio for coding agents |
| `noon import crontab FILE` | Preview a crontab as Noon YAML; `--write` to add it |
| `noon shell init bash` | Shell completion; optional job on `cd` |
| `noon init` / `config` | Create `noon.yaml`; show or check settings |

Commands that finish accept `-j` / `--json` and print one object. Everyday shortcuts include
`logs -f`, `history -n 5 -s failed`, `run NAME -d`, and `add NAME -a claude -p "Review" -t 10m`.
See [short flags](docs/cli.md#short-flags) for the full mapping. Commands that take a target list
candidates or print examples when run without one:

```console
$ noon add
Examples:
  noon add tests "every day at 2am" -- go test ./...
  noon add lint "every 30m" --sh 'golangci-lint run ./...'
  noon add review "weekdays at 9am" --agent claude --prompt "Review open PRs and list risks"
  noon add notes --agent claude --prompt-file prompts/notes.md
  noon add check-ci --in 2h --agent claude --prompt "Check CI and fix failures"
```

## Find a failure

```console
$ noon history --status failed
RUN  JOB    STATUS  TRIGGER   STARTED  DURATION  EXIT
7    check  failed  schedule  2h ago   3s        3

$ noon logs check --status failed --match FAIL --context 1
2-ok 2
3:FAIL widget
4-ok 3
next: noon logs 7 --after-cursor 27
```

## Agents

Give a coding agent the MCP server and it can list jobs, queue approved ones, and read logs. It
cannot add, edit, enable, or approve jobs.

```sh
claude mcp add noon -- noon mcp
codex mcp add noon -- noon mcp
```

Agent-prompt jobs run your installed `claude` or `codex` CLI, or any CLI you configure. See
[Agents](docs/agents.md).

## Documentation

- [Examples](examples/README.md): runnable projects for local dev, AI workflows, and integrations
- [Command reference](docs/cli.md): output, exit codes, queueing, history, logs, TUI
- [Configuration](docs/configuration.md): files, precedence, job fields, agent profiles
- [Agents](docs/agents.md): MCP, agent-facing jobs, reading failures
- [Lifecycle hooks](docs/hooks.md): notify on failure and recovery
- [Triggers](docs/triggers.md): Git hooks, Claude Code and Codex `Stop` hooks
- [Webhooks](docs/webhooks.md): signed GitHub webhooks
- [Shell setup](docs/shell.md): completion and project-entry jobs
- [Crontab import](docs/crontab.md)
- [Service](docs/service.md): launchd and systemd
- [Docker](docs/docker.md)
- [Design](docs/design.md): contracts and rationale
- [Development](docs/development.md)

## Upgrading from bump

Noon was called bump. With every old and new process stopped and a backup taken:

```sh
mv ~/.local/state/bump ~/.local/state/noon
mv ~/.config/bump ~/.config/noon
cd ~/.local/state/noon && mv bump.db noon.db
mv bump.db-wal noon.db-wal; mv bump.db-shm noon.db-shm   # if present
```

Don't overwrite existing `noon` directories. Leave `logs/` as is.

MIT licensed.
