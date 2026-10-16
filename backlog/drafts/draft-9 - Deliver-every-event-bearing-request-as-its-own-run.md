---
id: DRAFT-9
title: Deliver every event-bearing request as its own run
status: Draft
assignee: []
created_date: '2026-10-16 22:10'
labels:
  - scheduler
  - store
  - http
dependencies: []
documentation:
  - docs/triggers.md
  - docs/webhooks.md
  - docs/design.md
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Idea, not ready work. A due-now request (webhook, `noon run --detach`, MCP `run_job`, hook-triggered `noon run`) for a job that already has a due queued run returns that run instead of queueing another (internal/store/queue.go, Coalesce), and the new payload is discarded; a webhook answers `created: false`. That suits refresh-style jobs but loses events for per-event jobs such as "review this pull request" or "diagnose this failed run". The examples (examples/ai/pr-review, examples/ai/failure-triage, commit 3f2c196) work around it by treating requests as wake-ups and reconciling against GitHub or `noon history`.

Questions to settle before this becomes a task: whether reconciliation is the intended pattern (then only document the consequence prominently in docs/triggers.md and docs/webhooks.md, and close this); otherwise, the shape of an opt-out (a per-job field versus never merging requests that carry a distinct payload or idempotency key), its effect on the approval digest, a bound on queued runs per job to resist webhook floods, and interaction with cooldown, one-hour expiry, the one-run-per-project dispatch limit, and retention of 50 terminal runs. Check against decision-2 and decision-5.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
