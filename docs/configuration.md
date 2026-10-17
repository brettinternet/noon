# Configuration

## Files

Later sources override earlier ones, one setting at a time:

1. Built-in defaults
2. User config: `$XDG_CONFIG_HOME/noon/config.yaml`, else `~/.config/noon/config.yaml`
3. `noon.yaml` in the project (shared, committed)
4. `.noon.yaml` in the project (private, ignored by Git)
5. Environment variables
6. CLI flags

```console
$ noon config
Project:       /home/me/app
Timezone:      Local
State dir:     /home/me/.local/state/noon
Default agent: (none)
Files (low → high precedence):
  /home/me/app/noon.yaml
  /home/me/app/.noon.yaml
Agents:
  claude: "claude" "-p" "--permission-prompts" "none" (stdin)
  codex: "codex" "exec" "-" (stdin)
CLI flags override environment variables; both override files.
```

`noon config check` validates the same files without printing them. Neither checks that job
commands or agent CLIs exist; `noon doctor` does.

`--config FILE` (or `NOON_CONFIG`) replaces all three files. The file must exist. Defaults,
environment, and flags still apply. This keeps CI and container runs reproducible. It doesn't
change which project is selected; use `-C` for that.

```sh
NOON_TIMEZONE=UTC noon config
noon -C ../other-repo list
noon --config ./deploy.yaml config check
```

`noon init` creates `noon.yaml`; `noon init --local` creates `.noon.yaml` with mode 0600. Neither
overwrites an existing file. In a Git checkout, local init preserves effective ignore rules or
appends `/.noon.yaml` to the selected project's `.gitignore`, preserving existing contents.
Repeated init also protects an existing private manifest without rewriting it. If protection fails
(or the manifest is already tracked), init reports an error; it never untracks files. Outside Git,
it reports that ignore protection is not applicable and does not create a repository. Shared init
does not change ignore rules. Private manifests created by other means still need ignore protection.

## Where keys are allowed

| Key | User config | `noon.yaml` | `.noon.yaml` |
| --- | --- | --- | --- |
| `version`, `timezone` | yes | yes | yes |
| `jobs` | no | yes | yes |
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

## Project discovery

Noon walks up from the current directory to the nearest directory containing `noon.yaml`,
`.noon.yaml`, or `.git` (including a worktree's `.git` file), and stops there. It never reads
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
| `prompt` | — | Inline agent prompt, ≤ 64 KiB |
| `prompt_file` | — | Prompt read when the run starts, ≤ 64 KiB |
| `agent` | `default_agent` | Agent profile for prompt jobs |
| `cwd` | project root | Absolute or project-relative; checked by `doctor`, not validation |
| `env` | — | String values (quote numbers). `NOON_*` is reserved. Values are never printed |
| `timeout` | `1h` | `30s`, `10m`, `1h30m`, `2d` (≥ 1s), or `none` |
| `overlap` | `skip` | `skip` or `queue` (at most one pending) |
| `missed` | `run_once` | `run_once` or `skip`: what to do after sleep or downtime |
| `enabled` | `true` | `false` disables the job |
| `cooldown` | — | Minimum gap between queued requests ([Triggers](triggers.md)) |
| `on_success`, `on_failure` | user default | Hook command; `[]` disables the default ([Hooks](hooks.md)) |
| `webhook` | — | `{secret_env: NAME}` ([Webhooks](webhooks.md)) |

Unknown fields, duplicate keys, nulls, aliases, wrong types, multiple documents, and files over
1 MiB fail with the file, line, and YAML path:

```console
$ noon list
noon: config /home/me/app/noon.yaml:5: jobs.lint.run: must be a string (quote numbers)
```

A missing file, a zero-byte file, and `{}` are fine; a whitespace-only file is not. A broken lower-priority file is
still an error even if a later file overrides its settings.

### Local overrides

A job in `.noon.yaml` replaces the shared job of the same name entirely; fields don't merge. The
one exception turns a shared job off without copying it:

```yaml
# .noon.yaml
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

`add` writes `.noon.yaml` by default and edits lines in place, keeping comments and formatting.
Unusual `jobs` layouts must be edited by hand. `add`, `remove`, and `import --write` refuse to edit
a `--config` file. If a name exists in both files, `remove` needs `--shared` or `--local`.

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
timeout, overlap, missed, cooldown, hooks, and webhook. Changing the description doesn't require
re-approval. Agent profile commands and user-config hook defaults are yours and excluded. **The
digest doesn't cover the contents of referenced scripts or prompt files.**

`noon enable` records the checkout's canonical path and its config selection (automatic files or
the absolute `--config` path), then approves every current shared job. Re-running it keeps
existing approval times. `noon disable` revokes opt-in and approvals without reading project
files, so a broken edit can't block it. If user config is broken too, pass `--state-dir`.

`noon serve` reloads config every two seconds. A broken edit keeps the last valid jobs running and
shows the error in `noon status` until fixed.
