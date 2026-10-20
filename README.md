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
    env_file: .env                # gitignored, read at each run
    timeout: 10m
  review:
    schedule: weekdays at 9am
    jitter: 15m                   # start between 09:00 and 09:15
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

After an upgrade, run `noon service install` explicitly: it reconciles changed units and known
running-build mismatches. Restarts interrupt active jobs rather than draining them. Unknown build
metadata needs explicit `noon service restart`; matching known builds remain running. When switching installation methods, invoke the new executable's
`service install` with the same state directory and config location to retain history and settings.
Job config reloads automatically and `env_file` is read at run start; neither needs restart.
Changed shared jobs still need `noon enable`. See [upgrades and migration](docs/service.md#upgrades).

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

## Personal jobs

Use `--user` for recurring work that belongs to no repository. Jobs live in
`$XDG_CONFIG_HOME/noon/config.yaml`, or `~/.config/noon/config.yaml`, and run with that directory
as their project root. Relative command, `cwd`, and prompt-file paths are based there, so scripts
can live beside the config:

```sh
config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
umask 077
mkdir -p "$config_home/noon/bin"
chmod 700 "$config_home/noon"
cp examples/local-dev/housekeeping/config.yaml "$config_home/noon/config.yaml"
cp examples/local-dev/housekeeping/bin/*.sh "$config_home/noon/bin/"
chmod 600 "$config_home/noon/config.yaml"
$EDITOR "$config_home/noon/config.yaml"
noon --user list
noon --user run disk-space
noon --user enable
noon service install
```

The service and `noon serve` run all enabled projects; do not pass `--user` to them. User jobs are
private to you (`shared: false`, no approval). A scheduled or queued `noon --user add` enables the
user project; `noon --user enable` opts in explicitly. `add`, `remove`, and `import crontab --write`
edit the user `config.yaml`; `add` rejects `--shared` and `--local`.

`--user` conflicts with `-C`/`--project`, `NOON_PROJECT`, and `--config`/`NOON_CONFIG`, and is
not supported by `serve`, `service`, `init`, or `shell`. Passing the user directory to `-C` selects
the same user project; it cannot be combined with `--config`. Regular project loads validate user
config but never include its jobs. An empty read-only `noon --user` shows the `user` project and
suggests `noon --user add` without creating files.

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
| `noon pause NAME...` / `resume NAME...` | Stop or restart background runs; `--all` / `-a` selects all current project jobs |
| `noon history [NAME...]` | Recent runs across selected jobs (`--status failed`) |
| `noon logs [NAME\|RUN]` | Read, search, page, or `--follow` a run log; opt into timestamps with `--timestamps` / `-t` |
| `noon enable` / `disable` | Opt this checkout in or out of background runs |
| `noon serve` | Foreground scheduler; optionally serve signed webhooks and an authenticated remote API/MCP listener |
| `noon service install` | Run `noon serve` as a per-user launchd/systemd service |
| `noon service restart` | Explicitly restart the installed scheduler; interrupts active jobs |
| `noon doctor` | Read-only setup checks |
| `noon tui` | Keyboard-driven jobs, runs, and logs |
| `noon mcp` | MCP tools over stdio for coding agents |
| `noon import crontab FILE` | Preview a crontab as Noon YAML; `--write` to add it |
| `noon shell init bash` | Shell completion; optional job on `cd` |
| `noon init` / `config` | Create `noon.yaml`; show or check settings |
| `noon version` / `noon --version` | Print version, short commit SHA, and build time |

Human inspection output uses bold headings and restrained status colors in terminals. Set
`NO_COLOR=1` or use `--color=never` to disable styling; `--color=always` forces it. JSON and
redirected output stay unstyled by default; job output and logs are left untouched.

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

In `noon tui`, press `?` for every shortcut. Log timestamps are shown by default; `t` toggles them.
Log search uses `/` and `n`/`N` to step through matches; `w` toggles line wrapping. The TUI never
edits jobs or enables or approves a project.

## Find a failure

```console
$ noon history --status failed
RUN  JOB    STATUS  TRIGGER   STARTED  DURATION  EXIT
7    check  failed  schedule  2h ago   3s        3

$ noon logs check --status failed --match FAIL --context 1 --timestamps
2026-10-19T09:15:30.125-06:00 2-ok 2
2026-10-19T09:15:30.125-06:00 3:FAIL widget
2026-10-19T09:15:30.126-06:00 4-ok 3
next: noon logs 7 --after-cursor 27 --timestamps
```

## Agents

Log timestamps are opt-in for CLI, JSON, MCP, and HTTP readers; CLI display uses local time while
structured `line_times` use UTC and raw-byte offsets. Times mark when Noon received output (not when
the child produced it); a partial line is timestamped from its first byte and streams without waiting
for a newline. Historical or incomplete timing metadata is unavailable, never estimated.

Give a coding agent the MCP server and it can list jobs, queue approved ones, and read logs. It
cannot add, edit, enable, or approve jobs.

```sh
claude mcp add noon -- noon mcp
codex mcp add noon -- noon mcp
```

For remote API and MCP clients, run `noon serve --listen ADDR --api-token-file FILE` and use a TLS
reverse proxy. See [Agents](docs/agents.md#remote-mcp-and-json-api) for client setup and
[Webhooks](docs/webhooks.md) for the shared listener. Agent-prompt jobs have built-in profiles for
Claude Code (`claude`), Codex (`codex`), Gemini CLI (`gemini`), OpenCode (`opencode`), and Pi (`pi`).
Install and authenticate the CLI separately, or configure your own
[agent profile](docs/configuration.md#agent-profiles). See [Agents](docs/agents.md).

## Documentation

- [Examples](examples/README.md): runnable projects for local dev, AI workflows, and integrations
- [Command reference](docs/cli.md): output, exit codes, queueing, history, logs, TUI
- [Configuration](docs/configuration.md): files, precedence, job fields, agent profiles, [editor schema](docs/configuration.md#editor-schema)
- [Agents](docs/agents.md): MCP, agent-facing jobs, reading failures
- [Lifecycle hooks](docs/hooks.md): notify on failure and recovery
- [Triggers](docs/triggers.md): Git hooks, Claude Code and Codex `Stop` hooks
- [Webhooks](docs/webhooks.md): signed GitHub webhooks
- [Shell setup](docs/shell.md): completion and project-entry jobs
- [Crontab import](docs/crontab.md)
- [Service](docs/service.md): launchd and systemd
- [Docker](docs/docker.md)
- [Design](docs/design.md): contracts and rationale
- [Changelog](CHANGELOG.md): changes generated from Conventional Commits
- [Development](docs/development.md)
