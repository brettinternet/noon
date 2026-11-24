---
id: NOON-99
title: Queue, cancel, pause, and resume from the web viewer
status: done
type: feature
priority: medium
labels: [http, security, delight]
blocked_by: [NOON-98]
refs:
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
  - docs/design.md
files:
  - internal/web/**
  - internal/app/**
  - internal/store/**
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/jobs.go
  - internal/cli/dashboard_test.go
  - integration/web_actions_test.go
  - README.md
  - docs/web.md
  - docs/design.md
  - docs/cli.md
created: 2026-11-23T20:48:07Z
updated: 2026-11-24T08:36:13Z
---

Why: once the web viewer (NOON-98) shows failures and upcoming runs, the next remote needs are to re-run a job, stop a runaway run, and pause a misbehaving job or a whole project without opening a shell.

Outcome: a signed-in viewer can queue approved jobs, cancel runs, and pause or resume one job or every job in a project. This is decision-6 item 4's authority, with the item 5 guards.

Settled contract:
- Routes (POST only, form-encoded, behind the NOON-98 session check and `http.CrossOriginProtection`; GET returns 405 with `Allow`):
  - `POST /projects/{id}/jobs/{name}/runs`: `when=now|in|at`, `in=DURATION`, `at=TIME`, through `app.QueueDueAt` and `app.QueueRun` exactly as `/v1/` does. Redirects 303 to `/runs/{run}`.
  - `POST /runs/{id}/cancel`: `app.Cancel`. Cancelling a running run requires `confirm=1`.
  - `POST /projects/{id}/jobs/{name}/pause` and `POST /projects/{id}/pause` (every job, like `-a`): `window=1h|1d|tomorrow|open`, meaning `--for 1h`, `--for 24h`, `--until` the next 09:00 in the project's timezone, or until resumed. Same `store.PauseWindow` and `app.PauseJobs` as the CLI.
  - `POST /projects/{id}/jobs/{name}/resume` and `POST /projects/{id}/resume`.
  - Every action redirects 303 back to the page it came from (`/runs/{id}` for queue). The result shows from a fixed set of flash codes, never free text from the request. Errors use `/v1/` codes (404, 409 with the reason, 429) and render on the page.
- Confirmations are enforced by the server: without `confirm=1`, pausing that would end an active temporary schedule (naming each one), any project-wide pause (naming the job count), and cancelling a running run change nothing and render a confirmation page. With JS, the same confirmation opens in a native `<dialog>` first.
- Who paused: a store migration adds `requested_by` to pause rows, and existing rows read `cli`. `app.PauseJobs` takes a requester; the CLI passes `cli`, the viewer passes `web:REMOTE_ADDR`. `noon list --json` and `noon status --json` expose `paused_by` beside the existing resume time. The viewer shows "paused from web 2h ago · resumes Tue 09:00". Human CLI output is unchanged.
- Queued runs record `requested_by: web:REMOTE_ADDR` and a new event source `web` (store event validation, same size limits). The form sends no `data` payload in this phase.
- Controls: queue is disabled, with the reason and the CLI command, for paused, needs-approval, ended, or unsupported jobs. Resume appears only when paused. The project header has Pause all (with presets) and Resume all. Keyboard shortcuts reuse the TUI's key for each action it shares; `?` lists them.
- Authority stays decision-6's: no route enables, disables, approves, adds, edits, or removes jobs, and `/v1/` and MCP gain nothing.

Scope: internal/web actions and templates, the pause requester (internal/store migration, internal/app, the CLI caller), the `web` event source, JSON `paused_by`, one integration test, and docs: docs/web.md (actions), docs/design.md (security model row for the web token, event sources, pause rows), docs/cli.md (`paused_by`), README.

Tests (critical only): the migration and requester persistence (data integrity), session and cross-origin enforcement on every action route, server-side confirmations, and one built-binary integration test of the action paths. Don't test templates or flash wiring.

Non-goals: a `data` payload input; temporary schedules from the web; host-wide pause across projects; enable, disable, approve, add, edit, remove (decision-6); pause or resume on `/v1/` or MCP.

Dependencies: NOON-98. Follows decision-2, decision-4, decision-6. No new module dependency.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/store -- -count=1` exits 0: the migration adds `requested_by` to pause rows, rows written before it read `cli`, and a new pause keeps its requester through reads, expiry, and resume.
  Evidence: See AC#1 in Notes

- [x] `task cli:test PKG=./internal/web -- -count=1` exits 0: every action route redirects 303 to `/login` without a session, returns 403 to a cross-origin POST and 405 to GET; disabled-project and unknown IDs return 404; no web route enables, approves, adds, edits, or removes a job; a pause that would end a temporary schedule, a project-wide pause, and cancelling a running run change nothing without `confirm=1`.
  Evidence: See AC#2 in Notes

- [x] `task cli:test PKG=./integration -- -run '^TestWebActions$' -count=1 -v` exits 0. With the built binary, a web token, and an enabled project with `hello` and `slow` (`sleep 30`), a signed-in client: queues `hello` now and the run succeeds with `requested_by` `web:…` and event source `web`; queues `hello` `in=1h` then cancels it; queues `slow` and cancels it with `confirm=1` so its process group exits; pauses `hello` with `window=1h`, after which `noon list --json` shows it paused with `paused_by` `web:…` and a resume time, and queueing it returns 409 `paused`; resumes it; pauses all with `confirm=1` so every job is paused, then resumes all. The web token still gets 401 on `/v1/`, and the existing remote API and MCP tests pass unchanged.
  Evidence: See AC#3 in Notes

- [x] Browser check with the agent browser tooling: each action works with JavaScript disabled (plain form and redirect) and enabled (native `<dialog>` confirmations, keyboard shortcuts, `?` help), with no console errors or CSP violations. Observations recorded in Notes.
  Evidence: See AC#4 in Notes

- [x] docs/web.md, docs/design.md (security model row for the web token, the `web` event source, pause `requested_by`), docs/cli.md (`paused_by`), and README describe only implemented behavior. `task cli:test PKG=./internal/cli -- -count=1` exits 0 with human CLI output unchanged. `task ci` exits 0.
  Evidence: See AC#5 in Notes

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on rebased tree of 7fe1f16
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC#1-AC#5 evidence lines in Notes
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Deviations justified in Notes: internal/scheduler/serve_test.go, docs/triggers.md
- [x] No test was deleted, skipped, or weakened
  Evidence: No tests deleted, skipped, or weakened; serve_test.go only passes the new requester argument
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/web.md, design.md, cli.md, triggers.md, README describe implemented behavior

## Notes

Plan: add POST actions (queue, cancel, pause, resume) to the web viewer through internal/app helpers shared with the remote API; pause rows gain requested_by (migration 16, old rows read cli); risky actions need confirm=1 with a server confirm page and a native <dialog> when JS runs; integration test drives the built binary.

Decision: web-queued runs always get an event file with source web and data null, like webhook runs, so NOON_EVENT_FILE is uniform for non-CLI triggers.

Decision: the web queue sets RequireUnpaused, so a paused job returns 409 paused (AC#3). This intentionally differs from /v1/, which may queue a paused job; the viewer offers Resume instead of a manual override.

Deviation: internal/scheduler/serve_test.go is outside the file contract; one call site passes the new requester argument (cli) to store.PauseJobs; assertions unchanged.

Deviation: docs/triggers.md is outside the file contract; its event-source list now includes web so docs describe only implemented behavior.

Browser (Chrome devtools, isolated scheduler on 127.0.0.1:18787): JS on — j+r queued and landed on /runs/1 with Queued. and ?done stripped; queued run cancelled without dialog; c on running #3 opened native dialog 'Stop running #3 slow? Its process group is signalled.' and Enter cancelled it (exit 130); p opened pause menu, 1 hour showed 'paused from web now · resumes …' with Run disabled; Pause all dialog 'Pause all 2 jobs?' paused both; ? help dialog opened, Escape closed; j+p resumed. JS off (js class suppressed) — Pause all 1 hour rendered server confirm page 'Pause all 2 jobs?', Confirm redirected with 'Paused. Queued runs cancelled; running work continues.' and both jobs paused; Resume all redirected and resumed. list_console_messages (preserved): no messages, no CSP violations. Fixed during check: menu row alignment, focus into popover, popover labels no longer wrap.

AC#1: task cli:test PKG=./internal/store -- -count=1 -> ok (TestPauseWindows covers migration default cli, web requester through reads, started window, expiry, resume).

AC#2: task cli:test PKG=./internal/web -- -count=1 -> ok (TestActionRoutesRequireSessionSameOriginAndPost, TestRiskyActionsNeedConfirmation).

AC#3: task cli:test PKG=./integration -- -run '^TestWebActions$' -count=1 -v -> --- PASS: TestWebActions; remote API and MCP suites pass unchanged in task ci.

AC#4: browser check recorded above; JS on and off both work, no console errors or CSP violations.

AC#5: docs/web.md, docs/design.md, docs/cli.md, README.md, docs/triggers.md updated to implemented behavior; task cli:test PKG=./internal/cli -- -count=1 -> ok; task ci -> exit 0.

Merged: 7fe1f16 feat: queue, cancel, pause, and resume from the web viewer (fast-forward on main after rebasing over NOON-101 and the durations chart; pages.go FuncMap conflict resolved keeping both). task ci exit 0 on the rebased tree. Review pass: no defects found. Worktree .worktrees/noon-99-web-actions and branch removed via wt remove.

## Summary

Signed-in web viewers can queue (now, in, at), cancel, pause (1h, 1d, until 09:00, until resumed), and resume one job or a whole project. Risky actions use a server confirm page or a native dialog; pauses record web requester (migration 16). Verified with store, web, and integration tests, a browser check with JS on and off, and task ci.
