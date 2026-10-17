# Editing agents in a container

**Level:** advanced · **Shows:** a container as the agent's security boundary, Worktrunk-managed worktrees, untrusted output committed without hooks

[background-agents](../background-agents/) runs the agent on your machine in a plain worktree,
which keeps it off your checkout but is not a security boundary. This example also takes away
the agent's view of your machine: it runs in a throwaway container that can see one fresh
worktree and nothing else.

| | background-agents | container-agent |
| --- | --- | --- |
| Agent process | host, your account | container, your uid, no capabilities |
| Agent can see | everything you can | the worktree's files |
| Credentials | your whole environment | only the variables named in `AGENT_ENV` |
| `CHECK_CMD` | host | a second container, no credentials |
| Worktree | `git worktree` | `wt switch --create`, so the repo's pre-start hooks prepare it |
| Needs | the agent CLI | Docker, [Worktrunk](https://github.com/max-sixty/worktrunk), `jq` |

```sh
cp -R examples/ai/container-agent/. ~/src/my-app/
cd ~/src/my-app
$EDITOR Dockerfile                   # add what `make check` needs
docker build -t noon-codex-agent .
wt config approvals add              # once, if the repo has Worktrunk hooks
CODEX_API_KEY=... noon run todo-sweep
git log --oneline --branches='agent/todo-sweep-*'
```

The weekly schedule needs `noon enable` and `CODEX_API_KEY` in the scheduler's environment
(your shell for `noon serve`, the service environment for `noon service install`), never in YAML.

Each run:

1. creates `agent/todo-sweep-<time>` from `BASE_REF` (default `main`) with `wt switch --create`,
2. runs `AGENT_CMD` in the container with the prompt on stdin,
3. removes the worktree and branch if nothing changed,
4. runs `CHECK_CMD` in a fresh container,
5. commits on success and removes the worktree, keeping the branch.

If the agent exits nonzero, the check fails, or the job times out, the worktree stays for
inspection. Discard it with `wt remove --no-hooks --force -D agent/todo-sweep-<time>`.

Things to know:

- **Never mount `.git`.** The container gets the working tree, with its `.git` pointer file
  mounted read-only. Git config and hooks run commands on the host (`core.fsmonitor`,
  `post-commit`), so an agent that could write them could escape the container.
- **The host never runs the agent's files.** The commit sets `core.hooksPath=/dev/null`, and
  removal uses `--no-hooks`, because pre-remove hooks run from the worktree the agent edited.
  Pre-start hooks run before the agent starts. If they create resources, clean those up from
  your primary checkout.
- **The container is the sandbox, so Codex's isn't used.** Codex documents
  `--sandbox danger-full-access` for "an isolated CI runner or container", and
  `--skip-git-repo-check` is needed because Git metadata isn't mounted. Never use this
  `AGENT_CMD` outside the container.
- **The key is exposed to repository code.** Everything the agent runs, including `make check`,
  can read `CODEX_API_KEY`. Use a dedicated, revocable key with a spending limit.
- **The network is open.** The agent needs its API, so it can reach anything your machine can.
- **Docker must be able to see the repository.** Docker Desktop and Colima share your home
  directory by default. Elsewhere, the run fails with `bind source path does not exist`.
- **Unattended runs need approved Worktrunk hooks.** Unapproved hooks fail immediately with
  `Cannot prompt for approval in non-interactive environment`; run `wt config approvals add`
  once, and again whenever the hooks change.
- On timeout, Noon's TERM reaches the agent through Docker's signal forwarding and `--init`,
  and `--rm` removes the container.
- The branch is untrusted until reviewed. Reading `git diff main...agent/...` is safe; running its
  code on your host is not sandboxed.

## Verification (2026-10-17)

**Stub agents, no authenticated Codex run.** The image built from this `Dockerfile` with Codex
**0.163.0**, and `codex exec --help` inside it, run as uid 501 with all capabilities dropped,
listed `danger-full-access` and `--skip-git-repo-check`. With Worktrunk **0.80.0**, Colima, and
the built `noon`, stub agents in that image showed:

- a no-op agent left no worktree or branch;
- an editing agent couldn't see `/Users`, couldn't write `.git` (read-only file system), and got
  `CODEX_API_KEY`, while the check didn't. The change was committed on the branch, and neither
  the repository's pre-commit hook nor its pre-remove hook ran. The worktree was removed and the
  branch kept;
- a failing check exited 1 and kept the worktree;
- an agent that never exits timed out (exit 124) with no container left running;
- a worktree outside Docker's shared paths failed with exit 125 instead of mounting an empty
  directory.
