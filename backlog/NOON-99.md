---
id: NOON-99
title: Queue, cancel, pause, and resume from the web viewer
status: todo
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
updated: 2026-11-23T22:24:32Z
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

- [ ] `task cli:test PKG=./internal/store -- -count=1` exits 0: the migration adds `requested_by` to pause rows, rows written before it read `cli`, and a new pause keeps its requester through reads, expiry, and resume.

- [ ] `task cli:test PKG=./internal/web -- -count=1` exits 0: every action route redirects 303 to `/login` without a session, returns 403 to a cross-origin POST and 405 to GET; disabled-project and unknown IDs return 404; no web route enables, approves, adds, edits, or removes a job; a pause that would end a temporary schedule, a project-wide pause, and cancelling a running run change nothing without `confirm=1`.

- [ ] `task cli:test PKG=./integration -- -run '^TestWebActions$' -count=1 -v` exits 0. With the built binary, a web token, and an enabled project with `hello` and `slow` (`sleep 30`), a signed-in client: queues `hello` now and the run succeeds with `requested_by` `web:…` and event source `web`; queues `hello` `in=1h` then cancels it; queues `slow` and cancels it with `confirm=1` so its process group exits; pauses `hello` with `window=1h`, after which `noon list --json` shows it paused with `paused_by` `web:…` and a resume time, and queueing it returns 409 `paused`; resumes it; pauses all with `confirm=1` so every job is paused, then resumes all. The web token still gets 401 on `/v1/`, and the existing remote API and MCP tests pass unchanged.

- [ ] Browser check with the agent browser tooling: each action works with JavaScript disabled (plain form and redirect) and enabled (native `<dialog>` confirmations, keyboard shortcuts, `?` help), with no console errors or CSP violations. Observations recorded in Notes.

- [ ] docs/web.md, docs/design.md (security model row for the web token, the `web` event source, pause `requested_by`), docs/cli.md (`paused_by`), and README describe only implemented behavior. `task cli:test PKG=./internal/cli -- -count=1` exits 0 with human CLI output unchanged. `task ci` exits 0.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
