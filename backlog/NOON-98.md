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
  - docs/docker.md
  - examples/integrations/remote-access/**
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/dashboard.go
  - internal/cli/dashboard_test.go
  - internal/app/doctor.go
created: 2026-11-23T20:48:02Z
updated: 2026-11-24T03:55:41Z
---

Why: from a browser on another machine, users want every project the host's scheduler serves: what's failing, what runs next, and live logs. Today that needs a shell on the host. decision-6 settles the shape: an optional, authenticated adapter on the scheduler listener, read-only in this phase.

Outcome: with a listener (NOON-97) and a web token file (`serve.web_token_file` or `--web-token-file`), the scheduler serves a server-rendered web viewer at `/`. It runs in the background under `noon service install` with no extra steps, and it works with or without a reverse proxy on another host.

Settled contract (decision-6 governs; this phase adds no write actions):
- Enabling: the `serve.web_token_file` setting in user config (resolved like `api_token_file`), or the `--web-token-file` flag; flag beats setting. A web token file without a listen address is a usage error from `serve` (exit 2) and a config error from `noon config check`. Validation matches the API token: read once at startup, at least 32 bytes, an unreadable or short file fails startup with exit 1 before scheduling. A web token equal to the API token exits 2 and fails `noon config check`. Either token works without the other. Without a web token, `/`, `/login`, and every web route return 404 exactly as today; webhooks, `/healthz`, `/v1/`, and `/mcp` are unchanged, and they never accept the web token. `noon config` shows `Listener: ADDR (API, web)` as configured, and the startup log line names the web URL. `noon doctor` applies the NOON-97 token-file checks (readable, length, group/other permissions) to the web token too.
- Routes (no collision with `/hooks/`, `/healthz`, `/v1/`, `/mcp`):
  - `GET /login`, `POST /login` (form field `token`), `POST /logout`
  - `GET /` overview; `GET /projects/{id}` (`?job=`, `?status=`); `GET /runs/{id}` (`?before=CURSOR` for earlier log)
  - `GET /events` (SSE; `?run=ID` adds that run's log lines to the same stream)
  - `GET /assets/{hash}/*` (embedded; the only unauthenticated web route besides `/login`)
- Auth: login compares the web token in constant time. On success it sets `noon_session` = expiry plus HMAC-SHA256 keyed by the web token, valid 30 days, with `HttpOnly`, `SameSite=Strict`, `Path=/`, and `Secure` when `r.TLS != nil` or `X-Forwarded-Proto: https`, then redirects 303 to `/`. A wrong token re-renders the form with 401 and logs one line with the remote address (never the submitted value). An unauthenticated or tampered/expired-cookie page request redirects 303 to `/login`; SSE returns 401. All web routes sit behind `http.CrossOriginProtection` (zero value: no trusted origins, no bypasses), so cross-origin browser POSTs get 403. The proxy must preserve `Host`. Neither token appears in HTML, logs, cookies, or job environments, and the web token is never inherited by jobs or hooks. `requested_by` and logs use `RemoteAddr` (the proxy's address behind a proxy); `X-Forwarded-For` is never trusted.
- Headers on every web response: `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`. No inline script or style. Signed-in pages and SSE send `Cache-Control: no-store`, because logs can contain secrets. Assets are served from content-hashed URLs with `Cache-Control: public, max-age=31536000, immutable`.
- Reach: only enabled projects (as `noon projects` lists them, including the user project) and their runs. Anything else is 404.
- Overview `/`, one page:
  - Projects: name, path, state (ok, failing, needs approval, paused, missing checkout), next run, and a strip of the last 20 runs, each linking to its run.
  - Failing: jobs whose latest run failed, timed out, or was interrupted, across all projects, newest first.
  - Upcoming: scheduled runs in the next 24h across projects, in time order, with each jitter window, capped at 50 rows. A job scheduled more often than hourly takes one row ("every 1m · next 14:03"), never one row per occurrence.
  - Needs-approval rows show the project's suggested CLI command (from `internal/app`) with a copy button. The viewer never approves.
- Project page: jobs (schedule, next, last, paused or needs-approval state) and recent runs filterable by job and status.
- Run page: job, status, trigger, `requested_by`, start, duration, exit code; for prompt jobs, the prompt and the NOON-96 resume command with a copy button. The log renders its last 256 KiB, and an Earlier link pages back through the existing log cursors. Line anchors use absolute line numbers (`#L42` scrolls to and highlights the line, fetching earlier pages as needed). ANSI SGR colors map to CSS classes; every other control sequence and C0/C1 control character except tab and newline is dropped. A running job's log streams over SSE, and the status changes to final without a reload.
- Liveness and resource limits:
  - One watcher goroutine per scheduler polls `PRAGMA data_version` on a dedicated read connection about every 500ms and broadcasts a change to every subscriber, so SQLite work doesn't grow with open tabs and commits by other processes (`noon run`) are seen. Pages reflect a new or finished run within 3s.
  - Each page opens one `EventSource`. Browsers allow 6 HTTP/1.1 connections per origin, so a page never opens two streams. The script closes the stream while the tab is hidden, and on return reconnects and refetches `<main>`.
  - Streams send `X-Accel-Buffering: no` and a comment heartbeat every 15s. They get the extended write deadline like `/v1/` without extending webhook limits, and they are capped at 32 concurrent; beyond that, 503. The first event carries the build version, and the script does a full reload when it changes after an upgrade or restart.
  - Shutdown closes every stream through `http.Server.RegisterOnShutdown`, so open tabs never hold the scheduler's 5s shutdown window.
- Look and feel: system font stack, light and dark through `prefers-color-scheme`, status colors matching the TUI's meanings, responsive down to phone width with tap targets of at least 44px. No taglines or helper text. Empty states show one line and the next command (`noon enable`). Times render as `<time datetime>`: the script shows them in the viewer's local timezone and keeps relative times ("in 3m") current, and without the script they fall back to the project's timezone. Schedule expressions show the project's timezone.
- Tab status: the document title leads with the failing-job count when non-zero (`✗ 2 · noon`), and the favicon (embedded SVG) switches between ok and failing variants. Both update live.
- Accessibility: status is never shown by color alone (✓/✗ glyph plus text), lists of runs and jobs are semantic tables, focus is visible, live regions announce status changes politely, and motion respects `prefers-reduced-motion`.
- Stack (decision-6 item 9): a new `internal/web` package using only the standard library: `http.ServeMux` method-and-wildcard patterns, `http.CrossOriginProtection`, `http.ResponseController` write deadlines for SSE, `html/template` (contextual escaping; never `template.HTML` on run data or logs), `embed` for templates and assets, `crypto/hmac`. Every page works with JavaScript disabled, apart from live updates. One hand-written JS module (`assets/app.js`, under about 250 lines) handles the stream, `<main>` swaps, log lines, copy buttons, line anchors, local times, and tab status. One hand-written `assets/app.css` uses custom properties and `light-dark()`. No vendored third-party JS or CSS. New read operations (cross-project failing jobs, upcoming runs with collapsing) go in `internal/app` so the CLI, TUI, and MCP can reuse them.

Scope: internal/web (new), read-only operations in internal/app, mounting and shutdown in internal/httpapi/listener.go and internal/scheduler/serve.go, the `serve.web_token_file` setting and `--web-token-file` flag (internal/config, internal/cli, internal/scheduler), one built-binary integration test, and docs: new docs/web.md (enabling, token generation with `umask 077`, background service, Caddy and nginx examples preserving `Host` with `proxy_buffering off`, the private-link requirement, web vs API token), README, cli.md, configuration.md, design.md (remove the web UI non-goal and the RunWisp refusal; add the web viewer to the integrations and security model), docker.md (web token as a secret), service.md, webhooks.md, and examples/integrations/remote-access (add the viewer behind the same tunnel).

Tests (critical only): authentication on every web route, cookie integrity (tampered, expired, and wrong-token-signed cookies rejected), token separation (the web token is rejected by `/v1/` and `/mcp`, the API token by `/login`), one cross-origin POST rejected, the enabled-project boundary, token hygiene, `no-store` on signed-in pages, control-sequence stripping in log rendering, upcoming collapsing in internal/app, shutdown with an open stream, and 404 when web is off. One integration test. No template snapshot tests.

Non-goals: queue, cancel, pause, and resume (NOON-99); run links in notifications (NOON-100); duration charts (NOON-101); enable, approve, add, edit, or remove (never, per decision-6); accounts, OAuth, per-client tokens; TLS; CORS; mounting under a path prefix; a JS framework, Node, or build step; a separate web daemon or `noon web` command; browser push notifications, a web app manifest, or a service worker; log search beyond the browser's find; response compression (the proxy can do it).

Dependencies: NOON-97 (the `serve:` block that `serve.web_token_file` extends and that lets the service serve the listener). Follows decision-1, decision-2, decision-4, decision-6. No new module dependency.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [ ] `task cli:test PKG=./internal/web -- -count=1` exits 0, covering: every web route except `/login` and `/assets/*` redirects 303 to `/login` (SSE 401) without a valid cookie, including tampered, expired, and other-token-signed cookies; `POST /login` with a wrong token or the API token returns 401 and sets no cookie, the web token returns 303 with `HttpOnly`, `SameSite=Strict`, and `Secure` only for TLS or `X-Forwarded-Proto: https`; a cross-origin POST (`Sec-Fetch-Site: cross-site`) returns 403; every web response carries the CSP, nosniff, and referrer headers, and signed-in pages and SSE carry `Cache-Control: no-store`; a log containing SGR colors, an OSC 8 hyperlink, a title-setting OSC sequence, and C1 controls renders only the text and color classes.
- [ ] `task cli:test PKG=./integration -- -run '^TestWebViewer$' -count=1 -v` exits 0. With the built binary, isolated HOME/XDG/state, user config `serve: {listen: 127.0.0.1:0, api_token_file: A, web_token_file: W}`, one enabled project with jobs `hello` and failing `boom`, and one registered but disabled project: after logging in through a proxy-style request (`Host: noon.example.com`, `X-Forwarded-Proto: https`, matching `Origin`), the cookie is `Secure`; `/` lists only the enabled project and shows `boom` under Failing and both jobs under Upcoming; the disabled project and its runs and an unknown ID return 404; a run queued with `bin/noon run hello --detach` appears in the `/events` stream within 3s, and `/events?run=ID` delivers a log line before the run finishes; with an `/events` stream open, SIGTERM makes `serve` exit within 2s; the web token gets 401 on `/v1/projects` and `/mcp`; neither token string appears in any HTML response, cookie, scheduler output, or the environment of a job that runs `env`.
- [ ] Without a web token file, `/`, `/login`, and `/events` return 404 and the existing webhook, API, and MCP tests pass unchanged. A web token file without a listen address exits 2; a web token equal to the API token exits 2 from `serve` and fails `noon config check`; an unreadable or 31-byte web token exits 1 before writing a scheduler heartbeat; the viewer serves with no API token configured. `task cli:test PKG=./internal/cli -- -count=1` exits 0.
- [ ] Browser check with the agent browser tooling against a local `bin/noon serve`: overview, project, and run pages render in light and dark color schemes and at 390px width with no console errors or CSP violations; Lighthouse accessibility is at least 95 on the overview and run pages; `/runs/{id}#L3` highlights line 3; a running job streams log lines and flips to its final status without a reload; a failing run changes the title to `✗ 1 · noon` and the favicon to its failing variant live; hiding the tab closes the stream and showing it reconnects and refreshes; times show in the browser timezone (emulate one different from the project). Observations recorded in Notes.
- [ ] docs/web.md (enable, token generation, background service, Caddy and nginx examples preserving `Host` with `proxy_buffering off`, private link requirement, web vs API token), README, docs/cli.md, docs/configuration.md, docs/design.md (web UI non-goal and RunWisp refusal removed; integrations and security model updated), docs/docker.md, docs/service.md, docs/webhooks.md, and examples/integrations/remote-access describe only implemented behavior. `go list -m all` shows no new module and `internal/web/assets` holds no third-party code. `task ci` exits 0.

- [ ] `task cli:test PKG=./internal/app -- -count=1` exits 0, including upcoming runs: an `every 1m` job yields one collapsed row, an hourly job yields one row per occurrence, and the list caps at 50 in time order across projects.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
