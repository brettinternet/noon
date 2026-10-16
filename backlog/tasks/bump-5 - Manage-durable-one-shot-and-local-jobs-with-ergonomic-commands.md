---
id: BUMP-5
title: Manage durable one-shot and local jobs with ergonomic commands
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-4
documentation:
  - docs/design.md
modified_files:
  - internal/app/**
  - internal/store/**
  - internal/cli/**
  - internal/scheduler/**
  - integration/**
  - docs/configuration.md
  - README.md
priority: high
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users and scripts can schedule a named job for later, inspect what will happen, and safely control it.
Scope: durable one-shot delayed/absolute schedules with stable IDs and idempotency keys; add/list/pause/resume/remove/cancel/history/logs/status; clear declared-versus-local ownership and collisions; helpful bare command groups, no prompts in non-TTY mode, structured JSON success/errors, no mutation from missing targets. Local ad-hoc entries never rewrite tracked YAML. Status reports scheduler offline rather than claiming future execution is guaranteed.
Non-goals: arbitrary natural-language scheduling, implicit all-target operations, user-global job scope, TUI, MCP, automatic service start.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestDurableOneShot' -count=1 -v exits 0: accepted jobs survive client/server restart, idempotent submission returns the same ID, missed one-shot behavior is explicit, and cancellation prevents future dispatch.
- [ ] #2 task cli:test -- -run '^TestCommandErgonomics' -count=1 -v exits 0: every bare command has a useful safe result, absent targets never mutate all, JSON errors are single parseable objects, non-TTY paths never prompt.
- [ ] #3 task cli:test -- -run '^TestJobOwnership' -count=1 -v exits 0 with source collisions refused, tracked YAML byte-identical after local mutations, and scheduler-offline state reported accurately.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
