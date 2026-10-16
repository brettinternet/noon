# Configuration

This documents the working bootstrap. Jobs, hooks, cron imports, and scheduling
fields are not accepted yet; see [design](design.md) for the proposed job format.
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

## Bootstrap schema

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

Paths in a YAML file are relative to **that file's directory**. State-directory
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

Init prints this reminder but never modifies an existing ignore file. The bootstrap
has no secret fields. Do not commit credentials; future job environments will
support explicit references rather than dumping secrets into status/config output.
