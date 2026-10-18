---
id: DRAFT-15
title: Spread scheduled starts with a stable per-job offset
status: Draft
assignee: []
created_date: '2026-10-18 16:33'
labels:
  - scheduler
  - schedule
  - agents
dependencies: []
documentation:
  - docs/design.md
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote only when real use shows agent jobs from several projects starting on the same minute and hitting provider rate limits or usage windows, even with the existing global cap of four concurrent runs. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: humans pick round times (`every day at 9am`, `0 * * * *`), so many prompt jobs across projects become due on the same minute. The scheduler serializes them beyond the caps, but the first few still fire together, and a burst against one provider can trip rate limits.

Prior art: Claude Desktop scheduled tasks add a deterministic delay of a few minutes per task; Claude Code /loop delays recurring tasks by up to half the period (max 30 minutes); systemd timers offer RandomizedDelaySec with FixedRandomDelay.

Preferred direction: an opt-in job field (for example `splay: 10m`) giving a stable offset derived from project and job identity, so a job always starts at the same delay and history stays predictable. Displayed next times must include the offset. Part of the approval digest.

Promotion questions: opt-in per job versus a user-level default for prompt jobs; interaction with the 60s on-time window, missed-run detection, interval phase, DST rules, and `NOON_SCHEDULED_AT` (nominal or offset time); whether queued, webhook, and trigger requests are ever offset (presumably never); maximum offset relative to the period; how `noon list`, the dashboard, and MCP present it.

Non-goals: random per-run jitter, load-aware scheduling, rate-limit detection or retries, offsets for manual or due-now requests.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
