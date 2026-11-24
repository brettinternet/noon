---
id: NOON-101
title: Chart recent run durations per job in the web viewer
status: done
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
updated: 2026-11-24T08:32:57Z
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

- [x] With a local `bin/noon serve` and a job with at least 10 finished runs of mixed status plus a timeout, the browser check shows `/projects/{id}?job=NAME` rendering one bar per finished run (at most 50, oldest left), status classes matching the legend, each bar linking to its run, and a labeled timeout line; no CSP violations; Lighthouse accessibility stays at least 95. Screenshot recorded in Notes.
  Evidence: AC#1: chrome-devtools vs local bin/noon serve (isolated HOME/XDG, 127.0.0.1:8791), job flaky timeout 4s with 15 finished runs (succeeded, failed, timed_out, noop, cancelled-after-start) plus a cancelled-before-start #17: /projects/1?job=flaky renders 15 bars oldest-left (#1..#16; #17 and other-job #15 excluded), bar classes ok/bad/muted match the legend (succeeded, failed, noop, timed out, cancelled), each bar an <a href=/runs/ID> with <title> '#ID status duration' (bar link GET 200), dashed line labeled 'timeout 4s', SVG title 'flaky run durations' and desc 'Last 15 finished runs, oldest first. Median 1s, slowest 4s (#12). Timeout 4s.'. No console messages or CSP violations. Lighthouse accessibility 100 desktop and mobile. ?job=quick (1 run) and no job: no chart. Light/dark and 390px checked. Screenshots: ~/.claude/projects/-Users-brett-dev-me-noon/0d77e338-0c44-4e9c-a630-4004e44da15c/tool-results/mcp-chrome-devtools-blob-1795508912196-rak9nr.png (dark desktop) and mcp-chrome-devtools-blob-1795508926536-1jiqk0.png (light 390px).
- [x] Any new `internal/app` read has a test proving it excludes skipped, cancelled-before-start, and running runs and returns at most 50 newest-first: `task cli:test PKG=./internal/app -- -count=1` exits 0. `go list -m all` shows no new module.
  Evidence: AC#2: TestFinishedRunsExcludesUnstartedAndUnfinishedAndKeepsNewest (52 finished runs of all six finished statuses, another job, skipped, cancelled-before-start, running) asserts exactly the 50 newest finished IDs newest-first with durations; fails when startedOnly is disabled. task cli:test PKG=./internal/app -- -count=1 ok (within task ci). go.mod/go.sum unchanged; go list -m all unchanged.
- [x] docs/web.md describes the chart. `task ci` exits 0.
  Evidence: AC#3: docs/web.md Pages section describes the chart, exclusions, 2-run minimum, and timeout line rule. task ci exit 0 on rebased commit 01c6c69.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on final commit 01c6c69 (rebased onto main 2a68797).
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC#1-3 evidence lines recorded with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Diff limited to docs/web.md, internal/app/history.go, internal/app/history_test.go, internal/web/** (chart.go, pages.go, project.html, app.css).
- [x] No test was deleted, skipped, or weakened
  Evidence: No tests deleted, skipped, or weakened; one test added.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/web.md describes only implemented behavior; no design contract changed.

## Notes

Commit 01c6c69 feat: chart recent run durations per job in the web viewer (worktree .worktrees/noon-101-duration-chart, rebased onto 2a68797 and fast-forwarded into local main; not pushed). New read app.FinishedRuns reuses history's store query (statuses succeeded/noop/failed/timed_out/interrupted/cancelled, started_at required, newest 50). Chart built in internal/web/chart.go as percentage-positioned inline SVG (no viewBox, classes only); timeout line drawn when within 2x the slowest run so a default 1h timeout doesn't flatten bars. Diff within the files contract (internal/web/chart.go is under internal/web/**).
