---
id: DRAFT-19
title: End recurring jobs at a set time
status: Draft
assignee: []
created_date: '2026-10-22 02:43'
labels:
  - scheduler
  - config
dependencies: []
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote when a real job needs to stop running unattended at a known time, for example an agent prompt job that should watch CI every 10 minutes for the next two hours. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: Noon can run once later (`noon run NAME --in/--at`), bound one run (`timeout`), and pause after repeated failures (NOON-57 hook recipe), but a recurring job cannot end on its own. A script guard with `noop_exit_code` still fires every occurrence forever, and hooks only act after a run finishes, so only the scheduler can stop planning future occurrences.

Proposed syntax: a job field `until:` holding an absolute time (`YYYY-MM-DD HH:MM` in the project timezone, or RFC 3339), plus `noon add --for DURATION` and `noon add --until TIME`, which resolve to an absolute `until:` when the job is written. Config never stores a relative duration: it has no anchor, and approval or edits would silently restart the clock.

```yaml
jobs:
  watch-ci:
    schedule: every 10m
    until: 2026-10-20 17:00
    agent: claude
    prompt: Check CI and fix failures.
```

Proposed semantics:
- After `until`, the job is inactive: no scheduled occurrences, no missed-run catch-up, and queued requests (`--detach`, MCP, webhooks, Git and agent hooks, shell entry) are refused like a disabled job. Already queued runs are cancelled like those of inactive jobs.
- A run in progress at `until` finishes, bounded by `timeout`.
- Foreground `noon run NAME` still works, matching paused jobs.
- The definition stays; `list`, status, TUI, and MCP show it as `ended`. The scheduler never edits config files; `noon remove` cleans up.
- `until` is an execution field and belongs in the approval digest.

Non-goals: ephemeral auto-deleting jobs; `max_runs` or other run counts (they need a durable counter, since retention keeps 50 runs per job, and settled answers for noop, skipped, catch-up, foreground, and triggered runs and digest-change resets; queuing several `--in` one-shots or a hook recipe covers counts until a real need appears); agent session budgets, which belong to the agent harness; letting MCP clients add jobs (decision-2).

Promotion questions: whether an `until` already in the past is a validation error or just `ended`; whether `--for` is allowed without a schedule (likely not: one-shots already cover that); whether foreground runs should be refused after `until` instead; how `list` shows the end time; whether `until` and the DRAFT-10 `window` share one eligibility check and should be designed together.

Context: decision-2 (approval digest, MCP cannot add jobs), decision-5 (job semantics, missed runs), NOON-5 (one-shots and queued-run expiry), NOON-57 (failure-count pause via hooks), DRAFT-10 (time windows), docs/configuration.md#jobs.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
