---
id: NOON-98
title: Browse every project the scheduler serves in an optional read-only web viewer
status: todo
type: feature
priority: medium
labels: [http, security, delight]
blocked_by: [NOON-97]
refs:
  - docs/design.md
  - docs/webhooks.md
  - docs/cli.md
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
files:
  - internal/web/**
  - internal/app/**
  - internal/httpapi/listener.go
  - internal/httpapi/listener_test.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/config/**
  - internal/cli/serve.go
  - internal/cli/serve_test.go
  - integration/web_test.go
  - README.md
  - docs/web.md
  - docs/cli.md
  - docs/configuration.md
  - docs/design.md
  - docs/service.md
  - docs/webhooks.md
created: 2026-11-23T20:48:02Z
updated: 2026-11-23T20:48:07Z
---

Why: from a browser on another machine, users want every project the host's scheduler serves: what's failing, what runs next, and live logs. Today that needs a shell on the host. decision-6 settles the shape: an optional, authenticated adapter on the scheduler listener, read-only in this phase.

Outcome: with `serve.web: true` (or `--web`) plus a listener and API token (NOON-97), the scheduler serves a server-rendered web viewer at `/`. It runs in the background under `noon service install` with no extra steps, and it works with or without a reverse proxy on another host.

Settled contract (decision-6 governs; this phase adds no write actions):
- Enabling: the `serve.web` boolean in user config, or the `--web` flag. `web` without an API token file is a usage error from `serve` (exit 2) and a config error from `noon config check`. Without `web`, `/`, `/login`, and every web route return 404 exactly as today; webhooks, `/healthz`, `/v1/`, and `/mcp` are unchanged.
- Routes (no collision with `/hooks/`, `/healthz`, `/v1/`, `/mcp`):
  - `GET /login`, `POST /login` (form field `token`), `POST /logout`
  - `GET /` overview; `GET /projects/{id}` (`?job=`, `?status=`); `GET /runs/{id}`
  - `GET /events` and `GET /runs/{id}/log/stream` (SSE)
  - `GET /assets/*` (embedded; the only unauthenticated web route besides `/login`)
- Auth: login compares the token in constant time. On success it sets `noon_session` = expiry plus HMAC-SHA256 keyed by the token, valid 30 days, with `HttpOnly`, `SameSite=Strict`, `Path=/`, and `Secure` when `r.TLS != nil` or `X-Forwarded-Proto: https`, then redirects 303 to `/`. A wrong token re-renders the form with 401. An unauthenticated or tampered/expired-cookie page request redirects 303 to `/login`; SSE returns 401. Each POST needs an `Origin` whose host equals `r.Host`, otherwise 403. The token never appears in HTML, logs, cookies, or job environments.
- Headers on every web response: `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`. No inline script or style.
- Reach: only enabled projects (as `noon projects` lists them, including the user project) and their runs. Anything else is 404.
- Overview `/`, one page:
  - Projects: name, path, state (ok, failing, needs approval, paused, missing checkout), next run, and a strip of the last 20 runs, each linking to its run.
  - Failing: jobs whose latest run failed, timed out, or was interrupted, across all projects, newest first.
  - Upcoming: scheduled runs in the next 24h across projects, in time order, with each jitter window.
  - Needs-approval rows show the project's suggested CLI command (from `internal/app`) with a copy button. The viewer never approves.
- Project page: jobs (schedule, next, last, paused or needs-approval state) and recent runs filterable by job and status.
- Run page: job, status, trigger, `requested_by`, start, duration, exit code, and the log with line anchors (`#L42` scrolls to and highlights the line). A running job's log streams over SSE, and the status changes to final without a reload.
- Liveness: overview and project pages reflect a new or finished run within 3s without a manual reload, including runs started by another process (`noon run`) through the shared SQLite store. SSE routes get an extended write deadline like `/v1/`, and they never extend webhook limits.
- Look: system font stack, light and dark through `prefers-color-scheme`, status colors matching the TUI's meanings, responsive down to phone width. No taglines or helper text. Empty states show one line and the next command (`noon enable`).
- Implementation: a new `internal/web` package with `html/template`, `embed`, and the standard library only. New read operations (cross-project failing jobs, upcoming runs) go in `internal/app` so the CLI, TUI, and MCP can reuse them.

Scope: internal/web (new), read-only operations in internal/app, mounting in internal/httpapi/listener.go, the `serve.web` setting and `--web` flag (internal/config, internal/cli, internal/scheduler), one built-binary integration test, and docs: new docs/web.md (enabling, background service, Caddy and nginx proxy examples preserving `Host`, the private-link requirement), README, cli.md, configuration.md, design.md (remove the web UI non-goal and the RunWisp refusal; add the web viewer to the integrations and security model), service.md, webhooks.md.

Tests (critical only): authentication on every web route, cookie integrity (tampered, expired, and wrong-token-signed cookies rejected), the Origin check, the enabled-project boundary, token hygiene, and 404 when web is off. One integration test. No template snapshot tests.

Non-goals: queue and cancel (NOON-99); pause, resume, enable, approve, add, or edit (never, per decision-6); accounts, OAuth, per-client tokens; TLS; CORS; mounting under a path prefix; a JS framework, Node, or build step; a separate web daemon or `noon web` command; browser notifications; log search beyond the browser's find.

Dependencies: NOON-97 (the `serve:` block that `serve.web` extends and that lets the service serve the listener). Follows decision-1, decision-2, decision-4, decision-6. No new module dependency.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [ ] `task cli:test PKG=./internal/web -- -count=1` exits 0, covering: every web route except `/login` and `/assets/*` redirects 303 to `/login` (SSE routes 401) without a valid cookie, including tampered, expired, and other-token-signed cookies; `POST /login` with the wrong token returns 401 and sets no cookie, the right token returns 303 with `HttpOnly`, `SameSite=Strict`, and `Secure` only for TLS or `X-Forwarded-Proto: https`; POSTs with a missing or mismatched `Origin` return 403; every web response carries the CSP, nosniff, and referrer headers.
- [ ] `task cli:test PKG=./integration -- -run '^TestWebViewer$' -count=1 -v` exits 0. With the built binary, isolated HOME/XDG/state, user config `serve: {listen: 127.0.0.1:0, api_token_file: F, web: true}`, one enabled project with jobs `hello` and failing `boom`, and one registered but disabled project: after logging in through a proxy-style request (`Host: noon.example.com`, `X-Forwarded-Proto: https`, matching `Origin`), the cookie is `Secure`; `/` lists only the enabled project and shows `boom` under Failing and both jobs under Upcoming; the disabled project and its runs and an unknown ID return 404; a run queued with `bin/noon run hello --detach` appears in the `/events` stream within 3s, and `/runs/{id}/log/stream` delivers a log line before the run finishes; the token string appears in no HTML response, cookie, or scheduler output.
- [ ] Without `web`, `/`, `/login`, and `/events` return 404 and the existing webhook, API, and MCP tests pass unchanged. `serve --web` and `serve.web: true` without an API token file exit 2 from `serve` and fail `noon config check`. `task cli:test PKG=./internal/cli -- -count=1` exits 0.
- [ ] Browser check with the agent browser tooling against a local `bin/noon serve`: overview, project, and run pages render in light and dark color schemes and at 390px width with no console errors or CSP violations; `/runs/{id}#L3` highlights line 3; a running job streams log lines and flips to its final status without a reload. Screenshots or observations recorded in Notes.
- [ ] docs/web.md (enable, background service, Caddy and nginx examples preserving `Host`, private link requirement), README, docs/cli.md, docs/configuration.md, docs/design.md (web UI non-goal and RunWisp refusal removed; integrations and security model updated), docs/service.md, and docs/webhooks.md describe only implemented behavior. `go list -m all` shows no new module. `task ci` exits 0.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
