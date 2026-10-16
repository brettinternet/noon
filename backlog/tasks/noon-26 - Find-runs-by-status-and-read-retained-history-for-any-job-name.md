---
id: NOON-26
title: Find runs by status and read retained history for any job name
status: To Do
assignee: []
created_date: '2026-10-16 17:26'
labels:
  - cli
  - store
  - mcp
dependencies:
  - NOON-25
references:
  - internal/store/store.go
  - internal/app/history.go
  - internal/cli/history.go
  - internal/mcp/server.go
documentation:
  - docs/design.md
  - docs/development.md
  - ../hum/docs/coding-agents.md
modified_files:
  - internal/store/**
  - internal/app/**
  - internal/cli/**
  - internal/mcp/**
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 2030
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: "show me the last failure of tests" takes one command or one MCP call, and run history stays readable after a job is renamed or removed.

Why: the most common agent question is why a job failed last time. Today `history` and `list_runs` filter only by job, so an agent has to list runs and scan them. `noon logs NAME` picks the newest run by ID, even when that run is queued and has no log. A verified repro: after `noon run t --detach`, `noon logs t` fails with "log for run 3 is unavailable / Logs are kept for the newest 10 runs per job", even though run 1 of `t` has a retained log. `history NAME` and `logs NAME` also return `not_found` for a job that has been removed from config, even when its runs and logs are still retained (a NOON-17 review finding). hum's agent workflow starts from a failure filter (`events {failed: true}` in `../hum/docs/coding-agents.md`). Borrow that idea as a status filter.

Read first: docs/design.md#experience and #state (the run statuses list), NOON-17, NOON-24, and NOON-25. In code, read `store.RunsForProject` and `LatestRunsForProject`, `app.Runs`/`ListRuns`, `runLogs` in internal/cli/history.go, and the MCP `list_runs`/`get_run` handlers.

Scope:
- `--status S` on `noon history` and `noon logs`. It accepts a comma-separated list or a repeated flag of run statuses: `queued`, `running`, `succeeded`, `failed`, `timed_out`, `cancelled`, `interrupted`, `skipped`, `missed`, `expired`. An unknown value is a usage error whose hint lists the valid statuses. Filter in SQL, before `--limit`.
- Run selection for `noon logs` with no argument or with `NAME`: choose the newest run that has started (`started_at` is not null) and matches `--status` when given. A queued or never-started run is chosen only when `--status` names its status explicitly. A numeric `RUN` selector is unchanged and ignores `--status` (combining them is a usage error).
- No matching run: `noon logs NAME --status failed` prints `No runs of NAME match --status failed.` and exits 0. `history` prints the same kind of message. JSON keeps the existing empty shapes.
- Removed or renamed jobs: `history NAME` and `logs NAME` resolve NAME against the project's retained runs as well as the current config. `not_found` (with the existing suggestion) happens only when NAME is neither configured nor present in retained history. MCP `list_runs` `job` behaves the same way.
- MCP `list_runs`: optional `status` (array of strings, same validation).
- MCP `get_run`: accept either `run_id` or `job` (with optional absolute `project` and optional `status`). With `job`, select the newest started matching run, using the same rule as `noon logs NAME`. Giving both, or neither, is a usage error. No match is `not_found`. The other `get_run` arguments (wait, log query) apply to the selected run.
- Update docs/design.md (the `history` and `logs` rows) and README.md.

Non-goals: time-range filters, trigger filters, cross-project history, deleting history, and the follow behavior for queued runs (NOON-27).

Tests (critical regressions only):
- internal/cli `TestHistoryAndLogs`: `--status` filters before `--limit`. `logs NAME` skips a newer queued run and reads the latest started run. A removed job's retained runs are readable by name, while a never-seen name is `not_found`. An invalid status exits 2.
- internal/mcp `TestMCP`: `list_runs` with `status`. `get_run` by `job` plus `status` returns the newest matching run. `run_id` together with `job` is a usage error. A `job` with no matching run is `not_found`.

AC fixture: the same isolated prefix as NOON-24 (`t` failed as run 1, `ok` succeeded as run 2). If the shell tool rejects inline syntax, run the exact command from a temporary script and note that.

Fixture prefix (`N` below means `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"`): `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; `

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestHistoryAndLogs|TestMCP)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json history --status failed | jq -c '[.runs[] | [.id, .job, .status]]'; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json history --status failed,succeeded | jq '.runs | length'; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" history --status bogus; echo "exit=$?"` prints `[[1,"t","failed"]]`, then `2`, then a usage error whose hint lists the valid statuses, then `exit=2`
- [ ] #3 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs ok --status failed; echo "exit=$?"` prints `No runs of ok match --status failed.` and `exit=0`
- [ ] #4 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" enable >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --detach >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --tail 1` prints `boom` (the latest started run, not queued run 3)
- [ ] #5 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; printf 'jobs:\n  ok:\n    run: "echo fine"\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" history t && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --tail 1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" history nope; echo "exit=$?"` prints a history row for run 1, then `boom`, then a `not_found` error, then `exit=1`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
