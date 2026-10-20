# Command reference

Run `noon COMMAND --help` for every flag. This page covers behavior the help text doesn't.

## Global flags

These work before or after the subcommand.

| Flag | Environment | Effect |
| --- | --- | --- |
| `--user` | — | Select personal jobs from `$XDG_CONFIG_HOME/noon/config.yaml` (else `~/.config/noon/config.yaml`) |
| `-C DIR`, `--project DIR` | `NOON_PROJECT` | Use this project instead of discovery |
| `--config FILE` | `NOON_CONFIG` | Read only this config file |
| `--timezone TZ` | `NOON_TIMEZONE` | Override the timezone |
| `--state-dir DIR` | `NOON_STATE_DIR` | Override the per-user state directory |
| `-j`, `--json` | — | One JSON object on stdout |
| `--color auto\|always\|never` | `NO_COLOR`, `TERM` (in auto mode) | Control styling of human output; default `auto` |

`--user` works with user-project commands including add, remove, list, run, history, logs, cancel,
pause, resume, enable, disable, status, doctor, tui, mcp, and `import crontab --write`. It conflicts
with `-C`/`--project`, `NOON_PROJECT`, and `--config`/`NOON_CONFIG`; it is rejected by `serve`,
`service`, `init`, and `shell`. Passing `$XDG_CONFIG_HOME/noon` (or `~/.config/noon`) to `-C`
selects the same user project and conflicts with `--config` too. Copyable job guidance preserves
`--user`.

`noon serve` accepts `--state-dir DIR`, optional `--listen ADDR` for signed webhooks, and
`--api-token-file FILE` to enable the authenticated JSON API and MCP over HTTP. The token file
requires `--listen`, must contain at least 32 bytes, and is read once at startup; a trailing newline
is removed. Without the token-file flag, the listener serves only signed webhooks and `/healthz`.
No listener starts without `--listen`. `serve` rejects `--user`, `--project`, `--config`,
`--timezone`, and `--json`; each project uses the config selection recorded by `noon enable`.

## Remote API and MCP over HTTP

Start the listener with a secret file, never an environment variable:

```sh
noon serve --listen 127.0.0.1:8787 --api-token-file /run/secrets/noon-api-token
```

The token is shared, compared in constant time, and required before routing every `/v1/` and `/mcp`
request. Missing or invalid credentials receive `401` with `WWW-Authenticate: Bearer` and an empty
body. `/healthz` is unauthenticated; webhooks continue to use HMAC signatures. The token is not
logged, stored, echoed, or passed to jobs or hooks. Put remote access behind a TLS reverse proxy;
Noon does not provide TLS or CORS.

| Method and route | Operation |
| --- | --- |
| `GET /v1/projects` | List enabled projects as `{id,name,path}` |
| `GET /v1/projects/{id}` | Project status (`noon status --json` shape) |
| `GET /v1/projects/{id}/jobs` | Jobs (`noon list --json` shape) |
| `GET /v1/projects/{id}/runs?job=&status=&limit=` | Recent project runs |
| `POST /v1/projects/{id}/jobs/{name}/runs` | Queue an approved job; optional body `{key,in,at,data}` |
| `GET /v1/runs/{id}?wait_seconds=&log_bytes=&after=&tail=&match=&context=&section=&timestamps=` | Run and bounded log |
| `POST /v1/runs/{id}/cancel` | Cancel a queued or running run |

Responses use the same `schema_version: 1` objects as MCP structured output and CLI JSON. Queueing
returns `202` for a new run and `200` when it returns an existing run. The request body is at most
512 KiB; `data` is valid JSON of at most 256 KiB. `wait_seconds` is 0–60; `log_bytes` is 0–65536.
`requested_by` on API-created runs is `api:REMOTE_ADDR` metadata, not authorization.

| Status | Meaning |
| --- | --- |
| 400 | Invalid arguments or query |
| 401 | Missing or invalid bearer token |
| 404 | Unknown route, project, or run; disabled projects and their runs are also hidden |
| 405 | Wrong method; `Allow` names the supported method |
| 409 | Project/job disabled, paused, needs approval, unsupported platform, or recursion refused |
| 413 | Request body exceeds 512 KiB |
| 429 | Per-job queue is full |
| 500 | Other operational error |

The API has no add, edit, enable, disable, approve, pause, or resume endpoints. It accepts project
IDs, not arbitrary filesystem paths. MCP over Streamable HTTP is at `/mcp`; it has the stdio tools
plus `list_projects`, but no default project. Pass an enabled project's absolute `project` path to
project-scoped tools. `get_run` by ID and `cancel_run` are limited to enabled projects. For verified
client setup examples, see [Agents](agents.md#remote-mcp-and-json-api).

## Short flags

| Command | Short forms |
| --- | --- |
| All commands | `-C DIR` = `--project DIR`, `-j` = `--json`, `-h` = `--help` |
| Root | `-v` = `--version` |
| `history` | `-n N` = `--limit N`, `-s STATUS` = `--status STATUS` |
| `logs` | `-f` = `--follow`, `-t` = `--timestamps`, `-s STATUS` = `--status STATUS`; existing `-n/-c/-m/-b` below |
| `run` | `-d` = `--detach` |
| `pause`, `resume` | `-a` = `--all` |
| `add` | `-a NAME` = `--agent NAME`, `-p TEXT` = `--prompt TEXT`, `-t DURATION` = `--timeout DURATION` |

Use separate short flags, not bundles. Values may use a space or `=` (for example,
`-p=-j` keeps `-j` as prompt text). Everything after `add ... --` is literal command argv.

Other options deliberately stay long-only:
- Global `--config`, `--timezone`, and `--state-dir` avoid collisions with log cursors,
  timeouts, and status filters.
- Log `--context`, `--section`, and `--path` stay distinct from cursor, status, and prompt.
- Run/add `--in` and `--at`, run `--data` and `--key`, and add `--sh`,
  `--prompt-file`, and `--description` keep timing, payload, and action choices explicit.
- `--shared`, `--local`, import `--write`, service `--print`, serve `--listen`,
  and shell `--on-enter` are explicit scope or side-effect choices, not everyday shortcuts.
  Hidden shell plumbing flags are not interactive shortcuts.

## Pausing jobs

`noon pause NAME...` and `noon resume NAME...` change the selected jobs atomically.
For example, `noon pause lint tests`. Use `--all` (or `-a`) instead of names to change
all currently configured jobs in the selected project atomically.
Project discovery, `-C`, and `--user` select the scope; this never recurses into directories
or changes other registered projects. A name and `--all` cannot be combined. Without either,
these commands only list candidates.

Pausing cancels queued runs but leaves running work alone. Jobs added later are not paused;
use `noon disable` for a project-wide opt-out. Resuming starts schedules from now without
catch-up. `resume --all` also resumes individually paused jobs—it is not an undo of the last
bulk pause.

Bulk output reports how many pause states changed; an empty project succeeds with zero.
JSON includes sorted `jobs`, `paused`, and `changed` fields.

## Multiple targets

`pause`, `resume`, `remove`, `history`, and queued `run` accept multiple job names;
`cancel` accepts multiple job names or run IDs. Duplicate targets are processed once.
Foreground `run` and `logs` remain single-target. `enable` and `disable` remain project-scoped.

All explicit target selections are validated before mutations. Queue eligibility (including
approval, enabled state, and capacity) is checked separately for each request. Cancellation,
queueing, and multi-file removal are not all-or-nothing: operational failures do not undo
completed work. They continue with remaining targets, report failures, and exit 1.

Single-target JSON keeps its existing shape. Multiple-target JSON uses:
- `pause` / `resume`: `jobs`, `paused`, `changed`.
- `history`: `runs`, merged newest-first with one overall `--limit`.
- `cancel`: `runs`, `pending`, `errors`.
- queued `run`: `results` (each with `run`, `created`, `scheduler`, and optional
  `reason` / `cooldown_until`), `errors`.
- `remove`: `removed` (each with `removed` job name and `file`), `errors`.

Each `errors` entry contains `target` and `error`; successful batches have an empty array.
These commands still emit only one JSON object, including on partial failure.

## Output and exit codes

### Terminal styling

`status` (including bare `noon`), `list`, `history`, `doctor`, foreground-run headers and
summaries, and CLI errors use restrained styling. Project/job names, table headers, and next-step
commands are bold. Failures are red, attention states (such as needs approval, paused, disabled,
offline, and orphaned) yellow, running cyan, and success marks/status labels green. Noops are dimmed. Paths,
timestamps, durations, and ordinary text retain the terminal\'s default foreground.
Labels and symbols retain their meaning without color. Colors use the terminal\'s ANSI palette,
not hard-coded RGB values, so their appearance follows your terminal theme.

- `--color=auto` styles each output stream only when it is a terminal, `NO_COLOR` is empty or
  unset, and `TERM` is not `dumb`. Redirecting stdout does not suppress terminal stderr styling.
- `--color=always` explicitly enables styling, overriding terminal detection and those environment
  settings. `--color=never` disables all Noon-added styling, including bold.
- `--json` always disables Noon-added styling, even with `--color=always`.
- Child output and stored/read logs are never recolored or stripped; any escapes emitted by a
  child remain its own. Generated scripts, completion, help/version, and the TUI are unchanged.

Tables escape untrusted controls and measure/truncate visible text before adding styling, so
color does not change column alignment or make job identifiers ambiguous.

### Errors and exits

Human errors go to stderr as one line, optionally followed by a hint:

```console
$ noon add tests "every day" -- go test ./...
noon: every day needs a time: try every day at 9am, daily (midnight), or every 24h
Hint: Try every day at 9am
```

With `--json`, errors are one object on stdout. `hint` is omitted when empty:

```console
$ noon --json run nope
{"error":{"code":"not_found","message":"job \"nope\" not found","hint":"Run noon run to list jobs"},"schema_version":1}
```

Finishing actions emit exactly one `schema_version: 1` object with `--json`, including
bare-group guidance such as `noon shell --json`. Explicit `--help`, `help`, `--version`,
and shell-completion requests remain text, even with `--json`. Human results go to stdout;
human errors and bounded-log resume hints go to stderr. Foreground `run --json` sends job
stdout and stderr to stderr, keeping stdout exclusively for the JSON result.

`serve`, `mcp`, `tui`, and `logs --follow` reject `--json` because they are streaming or
interactive interfaces, not single-result actions.

Unknown or extra group arguments exit 2 with the relevant full command's help path.
An unambiguous single-typo command or flag match is suggested, never automatically executed.
Bare `import` lists importers; bare `config` shows effective configuration.

| Exit | Meaning |
| --- | --- |
| 0 | Success, including empty results and guidance |
| 1 | Operational error |
| 2 | Usage error |
| 75 | `noon serve`: the database is newer than this binary |

A job whose `os` or `arch` constraints don't match this binary fails in foreground or queue mode
with exit 1 and error code `unsupported_platform`; the hint names the allowed values and current
platform. See [job platform constraints](configuration.md#jobs).

`noon run NAME` exits with the job's own code instead, except that a configured `noop_exit_code`
matching a normal process exit records `noop` and returns 0:

| Exit | Meaning |
| --- | --- |
| 124 | Timed out |
| 130 | Cancelled |
| 128+N | Killed by signal N |
| 127 / 126 / 125 | Command not found / not executable / other start failure |

Read-only commands never create the state directory.

Without a target, `run`, `remove`, `pause`, and `resume` list candidates or explain how
to add a job; they never select the only job automatically. `cancel` lists active runs.
Empty history/logs point to `noon run` to list runnable jobs. `add` and `import crontab`
print examples, `service` shows status, and `shell init` shows explicit sourcing commands
for Bash, Zsh, and Fish. These inspections exit 0 without changing jobs or starting services.
Logs still default to the latest run when one exists.

`noon remove NAME... --shared` selects `noon.yaml`; `--local` selects `.git/noon.yaml`.
The selected scope applies to every name. Names, ambiguities, and candidate edits are checked
before any file is written; each affected file is written atomically once.
With `--user`, remove edits the user `config.yaml` and does not accept those scope flags.
If both repository files define the name, choose one explicitly. Without a name, nothing is removed.
`noon remove NAME` confirms the job and definition file removed; history remains queryable
with `noon history NAME` and `noon logs RUN`. Its JSON result contains `removed` and `file`.

## Run now

`noon run NAME` runs in the foreground, without a scheduler or approval. Job stdout goes to stdout
and stderr to stderr, so redirection works. Noon's status lines go to stderr.

```sh
noon run report > report.md
noon run tests --json 2>/dev/null   # job output → stderr; run summary → stdout
```

Only one run of a job is active per project at a time. SIGINT, SIGTERM, or SIGHUP sends TERM to the
job's process group, then KILL after 10 seconds. A second signal kills it immediately.

## Queue for later

Queued runs go to the scheduler. They need `noon enable` and a running `noon serve`. If no
scheduler is running, the run waits:

```console
$ noon run tests --detach
Queued tests as run 2 · due now
Follow it: noon logs 2 --follow
The scheduler isn't running, so nothing runs until it starts.
Next: noon service install
```

| Flag | Effect |
| --- | --- |
| `-d`, `--detach` | Due now. Merges into a run of the same job that is already due |
| `--in 2h` | Separate one-shot after a duration |
| `--at 17:00` | Separate one-shot at `HH:MM`, `YYYY-MM-DD HH:MM`, or RFC 3339 |
| `--key KEY` | Return the existing run if this project/job already used `KEY` |
| `--data JSON` | Payload as JSON, `@FILE`, or `-` for stdin; up to 256 KiB |

Queue multiple configured jobs with `noon run lint tests --detach`, `--in 2h`, or `--at 17:00`.
Each job receives the same time selection, `--key`, and `--data` payload; stdin payloads are read
once. Keys are scoped per job, so one key does not merge requests for different jobs. Multiple
names must exist in the current configuration, even when using a retained idempotency key.

Keys are remembered for the newest 50 finished runs per job. The payload is saved to a file
before dispatch, and the job reads its path from `NOON_EVENT_FILE`. A job's `cooldown` applies only
to queued requests. `noon --user run NAME --detach` uses the user project's scheduler slot and its
follow guidance keeps `--user`. See [Triggers](triggers.md).

## Cancel

```sh
noon cancel        # list active runs; changes nothing
noon cancel 12     # cancel run 12, even another terminal's foreground run
noon cancel tests  # cancel every active run of tests
noon cancel tests lint 42  # cancel selected jobs and run 42, without duplicate requests
```

Job names select a snapshot of their active runs; work queued afterward is not included.
Names may refer to configured jobs or retained history. Explicit run IDs retain their global
selection behavior. All selections are resolved before requesting cancellation.

Queued runs are cancelled immediately. Running ones get the same TERM-then-KILL sequence.
A run whose recorded owner is no longer live is listed as `orphaned`; `noon cancel ID` records it
as cancelled and signals only an identity-matched process group. Inspection never changes run
state. `noon disable` also cancels the project's queued runs.

## History

```sh
noon history                            # newest 20 runs
noon history tests -n 50                 # --limit 50
noon history tests lint -n 50            # newest 50 across both jobs
noon history -s failed,timed_out         # --status failed,timed_out
noon history -s noop                     # only no-op polls
```

Statuses: `queued`, `running`, `succeeded`, `noop`, `failed`, `timed_out`, `cancelled`, `interrupted`,
`skipped`, `missed`, `expired`. A noop is not a success or failure and does not run hooks. Repeat
`--status` or comma-separate values. Human history displays an orphaned running run as
`orphaned`; its stored and JSON `status` remains `running`, with JSON `orphaned: true`. Hook-only
orphans keep the main run's status label. `history NAME` and `logs NAME` still work after a job is
removed.

## Logs

```sh
noon logs                         # latest started run in this project
noon logs tests                   # latest started run of tests
noon logs tests --status failed   # latest failure
noon logs tests --status noop     # latest no-op poll
noon logs 12 -f                   # --follow: wait for run 12, then stream it
noon logs 12 -f -t                # stream with captured receipt-time prefixes
noon logs 12 -n 50                # last 50 lines
noon logs 12 --timestamps         # display timestamps without changing stored bytes
noon logs 12 -m 'panic|FAIL' --context 3
noon logs 12 --section job        # job output only, without hook output
noon logs 12 -b 4096 -c 0         # first 4 KiB page
noon logs 12 --path               # absolute log file path
```

| Flag | Effect |
| --- | --- |
| `--section all\|job\|hook` | Whole log (default), job output, or hook output |
| `-n`, `--tail N` | Last 1–10000 lines |
| `-m`, `--match RE` | Lines matching a case-sensitive Go RE2 expression; `(?i)` ignores case |
| `--context N` | Up to 50 lines around matches; needs `--match` |
| `-c`, `--after-cursor B` | Read forward from absolute byte offset `B` |
| `-b`, `--limit-bytes N` | Cap output, keeping whole lines |
| `-s`, `--status S` | Pick the latest run with these statuses |
| `-t`, `--timestamps` | Prefix displayed lines with local receipt time; JSON adds structured `line_times` |

Bounded reads print the resume command to stderr. JSON returns `log`, `next`, `size`, `truncated`,
`matched_lines`, and a copyable `resume` command. Resume commands pin the run and cursor and retain
`--section`, `--match`, `--context`, `--limit-bytes`, and `--timestamps`; they omit `--tail` and
`--status` so the next read moves forward within that run. The numeric `next` remains the raw-byte
cursor. `log` is never timestamp-prefixed. With `--timestamps`, JSON adds one `line_times` entry per
returned source line (including context and cut oversized lines), each `{ "offset": N, "time": T }`;
`offset` is the absolute byte offset of the raw line's first byte, and `time` is a UTC
`2006-01-02T15:04:05.000Z` string or null. Without the flag, JSON stays unchanged.

A displayed CLI prefix uses local `2006-01-02T15:04:05.000-07:00` (29 columns, explicit offset),
then a space and any existing search/context prefix. Missing timing displays as 29 dashes. The stored
time is when Noon received the bytes, not when the child produced them; child buffering can delay
receipt. Follow prints partial lines as bytes arrive without waiting for newline, and a resumed
continuation does not get a second timestamp prefix. Historical logs and incomplete sidecars remain
readable with unavailable timestamps, never estimated from neighboring lines or run start.

Action guidance preserves explicit project, config, state-directory, and timezone selectors,
including environment-selected overrides, with absolute, shell-quoted paths. It does not copy
output mode or event payloads. Service/serve guidance carries only state selection and clears
unsupported Noon selector environment variables; an explicit config’s state directory is resolved
before suggesting a service command.

```console
$ noon logs check --match FAIL --context 1
2-ok 2
3:FAIL widget
4-ok 3
next: noon logs 1 --after-cursor 27
```

Rules:

- Only `--status` can select a queued or never-started run. A run ID can't be combined with
  `--status`.
- `--follow` accepts only `--tail` and `--after-cursor`. `--path` combines with nothing, including `--timestamps`.
- Bounded reads omit an unfinished last line of an active run. Unflagged reads include it.
- If the run's owner exits without recording a result, `--follow` prints what's left and exits 1
  with code `orphaned`. Record it with `noon cancel RUN`. Reading never repairs or cancels runs.
- Noon keeps logs for the newest 10 non-noop runs and the newest 10 noops per job. See
  [limits](agents.md#limits-and-safety).

## Status and doctor

Bare `noon` and `noon status` show jobs, scheduler state, queued work, up to three notices, and
one `Next:` step. JSON has `project`, `scheduler`, `service`, `jobs`, `queued`, `notices`, and
`next`. Notices rank config errors, scheduled projects not enabled, pending approvals, an offline
enabled scheduler, orphaned runs, then queued work. An orphaned notice names each orphaned running
run ID and job with its copyable `noon cancel ID` command and says it blocks background runs in
the project. `Next:` prioritizes config check,
enablement, `noon service install` (or `noon serve` if installed), `noon cancel ID` for the lowest
orphaned ID, then a runnable job or `noon add`. Scheduler startup takes precedence because it
recovers orphans. `noon list` shows the same jobs with commands and next-run times; its `LAST` value
ignores noops, so a poll does not replace the latest result. A stale running last run is shown as
`orphaned` with its elapsed time; the stored status remains `running`.

Human list, history, and dashboard tables keep complete job names so similar names
remain distinguishable and copyable. Descriptive columns shrink to the output terminal
width (80 columns when redirected); very long names may make rows wrap.
Use `noon list --wide` for complete names, schedules, and command text without truncation,
including when redirected to a file. Agent jobs show a prompt summary or prompt-file path.
Both modes escape terminal controls and omit environment values. `--json` remains the
lossless machine interface.

`noon doctor` checks config, state, enablement, approvals, the scheduler heartbeat, the service,
and each job's command, cwd, and prompt file:

```console
$ noon doctor
ok config: Configuration valid
ok approvals: No approvals pending
warn scheduler: Scheduler heartbeat is absent or stale — noon service install, or noon serve
warn service: Per-user service is not installed — noon service install
ok job tests command: go
```

Warnings exit 0; failures exit 1. JSON has `ok` and `checks` (`name`, `status`, `message`,
`hint`). Doctor never fixes anything, runs jobs, migrates, or creates state.

## Service restart and upgrades

`noon service restart` explicitly restarts the installed per-user scheduler after a same-path
binary upgrade. It interrupts active jobs rather than draining them, uses the installed definition
without rewriting its environment, and fails with install guidance if no unit exists.
JSON returns `restarted: true` and `unit_path`; manager failures return an error.

Use `noon service install` for changed executable paths or captured environment settings.
An unchanged running unit is left alone by install. To switch installation methods, invoke the
new executable's `service install` with the same state directory, `HOME`, and `XDG_CONFIG_HOME`
to retain configuration and history. See [service upgrades](service.md#upgrades).

Job config reloads every two seconds and `env_file` is read at each run start, without restarting.
Changed shared job definitions still require `noon enable`; restart never grants approval.

## TUI

`noon tui` needs a terminal and refreshes every second. It never edits jobs or enables projects.
Log timestamps are shown by default, using the same local offset format and unavailable placeholder as
`noon logs --timestamps`; `t` toggles them without resetting follow or search position.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move |
| `enter` | Open runs for a job |
| `l` | Open a log |
| `f` | Cycle run status filter |
| `/` | Search the log (RE2, three lines of context) |
| `s` | Cycle job / hook / all log sections |
| `t` | Toggle log timestamps (shown by default) |
| `g` / `G` | Top / bottom of log |
| `r` | Queue the selected job now |
| `p` | Pause or resume the selected job |
| `c` | Cancel a run (asks first) |
| `esc`, `?`, `q` | Back, help, quit |
