---
id: NOON-52
title: Record runs that find nothing to do as noop
status: To Do
assignee: []
created_date: '2026-10-17 17:30'
updated_date: '2026-10-17 17:36'
labels:
  - runner
  - history
  - config
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
- [ ] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose private .git/noon.yaml defines `idle` (run as a /bin/sh string that exits 78, `noop_exit_code: 78`, `on_success` and `on_failure` hooks that each touch a marker file) and `plain` (exits 78, no field): `bin/noon run idle` exits 0 and prints a summary containing `noop`; `bin/noon run idle --json` reports status `noop` and exit_code 78; neither hook marker exists; `bin/noon run plain` exits 78 and `bin/noon history --json` reports it `failed`; `bin/noon history --status noop --json` lists only the `idle` runs; `bin/noon list` LAST for `idle` is not a noop.
- [ ] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for `noop_exit_code` values 0, 125, 128, 256, "78", and for the field on a prompt job. After `bin/noon enable` with the field on a shared job, changing only its value makes `bin/noon list --json` report `needs_approval`.
- [ ] #3 `task cli:test PKG=./internal/store -count=1` exits 0 and covers: with 60 noop runs after one succeeded and one failed run, both real runs and their logs are still retained and only the newest 10 noops remain; PreviousStatus and LastSuccess ignore noop runs.
- [ ] #4 `task cli:test PKG=./internal/scheduler -count=1`, `task cli:test PKG=./internal/config -count=1`, and `task cli:test PKG=./internal/cli -count=1` exit 0 and cover: a scheduled run exiting with the configured code is `noop` and runs no hook; a timed-out or cancelled run is never `noop` even if the process later exits with that code; a failed -> noop -> succeeded sequence gives the success hook previous_status `failed`; the digest covers the field.
- [ ] #5 The four example scripts and failure-triage triage.sh exit 78 when there is nothing to do, and their jobs set `noop_exit_code: 78`; notify.sh no longer reads the run log; examples/jobs.yaml uses the field; noon.schema.json accepts 1-124 and rejects other values; `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
