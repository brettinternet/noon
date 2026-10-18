---
id: DRAFT-1
title: Attach tmux or Herdr sessions to long agent runs
status: Draft
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-18 06:25'
labels:
  - runner
  - integration
dependencies: []
documentation:
  - docs/design.md
modified_files:
  - internal/runner/**
  - internal/config/**
priority: low
type: feature
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote only after the core scheduler ships and a user asks for it. Promotion rewrites this record to the ready-task standard in AGENTS.md (outcome, scope, non-goals, modified-file contract, dependencies, acceptance commands).

Idea: let a long agent run happen inside an attachable tmux (or Herdr) session so a person can watch or take over, while Noon still owns the run's lifecycle: overlap, cancel, exit code, and history.

Why deferred: jobs never get a TTY today, by design, so manual and scheduled runs behave the same. An attachable session adds a second execution path with its own failure modes: a detached launcher that exits early, sessions outliving Noon, and stale session IDs. `noon logs --follow` covers watching for now.

Open questions before promotion:
- Is it a job field (`session: tmux`) or an agent-profile option?
- How does Noon learn the real exit code: a wrapper that writes it to the state dir, or `tmux wait-for`?
- How do cancel and recovery find and verify the session after a scheduler restart, with no unrelated-session kills?
- Herdr: can its pane API provide the same contract without a second adapter?

Constraints if promoted: opt-in per job, an isolated tmux socket in the state dir, launcher success recorded as `running` and never as finished, and no dependency for jobs that don't use it.
<!-- SECTION:DESCRIPTION:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Evidence (scheduled Herdr agent): a `*/15` babysit job runs `agent NAME -t --wait ...` (dotfiles 910cabe). The launcher blocks until the agent process exits and, on SIGTERM, closes the identity-checked pane. Overlap skip, real duration, cancel, and timeout therefore work today without Noon changes. Two gaps remain for a supervised-run design:

1. Scheduler restart stops the agent. `noon serve` shutdown SIGTERMs every active group and records `interrupted`; the job cannot tell that from a cancel, so an upgrade or service restart closes a working agent. Surviving it needs a run whose process may outlive its owner and be re-adopted after restart: a stored session handle plus an identity/liveness check, never killing an unrelated session. This is the recovery open question.
2. Exit code is unavailable. The agent is not the launcher child and Herdr reports `exit_code: null`, so the launcher exits 0 on any agent exit and Noon records `succeeded` for a crash. Fixing it needs the pane command to record its own status (a wrapper writing it to the state dir, or `tmux wait-for`). This is the exit-code open question.

Also check before promotion: each `*/15` tick during a multi-hour run records a `skipped` run; confirm whether skipped runs count against per-job history retention.
<!-- SECTION:NOTES:END -->
