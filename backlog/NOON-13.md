---
id: NOON-13
title: Inspect and control jobs in a terminal UI
status: done
type: feature
priority: low
labels: [tui, reviewed]
blocked_by: [NOON-21, NOON-28]
rank: a53594d653594d638
refs: ['https://github.com/charmbracelet/bubbletea', internal/app/history.go, docs/design.md, docs/development.md]
files: [internal/tui/**, internal/app/**, internal/cli/**, go.mod, go.sum, README.md]
created: 2026-10-16T00:18:00Z
updated: 2026-10-16T20:40:00Z
---

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

## Acceptance criteria

- [x] `task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] `task cli:build && bin/noon tui < /dev/null; echo "exit=$?"` prints the terminal-required error immediately and `exit=2`
- [x] `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Reuse existing app dashboard, queue, pause, history, and log operations in a Bubble Tea v2 model with jobs/runs/log views and specified keys. 2. Wire terminal-only CLI entry and document implemented behavior. 3. Add critical real-store model and noninteractive CLI regression coverage after implementation. 4. Run all acceptance commands, independent review, full CI, commit, merge, and remove owned worktree.

## Notes

Worklease claim fc143caad22fe75f5f7e61f003c2b527 acquired by loop f48b0ec2-74b7-419d-8099-77f0ce9f10bc. Implementation checkout /Users/brett/dev/me/bump/.worktrees/noon-13-tui (branch noon-13-tui); receipt /Users/brett/dev/me/bump/.git/worktrees/noon-13-tui/agent-creation.json. Bubble Tea v2 is the task-required maintained TUI dependency, replacing no existing TUI framework; CGO-disabled build is an acceptance requirement.

Implementation is present in the owned worktree; executor reported focused acceptance checks and task ci passing. Its accidental worktree-local task completion was discarded, preserving this primary record as authoritative. docs/design.md expansion is accepted to correct stale claims that implemented MCP/TUI adapters remain future work, as required by DoD#5. Independent review and terminal checks pending; not complete yet.

AC#1: task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v -> exit 0; both named tests printed --- PASS (parent rerun). AC#2: task cli:build && bin/noon tui < /dev/null; echo "exit=$?" -> immediate terminal-required error and exit=2 (parent rerun). AC#3: CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon -> exit 0 (parent rerun). PTY direct execution with an isolated project rendered example job and restored identical stty attributes after q and SIGTERM. LSP diagnostics clean for internal/tui/tui.go. Independent review is still pending.

Independent general review completed: two concrete P2 defects found in TUI log cursor replacement (g/G can skip or duplicate output) and follow-cap notice detection (unfinished line at persisted 1 MiB cap). Bounded fixes and regression checks are in progress; no second general review planned. Integration remains pending.

Delivered implementation commit 9bed9dedb24f7deac35aaa95e2818f6502a14249, fast-forward merged into main. task ci passed on that final implementation commit (security, vet/staticcheck, full and race suites, build, smoke). All three exact acceptance commands were rerun after corrections and passed with the previously recorded results. Independent review found cursor replacement and unfinished-line follow-cap notice defects; both fixed and covered by real-store regressions. No tests deleted, skipped, or weakened. Documentation and help describe implemented behavior. Owned worktree and branch noon-13-tui removed through Worktrunk; matching Herdr workspace wDG verified absent. No retained checkout or workspace, no remaining blocker, no next resumable implementation step. No push performed.

Final main verification at completion-record commit 363345c failed twice in unchanged TestServeStdioConcurrentCancellationAndEOFDrain: cancelled get_run returned success when cancel_run made its run terminal before the wait loop. User explicitly approved expanding scope to a narrow GetRun cancellation fix in a fresh owned worktree, existing regression checks, full CI, commit, merge, cleanup. Prior implementation commit passed CI; final-main DoD verification reopened.

Approved cancellation correction delivered as e2aa083: GetRun checks context cancellation on entry and before returning a successful snapshot, including terminal-transition paths. Existing TestServeStdioConcurrentCancellationAndEOFDrain passed 100 runs without changes; task ci passed in the correction checkout and again on main after merge. No test weakened. Correction checkout/branch noon-13-cancellation and Herdr workspace wDJ removed and verified absent; original noon-13-tui/wDG also gone. All item requirements satisfied; no remaining blocker or resumable work. Concurrent unrelated documentation commit 1f7341a preserved.

Post-merge implementation review: inspected internal/tui/tui.go and CLI terminal gating against the task contract, including shared app operations, mutation authorization, cancellation confirmation, bounded log cursors, sections, search, and rendering. Finding (P2): runSearch accepts the valid empty RE2 pattern and stores it as logSearch="" (internal/tui/tui.go:695-711), but goBack uses nonempty logSearch to identify search mode (line 953 onward). Reproduction: open a log, press / then Enter without text, then Escape. Escape leaves the log screen instead of restoring the tail; polling can also append unnumbered output to these search results because pollLog treats the empty string as ordinary follow mode. Track search mode separately or explicitly treat an empty submission as cancellation. Finding recorded, not fixed in this review. Verification: task cli:test -- -run '^(TestTUIModel|TestTUINonInteractive)$' -count=1 -v passed; task cli:build passed; bin/noon tui < /dev/null immediately reported terminal required and exited 2; CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon passed. Existing tests do not cover empty search submission. Terminal restoration was inspected through the Bubble Tea entry point, not independently PTY-retested in this review.

## Summary

Completed noon tui with live jobs/runs, bounded log search/follow/sections, and safe controls. Review defects fixed; user-approved shared log cancellation race fixed in e2aa083. All acceptance checks and full CI passed; cancellation regression passed 100 runs. Implementation 9bed9de and correction merged locally; both owned worktrees and workspaces cleaned. No push.
