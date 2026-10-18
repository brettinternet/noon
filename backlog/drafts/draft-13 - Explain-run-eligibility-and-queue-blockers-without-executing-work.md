---
id: DRAFT-13
title: Explain run eligibility and queue blockers without executing work
status: Draft
assignee: []
created_date: '2026-10-18 16:09'
labels:
  - automation
  - cli
  - scheduler
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Priority within the smart-repo proposals: third. Before promotion, choose the smallest inspection surface and add the modified-file contract and executable acceptance commands with expected results.

Problem and outcome: status, list, doctor, and queue responses expose pieces of eligibility, but a user deciding whether to request work must combine them manually. Provide one read-only explanation of whether a named job could run now, why it would wait or be rejected, and which existing action can resolve a blocker.

Scope: distinguish foreground execution, a due-now queued request, and scheduler dispatch rather than applying one mode's rules to another. Explain relevant enablement, approval, platform, paused/disabled state, cooldown, merge/each behavior, queue capacity, scheduler availability, project/global concurrency, and orphaned-run blockers using existing rules. Extend an existing inspection surface if that is clearer than a new command. Human and versioned JSON output should agree.

Non-goals: executing shell preconditions, approving changes, enabling projects, starting a scheduler, reserving a queue slot, simulating script behavior, or adding new scheduling policy. Do not promise an exact start time or successful execution from a point-in-time eligibility snapshot.

Promotion questions: which request modes and optional request context belong in the first slice; how to distinguish queue acceptance from dispatch readiness; how stale/invalid configuration and scheduler last-valid config appear; stable reason codes and blocker ordering. An approval mismatch can be explained without promising a field-by-field diff of an approved definition that is not retained.

Candidate acceptance: fixture snapshots explain unapproved jobs, cooldown/merge outcomes, full queues and busy projects consistently with actual request/dispatch behavior; foreground exceptions remain explicit; inspection creates no run, event file, approval or service and does not initialize missing state; output does not expose environment values or payload secrets.

Dependencies: none. Likely surfaces: internal/app, internal/scheduler, internal/store read operations, internal/cli, focused tests, docs/cli.md and docs/design.md. Preserve pure planner rules and the no-argument inspection safety contract.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
