---
id: DRAFT-4
title: Run agent jobs in isolated worktrees or containers
status: Draft
assignee: []
created_date: '2026-10-16 01:43'
updated_date: '2026-10-17 17:37'
labels:
  - runner
  - security
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote when agent jobs that edit files collide with the user's own work in the same checkout. Promotion rewrites this record to the ready-task standard in AGENTS.md (outcome, scope, non-goals, modified-file contract, dependencies, acceptance commands).

Idea: an opt-in job setting that runs a job in a fresh Git worktree (created from a named base, removed afterwards unless it produced commits) or inside a container image, so unattended agents never touch the user's working tree.

Why deferred: Noon deliberately never creates worktrees or changes branches today, and the scheduler already starts at most one run per project at a time. Foreground `noon run` can still overlap a background job in the same checkout, which this draft would address. Worktree lifecycle (cleanup, retention, branch naming) and container execution (image pulls, credentials, mounts) are each a product of their own; existing tools such as Worktrunk may be better composed as a job command than built in.

Open questions: compose with `wt switch --create` inside a job's command vs. a native field; where results go (branch, PR, patch file); cleanup and retention; container runtime choice and credentials.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Resolved as an example, not a feature: examples/ai/container-agent composes wt switch --create with a Docker sandbox (worktree-only mount, read-only .git pointer, scoped credentials, hook-free commit). A native worktree or container field would contradict the design non-goals (Noon never creates worktrees; no container per job).
<!-- SECTION:NOTES:END -->
