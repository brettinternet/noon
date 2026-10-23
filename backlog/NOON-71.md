---
id: NOON-71
title: Attach tmux or Herdr sessions to long agent runs
status: draft
type: feature
priority: low
labels: [runner, integration]
rank: a7a0be82fa0be82d2
refs: [docs/design.md]
files: [internal/runner/**, internal/config/**]
created: 2026-10-16T00:18:00Z
updated: 2026-10-18T16:38:00Z
---

Draft, not ready work. The core scheduler has shipped; promote only when a concrete user need cannot be met by the existing blocking command wrappers. Promotion rewrites this record to the ready-task standard in AGENTS.md (outcome, scope, non-goals, modified-file contract, dependencies, acceptance commands).

Idea: let a long agent run happen inside an attachable tmux (or Herdr) session so a person can watch or take over, while Noon still owns the run's lifecycle: overlap, cancel, exit code, and history.

Why deferred: jobs never get a TTY today, by design, so manual and scheduled runs behave the same. An attachable session adds a second execution path with its own failure modes: a detached launcher that exits early, sessions outliving Noon, and stale session IDs. `noon logs --follow` covers watching for now.

Open questions before promotion:
- Is it a job field (`session: tmux`) or an agent-profile option?
- How does Noon learn the real exit code: a wrapper that writes it to the state dir, or `tmux wait-for`?
- How do cancel and recovery find and verify the session after a scheduler restart, with no unrelated-session kills?
- Herdr: can its pane API provide the same contract without a second adapter?

Constraints if promoted: opt-in per job, an isolated tmux socket in the state dir, launcher success recorded as `running` and never as finished, and no dependency for jobs that don't use it.

## Notes

Evidence (scheduled Herdr agent): a `*/15` babysit job runs `agent NAME -t --wait ...` (dotfiles 910cabe). The launcher blocks until the agent process exits and, on SIGTERM, closes the identity-checked pane. Overlap skip, real duration, cancel, and timeout therefore work today without Noon changes. Such jobs need `timeout` raised or `none`: the default 1h timeout now closes the agent. Two gaps remain for a supervised-run design:

1. Scheduler restart stops the agent. `noon serve` shutdown SIGTERMs every active group and records `interrupted`; the job cannot tell that from a cancel, so an upgrade or service restart closes a working agent. Surviving it needs a run whose process may outlive its owner and be re-adopted after restart: a stored session handle plus an identity/liveness check, never killing an unrelated session. This is the recovery open question.
2. Exit code is unavailable. The agent is not the launcher child and Herdr reports `exit_code: null`, so the launcher exits 0 on any agent exit and Noon records `succeeded` for a crash. Fixing it needs the pane command to record its own status (a wrapper writing it to the state dir, or `tmux wait-for`). This is the exit-code open question.

Historical observation: each `*/15` tick during a multi-hour run recorded a `skipped` run that consumed the regular retention budget. NOON-55 is now Done (v0.4.1): skips retain their newest ten rows independently and no longer evict real run history. This is resolved, not a remaining blocker for this draft.
