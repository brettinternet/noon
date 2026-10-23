---
id: NOON-27
title: Follow queued and orphaned runs to a clear end
status: done
type: bug
priority: medium
labels: [cli, mcp, store, reviewed]
blocked_by: [NOON-26]
rank: a4a6b29aca6b29ab2
refs: [internal/app/history.go, internal/app/errors.go, internal/store/store.go, internal/runner/process.go, internal/cli/queue_test.go, internal/store/store_test.go, docs/design.md, docs/development.md]
files: [internal/store/**, internal/app/**, internal/cli/**, internal/mcp/**, README.md, docs/design.md]
created: 2026-10-16T17:26:00Z
updated: 2026-10-16T20:39:00Z
---

Outcome: `noon logs RUN --follow` and MCP `get_run` with `wait_seconds` always reach a clear end. Missing-log errors say why the log is missing.

Why (all verified on the current main branch):
1. `noon run t --detach` prints `Queued t as run 3`. Then `noon logs 3` and `noon logs 3 --follow` both fail at once with "log for run 3 is unavailable / Logs are kept for the newest 10 runs per job". The run is queued, not pruned, so the message is wrong, and following a run you just queued is the obvious next step.
2. If the `noon run` process that owns a run is SIGKILLed, the run row stays `running`. Recovery happens only at `noon serve` startup, foreground `noon run`, and `noon cancel`. `logs --follow` then loops forever and `get_run` waits out its full `wait_seconds` (a NOON-17 review finding). `noon cancel RUN` already records such an orphan correctly (verified: it prints `Cancelled run 4.` and the row becomes `cancelled`).
3. Runs that never started (`skipped`, `missed`, `expired`, or `cancelled` before starting) report the misleading retention hint.

Read first: docs/design.md#execution (cancellation and recovery) and #state, NOON-17's Implementation Notes (review findings), and NOON-25 (the follow start offset). In code, read `FollowLog`, `GetRun`, and `activeStoredRun` in internal/app/history.go, `MissingRunLogError` in internal/app/errors.go, and `store.Recover`/`RecoverRuns` and `runner.ProcessOwnerAlive` for the owner identity check. Inspection commands must stay read-only: never call recovery or mutate state from `logs` or `get_run`.

Scope:
- Missing-log messages, chosen by run state, all exit 1 with code `not_found`:
  - queued: `run ID is queued and has not started`, hint `Follow it: noon logs ID --follow`.
  - never started (no `started_at`, terminal status): `run ID was STATUS and never started, so it has no log`.
  - pruned: the existing message and hint.
  The same wording appears in MCP errors where `get_run` reports an unavailable log for these cases. A queued `get_run` still succeeds with an empty `log`.
- `logs RUN --follow` on a queued run: print one stderr line `noon: run ID is queued; waiting for it to start`, poll its status every second, and once it starts, follow it as usual. If it reaches a terminal status without starting, exit 1 with the never-started message above. Ctrl+C stops waiting, as following does today.
- Orphan detection: an active run (status `running`, or hook `running`) is orphaned when its recorded owner fails the same identity check `store.Recover` uses (`runner.ProcessOwnerAlive` with `owner_pid`/`owner_start`). To avoid racing a normal finish, re-read the run after a failed check and treat it as orphaned only if it is still active.
  - `--follow`: check on each status poll. When orphaned, print any remaining bytes, then exit 1 with a new error code `orphaned`, message `run ID's owner process exited without recording a result`, hint `Record it: noon cancel ID`.
  - MCP `get_run` with `wait_seconds`: stop waiting as soon as the run is orphaned and return the detail with a new boolean `orphaned: true` (false otherwise).
- Document the `orphaned` error code and the queued-follow behavior in docs/design.md, and in README.md where follow is described.

Non-goals: periodic recovery in the serve loop, auto-cancelling orphans from read commands, changing the cancellation or recovery semantics, and a timeout flag for `--follow`.

Tests (critical regressions only):
- internal/cli `TestLogsFollow`:
  - Following a queued run that is then cancelled exits 1 within 2s with the never-started message.
  - Following an orphaned run exits 1 within 3s with code `orphaned`, after printing the output the job wrote before its owner died. For fixture patterns, see `TestCancelForeground` in internal/cli/queue_test.go and `TestRecovery`/`TestConcurrentRecoveryOfSameCancelledOrphan` in internal/store/store_test.go.
  - Following a queued run that the scheduler then starts streams its output to the end. Reuse the in-process scheduler pattern from internal/cli/serve_test.go; if that is impractical, record why in Implementation Notes and cover the transition at the app layer instead.
- internal/cli `TestHistoryAndLogs`: the queued, never-started, and pruned messages.
- internal/mcp `TestMCP`: `get_run` with `wait_seconds: 10` on an orphaned run returns within 3s with `orphaned: true`.

AC fixture: the same isolated prefix as NOON-24, whose config also defines `slow` (prints `started`, then sleeps 30s). `t` is run 1 and `ok` is run 2, so the next run is 3. If the shell tool rejects inline syntax, run the exact command from a temporary script and note that.

Fixture prefix (`N` below means `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"`): `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; `

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.

## Acceptance criteria

- [x] `task cli:test -- -run '^(TestLogsFollow|TestHistoryAndLogs|TestMCP)$' -count=1 -v` exits 0 and prints `--- PASS` for each, covering every case listed under Tests
- [x] `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" enable >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --detach >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs 3; echo "exit=$?"` prints `noon: run 3 is queued and has not started`, then `Hint: Follow it: noon logs 3 --follow`, then `exit=1`
- [x] `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" enable >/dev/null && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --detach >/dev/null; (sleep 1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" cancel 3 >/dev/null) & env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs 3 --follow; echo "exit=$?"` prints `noon: run 3 is queued; waiting for it to start`, then within about 2s `noon: run 3 was cancelled and never started, so it has no log`, then `exit=1`
- [x] `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run slow >/dev/null 2>&1 & noon_ac_pid=$!; sleep 1; kill -9 "$noon_ac_pid"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs 3 --follow; echo "exit=$?"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" cancel 3` prints `started`, then `noon: run 3's owner process exited without recording a result` with `Hint: Record it: noon cancel 3`, then `exit=1` within about 3s, then `Cancelled run 3.`

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Inspect follow/get_run and owner identity handling; implement read-only queued waiting and orphan detection with state-specific missing-log errors.
2. Add critical CLI/MCP regressions and update follow/error documentation.
3. Run all acceptance commands and task ci, perform one focused review, commit, merge main, and clean up the owned worktree.

## Notes

Implemented queued follow, read-only owner checks (including hooks), orphaned detail/error, state-specific missing-log errors, and documentation. Existing MCP pruned and never-started empty-detail successes are preserved, as required by existing tests; it has no unavailable-log error path to reword. No files outside the modified-file contract changed and no tests were removed, skipped, or weakened.
AC#1: task cli:test -- -run '^(TestLogsFollow|TestHistoryAndLogs|TestMCP)$' -count=1 -v -> exit 0, all three PASS; covers queued cancellation within 2s, scheduler dispatch/output completion, owner death during follow within 3s, orphan error code, read-only state, and MCP run/hook orphan waits within 3s.
AC#2: exact acceptance command (task cli:build, isolated fixture, enable, run t --detach, logs 3) via bash /tmp/noon-27-ac2.sh -> queued message, Follow it hint, exit=1.
AC#3: exact acceptance command via bash /tmp/noon-27-ac3.sh -> one waiting notice, never-started cancellation message within about 2s, exit=1.
AC#4: exact acceptance command via bash /tmp/noon-27-ac4.sh -> started, orphaned owner message and Record it hint, exit=1 within about 3s, Cancelled run 3. Temporary scripts preserve the full acceptance commands literally.
Validation: task ci passed (tests, race, vet, staticcheck, vulnerabilities/secrets, smoke); LSP diagnostics clean for app history and changed CLI/MCP tests. Single direct risk-focused review found no additional item-scoped defect. Independent reviewer could not launch: requested model absent, available model excluded by configured modelScope; no agent ran. Next: commit implementation, run CI on that commit, merge main, finalize provider and remove owned checkout.

Delivery complete: implementation e797e6f passed task check:staged and task ci on the committed tree, then fast-forward merged to main at e797e6f. Verified creation receipt and Worktrunk listing; removed owned noon-27-follow worktree and branch through Worktrunk. Matching Herdr workspace wDD contained only its idle shell and was removed by the post-remove hook. No retained task checkout, no blocker, no next implementation step. Nothing pushed.

Post-implementation review: inspected FollowLog/GetRun, owner-identity checks against recovery, missing-log errors, CLI/MCP adapters and transition regressions. No actionable defects found. Confirmed queued dispatch/cancellation, run/hook orphan detection, remaining-output delivery and read-only inspection. Verification: task cli:test -- -run '^(TestLogsFollow|TestHistoryAndLogs|TestMCP|TestHooks|TestStore|TestSelectLog|TestSkillFlags)$' -count=1 passed; mise exec go -- go test -race ./internal/app ./internal/cli ./internal/mcp -run 'TestLogsFollow|TestMCP|TestHooks|TestSelectLog|TestReadRunLog' -count=1 passed. No source changes.

## Summary

Queued follow now waits and streams to completion; cancelled-before-start runs explain why no log exists. Follow and MCP detect orphaned run/hook owners without recovery writes. All four acceptance commands and committed-tree CI pass. Implemented and merged as e797e6f; owned worktree/branch cleaned up.
