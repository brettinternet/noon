---
id: BUMP-2
title: Run named jobs with bounded process lifecycle
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-1
documentation:
  - docs/design.md
modified_files:
  - internal/job/**
  - internal/runner/**
  - internal/cli/**
  - integration/**
  - docs/design.md
  - docs/development.md
  - README.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: bump run NAME executes real repo work without requiring a resident service and reports its actual result.
Scope: explicit argv versus shell execution, source-relative cwd, inherited environment plus explicit overrides/references, bounded output, cancellation and timeout of owned process groups, graceful/forced termination, actual exit outcome. Bare bump run lists candidates rather than running the only job. Build process-group support for macOS/Linux only.
Non-goals: recurrence, background daemon, persistent history, terminal multiplexer adapters, automatic retries or worktree creation.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestRunJob' -count=1 -v exits 0 with observed argv/cwd/environment, bounded output, and correct child failure outcome; malformed arguments do not start a process.
- [ ] #2 task cli:test -- -run '^TestRunCancellation' -count=1 -v exits 0 after real child/grandchild timeout and interrupt cases, with no surviving owned processes.
- [ ] #3 task cli:build && bin/bump run in an empty isolated repo exits 0 with guidance and no process started.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
