# Re-run jobs after file changes

**Level:** intermediate · **Shows:** composing a file watcher, foreground vs queued runs, debouncing, never losing the last edit

Noon has no file watcher. Commands are its integration point, so a watcher you start yourself
decides *when*, and Noon runs the job with its timeout and keeps the history. This uses
[watchexec](https://github.com/watchexec/watchexec) (`brew install watchexec` or
`mise use -g watchexec`).

```sh
cp -R examples/local-dev/file-watch/. ~/src/my-go-app/
cd ~/src/my-go-app
scripts/watch.sh test             # foreground: output here, Ctrl-C to stop
scripts/watch.sh vet --detach     # background: needs noon enable and a running scheduler
noon history test
```

| Mode | Runs in | Needs | A burst of saves | A save during a run |
| --- | --- | --- | --- | --- |
| `watch.sh test` | your terminal (`noon run test`) | nothing | one run (1s debounce) | exactly one more run when it ends |
| `watch.sh vet --detach` | `noon serve` (`noon run vet --detach`) | `noon enable`, a scheduler | one queued run | one more queued run |

Foreground mode relies on watchexec's `--on-busy-update queue`. Detached mode relies on Noon: a
request made while a run of the same job is already waiting joins that run. Neither mode skips
the last edit or starts a run per filesystem event.

Things to know:

- **Watching is not Noon's job.** Noon never starts a watcher, listener, or background process
  on its own. Run the watcher in a terminal, or supervise it with a process manager such as hum.
- **Avoid trigger loops.** watchexec ignores `.git` and gitignored files by default, and
  `--exts go,mod,sum` limits events to Go sources. If a job writes files the watcher can see
  (coverage, generated code), gitignore them or add `--ignore`.
- **Don't pass changed paths as data.** A detached request that joins a waiting run loses its
  payload, so a job that needs the changed files should ask Git (`git status --porcelain`)
  rather than trust one event.
- A foreground `noon run test` fails with `already_running` while the scheduler is running the
  same job; watchexec reports it and keeps watching.
- watchexec runs commands through `$SHELL` by default; `-n` runs `noon` directly, so fish or
  zsh configuration doesn't affect it.

## Verification (2026-10-17)

watchexec **2.8.0** with the built `noon`, in an isolated Go module whose test sleeps 3 seconds.
Foreground: five saves 0.1s apart, then one save during the run, recorded exactly two manual
runs. Detached with the scheduler offline: five saves, then one more, left one queued run
(`vet is already queued as run 3`). Detached with `noon serve` running: a burst, then a save
during the run, recorded exactly two queued runs.
