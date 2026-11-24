---
id: NOON-101
title: Chart recent run durations per job in the web viewer
status: todo
type: feature
priority: low
labels: [http, delight]
blocked_by: [NOON-98]
refs:
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
files:
  - internal/web/**
  - internal/app/history.go
  - internal/app/history_test.go
  - docs/web.md
created: 2026-11-24T03:55:26Z
updated: 2026-11-24T03:55:26Z
---

Why: a job that slowly gets slower, or that times out now and then, is hard to see in a run list. A duration chart makes regressions and flaky timeouts obvious at a glance.

Outcome: the web viewer's project page shows, for a selected job (`?job=NAME`), a chart of the last 50 finished runs' durations, colored by status, with the timeout drawn as a reference line when the job has one.

Settled contract:
- Rendered on the server as inline SVG in the template. No chart library or client-side drawing, no new module, no inline style (classes only, so the CSP holds).
- One bar per run, oldest to newest, height proportional to duration on a linear axis starting at zero, colored by the same status meanings as the rest of the viewer (succeeded, failed, timed out, interrupted, noop). Each bar links to its run and carries a `<title>` with the run number, status, and duration. The timeout line is labeled with its value.
- Accessible: the SVG has a title and description summarizing median and slowest durations, and the same 50 runs are reachable in the existing runs table. Status is never shown by color alone.
- Data comes from one `internal/app` read (finished runs with durations for one job, newest 50) reused from history where possible.
- Skipped, cancelled-before-start, and still-running runs are excluded. With fewer than 2 finished runs, no chart renders.

Scope: internal/web templates and assets, one internal/app read if history can't already serve it, docs/web.md.

Tests (critical only): none beyond the internal/app read if it's new (data selection: excluded statuses and the 50 limit). Verify rendering in the browser.

Non-goals: charts on the overview; percentiles over time; zoom or brushing; charts for MCP or the CLI.

Dependencies: NOON-98. Follows decision-6 item 9.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [ ] With a local `bin/noon serve` and a job with at least 10 finished runs of mixed status plus a timeout, the browser check shows `/projects/{id}?job=NAME` rendering one bar per finished run (at most 50, oldest left), status classes matching the legend, each bar linking to its run, and a labeled timeout line; no CSP violations; Lighthouse accessibility stays at least 95. Screenshot recorded in Notes.
- [ ] Any new `internal/app` read has a test proving it excludes skipped, cancelled-before-start, and running runs and returns at most 50 newest-first: `task cli:test PKG=./internal/app -- -count=1` exits 0. `go list -m all` shows no new module.
- [ ] docs/web.md describes the chart. `task ci` exits 0.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
