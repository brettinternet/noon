---
id: DRAFT-18
title: Vary each scheduled start with per-run forward jitter
status: Draft
assignee: []
created_date: '2026-10-20 19:15'
updated_date: '2026-10-20 19:15'
labels:
  - scheduler
  - schedule
dependencies:
  - NOON-62
documentation:
  - docs/design.md
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote when a real job needs organic-looking start times, after NOON-62 lands. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: some jobs should not visibly fire at the same rigid time every run (for example exactly 09:00:00 every weekday). NOON-62 `splay` spreads jobs apart but keeps each job at one fixed offset forever, so its starts still look mechanical.

Preferred direction: an opt-in job field, separate from `splay`, giving a different forward delay on every scheduled run:

```yaml
jobs:
  digest:
    schedule: weekdays at 8:50am
    jitter: 20m       # starts anywhere from 08:50:00 to 09:09:59, different each run
    run: ./scripts/digest
```

```console
$ noon list
digest   weekdays at 08:50 ~20m   next Mon 09:03:27
```

- Offset per occurrence in whole seconds within [0, jitter), from SHA-256(project path, job name, nominal occurrence time). Deterministic rather than drawn at runtime: a restart recomputes the same start, so a run is never skipped or repeated; `list` can show the exact next start; nothing new is persisted; tests stay clock- and randomness-free. To an outside observer it is indistinguishable from random.
- Forward only, like `splay`. To center a window on 09:00, write an earlier nominal time. Never runs before the time the user wrote.
- Composes with `splay`: start = nominal + splay offset + jitter offset.
- Reuses NOON-62 rules: `scheduled_at` and `NOON_SCHEDULED_AT` stay nominal; on-time window and missed-run handling use the effective start; part of the approval digest; manual, `--detach`, `--in`, `--at`, MCP, triggers, hooks, and webhooks are never jittered.

Promotion questions: the bound that keeps occurrences in order (proposal: splay + jitter must not exceed half the shortest gap between consecutive times of day, counting the wrap over midnight); whether interval schedules accept jitter around their fixed phase; the planner change, since a per-occurrence offset breaks the constant-shift `Next(after - offset) + offset` formula from NOON-62 and the next effective start must be found across nominal occurrences; how DST-repeated or skipped nominal times seed the hash; display form in `list`, `list --json`, MCP, dashboard, and TUI.

Non-goals: offsets before the nominal time; non-uniform distributions; occasionally skipping runs or varying the day; true randomness or stored random state; jitter for manual or due-now requests; a user-level default.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
