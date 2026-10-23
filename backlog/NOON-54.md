---
id: NOON-54
title: Demonstrate recent features with runnable examples and secure remote access
status: done
labels: [reviewed]
blocked_by: [NOON-48, NOON-49, NOON-50, NOON-51, NOON-52, NOON-53]
rank: aa0be82fa0be82f6c
created: 2026-10-18T03:14:00Z
updated: 2026-10-18T17:49:00Z
---

Outcome: teach remote API/MCP, per-event requests, built-in agents, platform constraints, noop polls, and orphan recovery through safe copyable examples. Cloudflare named tunnels are the preferred public deployment; ngrok is a short optional alternative. Scope/modified-file contract: examples/**, integration/examples_test.go, integration/remote_api_test.go if existing helpers can be reused, and this task record. Non-goals: runtime feature changes, automatic tunnel/service installation, live credentials or model execution, new dependencies, changing reconciliation examples to each. Dependencies: NOON-48, NOON-49, NOON-50, NOON-51, NOON-52, NOON-53 (all Done); existing built-in agent profiles are implemented.

## Acceptance criteria

- [x] task cli:test PKG=./integration -- -run "^(TestExamplesValidate|TestExampleEventRequests|TestExampleRemoteAccess)$" -count=1 -v exits 0; shipped examples preserve event payloads and deduplicate requests, and the remote walkthrough queues and reads a harmless job behind bearer authentication in isolated local state.
- [x] task cli:test PKG=./integration -- -run "^(TestExampleScripts|TestExampleVerifyGate|TestRemoteAPI)$" -count=1 exits 0; existing integrations remain intact. Manual documentation review checks named Cloudflare deployment, optional ngrok, built-in profiles, platform filtering, noop outcomes and orphan recovery against implementation and vendor sources.
- [x] task ci exits 0 and task check:staged exits 0 before commit; no real agent calls or public tunnels are required for local verification.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Add isolated, harmless event-request and remote-access projects with fixture scripts; document Cloudflare named tunnels, ngrok, and HTTP MCP. 2. Extend agent profiles, local overrides, noop poll guidance, orphan recovery, and the examples index without changing runtime semantics. 3. Exercise shipped scripts through focused built-binary tests, obtain independent review of secret handling and copyable commands, run full CI, then commit and push as requested.

## Notes

AC#1: task cli:test PKG=./integration -- -run "^(TestExamplesValidate|TestExampleEventRequests|TestExampleRemoteAccess)$" -count=1 -v -> passed. Tests execute shipped scripts with the built binary, isolated HOME/XDG/state, merge/each payload retention, duplicate-key protection, and authenticated API discovery/queue/wait/logs. No live provider or agent calls.
AC#2: task cli:test PKG=./integration -- -run "^(TestExampleScripts|TestExampleVerifyGate|TestRemoteAPI)$" -count=1 -> passed. Independent read-only reviewer f0c38652-f576-47c3-81ae-bca42f9cc5d1 found no validated defects in secret handling, scope, copyable commands or semantic claims. Official Cloudflare named-tunnel/ingress/Quick-Tunnel/Access and ngrok CLI sources checked. Installed cloudflared 2026.10.0 ingress validate passed; ingress rule matched noon.example.com/mcp to loopback and other.example.com/mcp to 404. Installed codex mcp add --help confirmed URL and bearer-token environment flags. Isolated direct bin/noon platform execution: mac-info -> Darwin/exit 0; arm-info -> arm64/exit 0; linux-info -> unsupported_platform/exit 1; list JSON reports platform_supported correctly.
AC#3 partial: task ci -> passed (security, formatting/vet/Staticcheck, tests, race, smoke); log /tmp/noon-54-ci.log. Staged checks and final delivery pending. Go LSP clean; shell/YAML LSP reports unknown, so actual config validation and script execution are authoritative. Quoted existing Codex dash arguments to resolve advisory YAML syntax findings.
Scope: examples and integration/examples_test.go only, plus this record. Reused existing remote test helpers; no dependencies, runtime features, services, public tunnels, or worktrees added. Cloudflare named tunnel is preferred; ngrok is a short optional transport. Public authenticated tunnel deployment remains unverified and explicitly documented. All generated test scheduler processes stopped; temporary platform and smoke fixtures retained under the OS temp directory for inspection.

AC#3: task check:staged -> passed before implementation commit 1c50052 (format/re-stage and staged secret scan). task ci -> passed again on committed 1c50052; /tmp/noon-54-committed-ci.log records clean history secret scan, govulncheck, formatting/vet/Staticcheck, all tests, race, and smoke. No existing tests were deleted, skipped or weakened. No runtime contract changed, so docs/design.md needs no update. All acceptance criteria and Definition of Done are satisfied. No worktree was created or retained. User explicitly requested commit and push; delivery will push main after this task-finalization record is committed.

Post-completion review: one P3 fixed. integration/examples_test.go inserted the new tests between TestExampleVerifyGate's doc comment and its func, so TestExampleEventRequests carried the verify-gate comment and TestExampleVerifyGate had none; moved the comment back. Verified doc claims against code: HTTP status mapping (202 created, 200 duplicate, 401, 409 needs_approval, disabled/unknown 404 via EnabledProjectInput/EnabledRunProject), serve --listen/--api-token-file, unauthenticated /healthz, built-in gemini/opencode/pi commands in internal/config/jobs.go, noop_exit_code 78 present in each referenced example, history/logs --status. task cli:test PKG=./integration -- -run '^(TestExamplesValidate|TestExampleEventRequests|TestExampleRemoteAccess|TestExampleScripts|TestExampleVerifyGate|TestRemoteAPI)' -count=1 passed. Full CI not rerun (comment-only source change).

## Summary

Added runnable event-request and authenticated remote-access examples with a validated named Cloudflare Tunnel template and optional ngrok path. Refreshed built-in/custom agents, platform constraints, noop polling, orphan recovery, and the index. Built-binary example tests and full CI pass; independent review found no defects. Public provider connections are explicitly unverified. Implementation: 1c50052.
