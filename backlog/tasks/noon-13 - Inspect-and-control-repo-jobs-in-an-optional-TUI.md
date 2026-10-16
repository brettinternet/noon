---
id: NOON-13
title: Inspect and control jobs in a terminal UI
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 20:14'
labels:
  - tui
dependencies:
  - NOON-21
  - NOON-28
references:
  - 'https://github.com/charmbracelet/bubbletea'
  - internal/app/history.go
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

Read first: docs/design.md#experience and #architecture, and the final contracts of NOON-24 through NOON-28 (log paths and run context, bounded/searchable log reads, status filters, follow lifecycle, job/hook sections). The TUI must reuse those app operations rather than reading log files or the store itself. Use `charm.land/bubbletea/v2` (confirm the current module path and release in its README before adding it) and lipgloss only if layout needs it.

Scope:
- Views:
  - Jobs: JOB, SCHEDULE, NEXT, LAST, and state; refreshed every second from the store; header shows scheduler state.
  - Runs: history of the selected job, with a status filter that cycles all → failed,timed_out,interrupted → running,queued (NOON-26 `--status` semantics).
  - Log: opens on the last screenful of whole lines from the shared log read operation (NOON-25 `tail`) and follows while the run is active from that offset by paging with `next`, never re-reading from byte 0. It keeps the 1 MiB follow notice. Queued runs show the waiting notice and orphaned runs show the `orphaned` message and `noon cancel ID` hint, both from NOON-27. The header shows the run ID, status, `log.path`, and the hook state.
  - Log search: `/` prompts for an RE2 pattern and shows `match` results with 3 lines of context (NOON-25 grep-style output, line numbers from the start of the file). `esc` returns to the tail.
  - Log section: `s` cycles job → hook → all (NOON-28). The view opens on `job` for failed or timed-out runs that have hook output, and on `all` otherwise.
- Keys: `j`/`k` or arrows move; `enter` opens runs; `l` opens the log; `/` searches the log; `s` cycles the log section; `f` cycles the runs status filter; `g`/`G` jump to the top or bottom of the log; `r` queues the selected job now (same rules and errors as `noon run --detach`); `c` cancels the selected run after a `y`/`n` confirmation; `p` toggles pause; `esc` goes back; `q` quits; `?` shows help.
- Errors and notices (needs approval, not enabled, scheduler offline) render in a status line with the CLI command that fixes them. The TUI never adds, edits, enables, or approves jobs.
- When stdin or stdout is not a terminal: usage error `noon tui needs a terminal; use noon status`, exit 2. `--json` is a usage error.
- The terminal is restored on exit, panic, and SIGTERM.

Non-goals: new log capabilities beyond NOON-24..NOON-28 (if the TUI needs one, add it to internal/app for every client and justify it in Implementation Notes), loading whole logs into memory, a web UI, editing config, multiple projects in one view, mouse support, themes, embedding the scheduler.

Tests:
- internal/tui `TestTUIModel`: drive the model's Update with key messages against a temp project and real store. `r` creates a queued run, `p` pauses and unpauses, `c` then `n` cancels nothing while `c` then `y` cancels, and View contains job names and the needs-approval notice. Selection and rendering never write to the store. Opening the log of a run with more lines than the view shows the last lines, not the first. `/` with a pattern shows the matching line's number. `s` on a failed run with hook output toggles hook output out of and into view.
- internal/cli `TestTUINonInteractive`: `noon tui` with non-terminal stdio exits 2 without hanging.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task cli:build && bin/noon tui < /dev/null; echo "exit=$?"` prints the terminal-required error immediately and `exit=2`
- [x] #3 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse existing app dashboard, queue, pause, history, and log operations in a Bubble Tea v2 model with jobs/runs/log views and specified keys. 2. Wire terminal-only CLI entry and document implemented behavior. 3. Add critical real-store model and noninteractive CLI regression coverage after implementation. 4. Run all acceptance commands, independent review, full CI, commit, merge, and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Worklease claim fc143caad22fe75f5f7e61f003c2b527 acquired by loop f48b0ec2-74b7-419d-8099-77f0ce9f10bc. Implementation checkout /Users/brett/dev/me/bump/.worktrees/noon-13-tui (branch noon-13-tui); receipt /Users/brett/dev/me/bump/.git/worktrees/noon-13-tui/agent-creation.json. Bubble Tea v2 is the task-required maintained TUI dependency, replacing no existing TUI framework; CGO-disabled build is an acceptance requirement.

Implementation is present in the owned worktree; executor reported focused acceptance checks and task ci passing. Its accidental worktree-local task completion was discarded, preserving this primary record as authoritative. docs/design.md expansion is accepted to correct stale claims that implemented MCP/TUI adapters remain future work, as required by DoD#5. Independent review and terminal checks pending; not complete yet.

AC#1: task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v -> exit 0; both named tests printed --- PASS (parent rerun). AC#2: task cli:build && bin/noon tui < /dev/null; echo "exit=$?" -> immediate terminal-required error and exit=2 (parent rerun). AC#3: CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon -> exit 0 (parent rerun). PTY direct execution with an isolated project rendered example job and restored identical stty attributes after q and SIGTERM. LSP diagnostics clean for internal/tui/tui.go. Independent review is still pending.

Independent general review completed: two concrete P2 defects found in TUI log cursor replacement (g/G can skip or duplicate output) and follow-cap notice detection (unfinished line at persisted 1 MiB cap). Bounded fixes and regression checks are in progress; no second general review planned. Integration remains pending.

Delivered implementation commit 9bed9dedb24f7deac35aaa95e2818f6502a14249, fast-forward merged into main. task ci passed on that final implementation commit (security, vet/staticcheck, full and race suites, build, smoke). All three exact acceptance commands were rerun after corrections and passed with the previously recorded results. Independent review found cursor replacement and unfinished-line follow-cap notice defects; both fixed and covered by real-store regressions. No tests deleted, skipped, or weakened. Documentation and help describe implemented behavior. Owned worktree and branch noon-13-tui removed through Worktrunk; matching Herdr workspace wDG verified absent. No retained checkout or workspace, no remaining blocker, no next resumable implementation step. No push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented noon tui: live job/run views, bounded searchable sectional logs, and explicit queue/pause/confirmed cancel controls over shared app operations. Verified acceptance commands, task ci on implementation commit, independent review corrections, and PTY restoration on quit/SIGTERM. Merged locally as 9bed9de and cleaned owned worktree/workspace.
<!-- SECTION:FINAL_SUMMARY:END -->
