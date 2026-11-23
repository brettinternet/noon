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
machine, through a reverse proxy on another host or with none. The scheduler already runs in
the background (decision-1). With `serve --listen --api-token-file`, it already serves an
authenticated JSON API and MCP whose authority stops at inspect, queue, read, and cancel
(decision-2).

## Decision

1. The web viewer is an adapter inside the one scheduler process (decision-1), mounted on the
   existing listener. It is never a separate daemon, and Noon never backgrounds itself. It runs
   in the background wherever `noon serve` does: the per-user service or a container.
2. It is off by default. It needs a listener, the API token, and an explicit `web` setting or
   `--web` flag. Without them, web routes return 404 and the listener behaves as before.
   Listener settings come from flags or from user config only, never from repository or
   private project config, so a commit can't open a port.
3. Authority never exceeds the remote API's: inspect enabled projects, queue approved jobs, read
   runs and logs, and cancel runs. The viewer never adds, edits, removes, enables, disables,
   approves, pauses, or resumes jobs. Where an action needs that authority, it shows the CLI
   command. Widening authority needs a new decision.
4. Reach equals the remote API's: only enabled projects (including the user project) and their
   runs. No route accepts a filesystem path.
5. Authentication reuses the shared API token. A login form exchanges the token for a
   stateless session cookie: an expiry plus an HMAC keyed by the token, set `HttpOnly`,
   `SameSite=Strict`, and `Secure` when the request arrived over HTTPS (directly or with
   `X-Forwarded-Proto: https`). Changing the token and restarting signs everyone out. Every
   state-changing request must carry an `Origin` that matches the request `Host`. Responses send
   a same-origin Content Security Policy without inline script and refuse framing.
6. Noon still serves no TLS and no CORS. A reverse proxy is optional. It must preserve `Host`, and
   the link between the proxy and Noon must be private (loopback, a tailnet, or a VPN), because
   the token and cookie cross it unencrypted.
7. The viewer is rendered on the server with `html/template` and assets compiled in through
   `embed`. Live updates use Server-Sent Events, and any script is small, hand-written, and
   same-origin. There is no Node, no build step, no JS framework, and no new module dependency.
   Rules stay in `internal/app` (decision-4).

## Consequences

- This supersedes the web UI non-goal and the RunWisp "Web dashboard" refusal in
  `docs/design.md`. The implementing task updates both.
- There are now three interactive surfaces (CLI, TUI, web). Each read and action must be an
  `internal/app` operation shared with the others, never logic inside the web package.
- Listener changes in user config take effect only after `noon service restart` or a new
  `noon serve`.
- A single shared token means no per-user identity or audit beyond `requested_by` metadata.
  Accounts, OAuth, and per-client tokens remain out of scope.
- Delivery is phased: listener settings in user config so the background service can serve the
  listener (NOON-97), a read-only viewer (NOON-98), then queue and cancel actions (NOON-99).
