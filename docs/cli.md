# Command reference

Run `noon COMMAND --help` for every flag. This page covers behavior the help text doesn't.

## Global flags

These work before or after the subcommand.

| Flag | Environment | Effect |
| --- | --- | --- |
| `--user` | — | Select personal jobs from `~/.config/noon/config.yaml` ([user project](configuration.md#user-project)) |
| `-C DIR`, `--project DIR` | `NOON_PROJECT` | Use this project instead of discovery |
| `--config FILE` | `NOON_CONFIG` | Read only this config file |
| `--timezone TZ` | `NOON_TIMEZONE` | Override the timezone |
| `--state-dir DIR` | `NOON_STATE_DIR` | Override the per-user state directory |
| `-j`, `--json` | — | One JSON object on stdout |
| `--color auto\|always\|never` | `NO_COLOR`, `TERM` (auto mode) | Style human output; default `auto` |

`--user` conflicts with `-C`, `--config`, and their environment variables, and is rejected by
`serve`, `service`, `init`, and `shell`.

`noon serve` accepts only `--state-dir`, `--listen ADDR` (signed webhooks), and
`--api-token-file FILE` (remote API and MCP; needs `--listen`). Each project uses the config
selection recorded by `noon enable`. No listener starts without `--listen`.

## Remote API and MCP over HTTP

```sh
noon serve --listen 127.0.0.1:8787 --api-token-file /run/secrets/noon-api-token
```

The token file must hold at least 32 bytes (a trailing newline is dropped) and is read once. Every
`/v1/` and `/mcp` request needs `Authorization: Bearer TOKEN`; failures get an empty `401` with
`WWW-Authenticate: Bearer`. `/healthz` is open, webhooks keep HMAC signatures, and the token is
never logged, stored, or passed to jobs. Put a TLS reverse proxy in front; Noon has no TLS or CORS.

| Method and route | Operation |
| --- | --- |
| `GET /v1/projects` | List enabled projects as `{id,name,path}` |
| `GET /v1/projects/{id}` | Project status (`noon status --json` shape) |
| `GET /v1/projects/{id}/jobs` | Jobs (`noon list --json` shape) |
| `GET /v1/projects/{id}/runs?job=&status=&limit=` | Recent project runs |
| `POST /v1/projects/{id}/jobs/{name}/runs` | Queue an approved job; optional body `{key,in,at,data}` |
| `GET /v1/runs/{id}?wait_seconds=&log_bytes=&after=&tail=&match=&context=&section=&timestamps=` | Run and bounded log |
| `POST /v1/runs/{id}/cancel` | Cancel a queued or running run |

```console
$ curl -s -X POST -H "Authorization: Bearer $TOKEN" localhost:8787/v1/projects/12/jobs/tests/runs
{"schema_version":1,"run":{"id":7,…,"job":"tests","status":"queued",…},"created":true,"scheduler":"running"}
```

Responses match MCP and CLI JSON. Queueing returns `202` for a new run, `200` for an existing one.
Limits: body 512 KiB, `data` 256 KiB, `wait_seconds` 0–60, `log_bytes` 0–65536. API runs record
`requested_by: api:REMOTE_ADDR`.

| Status | Meaning |
| --- | --- |
| 400 | Invalid arguments or query |
| 401 | Missing or invalid bearer token |
| 404 | Unknown route, project, or run; disabled projects and their runs are also hidden |
| 405 | Wrong method; `Allow` names the supported method |
| 409 | Project/job disabled, ended, paused, needs approval, unsupported platform, or recursion refused |
| 413 | Request body exceeds 512 KiB |
| 429 | Per-job queue is full |
| 500 | Other operational error |

There are no add, edit, enable, disable, approve, pause, or resume endpoints, and routes take
project IDs, not paths. `/mcp` serves the stdio tools plus `list_projects`, with no default
project. See [Agents](agents.md#remote-mcp-and-json-api) for client setup.

## Short flags

| Command | Short forms |
| --- | --- |
| All commands | `-C DIR` = `--project DIR`, `-j` = `--json`, `-h` = `--help` |
| Root | `-v` = `--version` |
| `history` | `-n N` = `--limit N`, `-s STATUS` = `--status STATUS` |
| `logs` | `-f` = `--follow`, `-t` = `--timestamps`, `-s` = `--status`, `-n` = `--tail`, `-c` = `--after-cursor`, `-m` = `--match`, `-b` = `--limit-bytes` |
| `run` | `-d` = `--detach` |
| `pause`, `resume` | `-a` = `--all` |
| `add` | `-a NAME` = `--agent NAME`, `-p TEXT` = `--prompt TEXT`, `-t DURATION` = `--timeout DURATION` |

Don't bundle short flags. `-p=-j` keeps `-j` as prompt text; everything after `add ... --` is
literal argv. Options with scope or side effects (`--config`, `--in`, `--at`, `--data`, `--key`,
`--shared`, `--write`, `--listen`, …) are deliberately long-only.

## Ending jobs

```sh
noon add watch-ci "every 10m" --for 2h --agent claude --prompt "Check CI and fix failures"
noon add watch-ci "every 10m" --until "2026-10-20 17:00" --sh './check-ci'
```

`--for DURATION` (at least 1m) and `--until TIME` are mutually exclusive. Both resolve at write
time and store an absolute RFC 3339 end in the project timezone. `--for` rounds up to a whole
minute; `--until` accepts `HH:MM` as its next occurrence, `YYYY-MM-DD HH:MM`, or RFC 3339.
An end not after now is a usage error (exit 2), with no definition written.
See [Ending jobs](configuration.md#ending-jobs) for expiry and DST behavior.

Queued `run` requests fail with code `ended` (exit 1, HTTP 409) if the job has ended or the
requested due time is at or after its end. The message names the end time and the hint names the
config source to edit, or re-adding for a temporary job. This applies to detach, delayed requests, MCP, API, webhooks, and triggers.
Foreground `noon run NAME` remains available after the end.

## Temporary jobs

```sh
noon add watch-pr-42 "every 2h" --temporary --for 3d --agent claude --prompt "Check PR 42 CI; fix failures" --key pr-42
noon add check-ci --temporary --in 2h --for 3h --sh './check-ci'
noon cancel watch-pr-42
noon remove check-ci
```

`--temporary` writes no config file, including when `.git/noon.yaml` is a symlink. `--user`
selects the user project. It accepts the normal add actions, description, timeout, and a first run
via `--in` or `--at`. Exactly one of `--for`/`--until` is required, after now and within 7 days
including minute rounding. A schedule or queued first run must occur before the end. `--shared`,
explicit `--config`, a missing end or occurrence, and `--key` without `--temporary` are usage
errors (exit 2). Intervals retain their phase; use cron for wall-clock alignment.

Creation enables the project. The definition is private (`shared: false`, `approval:
"not_required"`), with `requested_by: "cli"`. A config name collision or an active temporary
job with a different/missing key fails with `exists` (exit 1). The same nonempty `--key` returns
the existing job with `created: false`, `reason: "duplicate"`; an ended definition is replaced.
A normal config add refuses any retained temporary name, suggesting `noon remove NAME`.
A later hand-written config definition wins and deletes the temporary definition. If a config
edit leaves a definition unresolvable, such as removing its agent, job commands fail with a hint to
run `noon remove NAME`; `cancel` and `remove` still work.

JSON creation returns `schema_version`, `job` (the list job object), `created`, optional `reason`,
and `scheduler` (`running`/`offline`). Human output warns when the scheduler is offline.
List JSON and MCP `list_jobs` use `source: "temporary"`, `until`, `ended`, and `temporary` with
`created_at`, `requested_by`, optional `key`/`ended_at`, and `removes_at`. List SCHEDULE marks
`temporary until YYYY-MM-DD HH:MM` or `temporary, ended YYYY-MM-DD HH:MM`; NEXT is `ended`
after ending. The dashboard and MCP status list active temporary jobs in one notice. TUI rows
mark temporary jobs, render ended ones faint, and show end/removal times in the selected detail.

At the exclusive end queued work is cancelled; running work finishes within its timeout. Pause,
resume, and project disable never extend the lifetime. Cancel by name ends it and cancels active
work, with `ended_temporary_jobs` in JSON. Ended definitions remain for 24h from `ended_at`
(or `until` on natural expiry), then are removed; reads exclude expired rows without a scheduler.
`remove` deletes immediately, retaining normal run/log history. Foreground `run` works while
the definition is retained; queued requests after the end fail with `ended`. `run --schedule`
refuses temporary jobs (exit 2). To edit one, remove and add again. MCP and the HTTP API never
create temporary definitions.

## Pausing jobs

```sh
noon pause lint tests   # atomically pause both
noon pause -a           # every job currently in this project
noon resume -a          # also resumes individually paused jobs
```

Pausing cancels queued runs but leaves running ones. Jobs added later aren't paused; use
`noon disable` for that. Resuming starts schedules from now, with no catch-up. Without names or
`--all`, both commands only list candidates. JSON has sorted `jobs`, `paused`, and `changed`.

## Multiple targets

`pause`, `resume`, `remove`, `history`, and queued `run` accept several job names; `cancel` also
accepts run IDs. Duplicates are processed once. Foreground `run` and `logs` take one target.

Targets are validated before anything changes. After that, cancel, queue, and remove continue past
failures, report them, and exit 1; completed work isn't undone. Multi-target JSON:

| Command | Fields |
| --- | --- |
| `pause` / `resume` | `jobs`, `paused`, `changed` |
| `history` | `runs`, merged newest-first under one `--limit` |
| `cancel` | `runs`, `pending`, optional `ended_temporary_schedules` / `ended_temporary_jobs`, `errors` |
| queued `run` | `results` (`run`, `created`, `scheduler`, optional `reason` / `cooldown_until`), `errors` |
| `remove` | `removed` (`removed`, `file`), `errors` |

Each `errors` entry is `{target, error}`; the array is empty on success.

## Output and exit codes

### Terminal styling

Names, headers, and suggested commands are bold. Failures are red; attention states (needs
approval, paused, disabled, offline, orphaned) yellow; running cyan; success green; noops dim.
Colors come from your terminal's ANSI palette, and every label reads the same without color.

| Mode | Styles |
| --- | --- |
| `--color=auto` | Each stream that is a terminal, unless `NO_COLOR` is set or `TERM=dumb` |
| `--color=always` | Always |
| `--color=never`, `--json` | Never |

Job output and logs are never recolored or stripped. Tables escape control characters and align
before styling.

### Errors and exits

Human errors are one stderr line, optionally followed by a hint:

```console
$ noon add tests "every day" -- go test ./...
noon: every day needs a time: try every day at 9am, daily (midnight), or every 24h
Hint: Try every day at 9am
```

With `--json`, errors are one object on stdout:

```console
$ noon --json run nope
{"error":{"code":"not_found","message":"job \"nope\" not found","hint":"Run noon run to list jobs"},"schema_version":1}
```

Every finishing command prints exactly one `schema_version: 1` object with `--json`. `--help`,
`--version`, and completion stay text. `serve`, `mcp`, `tui`, and `logs --follow` reject `--json`.
A foreground `run --json` sends job output to stderr. Typos get a suggestion, never autocorrection.

| Exit | Meaning |
| --- | --- |
| 0 | Success, including empty results and guidance |
| 1 | Operational error |
| 2 | Usage error |
| 75 | `noon serve`: the database is newer than this binary |

`noon run NAME` exits with the job's code instead (0 when `noop_exit_code` matches):

| Exit | Meaning |
| --- | --- |
| 124 | Timed out |
| 130 | Cancelled |
| 128+N | Killed by signal N |
| 127 / 126 / 125 | Command not found / not executable / other start failure |

A job whose `os` or `arch` doesn't match fails with exit 1 and code `unsupported_platform`.

### No target, no action

Without a target, commands inspect instead of acting, exit 0, and create no state:

| Command | Prints |
| --- | --- |
| `run`, `remove`, `pause`, `resume` | Candidate jobs (never auto-selects the only one) |
| `cancel` | Active runs |
| `add`, `import crontab` | Examples |
| `service` | Service status |
| `shell init` | Sourcing commands per shell |
| `logs` | The latest run's log |

Suggested commands keep your explicit `-C`, `--config`, `--state-dir`, and `--timezone` (including
from the environment) as absolute, quoted paths.

### Remove

```sh
noon remove tests           # history stays: noon history tests
noon remove tests --shared  # pick noon.yaml when both files define tests
```

`--shared` selects `noon.yaml`, `--local` `.git/noon.yaml`. Everything is checked before any file
is written. JSON is `{removed, file}`.

## Run now

`noon run NAME` runs in the foreground with no scheduler or approval. Job stdout and stderr pass
through; Noon's status lines go to stderr.

```sh
noon run report > report.md
noon run tests --json 2>/dev/null   # job output → stderr; run summary → stdout
```

One run per job per project at a time. SIGINT, SIGTERM, or SIGHUP sends TERM to the process
group, then KILL after 10 seconds; a second signal kills immediately.

## Queue for later

Queued runs need `noon enable` and a running scheduler:

```console
$ noon run tests --detach
Queued tests as run 2 · due now
Follow it: noon logs 2 --follow
The scheduler isn't running, so nothing runs until it starts.
Suggested action: noon service install
```

| Flag | Effect |
| --- | --- |
| `-d`, `--detach` | Due now. Merges into a run of the same job that is already due |
| `--in 2h` | Separate one-shot after a duration |
| `--at 17:00` | Separate one-shot at `HH:MM`, `YYYY-MM-DD HH:MM`, or RFC 3339 |
| `--key KEY` | Return the existing run if this project/job already used `KEY` |
| `--data JSON` | Payload as JSON, `@FILE`, or `-` for stdin; up to 256 KiB |

`noon run lint tests --detach` gives each job the same timing, key, and payload; keys are per job.
Keys are remembered for the newest 50 finished runs. The payload is written to a file the job reads
from `NOON_EVENT_FILE`. `cooldown` applies only to queued requests. See [Triggers](triggers.md).

## Temporary schedules for existing jobs

```sh
noon run watch-ci --schedule "*/10 * * * *" --for 2h
noon run standup --schedule "weekdays at 9am" --until "2026-10-23 10:00"
noon cancel watch-ci
```

This attaches recurring requests to an existing enabled, unpaused, platform-supported job in an
enabled project. Shared jobs need approval. It writes state, not config; it grants no authority
to create or edit jobs. The scheduler must be running to execute occurrences.

Use the same schedule grammar as config, with exactly one end: `--for DURATION` (at least 1m,
rounded up to a whole minute) or `--until TIME` (same resolution as `noon add`). The end must be
after now, within 7 days including rounding, and after at least one occurrence. Don't combine
`--schedule` with `--detach`, `--in`, or `--at`. Several names get the same schedule and end.

Only one temporary schedule per job is allowed. A repeated nonempty `--key` returns it unchanged
with `created: false` and `reason: "duplicate"`; otherwise a second request fails with
`already_scheduled` and hint `noon cancel NAME`. Optional `--data` is copied to a fresh event
file for each queued occurrence. Cooldown and `requests: each` don't apply.

Intervals retain their creation phase across restarts; cron forms align to the project clock.
The job's missed and overlap policies apply, shared with its config schedule. Coinciding
occurrences produce one queued run and one skipped record. Temporary schedules have no splay or
jitter. No occurrence starts at or after the temporary end or the job's own `until`; if the
first occurrence would, creation fails with `ended`.

Expiry, cancellation by job, pause, project disable, `noon remove`, and job end delete the
temporary schedule. The running scheduler also deletes it when it sees the job disabled, its
digest changed, or its platform unsupported; an edit reverted while no scheduler runs goes unseen. Resume, re-enable,
and re-approval never restore it. Already-created runs follow the ordinary queue/running rules.

Creation JSON is `{schema_version:1,temporary_schedule:{schedule,until,next_at,requested_by,created_at,key?},created,scheduler,reason?}`,
with times in the project timezone.
`scheduler` is `running` or `offline`; offline human output prints the same warning as queueing.
`list --json` includes the optional `temporary_schedule` on each job; job `next_at` and human
NEXT select the earlier config or temporary occurrence. The dashboard adds a temporary-schedule
notice. There are no temporary-schedule JSON API `/v1/` routes; MCP over HTTP supports them.

## Cancel

```sh
noon cancel                # list active runs; changes nothing
noon cancel 12             # cancel run 12, even another terminal's foreground run
noon cancel tests          # end its temporary job/schedule and cancel every active run
noon cancel tests lint 42  # jobs and run IDs together
```

Queued runs cancel immediately; running ones get TERM then KILL. An `orphaned` run (owner gone) is
recorded cancelled, and only an identity-matched process group is signaled. `noon disable` also
cancels queued runs and ends temporary schedules. Cancelling a run ID alone leaves its temporary
schedule intact. Cancelling a job prints `Ended temporary schedule for NAME.` when one existed,
and JSON lists those jobs in `ended_temporary_schedules`. For a temporary job definition,
it prints `Ended temporary job NAME.` and lists `ended_temporary_jobs`; the 24h grace starts now.

## History

```sh
noon history                     # newest 20 runs
noon history tests lint -n 50    # newest 50 across both jobs
noon history -s failed,timed_out # repeat or comma-separate statuses
noon history -s noop             # only no-op polls
```

Statuses: `queued`, `running`, `succeeded`, `noop`, `failed`, `timed_out`, `cancelled`,
`interrupted`, `skipped`, `missed`, `expired`. A noop is neither success nor failure and runs no
hooks. An orphaned run shows as `orphaned` but stays `running` in JSON, with `orphaned: true`.
History and logs work after a job is removed.

## Logs

```sh
noon logs                         # latest started run in this project
noon logs tests --status failed   # latest failure of tests
noon logs 12 -f                   # wait for run 12, then stream it
noon logs 12 -n 50                # last 50 lines
noon logs 12 -t                   # prefix local receipt times
noon logs 12 -m 'panic|FAIL' --context 3
noon logs 12 --section job        # without hook output
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
| `-t`, `--timestamps` | Prefix lines with local receipt time; JSON adds `line_times` |

Bounded reads print a resume command to stderr that pins the run and cursor:

```console
$ noon logs check --match FAIL --context 1 -t
2026-10-19T09:15:30.125-06:00 2-ok 2
2026-10-19T09:15:30.125-06:00 3:FAIL widget
2026-10-19T09:15:30.126-06:00 4-ok 3
Continue: noon logs 1 --after-cursor 27 --timestamps
```

JSON returns `log`, `next`, `size`, `truncated`, `matched_lines`, and `resume`. `log` is never
prefixed; `--timestamps` adds `line_times` ([format](agents.md#timestamps)). Unknown times display
as 29 dashes. Timestamps mark when Noon received output, not when the child wrote it.

- Only `--status` can select a queued or never-started run, and it can't be combined with a run ID.
- `--follow` accepts only `--tail` and `--after-cursor`; `--path` accepts nothing.
- Bounded reads omit an active run's unfinished last line; unflagged reads include it.
- If the run's owner dies, `--follow` prints what's left and exits 1 with code `orphaned`.
- Retention: see [limits](agents.md#limits-and-safety).

## Status and doctor

Bare `noon` and `noon status` show jobs, scheduler state, queued work, up to three priority notices
plus a temporary-schedule summary when present, and one `Suggested action:`. Both follow this priority:

| Notice | Suggested action |
| --- | --- |
| Config error | `noon config check` |
| Scheduled but not enabled / pending approval | `noon enable` |
| Scheduler offline | `noon service install` (or `noon serve` if installed) |
| Orphaned run (blocks background runs) | `noon cancel ID` |
| Queued work / otherwise | Run an unpaused job, or `noon add` |

The suggestion is a command, not the next scheduled run; that's the `NEXT` column. JSON has
`project`, `scheduler`, `service`, `jobs`, `queued`, `notices`, and `next`.

`noon list` adds commands. Its `LAST` column ignores noops. Columns shrink to the terminal (80 when
redirected) but job names never truncate; `--wide` shows everything. Environment values are never
shown.

```console
$ noon doctor
ok config: Configuration valid
ok approvals: No approvals pending
warn scheduler: Scheduler heartbeat is absent or stale — noon service install, or noon serve
warn service: Per-user service is not installed — noon service install
ok job tests command: go
```

Warnings exit 0; failures exit 1. JSON has `ok` and `checks` (`name`, `status`, `message`,
`hint`). Doctor never fixes, runs, migrates, or creates anything.

## Service restart and upgrades

```sh
noon service install   # after an upgrade or a changed PATH/XDG_CONFIG_HOME
noon service restart   # same unit, fresh process
```

Both interrupt active jobs. `restart` fails with `service_not_installed` if there's no unit; JSON
returns `restarted` and `unit_path`. Config and `env_file` changes never need a restart, and a
restart never approves jobs. See [service upgrades](service.md#upgrades).

## TUI

`noon tui` refreshes every second and never edits jobs or enables projects.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move |
| `enter` | Open runs for a job |
| `l` | Open a log |
| `f` | Cycle run status filter |
| `/`, `n` / `N` | Search the log (RE2, three lines of context), next / previous match |
| `s` | Cycle job / hook / all log sections |
| `t` | Toggle log timestamps (shown by default) |
| `w` | Toggle line wrapping |
| `g` / `G` | Top / bottom of log |
| `r` | Queue the selected job now |
| `p` | Pause or resume the selected job |
| `c` | Cancel a run (asks first) |
| `esc`, `?`, `q` | Back, help, quit |
