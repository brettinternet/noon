---
id: BUMP-3
title: Persist run history and enforce project ownership
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-2
documentation:
  - docs/design.md
modified_files:
  - internal/store/**
  - internal/runner/**
  - internal/cli/**
  - internal/config/**
  - integration/**
  - docs/design.md
  - docs/development.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: jobs leave durable, bounded evidence and concurrent Bump owners cannot corrupt the same project/state.
Scope: embedded SQLite run/event store and migrations, project identity, owner lock, per-run log files/retention, read-only offline history/logs, interrupted-run recovery, atomic run creation and transitions. Evaluate a Go SQLite driver against static Linux/macOS/container builds. Persist enough identity to avoid unsafe PID-reuse cancellation; interrupted side effects are never automatically retried.
Non-goals: distributed locks, cloud storage, exactly-once external effects, recurring schedules, schema-general ORM.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestRunStore' -count=1 -v exits 0 with committed transitions surviving reopen, failed transactions not partially applied, log bounds enforced, and migration failure preserving the original database.
- [ ] #2 task cli:test -- -run '^TestProjectOwnership' -count=1 -v exits 0: a second owner and a different project sharing state are refused; a crashed owner is recoverable without killing unrelated processes.
- [ ] #3 task cli:test -- -run '^TestHistoryRecovery' -count=1 -v exits 0 with interrupted runs reported honestly and no automatic re-execution.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
