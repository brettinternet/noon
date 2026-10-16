---
id: NOON-24
title: Associate runs with their log files and request context
status: Done
assignee: []
created_date: '2026-10-16 17:26'
updated_date: '2026-10-16 20:36'
labels:
  - cli
  - store
  - mcp
  - reviewed
dependencies: []
references:
  - internal/app/history.go
  - internal/store/store.go
  - internal/store/events.go
documentation:
  - docs/design.md
  - docs/development.md
  - docs/hooks.md
modified_files:
  - internal/store/**
  - internal/app/**
  - internal/cli/**
  - internal/mcp/**
  - README.md
  - docs/design.md
  - docs/hooks.md
priority: high
type: feature
ordinal: 2010
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: from any run a developer or agent sees, they can go straight to its log file and to the request that caused the run, without guessing state paths or doing extra lookups.

Why: run logs live at `STATE_DIR/logs/PROJECT_ID/RUN_ID.log` (docs/design.md#state). The shared run JSON (`app.Run`, built by `RunFromStore` in internal/app/history.go) exposes only `log.bytes/truncated/available`. Hooks get the absolute path as `NOON_LOG_FILE`, but the CLI, CLI JSON, and MCP do not expose it, so nobody can `rg`, `less`, or `tail -f` the file directly. `store.Run` already holds the request context (`digest`, `requested_by`, `idempotency_key`, `due_at`, `cancel_requested_at`, `event_path`, `depth`), but `RunFromStore` drops all of it, so an agent cannot connect a run to its trigger, payload, or job version. A foreground `noon run` never prints its run ID, and `noon run --detach` prints the ID with no hint for reading it.

This is the first task in a log-access series (this task, then bounded log reads, run filtering, follow lifecycle, hook sections, and agent docs). Later tasks depend on the fields added here.

Read first: docs/design.md#experience, #execution, #state, and #integrations. Then read NOON-17 for the original run JSON contract, and docs/hooks.md for the hook stdin summary.

Scope:
- Add these fields to the shared run JSON object. Every client gets them through `RunFromStore`, including CLI `history`/`logs`/`run --json`, the MCP `list_runs`/`get_run`/`run_job`/`status`, list `last_run`, and hook stdin. Absent values are null, matching the existing convention.
  - `project`: the canonical absolute project path, as `noon --json status` reports in `project.path`. Run IDs are global, so `noon logs 12` can return a run from another checkout, and this field makes that visible.
  - `log.path`: absolute path of the retained log, built with `store.RunLogFilePath` (the same function behind `NOON_LOG_FILE`). Null when `log.available` is false (pruned, queued, or never started). Present while the run is still running.
  - `event_file`: absolute path of the payload file, built with `store.EventFilePath` (the same value as `NOON_EVENT_FILE`), or null.
  - `digest`, `requested_by`, `idempotency_key`, `due_at`, `cancel_requested_at`: strings or null, copied from the stored row.
  - `depth`: integer, the stored request depth.
- `noon logs [NAME|RUN] --path`: print the selected run's absolute log path plus a newline and exit 0. Selection is the same as `noon logs`. If the log is unavailable, return the same error `noon logs` returns today. `--path` with `--json` or `--follow` is a usage error (exit 2) whose hint points to `log.path` in `--json` output.
- Foreground `noon run NAME` start line, on stderr: `▶ NAME · run ID · COMMAND` (today it is `▶ NAME · COMMAND`). Nothing else in foreground output changes.
- Human output of `noon run NAME --detach|--in|--at` (`printQueuedRun` in internal/cli/queue.go): after the existing first line, print `Follow it: noon logs ID --follow` when the referenced run is queued or running, otherwise `Read it: noon logs ID`. Keep this before the existing offline-scheduler lines. It is not a `Next:` line, so the design rule of at most one `Next:` line still holds.
- Update docs/design.md (shared run JSON, the `noon logs` row of the command table), README.md (Logs section), and docs/hooks.md (the stdin run object now carries these fields).

Non-goals: new log reading modes (NOON-25), status filters (NOON-26), changing log storage or retention, exposing environment values, and printing paths in human `history` output.

Tests (critical regressions only):
- internal/cli `TestHistoryAndLogs`: `logs --json` and `history --json` report a `log.path` that is absolute, exists, and equals `noon logs RUN --path` output. After retention prunes a run's log, `log.path` and `event_file` are null and `--path` exits 1 with the missing-log error. `--path --json` and `--path --follow` exit 2.
- internal/cli `TestHooks`: the hook's stdin `run.log.path` equals its `NOON_LOG_FILE`.

AC fixture: every acceptance command starts with the same fixture prefix. It builds the binary, creates an isolated HOME and project, and runs `t` (run 1, exit 3) and `ok` (run 2). If the shell tool rejects inline syntax, put the exact command in a temporary script and run it, recording that in Implementation Notes.

Fixture prefix (`N` below means `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"`): `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; `

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestHistoryAndLogs|TestHooks)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --path; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json history t | jq -r '.runs[0].log.path'; test -f "$(env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --path)" && echo exists` prints the same absolute path twice, ending in `/.local/state/noon/logs/1/1.log` under `$noon_ac_home`, then `exists`
- [x] #3 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json logs 1 | jq -c '.run | {project, digest: (.digest | type), depth, event_file, requested_by, idempotency_key, due_at, cancel_requested_at}'; (cd "$noon_ac_project" && pwd -P)` prints `{"project":P,"digest":"string","depth":0,"event_file":null,"requested_by":null,"idempotency_key":null,"due_at":null,"cancel_requested_at":null}` where P equals the path printed on the last line
- [x] #4 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t 2>&1 >/dev/null | head -1` prints `▶ t · run 3 · seq 1 2000 | sed s/^/line-/; echo boom; exit 3`
- [x] #5 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" enable >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --detach` prints `Queued t as run 3 · due now`, then `Follow it: noon logs 3 --follow`, then the existing offline-scheduler lines
- [x] #6 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --path --json; echo "exit=$?"` prints a usage error mentioning `log.path` and `exit=2`
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
Extend the shared run representation and log-path selection; update foreground/queued output and documentation; extend the specified regression tests, execute all acceptance commands and CI, independently review, commit and merge, then clean up the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:test -- -run '^(TestHistoryAndLogs|TestHooks)$' -count=1 -v -> exit 0, both PASS, including retained/pruned paths and hook stdin parity.
AC#2: exact fixture command from criterion, executed via bash /tmp/noon-24-ac-2.sh -> identical absolute log paths ending /.local/state/noon/logs/1/1.log and exists.
AC#3: exact fixture command via bash /tmp/noon-24-ac-3.sh -> canonical project equals pwd -P, digest string, depth 0, absent request fields null.
AC#4: exact fixture command via bash /tmp/noon-24-ac-4.sh -> expected foreground start line with run 3 and unchanged command.
AC#5: exact fixture command via bash /tmp/noon-24-ac-5.sh -> Queued t as run 3, Follow it: noon logs 3 --follow, then offline scheduler lines.
AC#6: exact fixture command via bash /tmp/noon-24-ac-6.sh -> JSON usage error hint names .run.log.path; exit=2.
Fixture scripts were written verbatim from the provider acceptance commands to avoid shell parser restrictions. Single independent review found cancellation retention exposed a deleted event path; reproduced with a queued payload plus 50 newer same-job runs, fixed both cached-row refresh paths, and extended TestCancelQueued. Focused cancellation regression passes; no other review findings. No tests deleted, skipped, or weakened; all modifications within contract. LSP diagnostics clean; task ci passed on final implementation merge f16d35e (tests, race, vet, Staticcheck, security, smoke). Implementation commit 6ce2d0e merged into main as f16d35e, preserving concurrent help-alias fix. Owned worktree and branch noon-24-run-context removed through Worktrunk; Herdr workspace wD9 removal verified. Nothing pushed. No blocker or remaining implementation step.

Post-implementation review: inspected shared RunFromStore mapping, version-compatible store projections, retained log/event paths, hook stdin, foreground run IDs, queued read/follow hints, and CLI --path validation. No additional defects found. Verified with task cli:test -- -run '^(TestSelectLog|TestHistoryAndLogs|TestLogsFollow|TestMCP|TestHooks|TestCancelQueued)$' -count=1 (exit 0), including retention and hook path regressions. task cli:build passed; an isolated built-binary run exposed the expected canonical project, absolute log path, nullable request fields, and foreground run ID. Review-only change; no implementation edits or full CI rerun.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Exposed shared run paths/request context, logs --path, foreground run IDs and queued read/follow hints; documented behavior and added critical regressions. All six acceptance commands and final implementation CI pass; independent review defect fixed. Committed 6ce2d0e, integrated f16d35e, owned worktree/workspace cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
