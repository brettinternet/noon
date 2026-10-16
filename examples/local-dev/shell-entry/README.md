# Check in on project entry

**Level:** intermediate · **Shows:** `noon shell init --on-enter`, `cooldown`, job-level `on_failure`, `NOON_LOG_FILE`

When you `cd` into the project, Noon queues `check-in` in the background. Your prompt is never
delayed. If the branch is behind upstream or dependencies are stale, the job exits 1 and its
`on_failure` hook raises a desktop notification.

```sh
cp -R examples/local-dev/shell-entry/. ~/src/my-app/
cd ~/src/my-app
noon enable
noon service install                                   # something must dispatch queued work

# ~/.zshrc (or bash / fish equivalents)
eval "$(noon shell init zsh --on-enter check-in)"
```

```sh
cd ~/src/my-app        # queues check-in (once per entry, at most every 15m)
noon logs check-in     # branch main: 3 behind, 0 ahead / -> pull before starting
```

The entry hook only queues approved jobs in enabled checkouts, and subdirectories of the same
project do not count as re-entry. Jobs run in a separate process, so they **cannot** change your
shell's environment; use mise or direnv for that.

The job name is yours. Any project that defines `check-in` gets the behavior; projects without it
are a quiet no-op, so one shell setup line works everywhere.
