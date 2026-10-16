# Configuration

This documents implemented settings, command jobs, agent-prompt jobs, and lifecycle hooks. Jobs can be
validated, listed, added, removed, paused, resumed, and executed with `noon run NAME`. Foreground
run history and bounded logs are stored in the per-user state directory. Cron import is not implemented;
see [design](design.md) for its proposed format.

## Sources

Lowest to highest priority:

1. Built-in defaults.
2. `$XDG_CONFIG_HOME/noon/config.yaml`, or `~/.config/noon/config.yaml` when XDG is unset.
3. `noon.yaml` in the selected project.
4. `.noon.yaml` in that same directory (machine-local overrides).
5. Environment variables.
6. Explicit CLI flags.

Files merge by setting, not by selecting one entire file. This is a deliberate
difference from hum's local-manifest selection. A malformed lower-priority file
is still an error; overriding a setting does not hide a broken source.

`--config FILE` / `NOON_CONFIG` replaces **all three automatic file sources**.
It must exist. This makes CI/container invocations reproducible. Defaults and
environment/flag overrides still apply. Its location does not change project
selection: use `--project` to change the project.

Missing automatic files are fine. Unreadable files, dangling symlinks, unknown
keys, duplicate keys, null values, aliases/merge keys, unsupported versions,
multiple documents, and files larger than 1 MiB fail with the source filename.
A zero-byte project file is an empty configuration and can be populated with `noon add`; a whitespace-only empty YAML document is invalid. An explicit empty mapping (`{}`) is valid.

## Project selection

`--project DIR` / `-C DIR` / `NOON_PROJECT` selects exactly that existing directory.
Otherwise start in the current directory and walk upward to the nearest directory
containing `noon.yaml`, `.noon.yaml`, or `.git`. Stop there, including when `.git`
is a worktree file. Never inherit config above a Git boundary. With no marker,
use the invocation directory. Symlinks in the selected project path are resolved.

Nested config files intentionally create a nearer project. Repository discovery
only reads files; it neither trusts them for execution nor starts a scheduler.

## Settings schema

```yaml
version: 1
timezone: Local
on_success: [my-notifier]
on_failure: [my-notifier]
```

`state_dir` may be set in user config or an explicit `--config` file, but not in
`noon.yaml` or `.noon.yaml`. Top-level `on_success` and `on_failure` hook defaults
are allowed only in user config or an explicit `--config` file; project files cannot
set defaults. A job-level hook overrides its inherited default, and an empty list
(`on_failure: []`) disables that default for the job. See [Lifecycle hooks](hooks.md).

| Setting | Default | Environment | Flag |
| --- | --- | --- | --- |
| Timezone | `Local` | `NOON_TIMEZONE` | `--timezone` |
| State directory | `$XDG_STATE_HOME/noon`, else `~/.local/state/noon` | `NOON_STATE_DIR` | `--state-dir` |
| Project selection | Discovery | `NOON_PROJECT` | `--project`, `-C` |
| Explicit config file | Automatic sources | `NOON_CONFIG` | `--config` |

`version` is optional in the bootstrap and defaults to 1. Timezones accept
`Local`, `UTC`, and IANA names such as `America/Chicago`; timezone data is
embedded for minimal container environments. Empty explicitly supplied settings,
including empty `NOON_*` environment variables, are errors: unset them instead.
`XDG_CONFIG_HOME` must be absolute when set; empty means use the home default.

Setting paths in a YAML file are relative to **that file's directory**.
Job `cwd` is instead relative to the selected project root. `NOON_STATE_DIR` and
`--state-dir` are relative to the invocation directory; project/config path flags
are also relative to the invocation directory. `$XDG_STATE_HOME` must be absolute
when set. No shell, tilde, or variable expansion happens inside YAML. A relative
user-level `state_dir` therefore refers to the user config directory, not each
repository; normally leave it unset.

`noon config` prints effective settings, loaded file paths, and available agent profiles.
`noon config --json` includes `agents` as profile-name-to-command mappings and `default_agent`.
`noon config check` validates the same snapshot. Neither creates state or checks job
executability or whether an agent CLI is installed. `noon run NAME` creates the per-user
SQLite store before starting the job and records its status and bounded output log.

## Private configuration

`noon init` exclusively creates `noon.yaml`; `noon init --local` exclusively
creates `.noon.yaml` with mode 0600. Existing paths are preserved, never replaced.
Generated settings are commented out so init does not shadow inherited values.
Init does not merge or repair existing YAML. Use `noon config check` afterward.
Invocation setting overrides are rejected by init rather than silently ignored.

Ignore the private override in your repository's `.gitignore`:

```gitignore
.noon.yaml
```

Init prints this reminder but never modifies an existing ignore file. Never commit
credentials or `.noon.yaml`: local job definitions are private.

## Jobs

Jobs are allowed in `noon.yaml`, `.noon.yaml`, and an explicit `--config` file,
not in user config. Names match `^[a-z][a-z0-9_-]{0,63}$`. Exactly one of `run`, `prompt`, or
`prompt_file` defines the action.

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

See [the complete example](../examples/jobs.yaml). For agent-facing descriptions,
required-action instructions, and completion checks, see
[Discover and run actions](agents.md#discover-and-run-actions).

| Field | Default | Accepted values |
| --- | --- | --- |
| `description` | empty | String |
| `schedule` | manual | Five-field cron, macros, calendar phrases, or intervals; [grammar](design.md#schedules) |
| `run` | — | Non-empty string (`/bin/sh -c`) or non-empty string list (exact argv) |
| `prompt` | — | Inline agent prompt, at most 64 KiB |
| `prompt_file` | — | Path relative to project root; read when run starts, at most 64 KiB |
| `agent` | `default_agent` | Profile name; required on a prompt job if no default is set |
| `cwd` | project root | Absolute path or path relative to project root; existence is not checked |
| `env` | empty | String values only; quote numbers. Names match `[A-Za-z_][A-Za-z0-9_]*`; `NOON_` is reserved |
| `timeout` | `1h` | Integer `s`, `m`, `h`, `d` components, at least `1s`, or `none` |
| `overlap` | `skip` | `skip` or `queue` |
| `missed` | `run_once` | `run_once` or `skip` |
| `enabled` | `true` | Boolean |
| `on_success` | inherited default | Non-empty string (`/bin/sh -c`) or non-empty string list (argv); empty list disables an inherited default |
| `on_failure` | inherited default | Non-empty string (`/bin/sh -c`) or non-empty string list (argv); empty list disables an inherited default |

Foreground and scheduled runs honor `cwd`, `env`, `timeout`, `enabled`, `on_success`, and
`on_failure`. `noon serve` executes schedules with the configured `overlap` and `missed` policies.
Hook commands set on a shared job, or inherited from an explicit `--config` file, are included in
its digest; user-config defaults are not, so changing them never revokes approvals. See
[Lifecycle hooks](hooks.md) for execution, output, timeout, and security details.
Unknown fields, aliases, nulls, duplicate keys, and wrong types are errors with file, line, and YAML path.

### Agent prompts

Profiles are machine-specific and belong in user config (`$XDG_CONFIG_HOME/noon/config.yaml`,
or `~/.config/noon/config.yaml`). `agents` is allowed only there or in an explicit `--config`
file. `default_agent` is allowed in user config, `.noon.yaml`, or an explicit config file.
Project `noon.yaml` cannot define either setting. Built-in `claude` and `codex` profiles can be
replaced by defining the same name in user config:

```yaml
default_agent: claude
agents:
  claude:
    command: [claude, -p, --permission-prompts, none, --permission-mode, acceptEdits]
    prompt: stdin
  codex:
    command: [codex, exec, -]
    prompt: stdin
  reviewer:
    command: [my-agent, --quiet]
    prompt: argument
```

Built-ins are `claude -p --permission-prompts none` and `codex exec -`, both receiving the prompt
on stdin. Their flags were checked against the installed CLIs' `--help`; Claude users who want edits
can add `--permission-mode acceptEdits` to their own profile. Unattended runs must never wait for
permission prompts. A custom profile's `prompt` defaults to `stdin`; `argument` appends the prompt
byte-for-byte as the final argument. In stdin mode the exact prompt bytes are the child's stdin.
There is no templating, variable expansion, or trimming. A missing, unreadable, or oversized prompt
file fails before the agent starts with exit code 125. Prompt files must be regular files;
FIFOs, devices, and directories are rejected. Prompt runs receive `NOON_AGENT=NAME`.
Noon does not check whether the configured agent executable is installed.

A local job replaces the entire shared job with the same name; fields do not
merge. The exception is `jobs: {tests: {enabled: false}}`, which toggles an
existing shared job. An enabled-only entry without a shared job is an error.
A toggle retains shared execution provenance; a complete local replacement does
not. Both record the local source path and `overrides: true`.

`noon add NAME [SCHEDULE] ACTION` creates a job in `.noon.yaml` by default; `--shared` targets
`noon.yaml`. Actions are `-- ARGV...`, `--sh STRING`, `--prompt TEXT`, and `--prompt-file PATH`.
`--agent`, `--description`, `--timeout`, `--in`, and `--at` configure the job or queue its first
run. Existing YAML is edited by splicing lines rather than re-encoding, and unsupported `jobs`
shapes must be edited by hand. `noon remove NAME` (alias `rm`) removes the definition while keeping
history; a version declaration added to an initially empty file remains after the last job is removed.
If the name exists in both files, select `--shared` or `--local`. Bare remove lists
candidates. `noon pause NAME` and `noon resume NAME` control background dispatch; bare forms list
candidates, and both human and JSON job listings show paused state. Manual `noon run` continues to
work, and resuming resets schedule state so there is no catch-up.

`noon list` (alias `ls`) shows sorted jobs, normalized schedules, next-run previews
in the project timezone, and commands. No jobs means a copy-paste example.
Bare `noon` and `noon status` add project information. None executes, writes state,
or checks whether commands or working directories exist. Intervals preview
`now + interval` until `noon serve` persists their phase; then NEXT uses that watermark. Manual and disabled
jobs have no next time. `noon config check` reports the validated job count.

`noon list --json` returns `schema_version: 1`, `project` (`path`, `timezone`, `enabled`,
`scheduler`, and `config_error` when present),
and `jobs`. Each job contains `name`, `description`, `schedule`, `next_at`,
`run`, `prompt`, `prompt_file`, the effective `agent` for prompt jobs, `cwd`, `env_keys`, `timeout`, `overlap`, `missed`, `enabled`,
`source`, `shared`, `overrides`, `digest`, and `approval` (`not_required`, `approved`, or `needs_approval`).
Absent schedules and next times are null; times are RFC 3339 with the project
timezone offset. Environment keys are sorted; environment values are never
printed, including by config inspection. Avoid putting secrets in commands or descriptions.

Digests are `sha256:` plus the SHA-256 of canonical JSON: sorted keys, explicitly
set execution fields only, normalized schedules and durations, and omitted unset
fields. Prompt text, prompt-file path, and the effective agent name participate; agent profile
commands are user-owned and excluded. Description, enabled, source paths, and YAML formatting do
not affect the digest. Digests cover definitions, **not the contents of referenced scripts or prompt files**.
`noon enable` validates first, records this canonical checkout and its configuration
selection (automatic files or the absolute explicit `--config` path), and approves
all current shared job digests. Repeating it preserves unchanged approval timestamps.
`noon disable` clears opt-in and approvals, keeping history. It does not read project
manifests or validate jobs, so invalid job edits cannot prevent revocation. It resolves
state from user config or an explicit config file; if that file is unreadable or invalid
YAML, pass `--state-dir` to select the existing store directly. Neither starts a scheduler;
run `noon serve` explicitly to execute enabled projects' schedules. Serve uses each project's
recorded config selection, rejects project/config/timezone/JSON flags and their environment
variables, and reloads changed files every two seconds. Invalid edits preserve the last valid
jobs and record a config error until fixed. Bare noon and list report scheduler status from
its heartbeat; a heartbeat older than five seconds is offline.

Local jobs and whole-job local overrides need no approval. An enabled-only local
toggle retains the shared digest and approval requirement. Changing a shared action,
schedule, or environment requires re-approval; editing only its description does not.
List and status read approval state without creating or migrating a database, and
show `Next: noon enable` when an enabled project has changed shared jobs or a
non-enabled project has scheduled jobs. Explicit foreground runs need no approval.
Trust only repositories whose contents you would run by hand; approval is not a sandbox.
