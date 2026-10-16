# Git hooks

**Level:** intermediate · **Shows:** `--detach`, idempotency keys, `--data` event payloads, `cooldown`, `NOON_EVENT_FILE`

Git hooks should be fast and must not fail your commit. These hooks only queue a job and return;
`noon serve` runs it in the background and records the result.

```sh
cp examples/local-dev/git-hooks/noon.yaml .
mkdir -p scripts && cp examples/local-dev/git-hooks/scripts/deps.sh scripts/
cp examples/local-dev/git-hooks/hooks/post-* .git/hooks/
chmod +x .git/hooks/post-* scripts/deps.sh
noon enable                 # queued runs require an enabled checkout
noon serve                  # or noon service install
git commit --allow-empty -m "try noon"
noon history test           # trigger "queued", one run per commit
```

| Hook | Queues | Key |
| --- | --- | --- |
| [`post-commit`](hooks/post-commit) | `test` | `commit-<sha>` |
| [`post-merge`](hooks/post-merge) | `test`, and `deps` if a lockfile changed | `merge-<sha>`, `deps-<sha>` |
| [`post-checkout`](hooks/post-checkout) | `ctags`, and `deps` if a lockfile changed | `deps-<sha>` |

The `deps` job reads the payload Noon stored for it:

```sh
noon logs deps
# triggered by post-checkout: 1a2b3c4d..5e6f7a8b
```

Payload values are data in a JSON file, never interpolated into the job's command. Noon never
installs hooks for you. For a hook that must block (pre-push), run `noon run test` in the
foreground instead: it returns the test's exit code.

Worktrees are separate projects; run `noon enable` in each one you want hooks to queue for.
