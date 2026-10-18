---
id: DRAFT-8
title: Chain follow-up jobs through completion hooks
status: Draft
assignee: []
created_date: '2026-10-16 19:20'
updated_date: '2026-10-18 16:38'
labels:
  - integration
  - hooks
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote only when a concrete follow-up workflow exposes a gap in the existing lifecycle hooks, then rewrite to the ready-task standard with executable acceptance commands.

Problem: workflows such as build then inspect artifacts, or failed test then produce a diagnostic summary, need a follow-up job with access to the upstream result. Existing on_success/on_failure commands can already queue another job; that composition already ships in examples/local-dev/pipelines and examples/ai/failure-triage, with offline execution coverage completed in NOON-35. Require a demonstrated remaining gap rather than inventing a new workflow engine.

Candidate scope: verified recipes, and only demonstrated small gaps, for queueing an existing approved downstream job from completion hooks. Carry upstream run ID, project, terminal status, and retained log/event references as structured data. Use an idempotency key scoped to the upstream run and downstream job so retries do not create duplicate follow-ups while retained. Preserve the current durable completion-before-hook ordering and recorded hook failure behavior.

Safety: downstream requests honor project enablement, approval, pause, and queue semantics. Preserve same-job/depth recursion guards; document that they intentionally bound chain length. Hook failure or downstream failure must not rewrite upstream success/failure. Cancellation/interruption must not unexpectedly become failure triggers.

Non-goals: DAG scheduling, fan-out/fan-in, cross-project orchestration, automatic retries, transactional exactly-once delivery, artifact transfer/storage, or weakening recursion protection.

Promotion questions: are recipes sufficient; which follow-up workflow is needed; key retention and replay expectations; missing/pruned upstream logs; crash between durable upstream completion and enqueue; surfacing hook/enqueue failure versus downstream failure; interaction with cooldown and due-now coalescing.

Dependencies/context: NOON-7 and NOON-22 are completed foundations. No dependency on NOON-30. Follow decision-4 and docs/hooks.md; avoid implying that hooks are a durable workflow engine.

Acceptance direction when promoted: real local job A completing queues B with correct context; a repeated hook request is deduplicated within retention; unauthorized/cyclic requests are rejected; hook or downstream failure leaves A unchanged; cancellation follows existing lifecycle semantics.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: hook-composition recipes now ship in examples/local-dev/pipelines and examples/ai/failure-triage. NOON-35 completed their offline executable validation in integration/examples_test.go (TestExampleScripts). Keep this as a draft for a demonstrated remaining product gap; do not promote it merely to duplicate those recipes or tests.
<!-- SECTION:NOTES:END -->
