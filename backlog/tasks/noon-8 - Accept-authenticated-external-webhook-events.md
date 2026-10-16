---
id: NOON-8
title: Accept signed webhooks
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 17:28'
labels:
  - http
  - security
dependencies:
  - NOON-7
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/httpapi/**
  - internal/config/**
  - internal/scheduler/**
  - internal/app/**
  - internal/cli/**
  - integration/**
  - docs/webhooks.md
  - README.md
priority: medium
type: feature
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: external services (GitHub, CI, monitors) can trigger opted-in jobs over HTTP with verified signatures. Payloads reach jobs as event files, and nothing in a request can choose or alter a command.

Read first: docs/design.md#integrations (Webhooks, Event files) and #security-model, decision-4. Use only `net/http`.

Scope:
- `noon serve --listen ADDR` (for example `127.0.0.1:8787`); there is no listener without the flag. A non-loopback address logs a warning that requests still require signatures. The listener shuts down gracefully (5s) with serve.
- Job field `webhook: {secret_env: NAME}`, in the digest. The secret is read from the scheduler's environment at startup. A missing or empty secret disables that endpoint and records a project config error that status and doctor show.
- Route `POST /hooks/{project_id}/{job}`; `project_id` is the integer project ID. List JSON adds `webhook_path` for webhook jobs.
- Request handling, in order:
  1. Unknown project or job, or a job without `webhook`: 404 with no detail.
  2. Body over 1 MiB (`http.MaxBytesReader`): 413.
  3. Missing or wrong signature: 401. The signature header is `X-Hub-Signature-256` or `X-Noon-Signature-256`, formatted `sha256=HEX` over the raw body, compared in constant time.
  4. Inactive (project disabled, job disabled, paused, or needs approval): 409 with a short reason.
  5. Otherwise queue a run with trigger `webhook` and `requested_by` `webhook:REMOTE_ADDR`. Use the shared queued-request operation, so keys, cooldowns, and coalescing behave exactly as on the CLI. Respond 202 `{"run_id":N,"created":true}`, or 200 `{"run_id":N,"created":false,"reason":...}` for a duplicate delivery, a cooldown, or a coalesced request.
- Deduplication: `X-GitHub-Delivery` or `X-Noon-Delivery` becomes the run's idempotency key; a value that is not a valid key gets 400. Document that senders without a delivery ID get no replay protection, and that deduplication only covers deliveries whose run is still retained (newest 50 terminal runs per job).
- Payload: an event file with `source: "webhook"` and `data` as parsed JSON (or a string when the body is not JSON). `headers` keeps only `content-type`, `user-agent`, `x-github-event`, `x-github-delivery`, and `x-noon-delivery`; never signature or authorization headers.
- `GET /healthz` returns 200 `{"status":"ok"}` while the scheduler loop is healthy. Other methods and paths return 404 or 405.
- Server limits: ReadHeaderTimeout 5s, ReadTimeout 10s, WriteTimeout 10s, MaxHeaderBytes 16 KiB.
- Add docs/webhooks.md: GitHub setup (secret, content type, events), a `curl` example computing the signature with `openssl dgst`, TLS via a reverse proxy, and Docker port publishing.

Non-goals: TLS termination, tunnels, unauthenticated endpoints, request-chosen commands or arguments, the remote API (draft), rate limiting beyond cooldown.

Tests:
- internal/httpapi `TestWebhooks` (httptest against the handler with a real store): a valid signature queues one run with the event file; bad or missing signatures get 401 and no run; an unknown job and a job without `webhook` get 404; an oversized body gets 413; a duplicate delivery returns the same run; an inactive job gets 409; signature headers are never stored; a non-JSON body is stored as a string.
- integration `TestServeListen`: with the real binary, `noon serve --listen 127.0.0.1:0` binds a free port and logs the bound address on stderr; the test reads it and gets 200 from `/healthz`. Without the flag, serve logs no listener and opens no port.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestWebhooks|TestServeListen)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `task race` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect queue, scheduler, config, and diagnostics contracts; implement signed webhook endpoints using shared queue operations.
2. Add security regression and real listener integration tests and webhook documentation.
3. Run acceptance commands and CI, review concrete security/lifecycle risks, commit and merge locally, finalize provider state and clean up owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract expansion (before edits): internal/store/** is required for shared QueueRun to persist trigger=webhook (currently hardcoded queued), serialize allowlisted event headers with the webhook payload limit, and expose persisted project lookup including disabled projects so known inactive endpoints return 409 after restart. Keep changes minimal and preserve CLI/MCP event limits. This supports existing task scope, not a new feature.

Modified-file contract expansion (before edits): docs/design.md must replace stale planned/future webhook claims and align integration details with this implemented contract, as explicitly required by Definition of Done #5. Keep this a focused documentation update.

AC#1: task cli:test -- -run '^(TestWebhooks|TestServeListen)$' -count=1 -v -> exit 0, --- PASS for both. Covers signatures/routing/inactive jobs, event payloads and safe headers, limits, duplicate/cooldown/coalescing, real listener health/shutdown and no default listener.
AC#2: task race -> exit 0 across all packages after review fixes.
Review: one independent security/integrity pass found wrong state-directory selection and insufficient serialized-header allowance. Fixed both; regressions exercise distinct default/selected stores and a signed 1 MiB non-JSON body plus a 6000-byte escaping User-Agent. Extended focused command including TestWebhookSecretSnapshotAndDiagnostics -> exit 0. No tests deleted, skipped or weakened.
Delivery: implementation 8384d3f; task check:staged and task ci passed on that commit. Merged locally as b3e4e97, preserving concurrent backlog planning commit 9554517. No push. Owned worktree/branch noon-8-webhooks removed with Worktrunk; matching Herdr workspace wD1 verified absent. No remaining item-scoped blocker or next implementation step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented opt-in signed webhook HTTP delivery through shared queue operations, startup-secret diagnostics, safe event files, replay keys, listener health/shutdown, and setup documentation. Focused acceptance, race, full CI and staged checks passed; both independent review findings fixed with regression coverage. Merged locally and cleaned up owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
