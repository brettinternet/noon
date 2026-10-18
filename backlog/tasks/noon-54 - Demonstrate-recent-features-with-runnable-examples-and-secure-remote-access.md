---
id: NOON-54
title: Demonstrate recent features with runnable examples and secure remote access
status: In Progress
assignee: []
created_date: '2026-10-18 03:14'
updated_date: '2026-10-18 03:25'
labels: []
dependencies:
  - NOON-48
  - NOON-49
  - NOON-50
  - NOON-51
  - NOON-52
  - NOON-53
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: teach remote API/MCP, per-event requests, built-in agents, platform constraints, noop polls, and orphan recovery through safe copyable examples. Cloudflare named tunnels are the preferred public deployment; ngrok is a short optional alternative. Scope/modified-file contract: examples/**, integration/examples_test.go, integration/remote_api_test.go if existing helpers can be reused, and this task record. Non-goals: runtime feature changes, automatic tunnel/service installation, live credentials or model execution, new dependencies, changing reconciliation examples to each. Dependencies: NOON-48, NOON-49, NOON-50, NOON-51, NOON-52, NOON-53 (all Done); existing built-in agent profiles are implemented.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 task cli:test PKG=./integration -- -run "^(TestExamplesValidate|TestExampleEventRequests|TestExampleRemoteAccess)$" -count=1 -v exits 0; shipped examples preserve event payloads and deduplicate requests, and the remote walkthrough queues and reads a harmless job behind bearer authentication in isolated local state.
- [x] #2 task cli:test PKG=./integration -- -run "^(TestExampleScripts|TestExampleVerifyGate|TestRemoteAPI)$" -count=1 exits 0; existing integrations remain intact. Manual documentation review checks named Cloudflare deployment, optional ngrok, built-in profiles, platform filtering, noop outcomes and orphan recovery against implementation and vendor sources.
- [ ] #3 task ci exits 0 and task check:staged exits 0 before commit; no real agent calls or public tunnels are required for local verification.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add isolated, harmless event-request and remote-access projects with fixture scripts; document Cloudflare named tunnels, ngrok, and HTTP MCP. 2. Extend agent profiles, local overrides, noop poll guidance, orphan recovery, and the examples index without changing runtime semantics. 3. Exercise shipped scripts through focused built-binary tests, obtain independent review of secret handling and copyable commands, run full CI, then commit and push as requested.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:test PKG=./integration -- -run "^(TestExamplesValidate|TestExampleEventRequests|TestExampleRemoteAccess)$" -count=1 -v -> passed. Tests execute shipped scripts with the built binary, isolated HOME/XDG/state, merge/each payload retention, duplicate-key protection, and authenticated API discovery/queue/wait/logs. No live provider or agent calls.
AC#2: task cli:test PKG=./integration -- -run "^(TestExampleScripts|TestExampleVerifyGate|TestRemoteAPI)$" -count=1 -> passed. Independent read-only reviewer f0c38652-f576-47c3-81ae-bca42f9cc5d1 found no validated defects in secret handling, scope, copyable commands or semantic claims. Official Cloudflare named-tunnel/ingress/Quick-Tunnel/Access and ngrok CLI sources checked. Installed cloudflared 2026.10.0 ingress validate passed; ingress rule matched noon.example.com/mcp to loopback and other.example.com/mcp to 404. Installed codex mcp add --help confirmed URL and bearer-token environment flags. Isolated direct bin/noon platform execution: mac-info -> Darwin/exit 0; arm-info -> arm64/exit 0; linux-info -> unsupported_platform/exit 1; list JSON reports platform_supported correctly.
AC#3 partial: task ci -> passed (security, formatting/vet/Staticcheck, tests, race, smoke); log /tmp/noon-54-ci.log. Staged checks and final delivery pending. Go LSP clean; shell/YAML LSP reports unknown, so actual config validation and script execution are authoritative. Quoted existing Codex dash arguments to resolve advisory YAML syntax findings.
Scope: examples and integration/examples_test.go only, plus this record. Reused existing remote test helpers; no dependencies, runtime features, services, public tunnels, or worktrees added. Cloudflare named tunnel is preferred; ngrok is a short optional transport. Public authenticated tunnel deployment remains unverified and explicitly documented. All generated test scheduler processes stopped; temporary platform and smoke fixtures retained under the OS temp directory for inspection.
<!-- SECTION:NOTES:END -->
