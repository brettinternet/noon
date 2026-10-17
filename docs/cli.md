# Command reference

Run `noon COMMAND --help` for every flag. This page covers behavior the help text doesn't.

## Global flags

These work before or after the subcommand.

| Flag | Environment | Effect |
| --- | --- | --- |
| `-C DIR`, `--project DIR` | `NOON_PROJECT` | Use this project instead of discovery |
| `--config FILE` | `NOON_CONFIG` | Read only this config file |
| `--timezone TZ` | `NOON_TIMEZONE` | Override the timezone |
| `--state-dir DIR` | `NOON_STATE_DIR` | Override the per-user state directory |
| `-j`, `--json` | — | One JSON object on stdout |

`noon serve` accepts only `--state-dir`. Each project uses the config selection recorded by
`noon enable`.

## Short flags

| Command | Short forms |
| --- | --- |
| All commands | `-C DIR` = `--project DIR`, `-j` = `--json`, `-h` = `--help` |
| Root | `-v` = `--version` |
| `history` | `-n N` = `--limit N`, `-s STATUS` = `--status STATUS` |
| `logs` | `-f` = `--follow`, `-s STATUS` = `--status STATUS`; existing `-n/-c/-m/-b` below |
| `run` | `-d` = `--detach` |
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

## Output and exit codes

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

`serve`, `mcp`, `tui`, and `logs --follow` don't finish, so they reject `--json`.

| Exit | Meaning |
| --- | --- |
| 0 | Success, including empty results and guidance |
| 1 | Operational error |
| 2 | Usage error |
| 75 | `noon serve`: the database is newer than this binary |

`noon run NAME` exits with the job's own code instead:

| Exit | Meaning |
| --- | --- |
| 124 | Timed out |
| 130 | Cancelled |
| 128+N | Killed by signal N |
| 127 / 126 / 125 | Command not found / not executable / other start failure |

Read-only commands never create the state directory.

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

Keys are remembered for the newest 50 finished runs per job. The payload is saved to a file
before dispatch, and the job reads its path from `NOON_EVENT_FILE`. A job's `cooldown` applies only
to queued requests. See [Triggers](triggers.md).

## Cancel

```sh
noon cancel        # list active runs; changes nothing
noon cancel 12     # cancel run 12, even another terminal's foreground run
noon cancel tests  # cancel every active run of tests
```

Queued runs are cancelled immediately. Running ones get the same TERM-then-KILL sequence.
`noon disable` also cancels the project's queued runs.

## History

```sh
noon history                            # newest 20 runs
noon history tests -n 50                 # --limit 50
noon history -s failed,timed_out          # --status failed,timed_out
```

Statuses: `queued`, `running`, `succeeded`, `failed`, `timed_out`, `cancelled`, `interrupted`,
`skipped`, `missed`, `expired`. Repeat `--status` or comma-separate values. `history NAME` and
`logs NAME` still work after a job is removed.

## Logs

```sh
noon logs                         # latest started run in this project
noon logs tests                   # latest started run of tests
noon logs tests --status failed   # latest failure
noon logs 12 -f                   # --follow: wait for run 12, then stream it
noon logs 12 -n 50                # last 50 lines
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

Bounded reads print the resume command to stderr. JSON returns `log`, `next`, `size`, `truncated`,
and `matched_lines`. Pass `next` back as `--after-cursor` with the same `--section`.

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
- `--follow` accepts only `--tail` and `--after-cursor`. `--path` combines with nothing.
- Bounded reads omit an unfinished last line of an active run. Unflagged reads include it.
- If the run's owner exits without recording a result, `--follow` prints what's left and exits 1
  with code `orphaned`. Record it with `noon cancel RUN`. Reading never repairs or cancels runs.
- Noon keeps logs for the newest 10 runs per job. See [limits](agents.md#limits-and-safety).

## Status and doctor

Bare `noon` and `noon status` show jobs, scheduler state, queued work, up to three notices, and
one `Next:` step. JSON has `project`, `scheduler`, `service`, `jobs`, `queued`, `notices`, and
`next`. `noon list` shows the same jobs with commands and full next-run times.

`noon doctor` checks config, state, enablement, approvals, the scheduler heartbeat, the service,
each job's command, cwd, and prompt file, and whether Git ignores `.noon.yaml`:

```console
$ noon doctor
ok config: Configuration valid
ok approvals: No approvals pending
warn scheduler: Scheduler heartbeat is absent or stale — noon service install, or noon serve
warn service: Per-user service is not installed — noon service install
ok job tests command: go
ok local_config_ignored: .noon.yaml is ignored by Git
```

Warnings exit 0; failures exit 1. JSON has `ok` and `checks` (`name`, `status`, `message`,
`hint`). Doctor never fixes anything, runs jobs, migrates, or creates state.

## TUI

`noon tui` needs a terminal and refreshes every second. It never edits jobs or enables projects.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move |
| `enter` | Open runs for a job |
| `l` | Open a log |
| `f` | Cycle run status filter |
| `/` | Search the log (RE2, three lines of context) |
| `s` | Cycle job / hook / all log sections |
| `g` / `G` | Top / bottom of log |
| `r` | Queue the selected job now |
| `p` | Pause or resume the selected job |
| `c` | Cancel a run (asks first) |
| `esc`, `?`, `q` | Back, help, quit |
