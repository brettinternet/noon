---
id: NOON-53
title: 'Report orphaned runs in status, history, and the TUI'
status: Done
assignee: []
created_date: '2026-10-17 18:09'
updated_date: '2026-10-17 21:35'
labels:
  - cli
  - tui
  - mcp
dependencies: []
documentation:
  - docs/design.md
  - docs/cli.md
  - docs/agents.md
modified_files:
  - internal/app/history.go
  - internal/app/history_test.go
  - internal/app/dashboard.go
  - internal/app/queue.go
  - internal/cli/jobs.go
  - internal/cli/history.go
  - internal/cli/cancel.go
  - internal/cli/dashboard.go
  - internal/cli/dashboard_test.go
  - internal/cli/style.go
  - internal/tui/view.go
  - internal/tui/tui.go
  - internal/tui/tui_test.go
  - internal/mcp/server.go
  - internal/mcp/server_test.go
  - docs/design.md
  - docs/cli.md
  - docs/agents.md
priority: medium
type: enhancement
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: a run's owner (a foreground `noon run` or `noon serve`) can die without recording a result: SIGKILL, a crash, or a closed terminal. The row stays `running`. Dispatch reads `running` rows, so the orphan holds the project's one concurrency slot and the job's overlap slot: scheduled occurrences are `skipped` and queued work waits or expires until `noon serve` restarts, someone runs `noon run`, or someone runs `noon cancel`. Today only `noon logs --follow`, MCP `get_run`, and the TUI log view check the owner. The dashboard, `list`, `history`, bare `cancel`, MCP `status`/`list_runs`, and the TUI job and run lists show `running 3h` indefinitely.

Outcome: every inspection surface reports such runs as orphaned and points to `noon cancel ID`, without changing state.

Settled contract:
- Shared run JSON gains `orphaned` (boolean) with the meaning of `get_run`'s existing top-level `orphaned`: the run or its hook is active (`status` or `hook.status` is `running`) and its recorded owner PID and start identity no longer match a live, non-zombie process. It is computed read-only with the check `inspectStoredRun` already uses; a run that finishes normally between the read and the identity check is never reported orphaned. Queued and terminal runs are `false`. Stored `status` stays `running`; inspection never recovers, signals, or writes. `get_run` keeps its top-level field and both agree. Hook stdin always reports `false` because its owner is alive.
- Human output, for a `running` run with `orphaned` true: the dashboard and `list` LAST cell shows `orphaned` plus elapsed time since start (for example `orphaned 3h`) instead of `running 3h`; `history` STATUS shows `orphaned`; the bare `noon cancel` active-run list shows `orphaned`. CLI styling uses the attention (yellow) role. The TUI job and run lists show `orphaned` with the icon and tone the TUI log view already uses. Hook-only orphans (terminal status, hook running) keep their status label in human output; JSON still marks them.
- Dashboard: when the project has any `running` run with `orphaned` true, including runs of removed jobs, add a notice with code `orphaned` that names each such run ID and job and says it blocks background runs in this project. It ranks after `scheduler_offline` and before `queued`, fits 80 columns, and the dashboard still shows at most three notices. The `Next:` step becomes `noon cancel ID` for the lowest such run ID, ranked after the `noon service install`/`noon serve` case and before the runnable-job/`noon add` default. Starting the scheduler recovers orphans, so an offline scheduler keeps priority.
- `noon cancel ID` stays the recovery path: it already records the run `cancelled` and terminates only an identity-matched process group. No new command.

Scope: orphan classification in app-level run mapping; dashboard notice and next step; CLI LAST, STATUS, and cancel-list rendering and styling; TUI job and run lists; MCP field descriptions if needed; docs: design.md (shared run JSON, notice and `Next:` order, inspection rules), cli.md (status, history, cancel), agents.md (`orphaned` in run objects and the cancel remedy).

Tests (critical only): orphan classification is a nontrivial state derivation, and inspection must never mutate state. Extend internal/app/history_test.go: a `running` run with a stale owner identity maps to `orphaned: true` and the stored row stays `running`; a run owned by the live test process and a terminal run map to `false`. Extend internal/cli/dashboard_test.go for the notice and next step. Extend the existing TUI and MCP orphan tests. No new test files.

Non-goals: recovering, cancelling, or terminating runs from inspection; periodic orphan recovery inside the `noon serve` loop; detecting stalled but live jobs (output-idle reporting, per-job health commands, idle timeouts); a container HEALTHCHECK; new `noon doctor` checks; changing the `orphaned` meaning used by `logs --follow` and `get_run`; a new run status.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose private .git/noon.yaml defines `sleeper` (`run: [sleep, "3147"]`): start `bin/noon run sleeper` in the background; while it runs, `bin/noon history --json` reports it `running` with `orphaned: false`. Send SIGKILL to that `bin/noon` process only and reap it with `wait`. Then `bin/noon status` shows `orphaned` in sleeper's LAST cell, a notice naming the run ID and `sleeper`, and `Next: noon cancel ID`; `bin/noon status --json` reports sleeper's `last_run.orphaned` true, a notice with code `orphaned`, and `next` `noon cancel ID`; `bin/noon list` LAST and `bin/noon history` STATUS show `orphaned`; bare `bin/noon cancel` lists the run as orphaned.
- [x] #2 In the same environment, after every command in AC #1, `bin/noon history --json` still reports the run `status: running` with `orphaned: true`, and `pgrep -f 'sleep 3147'` still finds the job process (inspection changed nothing). `bin/noon cancel ID` exits 0; then `bin/noon history --json` reports the run `cancelled` with `orphaned: false`, `pgrep -f 'sleep 3147'` finds nothing within 15 seconds, and `bin/noon status --json` has no `orphaned` notice.
- [x] #3 `task cli:test PKG=./internal/app -count=1` exits 0 and covers: a `running` run whose owner identity is stale maps to `orphaned: true` while the stored row stays `running`; a run owned by the live test process and a terminal run map to `orphaned: false`.
- [x] #4 `task cli:test PKG=./internal/cli -count=1`, `task cli:test PKG=./internal/tui -count=1`, and `task cli:test PKG=./internal/mcp -count=1` exit 0 and cover: the dashboard `orphaned` notice and `noon cancel ID` next step; the TUI job and run lists show `orphaned` for the seeded orphan; MCP `get_run` top-level `orphaned` and `run.orphaned` agree for an orphan.
- [x] #5 docs/design.md, docs/cli.md, and docs/agents.md describe the `orphaned` run field, the notice order, and the `Next:` order exactly as implemented. `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Acceptance clarification approved by user on 2026-10-17: AC#1 allows preserved invocation selectors. With NOON_STATE_DIR set, noon cancel --state-dir PATH ID is correct; retain the existing docs/design.md safe copyable-guidance contract. All other requirements remain unchanged.
AC#1: task cli:build and isolated live sleeper scenario in /tmp/noon53-acceptance.LXjN8m -> passed under approved selector clarification. Live owner mapped false; after SIGKILL/reap of owned noon PID 96714, status/list/history/bare cancel showed orphaned; JSON last_run.orphaned true, orphaned notice, cancel 1 guidance with state selector.
AC#2: after each inspection, history --json remained running/orphaned and pgrep found sleep 3147. noon cancel 1 -> exit 0, cancelled/orphaned false, sleeper absent, no orphaned notice; owner process also absent.
AC#3: task cli:test PKG=./internal/app -- -count=1 -> exit 0; stale owner, live owner, terminal run, unchanged stored state covered.
AC#4: task cli:test PKG=./internal/cli -- -count=1; task cli:test PKG=./internal/tui -- -count=1; task cli:test PKG=./internal/mcp -- -count=1 -> all exit 0; required dashboard/TUI/MCP coverage passed.
AC#5: docs/design.md, docs/cli.md, docs/agents.md updated; task ci -> exit 0 (security, format/vet/staticcheck, tests, race, smoke).
Independent verifier completed the single acceptance/review pass. Its only reported failure was literal Next spelling, resolved by user-approved preservation of selectors; no source correction needed. gopls check clean. Diff stays within modified-file contract; no tests deleted, skipped, or weakened. Next: commit implementation, merge main, final-commit CI, completion and owned worktree cleanup.

Delivery: implementation commit 842a1b204611dabaa6c9da381f7ab8e13725b5b6 fast-forward merged into main. task ci passed again on that final implementation commit in primary checkout. Owned worktree and branch noon-53-orphaned removed with Worktrunk after receipt verification; associated Herdr wG8 confirmed absent. No retained worktree, blocker, or next implementation step. Final task-record commit receives another task ci check before lease release.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Orphaned runs are now reported read-only across shared JSON, dashboard, CLI, TUI, and MCP with safe cancellation guidance. Critical regressions, isolated live acceptance and full CI passed. User approved selector-preserving guidance. Implementation merged locally as 842a1b2; owned worktree/branch/workspace cleaned up. No push.
<!-- SECTION:FINAL_SUMMARY:END -->
