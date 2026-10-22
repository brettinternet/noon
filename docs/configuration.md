# Configuration

## Files

For repository projects, later sources override earlier ones, one setting at a time:

1. Built-in defaults
2. User settings: `$XDG_CONFIG_HOME/noon/config.yaml`, else `~/.config/noon/config.yaml`
3. `noon.yaml` in the project (shared, committed)
4. `.git/noon.yaml` in the project's Git directory (private, can't be committed)
5. Environment variables
6. CLI flags

User config contributes settings, not jobs; its `jobs:` belong to the separate
[user project](#user-project).

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
  claude: "claude" "-p" "--permission-prompts" "none" (stdin, session_id_flag=--session-id)
  codex: "codex" "exec" (stdin, prompt_args=["-"])
CLI flags override environment variables; both override files.
```

`noon config check` validates without printing. Neither checks that commands or agent CLIs exist;
`noon doctor` does.

```sh
NOON_TIMEZONE=UTC noon config
noon -C ../other-repo list                 # select another project
noon --config ./deploy.yaml config check   # replace all three files, e.g. for CI
```

`--config FILE` (or `NOON_CONFIG`) must exist and replaces the three files above; defaults,
environment, and flags still apply. It doesn't select the project.

`noon init` creates `noon.yaml`; `noon init --local` creates `.git/noon.yaml` (mode 0600). Neither
overwrites.

### Private config

Private jobs run unattended without approval, so they must live where repository content can't
reach: `noon.yaml` inside the Git directory. Git refuses to commit or check out paths there.

| Checkout | Private file |
| --- | --- |
| Normal clone | `.git/noon.yaml` |
| Linked worktree | The main checkout's `.git/noon.yaml`, shared by all worktrees (each still needs `noon enable`) |
| No `.git` | None; `init --local`, private `add`, and `import --write` refuse |

Noon reads `.git` and `commondir` itself; it never runs `git`. An in-tree `.noon.yaml` is ignored,
even untracked, because a commit could supply it.

## Editor schema

The [JSON Schema](../noon.schema.json) gives editors completion and structural checks for shared
`noon.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/brettinternet/noon/main/noon.schema.json
version: 1
```

Replace `main` with a commit SHA to pin it. Use the comment, not a `$schema` key (an unknown field).
The schema doesn't cover user config, `.git/noon.yaml`, or `--config` files, nor schedule grammar,
timezones, or agent resolution; run `noon config check` for those.

## Where keys are allowed

| Key | User config | `noon.yaml` | `.git/noon.yaml` |
| --- | --- | --- | --- |
| `version`, `timezone`, `vars_file` | yes | yes | yes |
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

Timezones are `Local`, `UTC`, or IANA names like `America/Chicago`; data is built in. An empty
`NOON_*` variable is an error; unset it instead.

| Path in | Relative to |
| --- | --- |
| A YAML setting (`state_dir`) | That file's directory |
| Job `cwd`, `env_file`, `prompt_file`; `vars_file` | Project root, even in `.git/noon.yaml` |
| A flag or environment variable | Where you ran the command |

YAML never expands `~`, and keeps `$` literal outside the [config-time variable](#config-time-variables)
fields. `$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` must be absolute.

## User project

`noon --user` selects personal jobs from `config.yaml` in `$XDG_CONFIG_HOME/noon` (else
`~/.config/noon`). That directory is the project root and default `cwd`:

```yaml
# ~/.config/noon/config.yaml
jobs:
  disk-space:
    schedule: every 2h
    run: [./bin/disk-space.sh, "90"]   # ~/.config/noon/bin/disk-space.sh
```

```sh
noon --user add fetch "every 1h" -- ./bin/repos-fetch.sh   # creates the dir (0700) and file (0600)
noon --user enable                                          # or implied by a scheduled add
```

- Jobs are private (`shared: false`): no approval. The normal service runs them.
- Only `config.yaml` is read; never a `noon.yaml` or `.git/noon.yaml` in or above the directory.
- `-C ~/.config/noon` selects the same project.
- `--user` conflicts with `-C`, `--config`, `NOON_PROJECT`, and `NOON_CONFIG`; `serve`,
  `service`, `init`, and `shell` reject it. `add` rejects `--shared` and `--local`.
- Repository loads validate user jobs but never include them.

[examples/local-dev/housekeeping](../examples/local-dev/housekeeping/) is a complete setup.

## Project discovery

Noon walks up from the current directory to the nearest `noon.yaml` or `.git` (directory or
worktree file) and stops. It never reads above a Git boundary. With no marker, the current
directory is the project; a nested `noon.yaml` is a nested project. Discovery runs nothing.

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
| `until` | — | Exclusive end for unattended runs: local `YYYY-MM-DD HH:MM` or RFC 3339 ([Temporary jobs](#temporary-jobs)) |
| `splay` | — | Stable forward offset window, `1s`–`1h` ([Splay and jitter](#splay-and-jitter)) |
| `jitter` | — | Per-occurrence forward offset window ([Splay and jitter](#splay-and-jitter)) |
| `run` | — | String runs with `/bin/sh -c`; list is exact argv |
| `noop_exit_code` | — | Integer 1–124: record a normally exited run with this code as `noop` |
| `prompt` | — | Inline agent prompt, ≤ 64 KiB |
| `prompt_file` | — | Prompt read when the run starts, ≤ 64 KiB |
| `agent` | `default_agent` | Agent profile for prompt jobs |
| `agent_args` | — | Non-empty literal argv list for a prompt job; requires an explicit `agent` |
| `cwd` | project root | Absolute or project-relative; checked by `doctor`, not validation |
| `env_file` | — | Path or list of paths loaded at each run start ([Env files](#env-files)) |
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

`os` and `arch` use exact Go values; `macos`, `x86_64`, and `aarch64` are rejected with a
suggestion. A job for another platform stays visible as `other platform` but never runs:

```yaml
jobs:
  brew-update:
    os: [darwin]
    run: [brew, update]
```

Errors name the file, line, and YAML path:

```console
$ noon list
noon: config /home/me/app/noon.yaml:5: jobs.lint.run: must be a string (quote numbers)
```

Unknown fields, duplicate keys, nulls, wrong types, multiple documents, whitespace-only files, and
files over 1 MiB are errors. A missing file, an empty file, and `{}` are fine. A broken file is an
error even when a later file overrides it.

### Temporary jobs

Set an absolute `until` on any job, including manual-only jobs:

```yaml
jobs:
  watch-ci:
    schedule: every 10m
    until: 2026-10-20 17:00
    agent: claude
    prompt: Check CI and fix failures.
```

`YYYY-MM-DD HH:MM` uses the effective project timezone; RFC 3339 includes its own offset.
A repeated local time resolves to its first occurrence; a skipped time resolves to the first
instant after the gap. Durations and bare `HH:MM` are not valid in config: they have no anchor.
Past end times are valid and never make `config check` fail.

For a relative lifetime, use `noon add watch-ci "every 10m" --for 2h --agent claude --prompt "Check CI"`.
`--for` requires at least 1m and rounds up to the next whole minute. Alternatively, `--until`
accepts `HH:MM` (its next occurrence), a local date and time, or RFC 3339. These flags are mutually
exclusive, resolve once when written, must end after now, and store RFC 3339 with the project offset.

At `now >= until`, the job is ended: schedules stop without catch-up and queued runs are cancelled.
Scheduled starts after splay and jitter must be strictly before the end. Already-running work
finishes normally, bounded by its timeout. Queued requests are refused if the job has ended or
their due time is at or after the end. Foreground `noon run NAME` still works.
Noon keeps the definition and never edits config to expire it; use `noon remove NAME` to clean up.

`list` appends `until YYYY-MM-DD HH:MM` to the schedule and shows `ended` in NEXT after other
inactive states. If no occurrence starts before the end, NEXT is `-`. JSON and MCP expose
`until` (RFC 3339 in the project timezone, omitted when unset), `ended`, and omit `next_at`
when there is no eligible next occurrence. Changing `until` requires shared-job approval again;
`noon enable` can approve ended jobs too.

### Splay and jitter

Both push scheduled starts forward, never earlier:

```yaml
jobs:
  fetch:
    schedule: every day at 7am
    splay: 10m    # same offset every day, somewhere in 07:00:00–07:09:59
    run: ./fetch
  standup:
    schedule: weekdays at 8:50am
    jitter: 20m   # new offset each weekday, 08:50:00–09:09:59 (centered on 9am)
    run: ./standup
```

```console
$ noon list --wide
JOB      SCHEDULE                  NEXT                LAST   COMMAND
fetch    every day at 07:00 +1m8s  Wed 07:01 (in 12h)  never  ./fetch
standup  weekdays at 08:50 ~20m    Wed 08:54 (in 14h)  never  ./standup
```

| | `splay` | `jitter` |
| --- | --- | --- |
| Offset | One per job: hash of project path and job name | One per occurrence: hash of project path, job name, and nominal time |
| Range | `1s`–`1h` | ≥ `1s`, ≤ half the shortest gap (`*/15 * * * *` → `7m30s`; `every 1h` → `30m`) |
| Schedules | cron, macro, calendar | any, including intervals |

Offsets are deterministic whole seconds, so restarts and inspection agree; moving the checkout or
renaming the job changes them. Only scheduled occurrences move, not manual, queued, or triggered
runs. `NOON_SCHEDULED_AT` stays nominal, so lateness checks must allow for the offset. See
[Design](design.md#schedules) for the math.

### Config-time variables

A top-level `vars_file` fills `${...}` in a few job fields at load time, for machine-specific
schedules and paths:

```yaml
vars_file: .noon.vars # optional; gitignore it
jobs:
  sync:
    schedule: ${SYNC_SCHEDULE:-every 1h}
    cwd: ${SYNC_DIRECTORY:-.}
    run: ./sync.sh
```

```dotenv
# .noon.vars
SYNC_SCHEDULE=weekdays at 9am
```

| Syntax | Result |
| --- | --- |
| `${NAME}` | Value from the vars file; an error if absent (empty is allowed) |
| `${NAME:-default}` | Literal default when absent or empty, or when there is no vars file |
| `$$` | Literal `$` |
| `$NAME`, lone `$` | Unchanged |

| Expands | Stays literal |
| --- | --- |
| `schedule`, `cwd`, `env_file` paths, `prompt_file`, `timeout`, `cooldown`, `splay`, `jitter` | `run`, `prompt`, `env`, hooks, `agent_args`, everything else |

- The vars file is the only source: never the process environment, `env`, or `env_file`.
- Expansion is single-pass; nesting and other operators are errors.
- The file uses the [env-file format](#env-files), resolves from the project root, and applies only
  to jobs in the same config file. Its own path is literal.
- A missing file supplies nothing (so `:-` defaults work on a fresh clone); `doctor` warns. A
  malformed file fails loading, naming the line but never values.
- It's reread on every load; `noon serve` picks up changes without a restart.
- **Not secret, not exported.** Values appear in inspection and errors. To give them to the job,
  also list the file in `env_file`.
- Approval uses resolved values: only a changed effective value needs re-approval.
- `noon add` stores `SCHEDULE`, `--timeout`, and `--prompt-file` verbatim and checks them against
  the target file's `vars_file`. A new `.git/noon.yaml` has none, so use `:-` defaults or `--shared`.

[`examples/basics/env-files`](../examples/basics/env-files/) runs both files side by side.

### Env files

`env_file` loads environment from files at each run start, for values from a script or secret
manager:

```yaml
jobs:
  sync:
    run: ./sync.sh
    env_file: [.env, /home/me/.config/secrets/sync.env] # or a single path
    env:
      LOG_LEVEL: debug
```

Precedence: inherited environment < files in list order < job `env:` < `NOON_*` metadata.

- Relative paths resolve from the project root, so `.env` in `.git/noon.yaml` means each worktree's
  own `.env`. Use an absolute path to share one file.
- Files are reread at every run start; rotation needs no restart. Hooks reuse that run's values.
- Every file must exist, be regular, and be ≤ 64 KiB. A bad file fails the run with exit 125 and an
  error naming the path and line, never content.
- `config check` validates paths; `doctor` reads the files.
- **Approval pins paths and order, not contents.** Only enable jobs whose files you trust.
- Inspection shows `env_file` paths and `env:` keys, never values. Noon doesn't redact what a job
  prints.

The format is a strict, single-line, non-interpolating subset of
[Compose's env-file syntax](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/#env-file-syntax):

```dotenv
# Comments and blank lines are ignored; CRLF is fine.
export API_URL=https://api.example.com  # optional export; a comment needs a space before #
PAGE=docs#intro                         # no space, so # is part of the value
WINDOWS=C:\temp                         # unquoted: backslashes literal, trailing space trimmed
TOKEN="abc\tdef"                        # double quotes decode only \n \r \t \" \\
LITERAL='$NOT_EXPANDED'                 # single quotes are literal; only \' is decoded
EMPTY=
```

Keys match `^[A-Za-z_][A-Za-z0-9_]*$`, spaces around `=` are ignored, and the last duplicate wins.
Errors: `NOON_*` keys, `$` outside single quotes, other escapes, text after a closing quote,
multiline or unterminated values, `KEY: value`, bare `KEY`, and NUL.

### YAML composition

Anchors, aliases, and merges (`<<`) work within one file. Put reusable data under `definitions`:

```yaml
version: 1
definitions:
  command: &command [go, test, ./...]
  defaults: &defaults
    timeout: 10m
    env: {CI: "true"}
jobs:
  tests:
    <<: *defaults
    run: *command
  quick:
    <<: *defaults
    run: [go, test, -short, ./...]
    timeout: 2m                      # explicit keys win
```

Merges are shallow: an explicit `env` replaces the inherited one. With `<<: [*a, *b]`, `a` wins.
Approval digests see the resolved job, so refactoring into anchors doesn't need re-approval.
Cycles, custom tags, includes, and cross-file references are unsupported. `add`, `remove`, and
crontab import refuse to edit files that use anchors; edit those by hand.

### Local overrides

A job in `.git/noon.yaml` replaces the shared job of the same name entirely; fields don't merge.
The exception turns a shared job off without copying it:

```yaml
# .git/noon.yaml
jobs:
  tests: {enabled: false}
```

`noon list` counts both as overrides.

### Working hours

Use a cron hour range, plus `missed: skip` so a run missed during sleep doesn't fire at night:

```yaml
jobs:
  triage:
    schedule: "*/15 8-16 * * 1-5"   # every 15 minutes, 08:00–16:45, Monday–Friday
    missed: skip
    run: ./scripts/triage
```

Splay and jitter push the window's edges later. Intervals (`every 15m`) can't be windowed.

Queued runs (`--detach`, MCP, webhooks) ignore the schedule. To window them too, check the time and
exit with `noop_exit_code`. Jobs don't inherit the project timezone, so set `TZ`:

```yaml
jobs:
  triage:
    schedule: "*/15 8-16 * * 1-5"
    missed: skip
    noop_exit_code: 78
    env:
      TZ: America/Denver
    run: |
      h=$(date +%H)
      [ "$h" -ge 8 ] && [ "$h" -lt 17 ] || exit 78   # outside hours → noop
      ./scripts/triage
```

## Agent profiles

Prompt jobs pipe the prompt to an agent CLI. Profiles are machine choices, so they live in user
config. Define a built-in's name to replace it wholesale:

```yaml
# ~/.config/noon/config.yaml
default_agent: claude
agents:
  claude:
    command: [claude, -p, --permission-prompts, none, --permission-mode, acceptEdits]
    session_id_flag: --session-id   # overrides don't inherit built-in fields
  reviewer:
    command: [my-agent]
    prompt: argument
    prompt_args: [--quiet]
```

| Built-in | Command | Prompt delivery | `prompt_args` | Session ID flag |
| --- | --- | --- | --- | --- |
| `claude` | `claude -p --permission-prompts none` | stdin | — | `--session-id` |
| `codex` | `codex exec` | stdin | `-` | — |
| `gemini` | `gemini` | Final argument | `-p` | — |
| `opencode` | `opencode run` | Final argument | `--` | — |
| `pi` | `pi --print` | stdin | — | `--session-id` |

| Field | Effect |
| --- | --- |
| `command` | Argv prefix |
| `prompt` | `stdin` (default) or `argument` (appended last); bytes are never templated or trimmed |
| `session_id_flag` | Pass a fresh UUID per run as `FLAG UUID` and set `NOON_AGENT_SESSION_ID` |
| `prompt_args` | Trailing arguments placed after the job's `agent_args` |

`agent_args` is literal argv, never expanded, and needs an explicit `agent`:

```yaml
jobs:
  quick-review:
    prompt_file: prompts/review.md
    agent: claude
    agent_args: [--model, haiku, --effort, low]
```

The argv is assembled in this order (the prompt goes to stdin here):

```text
command  [session_id_flag UUID]  job agent_args  prompt_args  [prompt, if argument]
claude -p --permission-prompts none --session-id UUID --model haiku --effort low
```

If a custom profile's `command` ends in `--` or a value-taking flag, move that tail to
`prompt_args` before jobs use `agent_args`, or the job's flags land in the wrong place.

`agent_args` is part of the approval digest. Approval is not a sandbox: arguments can widen
permissions just as `run` can. Install and authenticate the CLI yourself, and configure it to deny
or pre-approve permissions, since unattended runs can't answer prompts. Pi print mode emits only
the final response.

A missing, unreadable, or oversized prompt file fails before the agent starts (exit 125). Prompt
runs get `NOON_AGENT=NAME`.

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

`add` writes `.git/noon.yaml` by default, editing in place and keeping comments. It enables the
project when the job is scheduled or queued, and approves only that job. `add`, `remove`, and
`import --write` refuse `--config` files, symlinked config files, and unusual `jobs` layouts;
edit a link's target by hand. Paused jobs still run with
`noon run`.

## Listing jobs

```console
$ noon list
Scheduler offline
JOB    SCHEDULE           NEXT                LAST   COMMAND
hello  manual             -                   never  echo hello from $NOON_JOB
tests  weekdays at 09:00  Mon 09:00 (in 64h)  never  "go" "version"
2 jobs · 0 overrides
```

Times use the project timezone. Intervals preview `now + interval` until the scheduler records
their phase. `--json` adds `source`, `shared`, `overrides`, `digest`, `approval` (`not_required`,
`approved`, `needs_approval`), `env_file`, `env_keys`, `agent_args`, `os`, `arch`,
`platform_supported`, and `webhook_path`. Environment values are never printed, but keep secrets
out of commands and descriptions.

## Approval

Shared jobs (from `noon.yaml` or `--config`) run in the background only while their digest matches
the one `noon enable` approved. Private jobs need no approval; a local `{enabled: false}` doesn't
change that.

| In the digest | Not in the digest |
| --- | --- |
| schedule, action, agent, `agent_args`, cwd, env, `env_file` paths and order, timeout, `noop_exit_code`, splay, jitter, until, overlap, missed, cooldown, hooks, webhook, `os`, `arch`, `requests` (only when set) | description; contents of scripts, prompt files, env files, and vars files; built-in and user-config profiles and hook defaults |

A profile defined in the same `--config` file is digested with its jobs.

`noon enable` records the canonical path and config selection, then approves every current shared
job; re-running keeps existing approval times. `noon disable` revokes everything without reading
project files, so a broken edit can't block it (pass `--state-dir` if user config is broken too).

`noon serve` reloads config every two seconds. A broken edit keeps the last valid jobs running and
shows the error in `noon status`.
