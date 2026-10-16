---
id: BUMP-10
title: Expose bounded scheduling and inspection over MCP
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-5
  - BUMP-7
documentation:
  - docs/design.md
modified_files:
  - internal/mcp/**
  - internal/app/**
  - internal/cli/**
  - internal/config/**
  - integration/**
  - docs/mcp.md
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: agents can inspect work, trigger existing jobs, and schedule bounded one-shot work through the same operations as the CLI.
Scope: official Go MCP SDK stdio adapter; list/status/history/logs, trigger named jobs, schedule/cancel one-shot tasks; stable IDs, structured errors, idempotency, bounded inputs/outputs, operator-configured policy and audit events. Default-deny arbitrary executables, recurring job creation, hooks, and shared-config writes; explicitly document opt-in policy rather than trusting actor metadata. Disconnecting MCP must not remove accepted durable work or start an implicit daemon.
Non-goals: second scheduler, provider SDKs, remote HTTP MCP, autonomous unlimited recurrence, agent-supplied authorization.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestMCPProtocol' -count=1 -v exits 0 via real stdio JSON-RPC: initialization, tools/list, structured tool results/errors, and no logging on protocol stdout.
- [ ] #2 task cli:test -- -run '^TestMCPPolicy' -count=1 -v exits 0: denied mutations perform no side effect; allowed one-shot submission is idempotent and has expiry/run limits; payload cannot grant itself permission.
- [ ] #3 task cli:test -- -run '^TestMCPDurability' -count=1 -v exits 0: client disconnect leaves accepted work intact and no scheduler auto-starts when unavailable.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
