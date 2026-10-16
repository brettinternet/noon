# Crontab import

`noon import crontab FILE|-` converts a crontab file to Noon YAML and prints a preview. Use `-` to read standard input:

```sh
noon import crontab ./crontab.txt
crontab -l | noon import crontab -
```

Previewing never writes files. Add `--write` to add every accepted entry to the project's `.noon.yaml`:

```sh
crontab -l | noon import crontab - --write
```

The write is all-or-nothing: if any input line is refused, Noon reports all line errors and leaves `.noon.yaml` unchanged. Existing comments and job definitions are preserved. `--write` is refused when an explicit `--config` file (or `NOON_CONFIG`) is selected. JSON output includes `schema_version`, job names, schedules, command strings, environment **keys** (not values), source line numbers, and whether jobs were written.

## Supported

- Blank lines and comment lines.
- `NAME=value` environment assignments, applied to following entries. One matching pair of single or double quotes around a value is stripped. `MAILTO=""` is accepted and ignored.
- Five-field cron schedules and `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, and `@annually`.
- The rest of an entry after its schedule is imported as a string `run`, executed by Noon through `/bin/sh -c`.

Names come from the basename of the command's first word, normalized to lowercase letters, digits, `_`, and `-`. Empty names become `cron-LINE`; names beginning with a digit get a `cron-` prefix. Duplicates and collisions with project jobs receive `-2`, `-3`, and so on, with names limited to 64 characters.

Every imported job sets `missed: skip` because cron does not catch up missed runs, and `timeout: none` because cron does not impose a job timeout. The YAML comments on those fields record the reasons. Noon never overlaps the same job. Schedules use the project's configured timezone; set it in project config rather than using cron timezone variables.

## Refused

Noon refuses unsupported semantics instead of silently approximating them:

- `@reboot` (Noon has no boot trigger).
- `%` in a command (cron turns it into newlines and stdin).
- Non-empty `MAILTO`; `MAILTO=""` is allowed and ignored.
- `SHELL` values other than `/bin/sh`.
- `CRON_TZ`, `TZ`, and `NOON_*` environment names. Set the project timezone instead of using `CRON_TZ` or `TZ`; Noon reserves `NOON_*` for its own environment.
- Malformed entries and schedules rejected by Noon. Import always treats the first five fields as the schedule and the remainder as the command; it does not infer a sixth schedule field.

Each refusal includes its crontab line number, and all invalid lines are reported together.

Import reads only the file you name or standard input. It does not read or synchronize system crontabs, `/etc/cron.d`, user columns, or anacron entries. Review working-directory and environment assumptions before enabling imported jobs; Noon runs jobs in the project context, not cron's full execution environment.
