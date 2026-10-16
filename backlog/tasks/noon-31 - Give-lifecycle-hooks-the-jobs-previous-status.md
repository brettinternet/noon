---
id: NOON-31
title: Give lifecycle hooks the job's previous status
status: To Do
assignee: []
created_date: '2026-10-16 22:07'
labels:
  - hooks
  - store
dependencies: []
references:
  - internal/app/hooks.go
  - internal/store/store.go
documentation:
  - docs/hooks.md
  - docs/design.md
modified_files:
  - internal/app/**
  - internal/store/**
  - internal/cli/**
  - docs/hooks.md
priority: medium
type: feature
ordinal: 3200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a lifecycle hook can tell whether a job's outcome changed, so notification hooks report the first failure and the recovery instead of every run.

Why: on_success runs after every successful run and on_failure after every failed one. A notification hook on a job that runs every five minutes notifies every five minutes, both during an outage and in normal operation. Today a hook must run noon history NAME --json and parse it inside its 30-second budget, and every notification hook repeats that logic. Noon already holds the history when it starts the hook. Alternatives considered and rejected: slowing a failing job's schedule (delays noticing recovery; the next scheduled run already acts as a retry), retries (a different problem: transient failures), and throttling or a notification policy in Noon config (policy belongs in the user's hook; Noon supplies the fact). Callers such as Tell stay stateless and rely on this.

Definition: the previous status is the status of the most recent earlier run (lower id) of the same project and job whose status is succeeded, failed, or timed_out, the statuses that select a hook. Cancelled, skipped, expired, interrupted, missed, queued, and running runs are ignored. It is absent when no such run is retained (first run, or pruned by retention).

Read first: docs/hooks.md, NOON-22, internal/app/hooks.go (FinishRunWithHook sets NOON_EVENT_FILE and NOON_LOG_FILE to an empty string when absent so inherited values never leak), and store.LastSuccess in internal/store/store.go. Today an inherited NOON_PREVIOUS_STATUS reaches hooks unchanged; the acceptance fixture currently prints leaked failed, leaked failed, leaked succeeded.

Scope:
- Hook environment: NOON_PREVIOUS_STATUS, always set, empty when absent.
- Hook stdin JSON: top-level previous_status (string or null), additive under schema_version 1. run keeps the history JSON shape.
- A store query beside LastSuccess.
- docs/hooks.md: document both, and change the macOS and Linux notification wrappers to notify only on change, treating no history as success so a first success stays quiet: [ "$NOON_STATUS" = "${NOON_PREVIOUS_STATUS:-succeeded}" ] && exit 0. Note that the wrapper must be configured for both on_success and on_failure to report recovery.

Non-goals: schedule backoff, retries, throttling, deduplication, notification policy in config, new hook events (on_change or on_recovery), the previous run's ID or log, NOON_PREVIOUS_STATUS in the job (non-hook) environment, a schema_version bump.

Tests (critical regressions only):
- internal/cli TestHooks: failure, failure, success records previous statuses empty, failed, failed in both the environment and previous_status (null for the first); an inherited NOON_PREVIOUS_STATUS does not reach the first hook.
- internal/store TestStore: the query ignores non-hook statuses and the current run, and reports absent when no earlier hook-eligible run exists.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestHooks|TestStore)$' -count=1 -v` exits 0 and prints `--- PASS` for each, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  flip:\n    run: "test -e ok"\n    on_success: [sh, -c, "echo ${NOON_PREVIOUS_STATUS:-none} $NOON_STATUS >> hooks.txt"]\n    on_failure: [sh, -c, "echo ${NOON_PREVIOUS_STATUS:-none} $NOON_STATUS >> hooks.txt"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" NOON_PREVIOUS_STATUS=leaked bin/noon -C "$noon_ac_project" run flip >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" NOON_PREVIOUS_STATUS=leaked bin/noon -C "$noon_ac_project" run flip >/dev/null 2>&1; touch "$noon_ac_project/ok" && env -i PATH="$PATH" HOME="$noon_ac_home" NOON_PREVIOUS_STATUS=leaked bin/noon -C "$noon_ac_project" run flip >/dev/null 2>&1; cat "$noon_ac_project/hooks.txt"` prints `none failed`, `failed failed`, then `failed succeeded`
- [ ] #3 `rg -n 'NOON_PREVIOUS_STATUS|previous_status' docs/hooks.md` shows the environment variable, the JSON field, and the change-only check in both the macOS and Linux notification wrappers
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
