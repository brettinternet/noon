---
id: DRAFT-3
title: Define user-scope jobs outside repositories
status: Draft
assignee: []
created_date: '2026-10-16 01:43'
labels:
  - config
  - scheduler
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote when users want recurring work that belongs to no repository (for example, a morning digest across repos). Promotion rewrites this record to the ready-task standard in AGENTS.md (outcome, scope, non-goals, modified-file contract, dependencies, acceptance commands).

Idea: allow `jobs:` in user config, run from a user-scope pseudo-project (cwd defaults to home, or required explicitly), and listed by `noon list --user` or a section of the dashboard.

Why deferred: project-scoped jobs cover the stated use cases, and user-scope jobs raise questions the project model avoids. Where do they show up when you're inside a repo? Do they need enabling (they are the user's own, so probably not)? How do they interact with per-project concurrency caps?

Open questions: pseudo-project identity in the store, `cwd` default, whether `noon add --user` writes user config, and how MCP clients see them.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
