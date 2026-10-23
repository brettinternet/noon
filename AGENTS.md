# Agents

Read README.md and docs/development.md before changing Noon. Read AGENTS.local.md if present.

## Tooling
- Use `task init` for pinned mise tools, modules, and hooks.
- Use project Task targets and the smallest relevant check. Run `task ci` before pushing.
- Before committing, stage intended files and run `task check:staged`; it formats/re-stages Go and scans staged secrets.
- Use Conventional Commits. Never push or open a PR without explicit instruction.
- Use Go, urfave/cli v3, and existing config/CLI patterns. No Python runtime or build dependency.
- Keep command defaults safe and useful. No-argument inspection must not start jobs, services, or network listeners.
- Distinguish implemented behavior from proposed behavior in docs/help. Do not add stub commands that pretend to succeed.

## Backlog
- `backlog/` is the only task queue. Read and change tasks only through `mise exec -- bl` (CLI or MCP), for example `bl show NOON-12 --json`; never edit task Markdown directly.
- Follow docs/development.md#worktrees-and-backlog for the per-task loop. Pick and claim work in one step, before changing any status, with one stable session for the whole loop: `mise exec -- bl next --claim --start --session SESSION --json`. A status change is not a claim. Drafts are never work.
- Renew with `mise exec -- bl heartbeat --session SESSION` at least once per half the claim TTL. Finish with `mise exec -- bl done ID --session SESSION`, which writes then releases before returning. If stopping incomplete, persist progress and use `mise exec -- bl release ID --session SESSION`.
- Read `mise exec -- bl instructions` and `mise exec -- bl instructions workflow` before acting. Use `bl note ID TEXT` and `bl ac ID check N --evidence TEXT` to record each criterion's command and observed result; read `bl show ID --json` first and pass `--if-version VERSION` when acting on criterion indices.
- Decisions in `docs/decisions/` are settled. Record a conflict in task notes instead of diverging.
- A ready task needs outcome, scope, non-goals, modified-file contract, dependencies, and locally executable acceptance commands with expected results.
- Follow docs/development.md#tests. Implement first, then add only critical regression tests. Never weaken tests to get green.

## Git and isolation
- Work in the current checkout unless directed to create a branch/worktree.
- When directed, create under `.worktrees/` with `mise exec worktrunk -- wt switch --create BRANCH --base main --no-cd --format=json`.
- Review .config/wt.toml hooks before creation. Use Worktrunk removal only for verified owned worktrees.
- Preserve unrelated changes. No speculative agents, plugins, or services running in the background.
