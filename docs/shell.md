# Shell setup

`noon shell init SHELL` prints code for you to source. It never edits startup files, creates
state, enables a project, or starts a scheduler.

## Completion

Add one line to your shell's startup file:

```bash
eval "$(noon shell init bash)"      # ~/.bashrc
```

```zsh
eval "$(noon shell init zsh)"       # ~/.zshrc
```

```fish
noon shell init fish | source       # ~/.config/fish/config.fish
```

Completion covers commands, aliases, flags, and current job names for `run`, `pause`, `resume`,
`remove`, `history`, `logs`, and `cancel`. It respects `-C` and `--config`. It only reads config:
no jobs, state, network, or services. Broken config hides job names but not commands.
Short flags are included: for example, `noon logs -<TAB>` offers `-f`, `-s`, `-n`, `-c`,
`-m`, `-b`, and global aliases. The [short-flag mapping](cli.md#short-flags) is shared with
command help. Completion does not suggest job names while a flag needs a value or after `--`.

Bash 3.2+ works without the bash-completion package. Zsh uses its bundled completion system and
writes no compdump. No Python, Node, or jq is needed.

## Run a job when you enter a project

Pick any job; there's no special name.

```yaml
jobs:
  check-in:
    run: [task, check]
    cooldown: 5m
```

Review the checkout, run `noon enable`, then source with `--on-enter` (this includes completion):

```bash
eval "$(noon shell init bash --on-enter check-in)"
```

```zsh
eval "$(noon shell init zsh --on-enter check-in)"
```

```fish
noon shell init fish --on-enter check-in | source
```

Moving into a different Noon project queues the job. Moving within a project, including
subdirectories and symlinked paths, doesn't. Leaving runs nothing; coming back queues again.
Loading the setup doesn't queue anything.

```console
~ $ cd ~/code/app          # queues check-in
~/code/app $ cd internal   # same project: nothing
~/code/app/internal $ cd ~ # leaving: nothing
~ $ cd ~/code/app          # queues check-in again (cooldown permitting)
```

Details:

- Zsh and Fish react to directory changes. Bash checks before each prompt, so only the final
  directory of a command counts.
- The entered directory picks the project, not `NOON_PROJECT`. Noon uses the config selection
  recorded by `noon enable` and ignores `NOON_CONFIG` and `NOON_TIMEZONE`. If you enabled with a
  custom state directory, set the same `NOON_STATE_DIR` in your shell.
- Projects that aren't enabled or lack the job are skipped silently. Paused jobs, jobs needing
  approval, and recursion failures don't queue.
- Queueing doesn't wait. If no scheduler is running, the run waits for one. `cooldown` limits
  repeats across visits and shells.
- Each worktree must be enabled separately.
- Queued jobs run in a separate process and can't change your shell's environment. Use mise or
  direnv for that.

The job's `$NOON_EVENT_FILE` describes the move:

```json
{"schema_version":1,"source":"cli","received_at":"…","data":{"event":"project_entry","shell":"bash","previous_directory":"/work","directory":"/work/repo/sub","project":"/work/repo"}}
```

## Diagnose and remove

Success prints nothing. Failures print to stderr without failing `cd` or blocking the prompt, and
aren't retried until you leave and re-enter. Inspect with:

```sh
noon status
noon history check-in
noon logs check-in
```

To turn off entry jobs in the current shell, source the completion-only line again. It removes
only Noon's hook. To remove completion too:

```bash
complete -r noon      # Bash
```

```zsh
compdef -d noon       # Zsh
```

```fish
complete -c noon -e   # Fish
```

Delete the line from your startup file to remove it permanently. This doesn't cancel queued work
or revoke approval; use `noon cancel check-in` or `noon disable`.

`task shell:smoke` tests all three shells; each must be installed on `PATH`.
