# Configuration

## Files

For repository projects, later sources override earlier ones, one setting at a time:

1. Built-in defaults
2. User settings: `$XDG_CONFIG_HOME/noon/config.yaml`, else `~/.config/noon/config.yaml`
3. `noon.yaml` in the project (shared, committed)
4. `.git/noon.yaml` in the project's Git directory (private, can't be committed)
5. Environment variables
6. CLI flags

Select the separate user project with `--user`; its jobs come only from the user config's
`jobs:` mapping. A normal project validates user config but never imports those jobs.

```console
$ noon config
Project:       /home/me/app
Timezone:      Local
State dir:     /home/me/.local/state/noon
Default agent: (none)
Files (low → high precedence):
  /home/me/app/noon.yaml
  /home/me/app/.git/noon.yaml
Agents:
  claude: "claude" "-p" "--permission-prompts" "none" (stdin)
  codex: "codex" "exec" "-" (stdin)
CLI flags override environment variables; both override files.
```

`noon config check` validates the same files without printing them. Neither checks that job
commands or agent CLIs exist; `noon doctor` does.

`--config FILE` (or `NOON_CONFIG`) replaces all three repository files. The file must exist.
Defaults, environment, and flags still apply. This keeps CI and container runs reproducible. It
doesn't change which project is selected; use `-C` for that. Every normal load still validates the
user config, including when `--config` is selected, but it never imports user jobs into a repository
project.

```sh
NOON_TIMEZONE=UTC noon config
noon -C ../other-repo list
noon --config ./deploy.yaml config check
```

`noon init` creates `noon.yaml`; `noon init --local` creates `.git/noon.yaml` with mode 0600.
Neither overwrites an existing file.

### Private config

Repository-private jobs run unattended without approval, so only a file that repository content can't supply
is private. That file is `noon.yaml` inside the project's Git directory. Git refuses to commit or
check out paths there, so no branch or pull request can add or replace it. A linked worktree uses
the main checkout's Git directory, so one `.git/noon.yaml` applies to the main checkout and every
worktree. Each checkout still needs its own `noon enable`. Noon finds the directory by reading the
project's `.git` directory or `.git` file and `commondir`. It never runs `git`. A malformed `.git`
file is a config error.

Noon ignores an in-tree `.noon.yaml`, even an untracked one, because a commit can supply that
filename. A project with no `.git` has no private config: `init --local`, `add` without
`--shared`, and `import --write` refuse and suggest `noon.yaml` instead.

## Editor schema

The published [JSON Schema](../noon.schema.json) provides completion and structural validation
for shared `noon.yaml` files in editors with YAML language-server support. Add this comment:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/brettinternet/noon/main/noon.schema.json
version: 1
```

The URL tracks `main`; replace `main` with a commit SHA to pin the schema. This schema covers
shared project files, not user config, private `.git/noon.yaml` overrides, or all-in-one `--config`
files. It rejects unknown fields and invalid job shapes, including conflicting actions and
reserved environment names. Run `noon config check` for full validation: schedule and duration
grammar, timezones, agent resolution, byte limits, and YAML-specific restrictions still need Noon.
Use the comment, not a `$schema` YAML key, which Noon rejects as an unknown field.

## Where keys are allowed

| Key | User config | `noon.yaml` | `.git/noon.yaml` |
| --- | --- | --- | --- |
| `version`, `timezone` | yes | yes | yes |
| `jobs` | yes | yes | yes |
| `default_agent` | yes | no | yes |
| `state_dir`, `agents`, `on_success`, `on_failure` | yes | no | no |

A `--config` file accepts every key. A key in the wrong file is an error naming the right one.

## Settings

| Setting | Default | Environment | Flag |
| --- | --- | --- | --- |
| `timezone` | `Local` | `NOON_TIMEZONE` | `--timezone` |
| `state_dir` | `$XDG_STATE_HOME/noon`, else `~/.local/state/noon` | `NOON_STATE_DIR` | `--state-dir` |
| Project | discovery | `NOON_PROJECT` | `-C`, `--project` |
| Config file | the three files above | `NOON_CONFIG` | `--config` |

Timezones are `Local`, `UTC`, or IANA names like `America/Chicago`. Timezone data is built in.
An empty `NOON_*` variable is an error; unset it instead.

Paths:

- A path in a YAML setting is relative to that file's directory.
- Job `cwd` and `prompt_file` are relative to the project root.
- Flag and environment paths are relative to where you ran the command.
- YAML never expands `~` or `$VARS`. `$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` must be absolute.

## User project

The user project is the canonical directory `$XDG_CONFIG_HOME/noon`, else `~/.config/noon`.
Its only job source is `config.yaml` in that directory. Noon never imports a `noon.yaml`,
`.git/noon.yaml`, or config above it, even when those files exist there. Select it with
`noon --user`; passing that directory to `-C` selects the same project and the same config
rules. Its identity is the absolute directory path.

User jobs are yours: they have `shared: false`, require no approval, and run with the user project
as their default `cwd`. Relative `cwd`, `prompt_file`, and command paths are based there. YAML
never expands `~`. `noon --user add` creates the directory with mode 0700 and `config.yaml` with
mode 0600; `remove` and `import crontab --write` edit that file. `add` rejects `--shared` and
`--local`. A scheduled or queued `add` enables this project. Use `noon --user enable` to opt in
explicitly; the normal `noon serve` / service runs it like any other enabled project.

`--user` conflicts with `-C`/`--project`, `NOON_PROJECT`, and `--config`/`NOON_CONFIG`; it is
not supported by `serve`, `service`, `init`, or `shell`. Selecting the user directory with `-C`
also conflicts with `--config`. Read-only `noon --user` works when no config exists: it shows an
empty `user` project and suggests `noon --user add` without creating files. Invalid user jobs are
still validated on every normal project load, but valid user jobs never appear in repository jobs.

## Project discovery

Noon walks up from the current directory to the nearest directory containing `noon.yaml` or
`.git` (including a worktree's `.git` file), and stops there. It never reads
config above a Git boundary. With no marker, the current directory is the project. A nested
`noon.yaml` makes a nested project. Discovery only reads files; it never runs anything.

## Jobs

```yaml
jobs:
  tests:
    description: Weekday test suite
    schedule: weekdays at 9am
    run: [go, test, ./...]
    env:
      GOFLAGS: "-count=1"
    timeout: 10m
```

Each job needs exactly one of `run`, `prompt`, or `prompt_file`. Names match
`^[a-z][a-z0-9_-]{0,63}$`, so a bare number on the command line is always a run ID.
[`examples/jobs.yaml`](../examples/jobs.yaml) uses every field.

| Field | Default | Values |
| --- | --- | --- |
| `description` | — | Shown by `list`, the TUI, and MCP |
| `schedule` | manual only | Cron, macro, calendar phrase, or interval ([grammar](design.md#schedules)) |
| `run` | — | String runs with `/bin/sh -c`; list is exact argv |
| `noop_exit_code` | — | Integer 1–124: record a normally exited run with this code as `noop` |
| `prompt` | — | Inline agent prompt, ≤ 64 KiB |
| `prompt_file` | — | Prompt read when the run starts, ≤ 64 KiB |
| `agent` | `default_agent` | Agent profile for prompt jobs |
| `cwd` | project root | Absolute or project-relative; checked by `doctor`, not validation |
| `env` | — | String values (quote numbers). `NOON_*` is reserved. Values are never printed |
| `timeout` | `1h` | `30s`, `10m`, `1h30m`, `2d` (≥ 1s), or `none` |
| `overlap` | `skip` | `skip` or `queue` (at most one pending) |
| `requests` | `merge` | `merge` due-now requests or queue `each` as a separate run ([Triggers](triggers.md)) |
| `missed` | `run_once` | `run_once` or `skip`: what to do after sleep or downtime |
| `enabled` | `true` | `false` disables the job |
| `os` | — | Non-empty list from `darwin`, `linux` |
| `arch` | — | Non-empty list from `amd64`, `arm64` |
| `cooldown` | — | Minimum gap between queued requests; incompatible with `requests: each` ([Triggers](triggers.md)) |
| `on_success`, `on_failure` | user default | Hook command; `[]` disables the default ([Hooks](hooks.md)) |
| `webhook` | — | `{secret_env: NAME}` ([Webhooks](webhooks.md)) |

A job's optional `os` and `arch` lists must be non-empty, duplicate-free, and use the exact supported Go `GOOS`/`GOARCH` values shown above. Aliases such as `macos`, `x86_64`, and `aarch64` are rejected with a suggestion. Every declared field must match the running binary; an omitted field matches any value. A nonmatching job stays visible as `other platform`, but isn't scheduled, caught up, dispatched, or runnable by `noon run` or any queueing interface. `list --json` includes the declared lists and `platform_supported`; `doctor` skips command and working-directory checks for nonmatching jobs.

Unknown fields, duplicate keys, nulls, aliases, wrong types, multiple documents, and files over
1 MiB fail with the file, line, and YAML path:

```console
$ noon list
noon: config /home/me/app/noon.yaml:5: jobs.lint.run: must be a string (quote numbers)
```

A missing file, a zero-byte file, and `{}` are fine; a whitespace-only file is not. A broken lower-priority file is
still an error even if a later file overrides its settings.

### Local overrides

A job in `.git/noon.yaml` replaces the shared job of the same name entirely; fields don't merge.
The one exception turns a shared job off without copying it:

```yaml
# .git/noon.yaml
jobs:
  tests: {enabled: false}
```

`noon list` marks both kinds as overrides.

## Agent profiles

Prompt jobs pipe the prompt to an agent CLI. Profiles are machine choices, so they live in user
config. `claude` and `codex` are built in; define the same name to replace one:

```yaml
# ~/.config/noon/config.yaml
default_agent: claude
agents:
  claude:
    command: [claude, -p, --permission-prompts, none, --permission-mode, acceptEdits]
  reviewer:
    command: [my-agent, --quiet]
    prompt: argument
```

| Built-in | Command |
| --- | --- |
| `claude` | `claude -p --permission-prompts none` |
| `codex` | `codex exec -` |

`prompt: stdin` (default) sends the exact prompt bytes on stdin. `prompt: argument` appends them as
the last argument. Nothing is templated, expanded, or trimmed. Unattended runs must never wait on
a permission prompt, so profiles should deny or pre-approve permissions.

A missing, unreadable, or oversized prompt file fails before the agent starts, with exit 125.
Prompt files must be regular files. Prompt runs get `NOON_AGENT=NAME`.

## Editing jobs

```sh
noon add tests "every day at 2am" -- go test ./...     # argv
noon add lint "every 30m" --sh 'golangci-lint run'     # shell string
noon add review "weekdays at 9am" --agent claude --prompt "Review open PRs"
noon add notes --agent claude --prompt-file prompts/notes.md
noon add check-ci --in 2h --agent claude --prompt "Check CI"  # queue a first run
noon remove tests                                      # alias rm; history stays
noon pause tests                                       # stop background runs
noon resume tests                                      # restart from now, no catch-up
```

`add` writes the private `.git/noon.yaml` by default and edits lines in place, keeping comments and formatting.
Unusual `jobs` layouts must be edited by hand. With `--user`, `add`, `remove`, and
`import crontab --write` edit the user project's `config.yaml` instead. `add` rejects
`--shared`/`--local` in that scope. `add`, `remove`, and `import --write` refuse to edit
a `--config` file. If a name exists in both repository files, `remove` needs `--shared` or `--local`.

`add` enables the project when the job has a schedule or queued run, and approves only that job.
Paused jobs still run with `noon run`.

## Listing jobs

```console
$ noon list
Scheduler offline
JOB    SCHEDULE           NEXT                LAST   COMMAND
hello  manual             -                   never  echo hello from $NOON_JOB
tests  weekdays at 09:00  Mon 09:00 (in 64h)  never  "go" "version"
2 jobs · 0 overrides
```

Times use the project timezone. Intervals preview `now + interval` until `noon serve` records their
phase. `noon list --json` adds each job's `source`, `shared`, `overrides`, `digest`, `approval`
(`not_required`, `approved`, `needs_approval`), sorted `env_keys`, and `webhook_path`. Environment
values are never printed; still, keep secrets out of commands and descriptions.

## Approval

Shared jobs (from `noon.yaml` or `--config`) run in the background only while their digest matches
the one `noon enable` approved. Private jobs need no approval. A local `enabled: false` toggle keeps
the shared job's approval requirement.

The digest is a SHA-256 of the job's execution fields: schedule, action, agent name, cwd, env,
timeout, `noop_exit_code`, overlap, missed, cooldown, hooks, webhook, and platform constraints
(`os` and `arch`). `requests` joins the digest only when
explicitly present, so jobs that omit it keep their existing digest and approval. Changing the
description doesn't require re-approval. Agent profile commands and user-config hook defaults are
yours and excluded. **The digest doesn't cover the contents of referenced scripts or prompt files.**

`noon enable` records the project's canonical path and its config selection (automatic files or
the absolute `--config` path), then approves every current shared job. Re-running it keeps
existing approval times. `noon disable` revokes opt-in and approvals without reading project
files, so a broken edit can't block it. If user config is broken too, pass `--state-dir`.

`noon serve` reloads config every two seconds. A broken edit keeps the last valid jobs running and
shows the error in `noon status` until fixed.
