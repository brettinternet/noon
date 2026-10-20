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
  claude: "claude" "-p" "--permission-prompts" "none" (stdin, session_id_flag=--session-id)
  codex: "codex" "exec" (stdin, prompt_args=["-"])
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
- Relative job `cwd`, `env_file`, and `prompt_file` paths are based on the project root,
  even in `.git/noon.yaml`; `cwd` and `env_file` also accept absolute paths.
- Flag and environment paths are relative to where you ran the command.
- YAML never expands `~` or `$VARS`. `$XDG_CONFIG_HOME` and `$XDG_STATE_HOME` must be absolute.

## User project

The user project is the canonical directory `$XDG_CONFIG_HOME/noon`, else `~/.config/noon`.
Its only job source is `config.yaml` in that directory. Noon never imports a `noon.yaml`,
`.git/noon.yaml`, or config above it, even when those files exist there. Select it with
`noon --user`; passing that directory to `-C` selects the same project and the same config
rules. Its identity is the absolute directory path.

User jobs are yours: they have `shared: false`, require no approval, and run with the user project
as their default `cwd`. Relative `cwd`, `env_file`, `prompt_file`, and command paths are based there. YAML
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
| `splay` | — | Stable forward offset window, `1s`–`1h`; cron, macro, or calendar schedules only |
| `jitter` | — | Duration of at least `1s`; per-occurrence forward window requiring a schedule and bounded by half its shortest gap |
| `run` | — | String runs with `/bin/sh -c`; list is exact argv |
| `noop_exit_code` | — | Integer 1–124: record a normally exited run with this code as `noop` |
| `prompt` | — | Inline agent prompt, ≤ 64 KiB |
| `prompt_file` | — | Prompt read when the run starts, ≤ 64 KiB |
| `agent` | `default_agent` | Agent profile for prompt jobs |
| `agent_args` | — | Non-empty literal argv list for a prompt job; requires an explicit `agent` |
| `cwd` | project root | Absolute or project-relative; checked by `doctor`, not validation |
| `env_file` | — | Path string or non-empty list; read at every run start, ≤ 64 KiB each. Approval covers ordered paths, not contents |
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

Set `splay: 10m` to spread jobs over a stable per-job window. Noon derives one whole-second
offset in [0, splay) from the canonical project path and job name. It stays the same across
restarts; moving the checkout, renaming the job, or changing splay may change it. Durations use
the same integer `s`, `m`, `h`, `d` components as timeout, between 1s and 1h.
Manual jobs and intervals reject splay.

Set `jitter: 20m` to vary the forward offset on every occurrence. Each occurrence gets a
whole-second offset in [0, jitter), derived deterministically from the canonical project path,
job name, and nominal occurrence time. Restarts and inspection produce the same offset; no random
or stored state is used. Jitter requires a schedule and cannot exceed half its shortest gap: for
intervals, half the interval; for clock schedules, half the smallest time-of-day gap, including the
wrap across midnight and ignoring day fields. For example, `*/15 * * * *` allows at most `7m30s`.
The effective start is nominal time plus any splay offset plus that occurrence's jitter. To center
a 20-minute jitter window around 9am, use `schedule: weekdays at 8:50am`; it starts from 08:50:00
through 09:09:59. Intervals retain their nominal phase and jitter never accumulates into it.

Both fields affect only scheduled occurrences, not foreground runs, queued requests, triggers,
hooks, or webhooks. Changing either on a shared job needs approval. `noon list` appends
`+<splay offset>` and then `~<jitter>` to SCHEDULE when set. All next-run displays include both
offsets; JSON and MCP `list_jobs` expose normalized `splay`, `splay_offset`, and `jitter`
duration strings only when configured. `NOON_SCHEDULED_AT` remains nominal; lateness checks
should allow for configured splay and jitter.

A job's optional `os` and `arch` lists must be non-empty, duplicate-free, and use the exact supported Go `GOOS`/`GOARCH` values shown above. Aliases such as `macos`, `x86_64`, and `aarch64` are rejected with a suggestion. Every declared field must match the running binary; an omitted field matches any value. A nonmatching job stays visible as `other platform`, but isn't scheduled, caught up, dispatched, or runnable by `noon run` or any queueing interface. `list --json` includes the declared lists and `platform_supported`; `doctor` skips command and working-directory checks for nonmatching jobs.

Unknown fields, explicit duplicate keys, null settings, wrong types, multiple documents, and files over
1 MiB fail with the file, line, and YAML path:

```console
$ noon list
noon: config /home/me/app/noon.yaml:5: jobs.lint.run: must be a string (quote numbers)
```

A missing file, a zero-byte file, and `{}` are fine; a whitespace-only file is not. A broken lower-priority file is
still an error even if a later file overrides its settings.

### Env files

Use `env_file` for values generated by a script or secret manager, without committing them:

```yaml
jobs:
  sync:
    run: ./sync.sh
    env_file: [.env, /home/me/.config/secrets/sync.env]
    env:
      LOG_LEVEL: debug
```

A single path (`env_file: .env`) is equivalent to a one-item list. The field works in every
job source, including user config, shared `noon.yaml`, private `.git/noon.yaml`, and `--config`.
Each path must be non-empty. Relative paths resolve from the project root, not the manifest
directory or job `cwd`: `.env` in `.git/noon.yaml` means the current checkout's `.env`,
including in linked worktrees. User jobs use the user project directory. Absolute paths can share
a file across worktrees. YAML does not expand `~` or `$VARS`.

Precedence is inherited environment < files in list order < job `env:` < `NOON_*` metadata.
All files are read at each run's start, for foreground and every background trigger. Rotation
needs no scheduler restart. Hooks receive the values loaded for that run and never reread files.
Every file is required, must be regular and readable, and must be at most 64 KiB. There are no
owner or permission-mode checks. A file failure stops the run before starting its process with
exit 125; errors name the configured path and syntax errors include a 1-based line number,
never the offending content. `config check` validates paths' shape without reading files;
`doctor` checks each file and uses its variables for command resolution.

The format is a strict, non-interpolating, single-line subset of
[Compose's env-file syntax](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/#env-file-syntax):

```dotenv
# .env — keep this file gitignored
export API_URL=https://api.example.com   # comment
TOKEN="abc\tdef"
LITERAL='$NOT_EXPANDED'
EMPTY=
```

- UTF-8 without NUL; LF lines may end in CRLF. Blank lines and comments beginning with `#`
  after leading whitespace are ignored.
- Optional `export` plus whitespace precedes `KEY=value`. Keys match
  `^[A-Za-z_][A-Za-z0-9_]*$`; `NOON_` names are reserved. Spaces/tabs around `=` are ignored.
- Unquoted values keep backslashes literally, trim trailing whitespace, and begin a comment at
  space followed by `#` (not `#` alone). Empty values are allowed.
- Single quotes preserve literal values, including `$`. Only `\'` becomes `'`; other
  backslash pairs remain unchanged.
- Double quotes decode only `\n`, `\r`, `\t`, `\"`, and `\\`.
  Other escapes fail. After either closing quote, only whitespace and an optional `#` comment
  are allowed.
- `$` in unquoted or double-quoted values fails rather than interpolating. Multiline values,
  unterminated quotes, `:` assignments, and bare inherit-only keys fail. Duplicate keys use the
  last assignment. Compose's long form and optional files are not supported.

Approval pins the ordered configured paths, **not file contents**. Changing a path or its order
needs approval; changing file contents does not. Only enable jobs whose referenced files you trust.
Inspection JSON (including MCP and HTTP) exposes `env_file` as a path list, or `null` when unset.
`env_keys` lists only job `env:` keys, never keys or values loaded from files. Child output is
still stored raw: Noon does not redact secrets a job prints. Webhook `secret_env` continues to
use the scheduler's startup environment, not job env files.

### YAML composition

Anchors (`&name`), aliases (`*name`), and mapping merges (`<<`) work within each
config file. Use the optional top-level `definitions` mapping for reusable data:

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
    timeout: 2m
```

Explicit keys override merged defaults, regardless of their position. For
`<<: [*first, *second]`, the first mapping wins on conflicts. Merges are shallow:
an explicit `env` replaces the inherited `env`; merge inside `env` to combine its keys.

Composition resolves before schema validation and config-file layering. Reusable
fragments are schema-validated wherever used; unused fragments do not define jobs.
Execution and approval digests use the same resolved settings, so composition alone
does not change approval, but changing an inherited execution setting does.
Diagnostics retain the defining value's source line.

Unknown fields and explicit duplicate keys remain errors, including typos inherited
through a merge. Cycles, custom tags, cross-file references, and includes are unsupported.
Resolution is limited to 100,000 visited nodes, 8 MiB of scalar values, and depth 100
(including unused definitions). Each file still has the 1 MiB input limit.
No new interpolation is performed.

Source-preserving commands such as `add`, `remove`, and crontab import refuse to edit
files containing anchors, aliases, or merges; edit these files by hand.

### Local overrides

A job in `.git/noon.yaml` replaces the shared job of the same name entirely; fields don't merge.
The one exception turns a shared job off without copying it:

```yaml
# .git/noon.yaml
jobs:
  tests: {enabled: false}
```

### Working hours

Use a cron hour range to keep scheduled runs inside a window, and `missed: skip` so a run missed
while the machine slept doesn't catch up at night:

```yaml
jobs:
  triage:
    schedule: "*/15 8-16 * * 1-5"   # every 15 minutes, 08:00–16:45, Monday–Friday
    missed: skip
    run: ./scripts/triage
```

An hour range covers whole hours, so `8-16` ends at 16:45. Splay shifts both window edges forward too: a five-minute offset makes this 08:05–16:50. Jitter can delay each start further, up to its
duration past the shifted time. Interval schedules (`every 15m`) count
elapsed time and can't be limited to a window.

The schedule doesn't limit queued runs from `noon run --detach`, MCP, webhooks, or hooks. To keep
those inside the window too, check the time in the script and exit with the job's
`noop_exit_code`. Jobs don't inherit the project timezone, so set `TZ` for `date`:

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
      [ "$h" -ge 8 ] && [ "$h" -lt 17 ] || exit 78
      ./scripts/triage
```

Outside the window the run is recorded as `noop`. Prompt jobs can't use `noop_exit_code`; wrap the
agent in a `run` script instead.

`noon list` marks both kinds as overrides.

## Agent profiles

Prompt jobs pipe the prompt to an agent CLI. Profiles are machine choices, so they live in user
config. `claude`, `codex`, `gemini`, `opencode`, and `pi` are built in; define the same name to
replace one:

```yaml
# ~/.config/noon/config.yaml
default_agent: claude
agents:
  claude:
    command: [claude, -p, --permission-prompts, none, --permission-mode, acceptEdits]
  reviewer:
    command: [my-agent]
    prompt: argument
    session_id_flag: --session-id
    prompt_args: [--quiet]
```

| Built-in | Command | Prompt delivery | `prompt_args` | Session ID flag |
| --- | --- | --- | --- | --- |
| `claude` | `claude -p --permission-prompts none` | stdin | — | `--session-id` |
| `codex` | `codex exec` | stdin | `-` | — |
| `gemini` | `gemini` | Final argument | `-p` | — |
| `opencode` | `opencode run` | Final argument | `--` | — |
| `pi` | `pi --print` | stdin | — | `--session-id` |

`session_id_flag` is optional and must be one flag token starting with `-`, other than `-` or `--`,
with no whitespace or NUL. When present, Noon generates a random v4 UUID for each execution,
places `FLAG UUID` after the profile command, and sets `NOON_AGENT_SESSION_ID` for the job and its
hooks. Redefining a built-in replaces it wholesale, so an override must include
`session_id_flag` to keep this behavior.

`prompt_args` is an optional string list in a user profile for arguments that must come after the
job's arguments. The effective argv is the profile `command`, the session flag and ID when
configured, the job's `agent_args`, the profile's `prompt_args`, then the prompt when
`prompt: argument`. With `prompt: stdin`, the prompt bytes go to stdin instead. Built-ins use
`prompt_args` for their trailing prompt flags/separators, preserving their current effective argv.
`noon config` shows configured `prompt_args`. Noon does not parse vendor flags in `agent_args`;
conflicting session or persistence arguments can prevent a requested session ID from being
resumable.

A custom profile whose `command` currently ends in `--` or a value-taking flag must move that tail
(including the flag's value) to `prompt_args` before its jobs use `agent_args`; otherwise job flags
can be consumed as the value or placed after the separator.

A prompt job's `agent_args` is a non-empty list of literal argv strings, not shell text; empty
string elements are allowed, and values are never templated or expanded. Such a job must name
`agent` explicitly: `default_agent` is not used for vendor-specific arguments. For example:

```yaml
jobs:
  quick-review:
    prompt_file: prompts/review.md
    agent: claude
    agent_args: [--model, haiku, --effort, low]
```

`agent_args` is part of the execution digest, so a change to it in a shared job needs approval
again. Approval covers the job definition, not the scripts or prompt files it references; it is not
a sandbox. Arguments can widen permissions just as `run` can and are visible configuration, not a
secret channel. Install and authenticate the selected CLI separately. Configure the agent's
permissions for unattended use; profiles are not sandboxes. Pi print mode emits the final response,
not intermediate tool activity.

`prompt: stdin` (default) sends the exact prompt bytes on stdin. `prompt: argument` appends them as
the last argument. Nothing is templated, expanded, or trimmed. Unattended runs must never wait on
a permission prompt, so profiles should deny or pre-approve permissions.

A missing, unreadable, or oversized prompt file fails before the agent starts, with exit 125.
Prompt files must be regular files. Prompt runs get `NOON_AGENT=NAME`; only profiles with
`session_id_flag` get `NOON_AGENT_SESSION_ID`. Inherited values of this reserved name are removed.

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
(`not_required`, `approved`, `needs_approval`), `env_file` paths, sorted `env_keys`, `agent_args` when set, and
`webhook_path`. Environment
values are never printed; still, keep secrets out of commands and descriptions.

## Approval

Shared jobs (from `noon.yaml` or `--config`) run in the background only while their digest matches
the one `noon enable` approved. Private jobs need no approval. A local `enabled: false` toggle keeps
the shared job's approval requirement.

The digest is a SHA-256 of the job's execution fields: schedule, action, agent name, cwd, env, ordered env_file paths,
timeout, `noop_exit_code`, splay, jitter, overlap, missed, cooldown, hooks, webhook, and platform constraints
(`os` and `arch`). `requests` joins the digest only when
explicitly present, so jobs that omit it keep their existing digest and approval. Changing the
description doesn't require re-approval. A prompt job whose agent profile is defined in the same
`--config` file also digests that profile's `command`, `prompt`, `prompt_args`, and
`session_id_flag`. Built-in and user-config profiles and user-config hook defaults are yours and
excluded. **The digest doesn't cover the contents of referenced scripts, prompt files, or env files.**

`noon enable` records the project's canonical path and its config selection (automatic files or
the absolute `--config` path), then approves every current shared job. Re-running it keeps
existing approval times. `noon disable` revokes opt-in and approvals without reading project
files, so a broken edit can't block it. If user config is broken too, pass `--state-dir`.

`noon serve` reloads config every two seconds. A broken edit keeps the last valid jobs running and
shows the error in `noon status` until fixed.
