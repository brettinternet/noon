---
id: BUMP-8
title: Accept authenticated external webhook events
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-7
documentation:
  - docs/design.md
modified_files:
  - internal/webhook/**
  - internal/events/**
  - internal/config/**
  - internal/cli/**
  - integration/**
  - docs/integrations.md
priority: medium
type: feature
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: remote CI/review events can safely trigger preconfigured jobs without exposing arbitrary command execution.
Scope: explicit listener enablement, loopback default, named endpoint routing, HMAC authentication and bounded replay window/IDs, body limits, cooldown/rate policy reuse, redacted diagnostics and shutdown. Include an adapter/example for GitHub delivery headers without allowing payload-defined commands. Require explicit non-loopback configuration and document TLS/reverse-proxy responsibility.
Non-goals: public tunnel provisioning, unauthenticated endpoints, generic remote shell, scheduler auto-start, outbound HTTP job actions.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestWebhookAuthorization' -count=1 -v exits 0: missing/invalid signatures, replay, stale timestamp where used, unknown targets, and oversized bodies are rejected without dispatch.
- [ ] #2 task cli:test -- -run '^TestWebhookDelivery' -count=1 -v exits 0: a signed local request starts the configured job once, propagates bounded event data without shell injection, and secrets never appear in logs.
- [ ] #3 task cli:test -- -run '^TestWebhookDisabledByDefault' -count=1 -v exits 0 with no listening socket until explicitly enabled.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
