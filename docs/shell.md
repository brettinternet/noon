# Shell setup

`noon shell init SHELL` prints code for you to source. It never edits startup files, creates state,
enables a project, or starts a scheduler. Without `SHELL`, it lists the options rather than
guessing.

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

```console
$ noon run <TAB>          # job names (also pause, resume, remove, history, logs, cancel)
$ noon add -a <TAB>       # configured agent profiles
$ noon logs -s <TAB>      # statuses, including after a comma
$ noon logs --section=<TAB>
$ noon -C <TAB>           # native directory completion
```

Completion respects `-C` and `--config`, offers nothing after `--`, and never evaluates what you
typed. It only reads config: no jobs, state, network, or services. Broken config hides job names but
keeps everything else.

Bash 3.2+ works without bash-completion. Zsh writes no compdump. No Python, Node, or jq.

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

- Bash checks before each prompt, so only a command's final directory counts.
- The entered directory picks the project, with the config recorded by `noon enable`.
  `NOON_PROJECT`, `NOON_CONFIG`, and `NOON_TIMEZONE` are ignored; a custom `NOON_STATE_DIR` must
  be set in your shell.
- Projects that aren't enabled or lack the job are skipped silently, as are paused jobs and jobs
  needing approval. Each worktree is enabled separately.
- Queueing doesn't wait; `cooldown` limits repeats across visits and shells.
- Jobs run in a separate process and can't change your shell's environment. Use mise or direnv.

The job's `$NOON_EVENT_FILE` describes the move:

```json
{"schema_version":1,"source":"cli","received_at":"…","data":{"event":"project_entry","shell":"bash","previous_directory":"/work","directory":"/work/repo/sub","project":"/work/repo"}}
```

## Diagnose and remove

Success prints nothing. Failures print to stderr without failing `cd` and aren't retried until you
re-enter. Inspect with:

```sh
noon status
noon history check-in
noon logs check-in
```

Source the completion-only line again to drop the entry hook. To remove completion too:

```bash
complete -r noon      # Bash
```

```zsh
compdef -d noon       # Zsh
```

```fish
complete -c noon -e   # Fish
```

Delete the startup line to remove it permanently. Neither cancels queued work nor revokes approval;
use `noon cancel check-in` or `noon disable`.

`task shell:smoke` tests all three shells; each must be installed on `PATH`.
