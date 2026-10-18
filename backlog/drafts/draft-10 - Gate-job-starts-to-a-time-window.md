---
id: DRAFT-10
title: Gate job starts to a time window
status: Draft
assignee: []
created_date: '2026-10-18 06:29'
labels:
  - scheduler
  - config
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote only when a real job must never start outside a time window regardless of trigger, for example a webhook- or hook-driven agent job that must not run overnight, and the docs/configuration.md#working-hours recipe (cron hour range, missed: skip, script guard with noop_exit_code) has proven insufficient. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: a cron hour range keeps scheduled runs inside working hours, but queued runs (noon run --detach, MCP, webhooks, Git and agent hooks, shell entry) ignore the schedule. Today each script must check the clock itself, and prompt jobs need a run wrapper because they cannot use noop_exit_code.

Preferred syntax: a five-field cron expression used as a window, following Renovate schedule semantics, e.g. `window: "* 8-16 * * 1-5"`. A run may start only during a minute the expression matches, evaluated in the project timezone with the existing internal/schedule parser. No free-text windows (decision-5), no systemd OnCalendar or iCalendar RRULE.

Promotion questions: what happens to a request outside the window (reject at queue time, record as noop/skipped, or hold until the window opens; holding conflicts with the one-hour expiry of overdue queued runs); whether foreground noon run is gated or exempt; interaction with schedule catch-up (missed: run_once landing outside the window), overlap: queue, cooldown, and requests: each; how list/status show the next allowed start; whether the window belongs in the approval digest (it should, as an execution field).

Related gap: jobs do not receive the project timezone as TZ, so a script guard using date can disagree with the schedule unless the job sets env TZ. Consider exporting TZ (or NOON_TIMEZONE) to jobs independently of this draft.

Context: decision-5 (schedule grammar, missed-run policy), NOON-51 and NOON-52 (conditions belong to scripts; Noon records outcomes), docs/triggers.md (queue expiry, merge vs each).
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
