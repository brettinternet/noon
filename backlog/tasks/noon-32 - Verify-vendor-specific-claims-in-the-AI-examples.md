---
id: NOON-32
title: Verify vendor-specific claims in the AI examples
status: To Do
assignee: []
created_date: '2026-10-16 22:09'
updated_date: '2026-10-16 22:21'
labels:
  - integration
  - examples
dependencies: []
documentation:
  - docs/triggers.md
  - docs/agents.md
  - examples/README.md
modified_files:
  - examples/ai/**
  - integration/examples_test.go
  - mise.toml
priority: medium
type: docs
ordinal: 3300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: AI examples rely on documented vendor flags and hook contracts, with permission and verification claims no stronger than the evidence.

Why: these examples were partly written from memory. Wrong flags can hang unattended jobs; claiming an agent has no tools when it retains file access is unsafe for untrusted PR input.

Verify each claim against versioned installed help and first-party documentation:
- Claude Code Stop hooks: exit 2/stderr feedback, session_id and stop_hook_active, CLAUDE_PROJECT_DIR expansion, timeout units, and whether the demonstrated headless mode executes Stop hooks.
- Codex hook JSON against the sources cited in docs/triggers.md.
- Every profile in ai/agent-profiles/user-config.yaml: permission-prompts, acceptEdits, allowedTools patterns, Read/Grep/Glob permissions, Codex stdin and sandbox flags, Gemini argument prompts, Ollama stdin prompts.
- Fresh-worktree trust and unattended Codex behavior in background-agents/scripts/agent-worktree.sh.
- pr-review/scripts/pr-review.sh and issue-triage.sh versus the README's "no tools" promise. No permission prompt is not proof of no tools. Use a documented tool-disable mode for untrusted input or correct the guarantee and state remaining access; never silently substitute permission-bypass flags.
- Whether cli/gh-webhook can forward signatures compatible with Noon's exact-body HMAC checks. Inspect GitHub sources through gh; document unsupported forwarding instead of inventing a secret flag.

Scope: correct affected AI scripts/config/READMEs; record a claim-by-claim table in Implementation Notes with date, tool version when available, source URL/exact help, result (confirmed/corrected/unresolved), and documentation versus live evidence. Add corresponding verification notes to affected READMEs, including background-agents. Share relevant profile findings with NOON-33; do not change its Docker files here.

Add a deterministic regression in integration/examples_test.go for the shipped verify-gate.sh using the built binary, disposable Git repo, synthetic Stop payloads, and harmless verify job. Isolate HOME/XDG/state/TMPDIR, leave the tree dirty for failure/passing-gate cases, and cover clean-tree bypass and the bounded-attempt escape. This catches a gate that bypasses verification or loops indefinitely; it does not prove vendor hook integration.

Use first-party docs/help before live probes. Where they cannot establish claimed behavior, get explicit permission for an authenticated/model-backed probe, use disposable repos and bounded duration/turns, and record sanitized commands/results. Missing credentials or budget means unresolved evidence, not a verified claim. Never call real agents or the network from automated tests.

Non-goals: core/built-in-profile changes, new agents/examples, real GitHub mutations. Record any built-in-profile defect for separately authorized implementation.

Dependencies: none. Local gate checks require Git and real jq; use existing mise setup for jq rather than mocking parsing or silently skipping. Documentation access and any approved live-probe credentials are external prerequisites.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `backlog task NOON-32 --plain` shows evidence for every claim above. Each affected README names the date, sources/tool versions, and documentation/help versus live verification; no unresolved claim is marked confirmed.
- [ ] #2 `task cli:test -- -run '^TestExampleVerifyGate$' -count=1 -v` exits 0 and prints PASS, not SKIP. The shipped gate returns 2 with bounded failure feedback for a dirty failing project, returns 0 and clears its counter on success, bypasses a clean tree, and stops blocking after MAX_ATTEMPTS. Missing jq fails with an actionable message.
- [ ] #3 Record exact first-party evidence for headless Stop-hook execution and untrusted-input tool restrictions. Where insufficient, record an explicitly authorized disposable live probe with version, command, duration/turn bound, and sanitized result before completing this criterion. Synthetic hook payloads alone are not vendor-integration evidence.
- [ ] #4 `task cli:test -- -run '^TestExamplesValidate$' -count=1 -v` exits 0 and prints PASS. Review `git diff -- examples/ai` against the evidence table: unsupported flags and guarantees are corrected, and verification notes include background-agents when its claims change.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: pr-review promises no tools, while both scripts use the Claude default described by agent-profiles as retaining Read/Grep/Glob. Verify that permission boundary explicitly. The gate skips clean trees, so the failing fixture must be dirty. Deterministic local gate checks and live vendor evidence are deliberately separate.
<!-- SECTION:NOTES:END -->
