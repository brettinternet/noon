---
id: NOON-13
title: Inspect and control jobs in a terminal UI
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 01:41'
labels:
  - tui
dependencies:
  - NOON-21
references:
  - 'https://github.com/charmbracelet/bubbletea'
documentation:
  - docs/design.md
  - docs/development.md
modified_files:
  - internal/tui/**
  - internal/app/**
  - internal/cli/**
  - go.mod
  - go.sum
  - README.md
priority: low
type: feature
ordinal: 2100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `noon tui` gives a live, keyboard-driven view of the current project's jobs and runs, built only on the app operations the CLI and MCP already use.

Read first: docs/design.md#experience and #architecture. Use `charm.land/bubbletea/v2` (confirm the current module path and release in its README before adding it) and lipgloss only if layout needs it.

Scope:
- Views:
  - Jobs: JOB, SCHEDULE, NEXT, LAST, and state; refreshed every second from the store; header shows scheduler state.
  - Runs: history of the selected job.
  - Log: tail of the selected run, following while it runs, with the 1 MiB follow behavior from the history task.
- Keys: `j`/`k` or arrows move; `enter` opens runs; `l` opens the log; `r` queues the selected job now (same rules and errors as `noon run --detach`); `c` cancels the selected run after a `y`/`n` confirmation; `p` toggles pause; `esc` goes back; `q` quits; `?` shows help.
- Errors and notices (needs approval, not enabled, scheduler offline) render in a status line with the CLI command that fixes them. The TUI never adds, edits, enables, or approves jobs.
- When stdin or stdout is not a terminal: usage error `noon tui needs a terminal; use noon status`, exit 2. `--json` is a usage error.
- The terminal is restored on exit, panic, and SIGTERM.

Non-goals: a web UI, editing config, multiple projects in one view, mouse support, themes, embedding the scheduler.

Tests:
- internal/tui `TestTUIModel`: drive the model's Update with key messages against a temp project and real store. `r` creates a queued run, `p` pauses and unpauses, `c` then `n` cancels nothing while `c` then `y` cancels, and View contains job names and the needs-approval notice. Selection and rendering never write to the store.
- internal/cli `TestTUINonInteractive`: `noon tui` with non-terminal stdio exits 2 without hanging.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `task cli:build && bin/noon tui < /dev/null; echo "exit=$?"` prints the terminal-required error immediately and `exit=2`
- [ ] #3 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
