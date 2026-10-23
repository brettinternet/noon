---
id: NOON-49
title: Control the scheduler remotely over an authenticated JSON API and MCP over HTTP
status: done
type: feature
priority: medium
labels: [http, mcp, security, reviewed]
blocked_by: [NOON-48]
rank: a91dc47711dc476e2
refs: [docs/design.md, docs/webhooks.md, docs/agents.md, docs/docker.md, backlog/decisions/decision-1 - One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md, backlog/decisions/decision-4 - Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md]
files: [internal/httpapi/**, internal/mcp/server.go, internal/mcp/server_test.go, internal/app/projects.go, internal/scheduler/serve.go, internal/scheduler/serve_test.go, internal/cli/serve.go, internal/cli/root.go, internal/cli/serve_test.go, integration/remote_api_test.go, README.md, docs/agents.md, docs/cli.md, docs/design.md, docs/docker.md, docs/webhooks.md]
created: 2026-10-16T01:43:00Z
updated: 2026-10-17T21:11:00Z
---

Why: a scheduler in a container (docs/docker.md, examples/integrations/docker-agent) or on another host can only be inspected or controlled through `docker exec` or a shell, and agents reach Noon only through stdio MCP on the same machine.

Outcome: `noon serve --listen ADDR --api-token-file FILE` also serves an authenticated JSON API under `/v1/` and MCP over Streamable HTTP at `/mcp`, so a remote client can list enabled projects and jobs, queue approved jobs, read runs and logs, and cancel runs. Without `--api-token-file` the listener behaves exactly as today: signed webhooks and `/healthz` only.

Settled contract:
- Auth: one shared bearer token, read once at startup from `--api-token-file`. A file works with Docker secrets and keeps the token out of the environment that jobs and hooks inherit. A trailing newline is trimmed; a missing or unreadable file, or a token shorter than 32 bytes, fails startup with exit 1 before scheduling. `--api-token-file` without `--listen` is a usage error (exit 2). Every `/v1/` and `/mcp` request needs `Authorization: Bearer TOKEN`, compared in constant time and checked before routing; otherwise 401 with `WWW-Authenticate: Bearer` and no detail. The token is never logged, stored, echoed, or passed to jobs or hooks. `/healthz` stays unauthenticated and webhooks keep their HMAC signatures.
- Authority equals stdio MCP: inspect, queue approved jobs, read logs, cancel runs. Nothing adds, edits, removes, enables, disables, approves, pauses, or resumes jobs (decision-2).
- Reach: only projects enabled in the state store. A disabled, unregistered, or unknown project, and a run belonging to one, is 404 `not_found`. Nothing reachable over HTTP accepts an arbitrary filesystem path.
- JSON API: one route per MCP tool. Bodies are the same objects as the matching MCP structured output and CLI `--json` (`schema_version: 1`); errors are the CLI JSON error object.
  - `GET /v1/projects`: enabled projects with `id`, `name`, `path`
  - `GET /v1/projects/{id}`: status
  - `GET /v1/projects/{id}/jobs`: list_jobs
  - `GET /v1/projects/{id}/runs?job=&status=&limit=`: list_runs
  - `POST /v1/projects/{id}/jobs/{name}/runs` with an optional JSON body `{key, in, at, data}`: run_job; 202 when a run is created, 200 when an existing run is returned
  - `GET /v1/runs/{id}?wait_seconds=&log_bytes=&after=&tail=&match=&context=&section=`: get_run
  - `POST /v1/runs/{id}/cancel`: cancel_run
  Status mapping: 400 usage, 401 auth, 404 not_found, 405 wrong method (with `Allow`), 409 not_enabled/disabled/paused/needs_approval/recursion, 413 body over 512 KiB (`data` stays at most 256 KiB), 429 queue_full, 500 otherwise. `requested_by` is `api:REMOTE_ADDR`.
- MCP over HTTP: the existing go-sdk's stateless `mcp.NewStreamableHTTPHandler`, with the stdio tools and schemas plus a read-only `list_projects` tool added to both transports (same output as `GET /v1/projects`). Over HTTP there is no default project, so project-scoped tools require `project` (the absolute path of an enabled project). `get_run` by `run_id` and `cancel_run` work as on stdio, limited to enabled projects. `requested_by` stays `mcp:CLIENT/VERSION`.
- Timeouts: `wait_seconds` up to 60 must work on both transports. Extend the write deadline per request; webhook body, header, and time limits stay unchanged.
- No CORS headers, cookies, web UI, or TLS; use a reverse proxy, as for webhooks. Binding a non-loopback address keeps the existing warning, now naming the API.

Scope: internal/httpapi (auth, API handlers, mounting the MCP handler beside webhooks), internal/mcp (HTTP server construction without a default project, `list_projects`, the enabled-project restriction), one internal/app operation listing enabled projects, flag parsing and startup validation in internal/cli and internal/scheduler, and docs: README, agents.md (remote MCP setup), cli.md, design.md (integrations and security model), docker.md (token via a secret file), webhooks.md (shared listener). Remote MCP client setup examples must be checked against the client's installed `--help` or official docs, recorded in Implementation Notes.

Tests (critical only): authentication on every route, the enabled-project boundary, and token hygiene are security boundaries; one built-binary integration test covers both transports. Extend existing internal/httpapi and internal/mcp tests for routing and error mapping.

Non-goals: named or per-client tokens, token rotation without restart, OAuth, TLS, CORS, a web UI, write or admin endpoints, the CLI acting as a remote client, pagination beyond `limit`/`after`, selecting runs by job name over the JSON API, and `noon service install` support for the new flags (document running `serve` directly or in Docker).

Dependencies: NOON-48. Both change queue error mapping in internal/httpapi; land per-event requests first so the API maps `queue_full`. Follows decision-1 (an adapter inside the one scheduler process; local clients still coordinate through SQLite), decision-2, and decision-4 item 5 (adapters over internal/app). No new module dependency.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/cli -count=1` exits 0. With the built binary in an isolated state dir: `bin/noon serve --api-token-file F` without `--listen` exits 2; `--listen 127.0.0.1:0` with a missing file or a 31-byte token exits 1 before writing a scheduler heartbeat. Without the flag, `/v1/projects` and `/mcp` return 404 while signed webhooks and `/healthz` behave as before (existing webhook tests pass unchanged).
- [x] `task cli:test PKG=./integration -- -run '^TestRemoteAPI$' -count=1 -v` exits 0. With the built binary, isolated HOME/XDG/state, one enabled project with job `hello` and one registered but disabled project: a missing or wrong token gets 401 on every `/v1/` route and `/mcp`; `GET /v1/projects` lists only the enabled project; queueing `hello` with `data` returns 202, and `GET /v1/runs/{id}?wait_seconds=30` returns `succeeded` with its log; cancelling an `in: 1h` run returns `cancelled`; the disabled project's ID and an unknown ID return 404; PUT/DELETE return 405 and no add, enable, approve, or pause route exists.
- [x] The same test drives `/mcp` with the go-sdk Streamable HTTP client: `tools/list` returns the stdio tools plus `list_projects`; `run_job` with the enabled project's path, then `get_run` with `wait_seconds`, reaches `succeeded`; `list_jobs` with the disabled project's path or any other absolute path returns a `not_found` tool error, and omitting `project` returns a `usage` tool error.
- [x] The same test proves token hygiene: the token string appears in no scheduler output, run log, event file, `noon history --json` output, or the environment of a job that runs `env`.
- [x] `task cli:test PKG=./internal/mcp -count=1` exits 0, including `list_projects` on stdio. The README and the docs in scope describe only implemented routes and flags, and remote client setup examples are verified and recorded. `task ci` exits 0.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Modified-file contract expansion: internal/store/events.go is authorized to accept api as an event source under the existing cli/mcp JSON validation and 256 KiB limit. Verified writeEvent currently rejects api, preventing the required JSON API queue-with-data acceptance case. Preserve truthful source attribution rather than labeling API events as mcp; verify source and persisted data in the already-required integration/remote_api_test.go. docs/design.md event-source contract will include api (already in scope).

Additional modified-file deviation: integration/serve_test.go changes only the shared listener-address helper to accept both unchanged webhook-only and new API-enabled listener log prefixes. This reuses the existing helper for TestRemoteAPI without changing any existing webhook assertions. Executor made this minimal deviation before requesting approval; recorded on parent inspection. AC command correction: Go test flags require -- before -count=1; use task cli:test PKG=./internal/cli -- -count=1 and task cli:test PKG=./internal/mcp -- -count=1. Executor reports both pass, HTTP API tests pass, TestRemoteAPI passes, and task ci passes. Remote client examples verified with mise exec -- claude mcp add --help and mise exec -- codex mcp add --help. Independent security review pending.

AC#1: task cli:test PKG=./internal/cli -- -count=1 -> passed (corrected argument separator); TestRemoteAPI built-binary startup cases prove exit 2 without listen, exit 1 for missing/31-byte token before heartbeat, and tokenless API/MCP 404 with signed webhooks and health preserved.
AC#2: task cli:test PKG=./integration -- -run ^TestRemoteAPI$ -count=1 -v -> passed; authenticated routes, queue-with-data, wait/logs, cancel, disabled/unknown resources, unsupported methods/admin routes covered.
AC#3: same TestRemoteAPI -> passed; SDK HTTP client tools, queue/wait, required project and enabled-project restrictions verified, now with public reverse-proxy Host on every MCP request.
AC#4: same TestRemoteAPI -> passed; token absent from scheduler output, logs, event files, history JSON and job environment.
AC#5: task cli:test PKG=./internal/mcp -- -count=1 -> passed (corrected argument separator), including stdio list_projects. README and scoped docs updated. Installed claude mcp add --help confirmed HTTP transport/header options; codex mcp add --help confirmed URL/bearer-token-env options. task ci -> passed on committed implementation and again on integrated main b65e6c9 (log /tmp/noon-49-integrated-ci.log).
Single independent security review completed. Fixed confirmed reverse-proxy Host rejection by disabling SDK localhost protection only behind bearer authentication; integration regression passes. Also reproduced delayed MCP body plus full 60s wait failing with socket hang up; extended API-only deadline to 75s (10s body budget + 60s wait + response margin), unchanged webhook limits. Isolated /tmp/noon-49-wait-probe.js then returned HTTP 200 after 67.010s. LSP diagnostics clean for scheduler and integration edits. No tests deleted, skipped or weakened; staged scanner false positive resolved by generating a synthetic test token.
Delivery: implementation commit b65e6c9d965caf7fe7196852b5367e42fbe31119 fast-forwarded into main after rebasing onto concurrent unrelated main commits. Worktrunk removed owned worktree and branch; Herdr cleanup hook ran. No push. No remaining blocker or resumable implementation step; final task-record commit and claim release remain.

Post-completion review: no actionable defects found. Inspected bearer authentication before routing, constant-time digest comparison, enabled-project and run restrictions across JSON API/HTTP MCP, absence of administrative routes, bounded bodies and error mapping, token startup validation before scheduler state, token isolation, and API-only write-deadline extension. task cli:test PKG="./internal/cli ./internal/httpapi ./internal/mcp" -- -count=1 passed all three packages. task cli:test PKG=./integration -- -run ^TestRemoteAPI$ -count=1 -v passed, exercising both live transports, reverse-proxy Host, unauthorized requests, disabled resources, queue/wait/cancel, token hygiene, and tokenless webhook compatibility. No source changes; full CI was not rerun for this review.
