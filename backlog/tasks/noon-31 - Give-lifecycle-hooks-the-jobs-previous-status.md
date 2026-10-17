---
id: NOON-31
title: Give lifecycle hooks the job's previous status
status: Done
assignee: []
created_date: '2026-10-16 22:07'
updated_date: '2026-10-17 01:56'
labels:
  - hooks
  - store
  - reviewed
dependencies: []
references:
  - internal/app/hooks.go
  - internal/store/store.go
documentation:
  - docs/hooks.md
  - docs/design.md
modified_files:
  - internal/app/hooks.go
  - internal/store/store.go
  - internal/store/store_test.go
  - internal/cli/run_test.go
  - docs/hooks.md
  - docs/design.md
priority: medium
type: feature
ordinal: 3200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: lifecycle hooks receive the previous job outcome so user-owned notification commands can report the first failure and recovery without querying Noon history.

Why: hooks run after every eligible result. Noon should supply a fact, not add notification policy, retries, throttling, or schedule backoff. Keep this a small extension of the shared foreground/scheduler hook path (decision-4).

Design:
- Eligible history is retained runs of the same canonical project and job name with status succeeded, failed, or timed_out, whether or not they had hooks. Ignore cancelled, interrupted, skipped, missed, expired, queued, and running. Job-definition digest changes do not reset this history.
- "Previous" means the latest eligible completed result available immediately before the current run's terminal transition: order by finished_at descending, then id descending to break equal stored timestamps. Exclude the current run, NOT all higher IDs. A queued one-shot receives its ID before execution and may run after a higher-ID manual run.
- Capture once while the current run still owns its running slot, or inside its terminal transaction, and reuse for both outputs. Looking up unbounded latest history after FinishRun releases that slot can observe a subsequent execution. No new migration, persisted delivery state, or change to history ordering is needed.
- NOON_PREVIOUS_STATUS is the raw status, or UNSET when no eligible history remains. Remove inherited/job-configured values even when absent. This follows existing optional hook metadata (setHookEnvironment removes empty values); do not change NOON_EVENT_FILE/NOON_LOG_FILE semantics.
- Hook stdin gains top-level previous_status: a string or explicit null under schema_version 1. The nested run stays identical to shared run JSON; do not add this hook-only context to CLI/MCP history.
- Look up only when a matching hook will run. A query error is not missing history: still attempt to record the child's actual terminal outcome, surface the state error, and do not invoke the hook with fabricated null or leave a never-started hook marked running. Existing state-error reporting applies. Successful lookups preserve durable completion-before-hook execution, hook time/output limits, and job exit-code semantics.

Notification examples in docs/hooks.md remain policy, not core behavior: group failed and timed_out as unhealthy, succeeded as healthy. With both hooks configured, notify on the first unhealthy result and on return to healthy; a first success stays quiet. Switching failed to timed_out is not a new outage. Unknown/pruned history is treated as healthy by these wrappers. Raw-status-change notification is a different optional policy. This is not guaranteed delivery: failed hooks are not retried, and pruning can make an old outage appear new.

Read first: docs/hooks.md, docs/design.md#integrations, NOON-22, internal/app/hooks.go, store.LastSuccess and FinishRun in internal/store/store.go. Reuse store query conventions, not LastSuccess's ID-based ordering.

Scope: one bounded store lookup, shared hook env/payload plumbing, the macOS/Linux wrappers and contract documentation in docs/hooks.md and docs/design.md.

Critical regressions to extend after implementation:
- TestHooks: failed, failed, succeeded yields absent/failed/failed in env and null/failed/failed in JSON; inherited and job-configured values never leak; nested run JSON is unchanged.
- TestStore: identity isolation, timed_out eligibility, ignored statuses, absent retained history, and queue-order inversion (a lower-ID delayed run sees the higher-ID result completed before it). Use supplied timestamps, including deterministic tie behavior.
- Review the shared call ordering: capture before the terminal transition, reuse the value after it. Do not add timing-sensitive race tests merely to restate that ordering.

Non-goals: notification deduplication/delivery guarantees, retry/backoff, new hook events, config policy, previous run IDs/logs, metadata injection into job environments, a schema bump, or changing LastSuccess/history/retention ordering.

Dependencies: none; NOON-22 is implemented. NOON-36 adopts the fact in the shipped notification example.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestHooks|TestStore)$' -count=1 -v` exits 0 and prints PASS for both tests, covering the env/JSON agreement, cleared inherited values, identity/status filtering, missing history, and queue-order inversion above.
- [x] #2 From the repository root, `(set -eu; task cli:build; noon_ac_home=$(mktemp -d); noon_ac_project=$(mktemp -d); printf 'jobs:\n  flip:\n    run: "test -e ok"\n    on_success: [sh, -c, "echo ${NOON_PREVIOUS_STATUS:-none} $NOON_STATUS >> hooks.txt"]\n    on_failure: [sh, -c, "echo ${NOON_PREVIOUS_STATUS:-none} $NOON_STATUS >> hooks.txt"]\n' > "$noon_ac_project/noon.yaml"; for noon_ac_attempt in 1 2; do noon_ac_code=0; env -i PATH="$PATH" HOME="$noon_ac_home" NOON_PREVIOUS_STATUS=leaked bin/noon -C "$noon_ac_project" run flip >/dev/null 2>&1 || noon_ac_code=$?; test "$noon_ac_code" -eq 1; done; touch "$noon_ac_project/ok"; env -i PATH="$PATH" HOME="$noon_ac_home" NOON_PREVIOUS_STATUS=leaked bin/noon -C "$noon_ac_project" run flip >/dev/null 2>&1; test "$(cat "$noon_ac_project/hooks.txt")" = "$(printf 'none failed\nfailed failed\nfailed succeeded')")` exits 0: foreground results are failure/failure/success, and hook output is exactly none failed, failed failed, failed succeeded. Setup errors or unexpected exits fail the command.
- [x] #3 `rg -n 'NOON_PREVIOUS_STATUS|previous_status' docs/hooks.md docs/design.md` shows the env and top-level JSON fields, unset/null and snapshot-order semantics. Both notification wrappers group failed/timed_out into one outage and explain first-success silence, recovery, retention, and lack of notification retries.
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
1. Add a retained-result lookup and capture hook-only previous status before terminal completion. 2. Extend critical store and hook regressions and document notification policy. 3. Run acceptance commands and CI, commit, merge into main, finalize task, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Design refinement: QueueRun allocates IDs before dispatch, so lower-ID history can miss the actual prior execution. FinishRun releases the same-job running slot before hook execution, so take one pre-transition snapshot. setHookEnvironment already unsets absent metadata; align with that rather than promise an exported empty string. Keep raw status in core, but group failed/timed_out in the first-failure/recovery wrappers. No implementation or passing acceptance is claimed.

AC#1: task cli:test -- -run '^(TestHooks|TestStore)$' -count=1 -v -> exit 0, both PASS. Covers env/null agreement, inherited clearing, rejection of job-configured reserved metadata, unchanged nested JSON shape, identity/status filtering, missing/pruned history, completion ordering and timestamp ties including higher IDs before a delayed queued ID. AC#2: exact command executed as sh /tmp/noon-31-ac2.sh from implementation root -> exit 0; failure/failure/success and none failed / failed failed / failed succeeded verified. AC#3: rg -n 'NOON_PREVIOUS_STATUS|previous_status' docs/hooks.md docs/design.md -> exit 0; contracts documented. Both extracted desktop wrappers passed 8 first-failure/recovery scenarios with fake osascript/notify-send via bun /tmp/noon-31-wrappers.js; no network/notifications. Full task ci passed before and after implementation commit 2d3d376 (security, vulnerabilities, formatting/vet/staticcheck, tests, race, smoke); LSP clean. One independent reviewer pass: no validated findings; capture-before-FinishRun and error completion semantics reviewed. Six implementation paths match contract, no tests weakened. Existing configuration rejects all NOON_ env keys, so job-configured leak coverage asserts rejection rather than relaxing that safety rule. Delivery: 2d3d376 fast-forward merged into main; session-owned worktree/branch removed via Worktrunk. No blockers or further item-scoped steps; no push requested.

Post-integration verification: task ci passed on main at 949dcc7. First main run hit an unrelated existing TestServeQueued/running_cancellation_kills_the_process_group EOF at queue_test.go:113: fixture waits for PID file existence before reading its contents. No source changes made; focused subtest passed 10 consecutive runs and full CI rerun passed including race tests. Owned Herdr workspace wE4 disappearance verified; pre-existing feat/tui-design worktree/workspace preserved as unrelated. Final evidence-only commit follows.

Review 2026-10-17: no defects. Snapshot is taken before FinishRun under the runs_one_running unique index; fixed-width UTC ms timestamps sort correctly; a lookup error skips the hook but still records the outcome. Focused TestHooks/TestStore pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Hooks now receive one pre-completion previous-result snapshot as NOON_PREVIOUS_STATUS and top-level previous_status, with cleared unset/null history and preserved shared run JSON. Added ordering/isolation regressions and first-failure/recovery wrappers. All acceptance checks, full CI on 2d3d376, and independent review pass; merged locally and cleaned owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
