---
id: BUMP-4
title: Serve durable cron and interval schedules per project
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-3
documentation:
  - docs/design.md
modified_files:
  - internal/scheduler/**
  - internal/store/**
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - integration/**
  - docs/design.md
  - docs/configuration.md
  - README.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: an explicitly started bump serve owns recurring work until interrupted and recovers state safely after restart.
Scope: foreground scheduler, local IPC ownership, shared manual-run submission, five-field cron and fixed intervals, local/IANA timezone and documented DST rules, durable occurrence claims, skip/coalesce missed runs, skip/bounded-queue overlap behavior, per-project concurrency, validate-first atomic config reload. Preserve running job snapshots. Choose/test explicit DST policy before shipping; do not inherit a library's undocumented behavior.
Non-goals: auto-start on discovery/MCP, OS service installation, distributed scheduling, catch-up bursts, unbounded retries or exactly-once effects.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestSchedulerClock' -count=1 -v exits 0 with deterministic fake-clock cases for cron/intervals, DST gaps/folds, missed-run skip/coalesce, and no burst after downtime.
- [ ] #2 task cli:test -- -run '^TestSchedulerOwnership' -count=1 -v exits 0 with one owner, manual submissions routed to that owner, bounded concurrency/overlap, and no duplicate dispatch on restart.
- [ ] #3 task cli:test -- -run '^TestSchedulerReload' -count=1 -v exits 0: invalid edits preserve the last valid schedule; valid edits apply atomically and do not alter active runs.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
