---
id: NOON-22
title: Run lifecycle hooks after jobs finish
status: Done
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 18:32'
labels:
  - integration
  - reviewed
dependencies:
  - NOON-4
  - NOON-17
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/config/**
  - internal/runner/**
  - internal/store/**
  - internal/scheduler/**
  - internal/app/**
  - internal/cli/**
  - docs/hooks.md
  - docs/configuration.md
  - README.md
priority: medium
type: feature
ordinal: 1600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users get notified, or chain follow-up work, when jobs succeed or fail, using any command, without Noon shipping notification providers.

Read first: docs/design.md#integrations (Hooks) and #security-model, decision-4.

Scope:
- Job fields `on_success` and `on_failure`: a string (`/bin/sh -c`) or a list (argv), validated like `run`. An empty list disables an inherited default. User config (and an explicit `--config` file) may set top-level `on_success`/`on_failure` defaults for every job; project files may not set top-level defaults. Hooks defined on shared jobs are part of the digest.
- When: `on_success` after `succeeded`; `on_failure` after `failed` or `timed_out`. No hook for other statuses, and never a hook for a hook.
- How: for every trigger, through the execution path shared by `noon run` and the scheduler, after the run is recorded as finished and outside the concurrency caps. cwd and environment are those of the job, plus `NOON_STATUS`, `NOON_EXIT_CODE`, and `NOON_LOG_FILE` (absolute path, or unset when no log). stdin is `{"schema_version":1,"project":{"path","name"},"run":RUN}`, where RUN is the shared run JSON object. Timeout 30s, then TERM, a 5s grace, and KILL on the hook's process group.
- Record: append the hook's combined output (first 64 KiB) to the run's log after a marker `[noon: on_failure hook exited 1]` (or `timed out`), and store `hook_status` (`running`, `ok`, `failed`, `timed_out`, or `interrupted`) and `hook_exit_code` on the run (migration). When a hook will run, the transaction that records the run's terminal status also sets `hook_status` to `running`, and `logs --follow` keeps following until it is no longer `running`. Recovery marks a `running` hook whose owner is gone as `interrupted`. A hook failure never changes the run's status or a foreground run's exit code. Run JSON gains `hook: {"event","status","exit_code"}` or null.
- A foreground `noon run` prints one stderr line after the hook, such as `on_failure hook: ok` or `on_failure hook: exit 1`.
- Add docs/hooks.md with tested examples: a macOS notification via `osascript`, Linux `notify-send`, and posting the summary to a chat webhook with `curl --data-binary @-`. Each example reads `$NOON_JOB` and `$NOON_STATUS` or stdin, never interpolating untrusted output into a shell string.

Non-goals: built-in notifiers, retries, hooks on start, hook chains, per-trigger hooks, templating.

Tests:
- internal/cli `TestHooks`, with fake hook scripts that write their stdin and env to files: success runs only `on_success` with valid JSON stdin; failure and timeout run `on_failure`; an empty list disables the user default; a hook that sleeps past the timeout is killed and recorded while the run status stays unchanged; hook output beyond 64 KiB is cut; `logs --follow`, started while the job runs, prints the output of a hook that sleeps 1s before writing, then exits.
- Extend `TestJobDigest`: changing a shared job's hook changes its digest.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestHooks|TestJobDigest)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "exit 3"\n    on_failure: [sh, -c, "cat > hook.json"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t; echo "exit=$?"` prints `exit=3`, and `$noon_ac_project/hook.json` contains the run summary JSON, whose `run.status` is `failed`
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
1. Inspect configuration, execution, persistence, and log-follow contracts. 2. Implement validated hooks and durable lifecycle state in the shared execution path. 3. Add focused regression coverage and documentation. 4. Run acceptance commands and task ci, review, commit, merge, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:test -- -run '^(TestHooks|TestJobDigest)$' -count=1 -v -> exit 0; both PASS. Covers success/failure/job timeout, inherited-default disabling, hook timeout/process group, bounded output, log following from a running job, and hook digest changes. AC#2: exact isolated build/run fixture executed via /tmp/noon-22-ac2.sh -> exit=3; jq confirmed hook.json run.status=failed (fixture /var/folders/jp/1mwx72h172955139pth4h8800000gn/T/tmp.KCH21Nsigd). Single independent review found hook log-write errors replacing job exit; corrected and regression tested. Retention now runs after hook completion so cleanup failure cannot prevent execution after the running-hook transition; existing retention-failure regression extended. Direct permission-denied fixture now returns job exit 7 with failed hook/1 JSON. Documented wrappers tested with fake osascript, notify-send, curl: quoted argv and stdin preserved, no external notifications/network. All paths remain within modified-file contract; no tests weakened. Executor timed out after its CI passed; parent resumed and completed focused corrections. No outstanding review findings.

Delivery: implementation commit dd965cf passed task ci (security/vulnerability scans, formatting/vet/staticcheck, all tests, race tests, CLI smoke) and was fast-forward merged into main. Clean session-owned noon-22-hooks worktree and branch removed via Worktrunk; matching Herdr workspace removed. No blockers or remaining item-scoped work. Final backlog-only commit follows; no push requested or performed.

Review (post-merge): hook selection by status, source restriction of top-level defaults, empty-list disabling, durable running transition before execution, job cwd/env plus NOON_STATUS/EXIT_CODE/LOG_FILE, JSON stdin, 30s+5s group kill, 64 KiB capture with marker, follow-through-hook, recovery to interrupted, unchanged job status/exit code, and argv-safe docs examples match the contract. Fixed in 4d39aea: inherited hook defaults from user config were folded into every shared job digest (with a test asserting it), so adding or editing a personal notify hook revoked approval of every shared job in every enabled project and halted their schedules until noon enable was rerun (reproduced with the built binary: needs approval after adding on_failure to user config). The spec digests hooks defined on shared jobs, and user-controlled agent commands are likewise excluded; now only job-defined hooks, or defaults from an approval-gated explicit --config file, join the digest. The subtest now asserts both directions; docs/configuration.md and docs/hooks.md updated. Users who already approved jobs with a user default hook re-approve once. Low, not fixed: hooks run under context.Background, so scheduler shutdown waits up to 35s for an in-progress hook and a second signal does not force-kill it. AC1 rerun and passes; task ci passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented lifecycle hooks with source-aware defaults and approval digests, shared foreground/scheduler execution, durable hook status/recovery, bounded logs and follow, preserved job exit codes, and notification/webhook examples. AC1 and AC2 pass; full CI passes on implementation commit dd965cf. Independent review finding fixed with regressions. Merged locally and cleaned owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
