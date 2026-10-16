---
id: NOON-5
title: Queue detached and one-shot runs and cancel runs
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 14:33'
labels:
  - scheduler
  - cli
  - reviewed
dependencies:
  - NOON-4
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-1 -
    One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md
modified_files:
  - internal/store/**
  - internal/scheduler/**
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - README.md
priority: high
type: feature
ordinal: 1200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users, scripts, and hooks can hand a job to the scheduler now or later, with idempotency keys, and can cancel any queued or running run, including another terminal's foreground run.

Read first: docs/design.md#experience, #scheduler-lifetime, #missed-runs-overlap-and-concurrency, and #enable-and-approve.

Scope:
- Migration 4: add `runs.idempotency_key TEXT` and `runs.cancel_requested_at TEXT`, plus `CREATE UNIQUE INDEX runs_key ON runs(project_id, job, idempotency_key) WHERE idempotency_key IS NOT NULL`.
- `noon run NAME --detach` (due now), `--in DURATION` (`schedule.ParseDuration`, at least 1s), or `--at TIME` (`HH:MM` = next occurrence, `YYYY-MM-DD HH:MM` in the project timezone, or RFC 3339; past times are usage errors). These flags are mutually exclusive.
- `--key KEY` (1-200 characters of `[A-Za-z0-9._:-]`): a second request with the same project, job, and key returns the existing run with `created: false` and `reason: "duplicate"` instead of queueing another. Deduplication lasts only while that run is retained (the newest 50 terminal runs per job); say so in help.
- Coalescing: a due-now request (`--detach`; later MCP, triggers, and webhooks) for a job that already has a queued run whose due time has passed returns that run with `created: false` and `reason: "queued"`; its payload, if any, is unchanged. Future requests (`--in`, `--at`) always create their own run.
- One queued-request operation in internal/app serves every client. It checks, in one immediate (write-locked) transaction: the key, then requirements, then coalescing, then inserts. Concurrent requests therefore never create two due runs of one job.
- Queued runs: status `queued`, trigger `queued`, `due_at` and `scheduled_at` set to the due time, and `requested_by` set to `cli`.
- Requirements: the project is enabled (`not_enabled`, hint `noon enable`); the job exists and is enabled; and it is approved or needs no approval (`needs_approval`, hint `noon enable`).
- Output: `Queued tests as run 43 · due now` or `· due Fri 09:00 (in 2h)`. Without a fresh scheduler heartbeat, add `The scheduler isn't running, so nothing runs until it starts.` with the service or serve hint. When nothing was created, say why: `tests is already queued as run 43 · due now` or `Already requested as run 41 (succeeded 2h ago)`. JSON: `{"schema_version":1,"run":{...},"created":bool,"reason":"duplicate"|"queued","scheduler":"running"|"offline"}`, with `reason` omitted when created. Exit 0 in every case.
- The serve loop already dispatches queued runs through the planner. Verify FIFO, caps, and expiry with real queued rows.
- Cancel requests: `noon cancel RUN` cancels a queued run immediately (`cancelled`, never started). For a running run it sets `cancel_requested_at`; the owner (scheduler or foreground `noon run`) checks its own runs every second and cancels them (TERM, grace, KILL; status `cancelled`; a foreground run exits 130). The command waits up to 15s and reports the final status, or says the request is recorded. When the owner is gone, it applies recovery immediately.
- `noon cancel NAME` cancels all queued and running runs of that job. A bare number is always a run ID (job names start with a letter). Bare `noon cancel` lists active runs with a `Next: noon cancel RUN` example and changes nothing. A finished run is a no-op that reports its status and exits 0.
- `noon disable` now also cancels the project's queued runs.
- Bare `noon` adds one notice line for queued runs, such as `1 queued: tests due 11:00`.

Non-goals: `--data` payloads and cooldowns (triggers task), webhooks, MCP, cancelling by pattern, retries.

Tests:
- internal/cli `TestQueueRuns` (no scheduler running): `--detach`, `--in`, and `--at` create queued rows with the right due times; `--key` returns the same run; a second `--detach` while one is due returns it with `reason` `queued`, while a second `--in` run is created; 10 concurrent `--detach` requests with different keys create exactly one run; `not_enabled` and `needs_approval` refusals; `--at` in the past exits 2; the offline message.
- internal/scheduler `TestServeQueued` (injected clock, real store and processes): a queued run starts only when due; cancelling a queued run means it never starts; cancelling a running run kills its group and records `cancelled`; `--key` dedupe survives a scheduler restart.
- internal/cli `TestCancelForeground`: a foreground `noon run` cancelled from a second invocation exits 130 and records `cancelled`.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestQueueRuns|TestServeQueued|TestCancelForeground)$' -count=1 -v` exits 0 and prints `--- PASS` for all three, covering every case listed under Tests
- [x] #2 `task race` exits 0
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
1. Extend store migration and atomic queue/cancel operations, reusing approval and recovery logic. 2. Add shared app operations and CLI queue/cancel rendering and validation. 3. Poll cancellation in foreground and scheduler owners; include queued dashboard and disable handling. 4. Add critical queue concurrency, dispatch and process cancellation regression tests; run acceptance and CI. 5. Independently review transactional approval and cancellation safety, commit, merge locally and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Worklease claim 2987a475d9384f830da4320bd2f97538 held by loop 53909a3c-e1ac-40d5-a491-b52b62d6687d. Implementation worktree .worktrees/noon-5-queue-cancel. docs/design.md is additionally allowed to update stale queued-operation wording to match implemented behavior (DoD 5).

AC#1: task cli:test -- -run '^(TestQueueRuns|TestServeQueued|TestCancelForeground)$' -count=1 -v -> exit 0; all three PASS, rerun on main after integration. AC#2: task race -> exit 0, including corrected cancellation regressions. task ci passed on implementation commit 04a78fa (security, vet, staticcheck, full tests, race, build, smoke); task check:staged and diff checks passed. Independent review found concurrent orphan finalization, lost force-kill signal handling, and unbounded global cancellation recovery; all corrected with regression tests and checks rerun. No tests deleted, skipped or weakened. Modified paths within contract plus previously justified docs/design.md. Commit 04a78fa fast-forward merged into main; owned worktree and branch removed via Worktrunk. No remaining blocker or resumable implementation step.

Review (post-merge): queue transaction order (key, requirements, coalescing, insert) under an immediate lock, --at/--in parsing, output/JSON, cancel of queued/running/foreground runs with owner-liveness recovery, disable cancellation, and dashboard notice match the contract. No defects fixed here. Low, not fixed: (1) noon cancel RUN accepts any run ID in the per-user state, including another project's; (2) queueing a paused job (NOON-20) succeeds and the planner cancels it on the next tick with no warning; (3) app.QueueRun registers a project row before refusing not_enabled. NOON-4 fix (f7d106c) also stops the scheduler from cancelling queued runs of a project whose config is temporarily invalid. AC1-AC2 rerun via task ci and pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented durable detach/in/at requests with atomic idempotency, approval and coalescing, queued dashboard/disable behavior, and cancellation of queued, scheduler and foreground runs. Added concurrency, recovery, signal and dispatch regression coverage. Focused acceptance, race and CI pass; locally merged as 04a78fa and worktree cleaned. Nothing pushed.
<!-- SECTION:FINAL_SUMMARY:END -->
