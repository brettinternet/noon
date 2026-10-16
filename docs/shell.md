# Shell setup

`noon shell init bash|zsh|fish` prints sourceable code. It never edits startup files,
creates Noon state, enables a checkout, or starts a scheduler. Choose your shell
explicitly. Bash, Zsh, and Fish are exercised by `task shell:smoke`; all three must
be installed on PATH (an unresolved tool-manager shim is not an installation).
Bash 3.2 and newer are supported, including scalar prompt commands on older Bash
and prompt-command arrays on Bash 5.1+.

## Completion only

Run the matching command in your interactive shell, or add it yourself to
`.bashrc`, `.zshrc`, or `~/.config/fish/config.fish`:

```bash
# Bash: no bash-completion package needed
eval "$(noon shell init bash)"
```

```zsh
# Zsh: uses its bundled completion system; creates no compdump file
eval "$(noon shell init zsh)"
```

```fish
noon shell init fish | source
```

Completion suggests commands, aliases, flags, and configured job names for
`run`, `pause`, `resume`, `remove`/`rm`, `history`, `logs`, and `cancel`. It respects
`--project`/`-C`, `--config`, and their environment variables. Existing names are
not suggested as a new `add` name, after an existing selector, or as flag values.
Only currently configured names are offered, not historical names or run IDs.
Broken configuration suppresses job names, not static commands and flags.

Loading and invoking completion is read-only: no job or lifecycle hook execution,
state initialization/migration, approval changes, services, or network calls.
Arguments are passed as quoted arrays, not evaluated as shell source. The binary
contains all glue; Python, Node, jq, and external completion frameworks are not
needed. Keep `noon` on PATH in the shell using the integration.

## Queue a job on entry

Define a job with a name you choose; there is no magic default or new job schema:

```yaml
jobs:
  check-in:
    run: [task, check]
    cooldown: 5m
```

Review the checkout and explicitly run `noon enable`. Then source:

```bash
eval "$(noon shell init bash --on-enter check-in)"
```

```zsh
eval "$(noon shell init zsh --on-enter check-in)"
```

```fish
noon shell init fish --on-enter check-in | source
```

This includes completion. Loading or reloading records the current location
without queueing anything; it does not register duplicate hooks. Existing prompt
and directory hooks remain installed, and Noon preserves the hook's incoming exit
status. Each shell maintains its own entry state.

Zsh and Fish use directory-change events. Bash checks PWD just before the next
prompt, so multiple moves within a single command are observed only at the final
location. A transition into a different canonical Noon project queues the named
job. Subdirectories and symlink aliases of the same project do not count as
another entry. Leaving runs nothing; leaving and re-entering permits another
request. Nearest Noon markers and Git boundaries apply, including nested projects
and worktree `.git` files. Worktrees must be enabled independently.

The entered directory determines the project, **not inherited `NOON_PROJECT`**.
Entry uses the automatic or explicit configuration selection recorded by
`noon enable`, ignoring invocation `NOON_CONFIG` and `NOON_TIMEZONE`. State is
located through user settings and `NOON_STATE_DIR`/`--state-dir`, like the scheduler;
if an explicit config selected a private store when enabling, select that same
store in the shell environment. An enabled checkout missing the named job is a
quiet no-op. Unenabled projects are also quiet; disabled, paused, changed shared
jobs and recursion failures cannot queue entry work.

Queueing does not wait for execution. An offline scheduler is fine: accepted work
waits for an explicitly started `noon serve` or service. Nothing automatically
enables, approves, or starts anything. Due-now requests coalesce with already due
queued work. A `cooldown` reduces repeated requests across visits or separate
shells; it is part of the approved job definition. There is no permanent
project-entry idempotency key. The normal recursion limits still apply.

The job's `NOON_EVENT_FILE` contains the normal CLI envelope:

```json
{"schema_version":1,"source":"cli","received_at":"…","data":{"event":"project_entry","shell":"bash","previous_directory":"/work","directory":"/work/repo/sub","project":"/work/repo"}}
```

Directory strings remain data, including spaces, quotes, newlines, and shell
metacharacters. Queued jobs run in separate processes: they **cannot change the
parent shell environment**. Use mise or direnv for environment activation.

## Diagnostics and removal

Successful queueing and ordinary no-op transitions print nothing. Failures go to
stderr without changing `cd` success or interrupting the prompt. Failed entries
are not retried on every prompt: fix the problem and leave/re-enter the project.
Use `noon status`, `noon list`, `noon history check-in`, and `noon logs check-in` to
inspect approval, paused state, queue/history, and output. Start the scheduler
explicitly when ready. Re-approve changed shared definitions only after review.

To disable entry automation in the current shell, source the completion-only
command again. It removes only Noon's entry hook, keeping other hooks intact.
To remove everything, remove the setup line from your startup file and open a
fresh shell. To also remove completion immediately after disabling entry:

```bash
complete -r noon             # Bash
```

```zsh
compdef -d noon               # Zsh
```

```fish
complete -c noon -e           # Fish
```

Removing shell integration does not cancel already queued work or revoke project
approval. Use `noon cancel check-in` or `noon disable` explicitly if desired.
