# Agents

Read README.md and docs/development.md before changing Bump. Read AGENTS.local.md if present.

## Tooling
- Use `task init` for pinned mise tools, modules, and hooks.
- Use project Task targets and the smallest relevant check. Run `task ci` before pushing.
- Before committing, stage intended files and run `task check:staged`; it formats/re-stages Go and scans staged secrets.
- Use Conventional Commits. Never push or open a PR without explicit instruction.
- Use Go, urfave/cli v3, and existing config/CLI patterns. No Python runtime or build dependency.
- Keep command defaults safe and useful. No-argument inspection must not start jobs, services, or network listeners.
- Distinguish implemented behavior from proposed behavior in docs/help. Do not add stub commands that pretend to succeed.

## Backlog
- `backlog/` is the only task queue. Read and mutate task files through the Backlog.md CLI, never direct Markdown edits.
- Follow docs/development.md#worktrees-and-backlog for the per-task loop. `task backlog:next` picks work; drafts are never work.
- Decisions in `backlog/decisions/` are settled. Record a conflict in task notes instead of diverging.
- A ready task needs outcome, scope, non-goals, modified-file contract, dependencies, and locally executable acceptance commands with expected results.
- Claim tasks in the primary checkout with `backlog task edit ID --status "In Progress"`; reread after every write.
- Follow docs/development.md#tests. Implement first, then add only critical regression tests. Never weaken tests to get green.

## Git and isolation
- Work in the current checkout unless directed to create a branch/worktree.
- When directed, create under `.worktrees/` with `mise exec worktrunk -- wt switch --create BRANCH --base main --no-cd --format=json`.
- Review .config/wt.toml hooks before creation. Use Worktrunk removal only for verified owned worktrees.
- Preserve unrelated changes. No speculative agents, plugins, or services running in the background.
