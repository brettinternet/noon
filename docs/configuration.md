# Configuration

This documents implemented settings and command job definitions. Jobs can be
validated and listed, but are not executed. Hooks, prompts, cron import, and the
scheduler are not implemented; see [design](design.md) for their proposed format.
The default state directory will move from `PROJECT/.bump` to a per-user
directory (decision-3); until then, nothing writes state.

## Sources

Lowest to highest priority:

1. Built-in defaults.
2. `$XDG_CONFIG_HOME/bump/config.yaml`, or `~/.config/bump/config.yaml` when XDG is unset.
3. `bump.yaml` in the selected project.
4. `.bump.yaml` in that same directory (machine-local overrides).
5. Environment variables.
6. Explicit CLI flags.

Files merge by setting, not by selecting one entire file. This is a deliberate
difference from hum's local-manifest selection. A malformed lower-priority file
is still an error; overriding a setting does not hide a broken source.

`--config FILE` / `BUMP_CONFIG` replaces **all three automatic file sources**.
It must exist. This makes CI/container invocations reproducible. Defaults and
environment/flag overrides still apply. Its location does not change project
selection: use `--project` to change the project.

Missing automatic files are fine. Unreadable files, dangling symlinks, unknown
keys, duplicate keys, null values, aliases/merge keys, unsupported versions,
multiple documents, and files larger than 1 MiB fail with the source filename.
A file may be an empty mapping (`{}`), but not an empty document.

## Project selection

`--project DIR` / `-C DIR` / `BUMP_PROJECT` selects exactly that existing directory.
Otherwise start in the current directory and walk upward to the nearest directory
containing `bump.yaml`, `.bump.yaml`, or `.git`. Stop there, including when `.git`
is a worktree file. Never inherit config above a Git boundary. With no marker,
use the invocation directory. Symlinks in the selected project path are resolved.

Nested config files intentionally create a nearer project. Repository discovery
only reads files; it neither trusts them for execution nor starts a scheduler.

## Settings schema

```yaml
version: 1
timezone: Local
# state_dir: .bump
```

| Setting | Default | Environment | Flag |
| --- | --- | --- | --- |
| Timezone | `Local` | `BUMP_TIMEZONE` | `--timezone` |
| State directory | `PROJECT/.bump` | `BUMP_STATE_DIR` | `--state-dir` |
| Project selection | Discovery | `BUMP_PROJECT` | `--project`, `-C` |
| Explicit config file | Automatic sources | `BUMP_CONFIG` | `--config` |

`version` is optional in the bootstrap and defaults to 1. Timezones accept
`Local`, `UTC`, and IANA names such as `America/Chicago`; timezone data is
embedded for minimal container environments. Empty explicitly supplied settings,
including empty `BUMP_*` environment variables, are errors: unset them instead.
`XDG_CONFIG_HOME` must be absolute when set; empty means use the home default.

Setting paths in a YAML file are relative to **that file's directory**.
Job `cwd` is instead relative to the selected project root. State-directory
flags/environment values are relative to the selected project; project/config
path flags are relative to the invocation directory. No shell, tilde, or variable
expansion happens inside YAML. A relative user-level `state_dir` therefore
refers to the user config directory, not each repository; normally leave it unset.

`bump config` prints effective settings and the loaded file paths in order.
`bump config check` validates the same snapshot. Neither creates state or checks
future job executability. State ownership/writability belongs to the later runner.

## Private configuration

`bump init` exclusively creates `bump.yaml`; `bump init --local` exclusively
creates `.bump.yaml` with mode 0600. Existing paths are preserved, never replaced.
Generated settings are commented out so init does not shadow inherited values.
Init does not merge or repair existing YAML. Use `bump config check` afterward.
Invocation setting overrides are rejected by init rather than silently ignored.

Add both entries to your repository's `.gitignore`:

```gitignore
.bump.yaml
.bump/
```

Init prints this reminder but never modifies an existing ignore file. Never commit
credentials or `.bump.yaml`: local job definitions are private.

## Command jobs

Jobs are allowed in `bump.yaml`, `.bump.yaml`, and an explicit `--config` file,
not in user config. Names match `^[a-z][a-z0-9_-]{0,63}$`.

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

See [the complete example](../examples/jobs.yaml).

| Field | Default | Accepted values |
| --- | --- | --- |
| `description` | empty | String |
| `schedule` | manual | Five-field cron, macros, calendar phrases, or intervals; [grammar](design.md#schedules) |
| `run` | required | Non-empty string (future `/bin/sh -c`) or non-empty string list (exact argv) |
| `cwd` | project root | Absolute path or path relative to project root; existence is not checked |
| `env` | empty | String values only; quote numbers. Names match `[A-Za-z_][A-Za-z0-9_]*`; `BUMP_` is reserved |
| `timeout` | `1h` | Integer `s`, `m`, `h`, `d` components, at least `1s`, or `none` |
| `overlap` | `skip` | `skip` or `queue` |
| `missed` | `run_once` | `run_once` or `skip` |
| `enabled` | `true` | Boolean |

Execution policies are validated now but take effect only when execution is
implemented. Unknown fields (including prompts and hooks), aliases, nulls,
duplicate keys, and wrong types are errors with file, line, and YAML path.

A local job replaces the entire shared job with the same name; fields do not
merge. The exception is `jobs: {tests: {enabled: false}}`, which toggles an
existing shared job. An enabled-only entry without a shared job is an error.
A toggle retains shared execution provenance; a complete local replacement does
not. Both record the local source path and `overrides: true`.

`bump list` (alias `ls`) shows sorted jobs, normalized schedules, next-run previews
in the project timezone, and commands. No jobs means a copy-paste example.
Bare `bump` and `bump status` add project information. None executes, writes state,
or checks whether commands or working directories exist. Intervals preview
`now + interval` until a future scheduler persists their phase. Manual and disabled
jobs have no next time. `bump config check` reports the validated job count.

`bump list --json` returns `schema_version: 1`, `project` (`path`, `timezone`),
and `jobs`. Each job contains `name`, `description`, `schedule`, `next_at`,
`run`, `cwd`, `env_keys`, `timeout`, `overlap`, `missed`, `enabled`,
`source`, `shared`, `overrides`, and `digest`.
Absent schedules and next times are null; times are RFC 3339 with the project
timezone offset. Environment keys are sorted; environment values are never
printed, including by config inspection. Avoid putting secrets in commands or descriptions.

Digests are `sha256:` plus the SHA-256 of canonical JSON: sorted keys, explicitly
set execution fields only, normalized schedules and durations, and omitted unset
fields. Description, enabled, source paths, and YAML formatting do not affect
the digest. Digests cover definitions, **not the contents of referenced scripts**.
Approval and execution are not yet implemented; eventually shared jobs will
require approval of their current digest. Trust only repositories whose contents
you would run by hand.
