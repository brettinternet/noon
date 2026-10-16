---
id: DRAFT-2
title: Serve a remote API and MCP over HTTP
status: Draft
assignee: []
created_date: '2026-10-16 01:43'
labels:
  - http
  - mcp
  - security
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Promote when someone runs Noon in a container or remote host and needs to control it without `docker exec`. Promotion rewrites this record to the ready-task standard in AGENTS.md (outcome, scope, non-goals, modified-file contract, dependencies, acceptance commands).

Idea: on the same `noon serve --listen` listener as webhooks, serve (1) a JSON API mirroring the CLI's JSON objects (`GET /v1/status`, `/v1/jobs`, `/v1/runs`, `/v1/runs/{id}` with its log tail, `POST /v1/jobs/{name}/runs`, `POST /v1/runs/{id}/cancel`) and (2) MCP over Streamable HTTP at `/mcp` with the same six tools as stdio MCP.

Constraints if promoted:
- Off unless a token is configured. Bearer token from a file in the state dir (created by an explicit command) or an environment variable; constant-time comparison.
- Same rules as MCP: no add, edit, enable, approve, or pause endpoints.
- The same `internal/app` operations; no new state.
- No web UI; TLS via a reverse proxy.

Open questions: a single shared token vs. named tokens recorded as `requested_by`; CORS (probably never); whether `/v1` needs pagination beyond `limit`.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
