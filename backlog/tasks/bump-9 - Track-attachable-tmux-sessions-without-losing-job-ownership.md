---
id: BUMP-9
title: Track attachable tmux sessions without losing job ownership
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 00:19'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-4
documentation:
  - docs/design.md
modified_files:
  - internal/runner/**
  - internal/session/**
  - internal/config/**
  - internal/app/**
  - internal/cli/**
  - integration/**
  - docs/integrations.md
  - examples/**
  - Taskfile.dist.yaml
  - .taskfiles/**
  - scripts/**
priority: medium
type: feature
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: an agent can run in an attachable tmux session while Bump still knows whether the real job is active, completed, or cancelled.
Scope: one opt-in tmux adapter using an explicit isolated socket/session namespace, durable session locator, readiness/completion/exit/cancel contract, overlap held for the actual session lifetime, attach command guidance, restart reconciliation. Missing tmux fails clearly; normal jobs do not depend on it. Treat launcher success as launched, never completed. Document how Herdr could implement the same bounded contract without adding a second adapter yet.
Non-goals: terminal emulator, built-in PTY rendering, automatic dependency install, Herdr-specific plugin implementation, detached sessions without lifecycle evidence.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestSessionLifecycle' -count=1 -v exits 0: detached launcher exit does not release overlap, eventual exit code is recorded, cancel stops owned work, and stale session IDs cannot target unrelated work.
- [ ] #2 task integration:tmux exits 0 against a real isolated tmux server: start an agent-like command, attach/capture it, detach without cancellation, observe completion, and stop only fixture-owned resources. Add this explicit opt-in target; missing tmux must fail rather than skip.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
