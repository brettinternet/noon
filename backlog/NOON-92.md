---
id: NOON-92
title: Surface failing jobs on the dashboard
status: done
type: feature
priority: high
labels: [cli, tui, delight]
refs: [docs/design.md, docs/cli.md]
files:
  - internal/store/store.go
  - internal/store/store_test.go
  - internal/app/history.go
  - internal/app/inspect.go
  - internal/app/dashboard.go
  - internal/cli/dashboard_test.go
  - internal/tui/view.go
  - internal/cli/tui_test.go
  - docs/design.md
  - docs/cli.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T18:56:06Z
---

Why: noon exists so unattended work keeps happening, and the most important thing it can tell a user is that a job has stopped working. Today bare `noon` shows config, approval, scheduler, orphan, temporary, and queued notices, but a nightly job that has failed for a week looks the same as a healthy one unless the user runs `noon history -s failed`. The data already exists: `store.PreviousResults` counts consecutive failed/timed_out runs back to the last success for hooks.

Outcome:

```console
$ noon
tests failing: 4 runs since Mon 02:00.
Suggested action: noon logs tests -s failed
```

Settled contract:
- A job is failing when its latest retained `succeeded`, `failed`, or `timed_out` run is `failed` or `timed_out`; `noop`, `cancelled`, and `skipped` runs are ignored, matching `PreviousResults`. All triggers count. Jobs that are disabled, paused, or ended are never reported as failing.
- List, status, and MCP job JSON add `consecutive_failures` (count back to the last success, omitted when 0) and `failing_since` (finished time of the oldest failure in the streak, RFC 3339 in the project timezone, omitted when not failing). The store returns both in one read alongside `LatestRunsForProject`; `WithLastRuns` maps them.
- Dashboard notice code `failing`, ranked after `orphaned` and before temporary notices, within the existing three-notice and 80-column limits. One job: `JOB failing: N runs since DAY HH:MM.`; several: `N jobs failing: a, b, c.`.
- Suggested action, after the orphaned-run action and before the default: `noon logs JOB -s failed` for the first failing job in list order.
- The TUI banner shows the `failing` notice with failure tone and the same fix. docs/design.md notice table and docs/cli.md JSON fields describe it.

Scope: store failure-streak read, app enrichment and dashboard notice/next, TUI banner case, docs.

Tests (critical only):
- Nontrivial state: the streak counts failed/timed_out back to the last success, ignores noop/cancelled/skipped, and resets after a success; `failing_since` is the oldest failure in the streak. Extend the store tests.
- Dashboard: a failing job produces the notice and suggested action, and a paused failing job does not. Extend TestDashboard.

Non-goals: notifications or hooks on failure (existing `on_failure`/hook mechanisms cover that); failure thresholds or configuration; a failure rate over time; MCP or HTTP shape changes beyond the additive job fields and notices they already relay.

Dependencies: none.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo whose noon.yaml defines `flaky` (run ["false"]) and `ok` (run ["true"]): after `bin/noon run ok` and three `bin/noon run flaky` (each exits 1), bare `bin/noon` prints a notice starting `flaky failing: 3 runs since` and `Suggested action: noon logs flaky -s failed`; `bin/noon status --json` has a notice with code `failing`; `bin/noon list --json` reports `flaky` with `consecutive_failures` 3 and a `failing_since`, and `ok` with neither field.
  Evidence: Scenario output matched; suggested action includes documented --state-dir selector from NOON_STATE_DIR (see AC#1 note).
- [x] In the same environment, after `bin/noon pause flaky`, bare `bin/noon` shows no `failing` notice; after `bin/noon resume flaky`, changing `flaky` to run ["true"] and running it once, `bin/noon list --json` reports no `consecutive_failures` for `flaky` and the dashboard shows no `failing` notice.
  Evidence: Paused: no failing notice; after fix+run: no consecutive_failures, no failing notice.
- [x] `task cli:test PKG=./internal/store -- -count=1` and `task cli:test PKG=./internal/cli -- -run 'TestDashboard|TestTUI' -count=1` exit 0 and cover: the streak counts failed and timed_out back to the last success, ignores noop/cancelled/skipped, resets after success, and `failing_since` is the oldest failure in the streak; the dashboard notice and suggested action appear for a failing job and not for a paused one.
  Evidence: Both focused test commands exit 0 with streak and dashboard coverage.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on 825ac1f.
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC#1-3 evidence notes recorded.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Only contract files changed (task record via bl).
- [x] No test was deleted, skipped, or weakened
  Evidence: Only additions to store_test and dashboard_test.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/design.md notice table + failing rule; docs/cli.md notice row + JSON fields.

## Notes

AC#1: task cli:build + /tmp scenario (HOME/XDG_*/NOON_STATE_DIR under one mktemp -d, temp git repo, run ok, 3x run flaky each exit 1): bare noon printed 'flaky failing: 3 runs since Tue 11:45.' and 'Suggested action: noon logs --state-dir <tmp>/noon-state flaky -s failed'; status --json notice codes ["failing"]; list --json flaky consecutive_failures=3 failing_since=2026-11-17T11:45:53.809-07:00, ok has neither. The --state-dir selector is the documented guidance rewrite (docs/cli.md 'Suggested commands keep your explicit ... --state-dir') triggered by the criterion's own NOON_STATE_DIR; without selectors TestDashboard/failing asserts the exact 'Suggested action: noon logs t -s failed'.

AC#2: same env: after pause flaky, bare noon showed no failing notice (Suggested action: noon run ... ok); after resume, run ["true"], run flaky (exit 0): list --json flaky consecutive_failures/failing_since absent, dashboard has no failing notice.

AC#3: task cli:test PKG=./internal/store -- -count=1 -> ok; task cli:test PKG=./internal/cli -- -run 'TestDashboard|TestTUI' -count=1 -> ok. Store subtest 'failure streaks count back to the last success in completion order' covers failed+timed_out counting, noop/cancelled/skipped ignored, completion-order failing_since, reset after success; TestDashboard cases 'failing' and 'paused failing' cover notice+next present/absent.

Gate: task ci exit 0 on final commit 825ac1f (merged ff to main). Earlier, before main pinned staticcheck 0.8.2 (523e500), 'staticcheck = latest' failed in mise; and TestRunner/leader_exits_with_open_grandchild_pipe flaked once under load (untouched package; passes on rerun and on main). Interpretation: consecutive_failures/failing_since reflect history for every job; Job.Failing() (enabled, not paused, not ended) gates the notice, next, and TUI banner. Diff within modified-file contract; no tests weakened. Review: one pass over diff, no defects found.

## Summary

Store FailureStreaksForProject reads per-job failed/timed_out streaks since last success in completion order; WithLastRuns maps consecutive_failures/failing_since (project tz) onto job JSON; dashboard adds 'failing' notice after orphaned and 'noon logs JOB -s failed' next; TUI banner shows it in failure tone with the fix; docs updated. Commit 825ac1f on main. Verified with task ci and the AC scenario.
