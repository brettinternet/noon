# Worktree setup and teardown with Worktrunk

**Level:** intermediate · **Shows:** Worktrunk `pre-start`/`pre-remove` hooks calling foreground `noon run`, per-checkout resources, teardown that blocks removal

[Worktrunk](https://github.com/max-sixty/worktrunk) owns the worktree lifecycle; Noon runs the jobs inside it.
Each new worktree gets its own Postgres container, and removing the worktree deletes exactly that
container and its volume.

```sh
cp -R examples/integrations/worktrunk/. ~/src/my-app/
cd ~/src/my-app
mkdir -p .config && mv wt.toml .config/wt.toml
printf '.env.local\n' >> .gitignore
git add -A && git commit -m "Prepare worktrees with noon"
wt switch --create feature-x       # pre-start: noon run setup, then your shell or agent starts
wt remove feature-x                # pre-remove: noon run teardown, then the directory goes
```

| Hook | Job | Does | If it fails |
| --- | --- | --- | --- |
| `pre-start` | `setup` | starts this worktree's database, writes `.env.local` | `wt switch` exits nonzero before `--execute` or post-start hooks; the worktree stays |
| `pre-remove` | `teardown` | `docker compose down --volumes` for this worktree only | `wt remove` stops; nothing is deleted until teardown succeeds or you pass `--no-hooks` |

Why run these through Noon instead of directly in `wt.toml`:

- **History per checkout.** `noon history setup` and `noon logs setup` in the worktree show what
  happened, including runs that failed in a hook.
- **Bounded time.** A hung install hits the job's `timeout`; Noon stops its whole process group
  instead of leaving `wt switch` waiting forever.
- **One command everywhere.** The same `noon run setup` repairs a worktree by hand when a hook
  was skipped with `--no-hooks`.

If you don't need those, put the commands in `wt.toml` directly.

Things to know:

- **Foreground on purpose.** `noon run NAME` needs no `noon enable` or approval, so it works in a
  worktree that was created a second ago. `noon run --detach` would need each new worktree
  enabled, which Noon never does for you.
- **Teardown only touches what the checkout owns.** `scripts/worktree.sh` derives the Compose
  project name from the checkout's absolute path, so worktrees never share or remove each
  other's containers, volumes, or networks. Docker picks a free loopback port for each one.
- **Use `pre-remove`, not `post-remove`.** `pre-remove` runs inside the worktree while its
  scripts still exist. `post-remove` runs in the primary worktree after the directory is gone.
  Leaving a directory in your shell is never a removal event; nothing here runs on `cd`.
- Noon doesn't supervise the database. Docker does; teardown just removes it.
- Worktrunk asks you to approve project hooks on first use and again when they change.

## Verification (2026-10-17)

Worktrunk **0.80.0**, Docker Compose **v5.6.0**, and the built `noon` in a disposable repository
with an empty Worktrunk user config. Creating two worktrees started two healthy databases with
different Compose projects and ports, and recorded `setup` in each checkout's history. Removing
one deleted only its container, network, and volume. With Docker unreachable, teardown failed,
Noon recorded the failure, and `wt remove` exited 1 with the worktree intact; once Docker was
reachable, removal succeeded and left no containers or volumes. A failing `pre-start` command
left the new worktree in place and made `wt switch --create` exit nonzero.
