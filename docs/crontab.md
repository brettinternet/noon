# Crontab import

`noon import crontab` converts a crontab to Noon YAML. It previews by default:

```console
$ cat cron.txt
MAILTO=""
0 2 * * * /usr/local/bin/backup.sh --full
*/15 * * * * curl -fsS https://example.com/ping

$ noon import crontab cron.txt
jobs:
  # crontab line 2
  backup-sh:
    # Cron never catches up; skip missed runs.
    missed: skip
    run: /usr/local/bin/backup.sh --full
    schedule: 0 2 * * *
    # Cron has no job timeout.
    timeout: none
  # crontab line 3
  curl:
    # Cron never catches up; skip missed runs.
    missed: skip
    run: curl -fsS https://example.com/ping
    schedule: '*/15 * * * *'
    # Cron has no job timeout.
    timeout: none
```

Add the jobs to the private `.git/noon.yaml` with `--write`, or to personal jobs with `--user`:

```sh
crontab -l | noon import crontab - --write
crontab -l | noon --user import crontab - --write
```

The write is all or nothing and keeps existing comments and jobs. User jobs run from
`~/.config/noon` and need `noon --user enable` to schedule. `--config` files are never written.
JSON lists names, schedules, commands, environment keys (not values), and source lines.

## Conversion

- Five-field schedules and `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`,
  `@annually` are kept.
- The command becomes a string `run`, executed with `/bin/sh -c`.
- `NAME=value` lines set `env` on the entries after them. One pair of matching quotes is stripped.
- Names come from the command's basename, lowercased: `/usr/local/bin/backup.sh` → `backup-sh`.
  Collisions get `-2`, `-3`; names starting with a digit get `cron-`; empty names become
  `cron-LINE`.
- Every job gets `missed: skip` and `timeout: none` to match cron.
- Schedules use the project timezone.

## Refused

Noon refuses what it can't reproduce instead of approximating:

| Input | Why |
| --- | --- |
| `@reboot` | No boot trigger |
| `%` in a command | Cron turns it into newlines and stdin |
| Non-empty `MAILTO` | No mail; `MAILTO=""` is ignored |
| `SHELL` other than `/bin/sh` | Jobs run with `/bin/sh` |
| `CRON_TZ`, `TZ` | Set the project `timezone` instead |
| `NOON_*` variables | Reserved |
| Malformed entries or schedules | Fields 1–5 are always the schedule; the rest is the command. Noon's shortened cron syntax is not accepted here |

```console
$ noon import crontab cron.txt
noon: line 3: @reboot is not supported; Noon has no boot trigger
Hint: Fix the listed crontab lines; nothing was written.
```

Every refused line is reported at once. Import reads only the file or stdin you give it, never
system crontabs or `/etc/cron.d`. Jobs run in the project directory with Noon's environment, not
cron's, so check paths and variables before enabling.
