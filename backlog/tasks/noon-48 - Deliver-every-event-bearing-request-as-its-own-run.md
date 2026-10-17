---
id: NOON-48
title: Let per-event jobs queue every request as its own run
status: Done
assignee: []
created_date: '2026-10-16 22:10'
updated_date: '2026-10-17 21:06'
labels:
  - scheduler
  - store
  - http
  - config
  - reviewed
dependencies: []
documentation:
  - docs/triggers.md
  - docs/webhooks.md
  - docs/design.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/app/queue.go
  - internal/store/queue.go
  - internal/store/store_test.go
  - internal/scheduler/queue_test.go
  - internal/httpapi/webhooks.go
  - internal/httpapi/webhooks_test.go
  - noon.schema.json
  - examples/jobs.yaml
  - examples/README.md
  - docs/configuration.md
  - docs/triggers.md
  - docs/webhooks.md
  - docs/design.md
priority: high
type: feature
ordinal: 14500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: a due-now request (`noon run --detach`, MCP `run_job`, webhooks, Git/agent/lifecycle hooks, shell `--on-enter`, TUI queue) for a job that already has a due queued run returns that run and discards the new request's payload (internal/store/queue.go `Coalesce`; webhooks answer 200 `created: false`). That suits refresh jobs such as tests after a commit, but silently loses events for per-event jobs such as "review this pull request" or "diagnose this failed run". examples/ai/pr-review and examples/ai/failure-triage work around it by treating requests as wake-ups and reconciling. docs/triggers.md and docs/webhooks.md mention merging only in passing and never say the payload is lost.

Outcome: a job opts out of merging with `requests: each`. Every due-now request for it creates its own queued run with its own event file, deduplicated only by idempotency key and bounded per job. The default, `requests: merge`, keeps today's behavior exactly, and the docs say plainly that a merged request's payload is dropped.

Settled contract:
- New job field `requests`: `merge` (default) or `each`. It joins the approval digest only when present, so every existing job keeps its digest and approval. It belongs in the digest because it changes how many runs happen (decision-2).
- For `each`, a due-now request never returns another queued run. Existing checks keep their order; the per-job cap takes the place of due-run merging. A repeated idempotency key still returns the existing run with reason `duplicate`, so webhook redelivery stays idempotent while that run is retained.
- Cap: at most 20 queued runs per `each` job, counting all of its queued runs (due or future). At the cap a due-now request fails with code `queue_full` and leaves no run or event file: CLI exit 1 with a hint naming `noon cancel NAME`; MCP tool error; webhook 429 `{"error":"job queue is full"}`. The cap is a constant, not a setting.
- `cooldown` with `requests: each` is a config error (file, line, YAML path): cooldown returns an earlier run and would drop events again.
- The job's setting governs every due-now source; there is no per-request flag. `--in`/`--at` one-shots are already independent and stay unchanged.
- Unchanged: one-hour expiry of overdue queued runs, one active run per project and four overall, FIFO dispatch by due time then ID, scheduled occurrences recorded `skipped` while a due queued run exists, retention of 50 terminal runs per job (keys deduplicate only against retained runs), recursion limits, and `overlap`/`missed` (decision-5). Docs state that a backlog waiting more than an hour expires and shows as `expired`, so per-event jobs that must never miss work should still reconcile.

Scope: parse, validate, and digest `requests`; apply it in the shared queue path (app.QueueRun to store.QueueRun) with the cap checked inside the existing immediate transaction before any event file is written; add `queue_full` to the queue errors and map it in the webhook handler (CLI and MCP error rendering is already generic). Update noon.schema.json (enum plus the cooldown conflict) and examples/jobs.yaml, which uses every field. Docs: configuration.md field table and digest list; a triggers.md section on merge vs each (payload loss, cap, expiry); webhooks.md response table and request handling; design.md queue and trigger bullets; the examples/README.md "Triggers are wake-ups" pattern.

Tests (critical only): each vs merge queueing, key dedupe, and the cap leaving no run or event file; a digest regression asserting that a job without `requests` keeps a literal digest captured before the change (a silent digest change would revoke every approval); webhook 202/200/429 mapping. Extend existing tests in internal/config, internal/scheduler/queue_test.go or internal/store, and internal/httpapi.

Non-goals: per-request merge flags; changing expiry, retention, concurrency caps, dispatch order, or cooldown semantics for merge jobs; capping `--in`/`--at` one-shots of merge jobs; switching examples/ai/pr-review or failure-triage to `each` (TestExampleScripts asserts their merge behavior); an event log separate from runs.

Dependencies: none. Checked against decision-2 and decision-5; no conflict.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test PKG=./internal/config -count=1` exits 0 and covers: `requests: merge` and `requests: each` parse; any other value, and `cooldown` combined with `requests: each`, fail with file:line and YAML path; a job without `requests` keeps a literal digest captured before the change.
- [x] #2 After `task cli:build`, in a temp Git project with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d`, a noon.yaml with `review` (`run: ["true"]`, `requests: each`) and `refresh` (`run: ["true"]`), and `bin/noon enable`: `bin/noon run review --detach --data '{"n":1}' --json` and the same with `{"n":2}` both print `created: true` with different run IDs, and each `.run.event_file` holds its own `data.n`; `--key k1` twice prints `created: true` then `created: false` with `reason: duplicate`; `bin/noon run refresh --detach --json` twice prints `created: true` then `created: false` with `reason: queued`.
- [x] #3 Same fixture, scheduler not running: once `review` has 20 queued runs, `bin/noon run review --detach --data '{"n":99}' --json` exits 1 with `error.code` `queue_full`; `bin/noon history review --status queued --json` still lists 20 runs, and no file under `$NOON_STATE_DIR/events` contains `99`.
- [x] #4 `task cli:test PKG=./internal/httpapi -count=1` exits 0 and covers, for an `each` job: two signed deliveries with distinct delivery IDs get 202 with different run IDs, a redelivery gets 200 with `reason: duplicate`, and a delivery at the cap gets 429 without creating a run. Responses for merge jobs are unchanged.
- [x] #5 `task cli:test PKG=./integration -- -run '^(TestExamplesValidate|TestExampleScripts)$' -count=1` exits 0: examples/jobs.yaml uses `requests` and the shipped examples keep merge semantics. The docs in scope describe both modes, payload loss under merge, the cap, and expiry. `task ci` exits 0.
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
AC#1: task cli:test PKG=./internal/config -- -count=1 -> passed. Covers both modes, invalid value/cooldown path diagnostics, and pre-change literal digest sha256:98dc0d61f34692d1b2b36b24bf272cbcc95c4f81aa817a971f6b700d6857c9cf. Go flags use the documented -- separator.
AC#2: task cli:build; isolated temp Git fixture /tmp/noon-48-ac.kQXBCw with HOME/XDG_CONFIG_HOME/XDG_STATE_HOME/NOON_STATE_DIR under fixture; bin/noon enable; run review --detach --data for n=1 and n=2 -> created true, IDs 1/2 and distinct event data; run review --detach --key k1 twice -> created then duplicate; run refresh --detach twice -> created then queued. All JSON assertions passed.
AC#3: Same fixture, scheduler offline; filled review to 20; run review --detach --data n=99 --json -> exit 1, queue_full and cancel hint. history review --status queued --json -> 20; rg for n=99 in events -> no match. No extra run or payload.
AC#4: task cli:test PKG=./internal/httpapi -- -count=1 -> passed; signed each deliveries 202/202, duplicate 200, cap 429, merge unchanged.
AC#5: task cli:test PKG=./integration -- -run ^(TestExamplesValidate|TestExampleScripts)$ -count=1 -> passed. task ci -> passed in worktree on a4f66d4 and on main merge a1fc771, including security, formatting, vet, Staticcheck, all tests, race, smoke. Schema parses; docs cover default merge payload loss, each cap and expiry.
Delivery: implementation a4f66d4c7188f79d4dfc9143ba2276beabb675ba merged as a1fc771ca1deaba16c777ea9875bd0147a586642. task check:staged passed before both commits. One independent reviewer pass: no validated findings. Changed files stay within contract; no test deleted, skipped or weakened. Source/test LSP diagnostics clean. Worktree, branch feat/noon-48, and exact associated Herdr workspace wFY removed and verified absent. No remaining blocker or resumable implementation step. Nothing pushed.

Post-completion review 2026-10-17: no validated defects. Traced every due-now adapter through app.QueueRun, immediate-transaction queue admission before event-file creation, idempotency before cap, merge payload preservation, future-run counting, unchanged one-shot behavior, cooldown rejection, optional-field digest compatibility, and webhook 202/200/429 mapping. Existing store regression also covers concurrent admission at 19 queued runs. Verification: task cli:test PKG='./internal/config ./internal/store ./internal/scheduler ./internal/httpapi ./internal/cli ./internal/mcp ./integration' -- -count=1 -> all seven packages passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented requests: each with atomic 20-queued-run admission, preserved default merge and existing approvals, key deduplication, queue_full CLI/MCP errors and webhook 429; updated schema, critical regressions and docs. All acceptance checks and full CI passed; merged to main and cleaned owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
