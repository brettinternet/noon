---
id: BUMP-10
title: Serve MCP tools over stdio
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 01:41'
labels:
  - mcp
  - security
dependencies:
  - BUMP-21
references:
  - 'https://github.com/modelcontextprotocol/go-sdk'
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/mcp/**
  - internal/app/**
  - internal/cli/**
  - go.mod
  - go.sum
  - README.md
priority: high
type: feature
ordinal: 1500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: coding agents can inspect jobs, queue approved jobs, read results, and cancel runs through `bump mcp`. The same rules as the CLI apply, and an agent has no way to grant itself new commands.

Read first: decision-4, docs/design.md#integrations and #security-model. Use the official SDK `github.com/modelcontextprotocol/go-sdk` (latest v1 release). Read its README and examples for server setup, typed tool handlers, and the in-memory transport.

Scope:
- `bump mcp` serves over stdio. If stdin is a terminal, it prints setup help and exits 0: `claude mcp add bump -- bump mcp`, `codex mcp add bump -- bump mcp`, and a generic JSON config snippet. `--json` is a usage error. Nothing but protocol messages goes to stdout; diagnostics go to stderr.
- Project: the server's default project comes from discovery or `-C`. Every tool takes an optional absolute `project` path to target another checkout.
- Tools. Each returns the same struct the CLI serializes for its JSON, as structured content plus a short text summary:
  - `status` {project?}: the dashboard object.
  - `list_jobs` {project?}: the list object.
  - `run_job` {name, project?, key?, in?, at?}: queue a run with exactly the rules of `bump run --detach/--in/--at`. `requested_by` is `mcp:` plus the client name and version from initialize, stored as metadata only.
  - `list_runs` {project?, job?, limit?}: the history object.
  - `get_run` {run_id, wait_seconds? (0-60), log_bytes? (default 16384, max 65536)}: the run plus the tail of its log. A wait returns as soon as the run leaves `queued` or `running`.
  - `cancel_run` {run_id}: same semantics as `bump cancel RUN`.
- Annotations: `readOnlyHint` on status, list_jobs, list_runs, and get_run; `destructiveHint` on cancel_run.
- Errors are tool errors (`isError`) carrying the same code, message, and hint as the CLI. A `needs_approval` or `not_enabled` hint tells the agent to ask the user to run `bump enable`.
- There are no tools to add, edit, remove, enable, approve, pause, or resume jobs.
- README: an MCP section with the setup lines and the security boundary.

Non-goals: HTTP transport (draft), resources and prompts, `--data` payloads (the triggers task adds a `data` argument), authentication, notifications.

Tests:
- internal/mcp `TestMCP` using the SDK's in-memory client and server transports: tools/list returns exactly the six tools; `list_jobs` content equals the `bump list --json` object; `run_job` on an unapproved shared job returns `needs_approval`; `run_job` on an approved job creates a queued run whose `requested_by` names the test client; `get_run` with a wait returns after the run finishes; cancel works on a queued run.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestMCP$' -count=1 -v` exits 0 and prints `--- PASS: TestMCP`, covering every case listed under Tests
- [ ] #2 `task cli:build && printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ac","version":"0"}}}' '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | BUMP_STATE_DIR=$(mktemp -d) bin/bump --config examples/jobs.yaml mcp` exits 0 after stdin closes and prints only JSON-RPC lines; the tools/list result names exactly status, list_jobs, run_job, list_runs, get_run, and cancel_run
- [ ] #3 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/bump` exits 0
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
