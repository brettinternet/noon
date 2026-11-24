---
id: decision-6
title: An optional web viewer is an authenticated adapter on the scheduler listener
date: '2026-11-23 12:00'
status: accepted
---
## Context

`docs/design.md` listed a web UI as a non-goal and refused RunWisp's web dashboard. The CLI and
TUI see one host only from a terminal on that host. Users want to see every project the host's
scheduler serves, with recent failures, upcoming runs, and live logs, from a browser on another
machine, through a reverse proxy on another host or with none. From there they also want to stop
and restart scheduled work. The scheduler already runs in the background (decision-1). With
`serve --listen --api-token-file`, it already serves an authenticated JSON API and MCP whose
authority stops at inspect, queue, read, and cancel (decision-2). That token is meant for
automation, including agents.

## Decision

1. The web viewer is an adapter inside the one scheduler process (decision-1), mounted on the
   existing listener. It is never a separate daemon, and Noon never backgrounds itself. It runs
   in the background wherever `noon serve` does: the per-user service or a container.
2. It is off by default. A listener plus a web token file (`serve.web_token_file` in user config,
   or `--web-token-file`) turns it on; nothing else does. Without one, web routes return 404 and
   the listener behaves as before. Listener settings come from flags or from user config only,
   never from repository or private project config, so a commit can't open a port.
3. The web token is a separate credential for people; the API token stays the automation
   credential. They must differ, and either works without the other. Holding the web token
   grants web authority only; `/v1/` and `/mcp` never accept it.
4. Web authority is the remote API's (inspect enabled projects, queue approved jobs, read runs and
   logs, cancel runs) plus pause and resume: of one job, or of every job in a project at once,
   with the same windows as the CLI. Pause and resume can't widen what runs. They stop work, or
   restart an already-approved definition on its own schedule with no catch-up. The viewer never
   adds, edits, removes, enables, disables, or approves jobs, and shows the CLI command where
   that is needed. `/v1/`, HTTP MCP, and stdio MCP still can't pause or resume. Widening any
   surface's authority further needs a new decision.
5. Every pause records who requested it (`cli`, `web:ADDR`), so status and the viewer can show who
   paused a job and when it resumes. A pause that would end an active temporary schedule
   (which resume never restores) needs an explicit confirmation in the viewer.
6. Reach equals the remote API's: only enabled projects (including the user project) and their
   runs. No route accepts a filesystem path.
7. A login form exchanges the web token for a stateless session cookie: an expiry plus an HMAC
   keyed by the web token, set `HttpOnly`, `SameSite=Strict`, and `Secure` when the request
   arrived over HTTPS (directly or with `X-Forwarded-Proto: https`). Changing the web token and
   restarting signs everyone out. State-changing requests go through the standard library's
   `http.CrossOriginProtection`. Responses send a same-origin Content Security Policy without
   inline script or style and refuse framing.
8. Noon still serves no TLS and no CORS. A reverse proxy is optional. It must preserve `Host`, and
   the link between the proxy and Noon must be private (loopback, a tailnet, or a VPN), because
   the token and cookie cross it unencrypted.
9. Stack: the standard library only: `net/http` method-and-wildcard routing,
   `http.CrossOriginProtection`, `http.ResponseController` for SSE write deadlines,
   `html/template`, `embed`, and `crypto/hmac`. Pages are rendered on the server and work with
   forms and redirects before any script runs. One hand-written, same-origin JS module adds the
   rest: an `EventSource` that refetches and swaps the page's `<main>`, log-line streaming,
   copy buttons, native `<dialog>` confirmations, and keyboard shortcuts. One hand-written CSS
   file uses custom properties and `light-dark()`. There is no htmx, Alpine, templ, CSS
   framework, Node, build step, or new module dependency. Rules stay in `internal/app`
   (decision-4).

## Consequences

- This supersedes the web UI non-goal and the RunWisp "Web dashboard" refusal in
  `docs/design.md`. NOON-98 updates both, and NOON-99 updates the security model's authority row.
- There are now three interactive surfaces (CLI, TUI, web). Each read and action must be an
  `internal/app` operation shared with the others, never logic inside the web package.
- Two shared tokens, not accounts: anyone with the web token can pause the host's jobs. Identity
  beyond `requested_by` metadata, OAuth, and per-user tokens remain out of scope.
- Listener changes in user config take effect only after `noon service restart` or a new
  `noon serve`.
- Pause rows gain a `requested_by` column (a store migration). Existing rows read as `cli`.
- Logs can contain secrets, so signed-in pages are never cached, and log rendering drops terminal
  control sequences.
- The viewer shares the scheduler process, so its cost must not grow with open tabs: one SQLite
  change watcher, one stream per tab, bounded streams, and streams closed at shutdown.
- Delivery is phased: listener settings in user config so the background service can serve the
  listener (NOON-97), a read-only viewer (NOON-98), then queue, cancel, pause, and resume
  (NOON-99). Run links in hook notifications (NOON-100) and per-job duration charts (NOON-101)
  follow the viewer.
