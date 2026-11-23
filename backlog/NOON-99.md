---
id: NOON-99
title: Queue and cancel runs from the web viewer
status: draft
type: feature
priority: low
labels: [http, security, delight]
blocked_by: [NOON-98]
refs:
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
created: 2026-11-23T20:48:07Z
updated: 2026-11-23T20:48:07Z
---

Why: once failures and upcoming runs are visible in the web viewer (NOON-98), the natural next step is to re-run a job or stop a runaway run without opening a shell.

Outcome (draft, needs refinement before ready): the web viewer can queue approved jobs (now, `in`, or `at`) and cancel queued or running runs, with exactly the remote API's authority (decision-6 item 3).

Open points to settle before ready:
- Forms post and redirect (303) with a flash result. Reuse `app.QueueDueAt`, `app.QueueRun`, and `app.Cancel` as `/v1/` does. Disabled buttons show the reason (paused, needs approval, ended) and the CLI command.
- `requested_by` is `web:REMOTE_ADDR`. Decide whether the event source is a new `web` value (store event validation and design.md event-source contract) or `api`.
- Cancelling a running run asks for confirmation. Queueing does not.
- Optional `data` payload input, or none in this phase.
- Keyboard shortcuts matching the TUI (`r` run, `c` cancel, `?` help).

Non-goals: pause, resume, enable, approve, add, edit, remove (decision-6); a temporary-schedule UI.

Dependencies: NOON-98.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
