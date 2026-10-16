---
id: NOON-10
title: Serve MCP tools over stdio
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 14:50'
labels:
  - mcp
  - security
dependencies:
  - NOON-21
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
Outcome: coding agents can inspect jobs, queue approved jobs, read results, and cancel runs through `noon mcp`. The same rules as the CLI apply, and an agent has no way to grant itself new commands.

Read first: decision-4, docs/design.md#integrations and #security-model. Use the official SDK `github.com/modelcontextprotocol/go-sdk` (latest v1 release). Read its README and examples for server setup, typed tool handlers, and the in-memory transport.

Scope:
- `noon mcp` serves over stdio. If stdin is a terminal, it prints setup help and exits 0: `claude mcp add noon -- noon mcp`, `codex mcp add noon -- noon mcp`, and a generic JSON config snippet. `--json` is a usage error. Nothing but protocol messages goes to stdout; diagnostics go to stderr.
- Project: the server's default project comes from discovery or `-C`. Every tool takes an optional absolute `project` path to target another checkout.
- Tools. Each returns the same struct the CLI serializes for its JSON, as structured content plus a short text summary:
  - `status` {project?}: the dashboard object.
  - `list_jobs` {project?}: the list object.
  - `run_job` {name, project?, key?, in?, at?}: queue a run with exactly the rules of `noon run --detach/--in/--at`. `requested_by` is `mcp:` plus the client name and version from initialize, stored as metadata only.
  - `list_runs` {project?, job?, limit?}: the history object.
  - `get_run` {run_id, wait_seconds? (0-60), log_bytes? (default 16384, max 65536)}: the run plus the tail of its log. A wait returns as soon as the run leaves `queued` or `running`.
  - `cancel_run` {run_id}: same semantics as `noon cancel RUN`.
- Annotations: `readOnlyHint` on status, list_jobs, list_runs, and get_run; `destructiveHint` on cancel_run.
- Errors are tool errors (`isError`) carrying the same code, message, and hint as the CLI. A `needs_approval` or `not_enabled` hint tells the agent to ask the user to run `noon enable`.
- There are no tools to add, edit, remove, enable, approve, pause, or resume jobs.
- README: an MCP section with the setup lines and the security boundary.

Non-goals: HTTP transport (draft), resources and prompts, `--data` payloads (the triggers task adds a `data` argument), authentication, notifications.

Tests:
- internal/mcp `TestMCP` using the SDK's in-memory client and server transports: tools/list returns exactly the six tools; `list_jobs` content equals the `noon list --json` object; `run_job` on an unapproved shared job returns `needs_approval`; `run_job` on an approved job creates a queued run whose `requested_by` names the test client; `get_run` with a wait returns after the run finishes; cancel works on a queued run.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^TestMCP$' -count=1 -v` exits 0 and prints `--- PASS: TestMCP`, covering every case listed under Tests
- [x] #2 `task cli:build && printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ac","version":"0"}}}' '{"jsonrpc":"2.0","method":"notifications/initialized"}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | NOON_STATE_DIR=$(mktemp -d) bin/noon --config examples/jobs.yaml mcp` exits 0 after stdin closes and prints only JSON-RPC lines; the tools/list result names exactly status, list_jobs, run_job, list_runs, get_run, and cancel_run
- [x] #3 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0
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
1. Inspect shared app/CLI operations and official MCP SDK examples; add a thin stdio adapter with six bounded tools and shared JSON/error contracts. 2. Exercise approval, queue metadata, waiting, cancellation and transport behavior through SDK clients; update implemented help and README. 3. Run all acceptance commands and task ci, review the security boundary once, commit, merge locally, finalize backlog and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Contract clarification: the explicit per-tool schemas govern project selection. status/list_jobs/run_job/list_runs accept an optional absolute project; get_run/cancel_run resolve globally unique run IDs like CLI logs RUN/cancel RUN and do not accept an ignored project field. README will describe this distinction. Worklease claim a3045538ee11195068d92afeb3ea543c belongs to loop 53909a3c-e1ac-40d5-a491-b52b62d6687d; implementation checkout .worktrees/noon-10-mcp, branch noon-10-mcp, creation receipt .git/worktrees/noon-10-mcp/agent-creation.json.

Modified-file contract expansion: docs/design.md may be updated narrowly to describe implemented MCP queue integration and the settled project/run-ID distinction, removing now-stale future-client wording. This is required by DoD #5 and does not expand implementation scope.

Single independent review completed: three reproduced P2 defects (get_run rejects existing runs without logs; stdio serial forwarding blocks cancellation during waits; raw escaped JSON-RPC IDs hang response draining). Corrections and critical regression tests are in progress. Initial task ci and AC1/AC3 passed; AC2 equivalent pipe passed, with mktemp performed separately because shell guard rejects inline command substitution. Final checks will be rerun after corrections.

AC#1: task cli:test -- -run ^TestMCP$ -count=1 -v -> exit 0, PASS: TestMCP, covering six tools, CLI list parity, approval rejection, queued client metadata, finished wait and cancellation. AC#2: task cli:build followed by the specified initialize/initialized/tools-list printf pipe -> exit 0, two JSON-RPC response lines, exactly the six required tools. Shell guard rejects inline $(mktemp -d), so mktemp -d ran separately and its literal temporary path was supplied as NOON_STATE_DIR; protocol bytes and invocation otherwise unchanged. AC#3: CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon -> exit 0. Final task ci passed on integrated implementation commit a8582a6 (unit/integration, race, vet, Staticcheck, secrets, vulnerabilities, build and smoke). MCP SDK v1.8.0 is the official required v1 dependency; no replacement SDK or runtime added. All three review findings fixed with regression coverage; direct built-binary probes return queued/cancelled details without logs, escaped IDs exit normally, and cancellation during a 3s wait completes in 112ms. No tests deleted, skipped or weakened. Source commits f8a5525 and cc1bb4e; integrated concurrent main changes in a8582a6 and fast-forwarded main. Owned worktree/branch noon-10-mcp removed with Worktrunk and Herdr workspace wCE removed; no retained task checkout. No remaining blocker or resumable implementation step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented six bounded MCP stdio tools using shared CLI/app contracts, existing approval and opt-in checks, client metadata, bounded wait/log reads and concurrent cancellation. README/setup help/design updated. All acceptance scenarios and task ci passed; independent review findings fixed and covered by regressions. Merged locally into main at a8582a6; owned worktree and workspace cleaned up. No push.
<!-- SECTION:FINAL_SUMMARY:END -->
