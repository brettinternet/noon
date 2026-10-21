# noon

Cron for repositories and coding agents. Jobs live in the repo, run on your machine or in a
container, leave history you can inspect, and never run unattended until you say so.

[![Demo of adding a noon job, running it, and checking the dashboard](docs/demo.gif)](docs/demo.tape)

<table>
<tr>
<td width="50%"><a href="docs/demo-tui.tape"><img src="docs/demo-tui.gif" alt="Demo of the noon TUI opening a failed run's log, searching it, and queuing a job"></a><br>Browse jobs, runs, and logs in the TUI</td>
<td width="50%"><a href="docs/demo-logs.tape"><img src="docs/demo-logs.gif" alt="Demo of queuing a noon job, following its log, and finding the failing line"></a><br>Queue a job, follow its log, find the failure</td>
</tr>
</table>

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
    env_file: .env                # gitignored, read at each run
    timeout: 10m
  review:
    schedule: weekdays at 9am
    jitter: 15m                   # start between 09:00 and 09:15
    agent: claude
    prompt: Review open PRs and list risks.
```

## Install

macOS or Linux, amd64 or arm64:

```sh
brew install brettinternet/tap/noon                        # Homebrew
mise use -g github:brettinternet/noon@latest               # or mise (pin with @0.1.0)
mise trust && mise exec -- task init && task cli:build     # or from source → bin/noon
```

If mise's minimum-release-age policy hides a new release from `latest`, pin the version.

Installing never starts a scheduler. Start one explicitly, and re-run install after each upgrade:

```sh
noon service install   # per-user launchd/systemd service; reconciles upgrades
noon serve             # or run in the foreground
```

Restarts interrupt active jobs. Config edits and `env_file` changes need no restart. See
[upgrades and migration](docs/service.md#upgrades).

## Quick start

`noon add` writes to the private `.git/noon.yaml`, which Git can't commit. `--shared` writes
`noon.yaml` instead.

```console
$ noon add tests "every day at 2am" -- go test ./...
Added tests to .git/noon.yaml (private) · every day at 02:00 · next Sat 02:00
Scheduling enabled for /home/me/app.
The scheduler isn't running, so nothing runs in the background.
Suggested action: noon service install · noon run tests

$ noon run tests
▶ tests · run 1 · "go" "test" "./..."
ok      example.com/app    0.412s
✓ tests succeeded in 1.2s

$ noon
app · /home/me/app · scheduler offline · America/Denver
JOB    SCHEDULE            NEXT       LAST
tests  every day at 02:00  Sat 02:00  ✓ now (1s)
Scheduler offline; background work will wait.
Suggested action: noon service install
```

Bare `noon` shows the project and one suggested next step. It never starts anything.

## Background runs are opt-in

Cloning or pulling a repository can't schedule commands on your machine:

```console
$ git clone https://example.com/team/app && cd app
$ noon
app · /home/me/app · scheduler offline · Local
JOB    SCHEDULE  NEXT            LAST
lint   every 6h  needs approval  never
Not enabled for background runs.
Suggested action: noon enable

$ noon enable
Enabled /home/me/app · 1 jobs · approved 1 shared jobs
```

| Job source | Background runs need |
| --- | --- |
| `noon.yaml` (shared) | `noon enable`, again after a pull changes the job |
| `.git/noon.yaml` (private) | `noon enable` once; no approval |
| `noon run NAME` (foreground) | nothing |

`noon disable` revokes everything and keeps history. Approval covers job definitions, not the
scripts or prompt files they reference, so only enable repositories you would run by hand. Noon
ignores an in-tree `.noon.yaml`, since a commit could supply it.

## Personal jobs

`--user` selects jobs that belong to no repository. They live in `~/.config/noon/config.yaml`
(honoring `$XDG_CONFIG_HOME`), run from that directory, and need no approval:

```sh
noon --user add disk-space "every day at 9am" -- ./bin/disk-space.sh
noon --user list
noon --user run disk-space
```

The service runs them with every other enabled project. See [user project](docs/configuration.md#user-project)
and [examples/local-dev/housekeeping](examples/local-dev/housekeeping/).

## Commands

| Command | What it does |
| --- | --- |
| `noon` / `noon status` | Dashboard: jobs, scheduler, queued work, next step |
| `noon list` | Jobs and next run times |
| `noon add NAME [SCHEDULE] -- CMD…` | Add a job (`--sh`, `--prompt`, `--prompt-file`, `--shared`) |
| `noon remove NAME...` | Remove job definitions; history stays |
| `noon run NAME` | Run now; returns 0 when `noop_exit_code` matches |
| `noon run NAME... --detach` | Queue jobs for the scheduler (`--in 2h`, `--at 17:00`, `--key`, `--data`) |
| `noon cancel RUN\|NAME...` | Cancel selected runs or jobs' active work |
| `noon pause NAME...` / `resume NAME...` | Stop or restart background runs; `-a` for all jobs |
| `noon history [NAME...]` | Recent runs (`--status failed`) |
| `noon logs [NAME\|RUN]` | Read, search, page, or `--follow` a run log (`-t` for timestamps) |
| `noon enable` / `disable` | Opt this checkout in or out of background runs |
| `noon serve` | Foreground scheduler; optional webhooks and remote API/MCP |
| `noon service install` / `restart` | Per-user launchd/systemd service; restart interrupts jobs |
| `noon doctor` | Read-only setup checks |
| `noon tui` | Keyboard-driven jobs, runs, and logs (`?` for keys) |
| `noon mcp` | MCP tools over stdio for coding agents |
| `noon import crontab FILE` | Preview a crontab as Noon YAML; `--write` to add it |
| `noon shell init bash` | Shell completion; optional job on `cd` |
| `noon init` / `config` | Create `noon.yaml`; show or check settings |
| `noon version` | Version, commit, and build time |

Finishing commands accept `-j` / `--json`. `NO_COLOR=1` or `--color=never` disables styling.
Commands that need a target list candidates or print examples instead of guessing:

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

$ noon logs check --status failed --match FAIL --context 1 --timestamps
2026-10-19T09:15:30.125-06:00 2-ok 2
2026-10-19T09:15:30.125-06:00 3:FAIL widget
2026-10-19T09:15:30.126-06:00 4-ok 3
Continue: noon logs 7 --after-cursor 27 --timestamps
```

## Agents

With MCP, a coding agent can list jobs, queue approved ones, and read logs. It can't add, edit,
enable, or approve jobs.

```sh
claude mcp add noon -- noon mcp
codex mcp add noon -- noon mcp
```

Prompt jobs have built-in profiles for `claude`, `codex`, `gemini`, `opencode`, and `pi`; install
and authenticate the CLI yourself. See [Agents](docs/agents.md) for remote MCP and prompt jobs.

## Documentation

- [Examples](examples/README.md): runnable projects for local dev, AI workflows, and integrations
- [Command reference](docs/cli.md): output, exit codes, queueing, history, logs, TUI
- [Configuration](docs/configuration.md): files, precedence, job fields, agent profiles, [editor schema](docs/configuration.md#editor-schema)
- [Agents](docs/agents.md): MCP, agent-facing jobs, reading failures
- [Lifecycle hooks](docs/hooks.md): notify on failure and recovery
- [Triggers](docs/triggers.md): Git hooks, Claude Code and Codex `Stop` hooks
- [Webhooks](docs/webhooks.md): signed GitHub webhooks and the remote API listener
- [Shell setup](docs/shell.md): completion and project-entry jobs
- [Crontab import](docs/crontab.md)
- [Service](docs/service.md): launchd and systemd
- [Docker](docs/docker.md)
- [Design](docs/design.md): contracts and rationale
- [Changelog](CHANGELOG.md): changes generated from Conventional Commits
- [Development](docs/development.md)
