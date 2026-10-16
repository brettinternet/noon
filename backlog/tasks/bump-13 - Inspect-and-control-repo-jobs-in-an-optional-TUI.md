---
id: BUMP-13
title: Inspect and control repo jobs in an optional TUI
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 00:19'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-5
  - BUMP-9
documentation:
  - docs/design.md
modified_files:
  - internal/tui/**
  - internal/app/**
  - internal/cli/**
  - docs/tui.md
  - go.mod
  - go.sum
  - Taskfile.dist.yaml
  - .taskfiles/**
  - scripts/**
  - integration/**
priority: low
type: feature
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: an explicit bump tui provides terminal-native visibility without changing the safe default CLI or duplicating scheduler behavior.
Scope: job/next-run overview, run history, bounded live logs, clear offline state, explicit run/pause/cancel actions, attach-session handoff, resize/keyboard accessibility, confirmation for destructive actions. Reuse application operations; choose a maintained Go TUI library during implementation. Non-TTY invocation fails with a CLI alternative rather than hanging.
Non-goals: web UI, auto-TUI on bare bump, scheduler embedded in the view, terminal emulator, a separate persistence model.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestTUITransitions' -count=1 -v exits 0 with state transitions proving no mutation on selection/render, explicit action dispatch, cancellation confirmation, and offline/error display.
- [ ] #2 task integration:tui exits 0 in a real PTY: open/resize/navigate/exit, capture bounded logs, and verify terminal restoration. Add the target without requiring a running production scheduler.
- [ ] #3 task cli:test -- -run '^TestTUINonInteractive' -count=1 -v exits 0: redirected input never hangs, and bare bump remains a noninteractive summary.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
