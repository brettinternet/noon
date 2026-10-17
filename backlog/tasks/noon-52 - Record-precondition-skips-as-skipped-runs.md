---
id: NOON-52
title: Record runs that find nothing to do as noop
status: Done
assignee: []
created_date: '2026-10-17 17:30'
updated_date: '2026-10-17 22:28'
labels:
  - runner
  - history
  - config
  - reviewed
dependencies: []
documentation:
  - docs/design.md
  - docs/configuration.md
  - docs/hooks.md
  - docs/cli.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/config/edit.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/cli/run.go
  - internal/cli/run_test.go
  - internal/cli/style.go
  - internal/cli/dashboard.go
  - internal/cli/jobs.go
  - internal/cli/history.go
  - internal/app/hooks.go
  - internal/app/history.go
  - internal/app/errors.go
  - internal/app/dashboard.go
  - internal/app/inspect.go
  - internal/store/store.go
  - internal/store/store_test.go
  - internal/tui/tui.go
  - internal/mcp/server.go
  - noon.schema.json
  - examples/jobs.yaml
  - examples/ai/background-agents/noon.yaml
  - examples/ai/background-agents/scripts/inbox.sh
  - examples/ai/research-digest/noon.yaml
  - examples/ai/research-digest/scripts/release-watch.sh
  - examples/ai/local-llm/noon.yaml
  - examples/ai/local-llm/scripts/failures-digest.sh
  - examples/ai/local-llm/scripts/notes-digest.sh
  - examples/ai/failure-triage/noon.yaml
  - examples/ai/failure-triage/scripts/triage.sh
  - examples/ai/failure-triage/scripts/notify.sh
  - docs/configuration.md
  - docs/design.md
  - docs/cli.md
  - docs/hooks.md
  - docs/agents.md
priority: medium
type: feature
ordinal: 14900
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: jobs that poll for work (an inbox, new releases, recent failures, changed notes) usually find nothing and exit 0. History, the dashboard, and hooks then report a success that did nothing. Noon's own examples show the cost: background-agents/inbox.sh, research-digest/release-watch.sh, local-llm/failures-digest.sh, and local-llm/notes-digest.sh all `echo ...; exit 0`. failure-triage/notify.sh searches the run log for "no undiagnosed failures" to suppress its own on_success hook. Frequent jobs also evict real results from the 50-run retention window. Shell `if:` conditions and built-in machine probes were rejected (see NOON-51): the script owns the check, and Noon only records the outcome.

Outcome: a job opts in to a "nothing to do" exit code, and Noon records those runs as `noop`:

```yaml
jobs:
  inbox:
    schedule: every 5m
    run: ./scripts/inbox.sh     # exits 78 when the inbox is empty
    noop_exit_code: 78
```

Settled contract:
- `noop_exit_code` is an optional integer 1-124. 0, 125-127 (Noon start failures), 128+ (signals), non-integers, and strings are config errors with file, line, and YAML path. It is valid only on `run` jobs; on prompt jobs it is a config error. It is an execution field in the approval digest. Without it, behavior is unchanged: no exit code is ever special by default, so a tool that happens to exit 78 still fails.
- A run is `noop` when the process started, exited normally with exactly that code, and was not timed out or cancelled. `noop` is a new terminal status, distinct from the existing `skipped` (an occurrence that never started because of overlap). It has a log, duration, and exit code.
- A noop is not a result: no on_success or on_failure hook runs. It never becomes NOON_PREVIOUS_STATUS / previous_status and never sets NOON_LAST_SUCCESS_AT. A failed -> noop -> succeeded sequence still reports previous_status `failed` to the success hook.
- Foreground `noon run` exits 0 for a noop, as a documented exception to "exits with the job's code", so Git hooks, Stop hooks, and wrapper scripts treat it as not a failure. The summary line says noop and shows the exit code; `--json` reports status `noop` and the real exit code.
- Scheduling: a noop is a terminal run like any other for overlap, queue, cooldown, missed-run catch-up, and idempotency keys.
- Display: `history` shows noop runs, and `--status noop` filters them (CLI, MCP list_runs/get_run enums). The dashboard, list LAST column, and TUI show the latest result, not a noop, so a failure is never hidden behind later noops. The TUI all-runs view includes them with muted styling.
- Retention: noop runs have their own budget, the newest 10 per job with their logs, and never count toward the 50-run / 10-log budget for other terminal runs. A frequent noop job therefore can't evict its last real result.
- State compatibility: if an older Noon binary would misbehave on a database containing `noop` runs, bump the state schema version so it is refused with the existing upgrade hint. Record which way it went in Implementation Notes.

Scope: config field, validation, and digest; status classification in scheduled and foreground runs; hook, previous-status, and last-success exclusion; retention budgets; history/MCP/TUI/dashboard display; schema; examples/jobs.yaml (uses every field); convert the four example scripts to exit 78 with `noop_exit_code: 78`; make failure-triage triage.sh exit 78 with nothing to diagnose and delete the log search from notify.sh; docs: configuration.md (field), design.md (statuses, foreground exit codes, retention, hooks), cli.md (statuses, run exit code), hooks.md (no hook for noop), agents.md (never report a noop as completed work).

Tests (critical only): field validation (parsing); digest includes the field (authorization boundary); classification only for a normal exit with the configured code, never for timeout, cancellation, start failure, or jobs without the field (state transition); noop never fires hooks or changes previous status or last success (regression-prone); retention keeps the last real result under many noops (data integrity). Extend existing internal/config, internal/store, internal/scheduler, and internal/cli tests.

Non-goals: built-in condition probes or `if:` expressions; multiple noop codes per job; noop for agent prompt jobs; a default reserved code; hiding noop runs from `history` by default; configurable retention budgets; changing the existing `skipped` status.

Dependencies: none (independent of NOON-51).

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose private .git/noon.yaml defines `idle` (run as a /bin/sh string that exits 78, `noop_exit_code: 78`, `on_success` and `on_failure` hooks that each touch a marker file) and `plain` (exits 78, no field): `bin/noon run idle` exits 0 and prints a summary containing `noop`; `bin/noon run idle --json` reports status `noop` and exit_code 78; neither hook marker exists; `bin/noon run plain` exits 78 and `bin/noon history --json` reports it `failed`; `bin/noon history --status noop --json` lists only the `idle` runs; `bin/noon list` LAST for `idle` is not a noop.
- [x] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for `noop_exit_code` values 0, 125, 128, 256, "78", and for the field on a prompt job. After `bin/noon enable` with the field on a shared job, changing only its value makes `bin/noon list --json` report `needs_approval`.
- [x] #3 `task cli:test PKG=./internal/store -count=1` exits 0 and covers: with 60 noop runs after one succeeded and one failed run, both real runs and their logs are still retained and only the newest 10 noops remain; PreviousStatus and LastSuccess ignore noop runs.
- [x] #4 `task cli:test PKG=./internal/scheduler -count=1`, `task cli:test PKG=./internal/config -count=1`, and `task cli:test PKG=./internal/cli -count=1` exit 0 and cover: a scheduled run exiting with the configured code is `noop` and runs no hook; a timed-out or cancelled run is never `noop` even if the process later exits with that code; a failed -> noop -> succeeded sequence gives the success hook previous_status `failed`; the digest covers the field.
- [x] #5 The four example scripts and failure-triage triage.sh exit 78 when there is nothing to do, and their jobs set `noop_exit_code: 78`; notify.sh no longer reads the run log; examples/jobs.yaml uses the field; noon.schema.json accepts 1-124 and rejects other values; `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
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
1. Add validated noop exit-code config and terminal-run semantics, preserving approval and prior-result boundaries. 2. Update retention, adapters, examples and documentation. 3. Exercise every acceptance criterion, run one scoped review and task ci, commit and merge locally, finalize task and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract expansion: internal/cli/shell_test.go must update its exact expected completion-status array because noop is now a supported CLI status. This preserves the existing completion assertion rather than weakening it.

Modified-file contract expansion: internal/tui/view.go must render the configured nonzero noop exit code with muted rather than failure-red styling in run rows and detail views, as required by the settled display contract. No new styling-only test is required.

Modified-file contract expansion: internal/cli/history_test.go must include noop in its exact expected valid-status error message. Focused CLI tests identified this existing assertion; updating it preserves validation coverage for the expanded enum.

Modified-file contract expansion: README.md command-table wording must document the foreground noop exit-0 exception rather than claiming noon run always returns the job exit code. This satisfies the explicit documentation Definition of Done.

AC#1: task cli:build and bun /tmp/noon-52-acceptance.mjs -> PASS. Isolated HOME/config/state/temp Git repo: foreground idle returns 0 with noop summary; JSON preserves exit 78; no hook markers; plain returns 78/failed; noop history filters correctly; idle LAST is never.
AC#2: bun /tmp/noon-52-acceptance.mjs -> PASS. Config rejects 0, 125, 128, 256, quoted 78 and prompt-job field with file:line:YAML-path; changing only shared noop_exit_code revokes approval.
AC#3: task cli:test PKG=./internal/store -- -count=1 -> PASS (executor and independent verifier). 60 noops preserve both real result rows/logs and newest 10 noops; previous status and last success exclude noops.
AC#4: task cli:test PKG=./internal/scheduler -- -count=1; task cli:test PKG=./internal/config -- -count=1; task cli:test PKG=./internal/cli -- -count=1 -> all PASS. Tests cover scheduled noop/no hooks, timeout/cancellation exclusion, failed-noop-success previous status, config digest and foreground/start-failure behavior. AC3/4 command correction: Task rejects bare -count=1; the documented -- separator forwards it to go test.
AC#5: bun /tmp/noon-52-acceptance.mjs -> all five no-work example scripts exit 78 without agents/network; task cli:test PKG=./integration -- -run ^TestExamplesValidate$ -count=1 -> PASS; config schema validation test passes valid boundaries and rejects invalid values/prompt jobs. task ci -> PASS on implementation af5c523 and integrated main (merge b7c3583). Documentation and example changes match implemented behavior.
State compatibility: bumped SQLite user_version from 9 to 10 so older binaries refuse noop-aware state; older retention/latest-run queries would otherwise mix noops into result budgets and display.
One independent critical-correctness verification pass found no concrete defects; git diff --check and store LSP diagnostics clean. No tests deleted, skipped or weakened. Executor timeout preserved completed changes; parent completed acceptance and CI. Implementation af5c523 merged locally via b7c3583; no push. Remaining step: remove verified owned worktree/workspace and finalize task.

Cleanup complete: Worktrunk removed the session-receipted feat/noon-52-noop checkout and branch; post-remove hook closed its exact Herdr workspace wG4, verified absent. No owned checkout/workspace retained; no blocker or resumable step remains.

Post-completion review: no actionable defects found. Inspected noop_exit_code parsing (!!int tag, 1-124, prompt-job rejection with YAML path), digest/edit round-trip, classification order in foreground and scheduled runs (timeout, cancellation and start failure precede noop; cancellation-check failure still overrides to failed), matchingHook/PreviousStatus/LastSuccess exclusion, split retention in pruneRuns (10 noop rows+logs vs 50/10 regular), LatestRunsForProject noop exclusion feeding dashboard/list/TUI LAST, schema v10 bump, TUI muted exit styling, and example scripts (failures-digest/release-watch windows stay correct because noops no longer advance NOON_LAST_SUCCESS_AT). task cli:test PKG="./internal/config ./internal/store ./internal/scheduler ./internal/cli ./internal/app ./internal/tui ./internal/mcp" -- -count=1 passed. No source changes; full CI not rerun for this review.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented opt-in noop_exit_code with strict validation/approval digest, terminal noop status, no hooks/result displacement, separate retention, adapter display/filtering, examples and documentation. Verified isolated CLI acceptance, five example no-work paths, focused tests, independent verification and task ci. Committed af5c523 and merged locally via b7c3583; owned worktree/branch/workspace removed; no push.
<!-- SECTION:FINAL_SUMMARY:END -->
