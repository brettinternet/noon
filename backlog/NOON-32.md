---
id: NOON-32
title: Verify vendor-specific claims in the AI examples
status: done
type: docs
priority: medium
labels: [integration, examples, reviewed]
rank: a623b88ee23b88ec2
refs: [docs/triggers.md, docs/agents.md, examples/README.md]
files: [examples/ai/**, integration/examples_test.go, mise.toml]
created: 2026-10-16T22:09:00Z
updated: 2026-10-17T01:56:00Z
---

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

## Acceptance criteria

- [x] `backlog task NOON-32 --plain` shows evidence for every claim above. Each affected README names the date, sources/tool versions, and documentation/help versus live verification; no unresolved claim is marked confirmed.
- [x] `task cli:test -- -run '^TestExampleVerifyGate$' -count=1 -v` exits 0 and prints PASS, not SKIP. The shipped gate returns 2 with bounded failure feedback for a dirty failing project, returns 0 and clears its counter on success, bypasses a clean tree, and stops blocking after MAX_ATTEMPTS. Missing jq fails with an actionable message.
- [x] Record exact first-party evidence for headless Stop-hook execution and untrusted-input tool restrictions. Where insufficient, record an explicitly authorized disposable live probe with version, command, duration/turn bound, and sanitized result before completing this criterion. Synthetic hook payloads alone are not vendor-integration evidence.
- [x] `task cli:test -- -run '^TestExamplesValidate$' -count=1 -v` exits 0 and prints PASS. Review `git diff -- examples/ai` against the evidence table: unsupported flags and guarantees are corrected, and verification notes include background-agents when its claims change.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Audit shipped examples against installed help and first-party vendor sources; record claim-by-claim evidence. 2. Correct unsupported flags/guarantees and add deterministic verify-gate coverage. 3. Run acceptance commands and CI, review once, commit, merge, finalize provider evidence, and clean owned worktree.

## Notes

Refinement: pr-review promises no tools, while both scripts use the Claude default described by agent-profiles as retaining Read/Grep/Glob. Verify that permission boundary explicitly. The gate skips clean trees, so the failing fixture must be dirty. Deterministic local gate checks and live vendor evidence are deliberately separate.

Vendor evidence audit, 2026-10-16. No authenticated/model-backed probe was run; no real GitHub mutation. First-party documentation/help/source establishes the contracts below, not end-to-end inference. Installed: Claude Code 2.1.302; codex-cli 0.163.0. Gemini/Ollama/gh-webhook not live-probed.

| Claim | Version and exact evidence/source | Result / evidence type |
| --- | --- | --- |
| Claude Stop exit 2 and stderr | https://code.claude.com/docs/en/hooks#stop and exit-code table: Stop exit 2 “Prevents Claude from stopping, continues the conversation”; Stop decision control routes stderr back as feedback | Confirmed documentation; not live integration. Interrupt/API-error stops excluded. |
| session_id, stop_hook_active | Same hooks reference, common input fields: session_id is current session identifier; Stop input says stop_hook_active is true when already continuing due to a stop hook | Confirmed documentation. Gate uses session counter instead of bypassing active hooks; synthetic test covers both values. |
| CLAUDE_PROJECT_DIR and timeout | Same hooks reference: project-root variable usable in command strings; timeout “Seconds before canceling” | Confirmed documentation; quoted snippet path and 900-second timeout retained. Timeout is not success. |
| Headless Stop execution | https://code.claude.com/docs/en/headless#start-faster-with-bare-mode: “Without --bare, a -p session runs the hooks in a project's .claude/settings.json” and loads them as an interactive session would; hooks#stop defines normal response completion | Confirmed exact first-party documentation. --bare/--safe-mode disable project hooks. No live probe necessary to establish this documented claim. |
| Codex hook JSON | https://developers.openai.com/codex/hooks: repo .codex/hooks.json, three-level hooks/event/matcher/command structure, common JSON stdin, Stop expects JSON stdout; non-managed hooks reviewed/trusted by definition hash; project layer must be trusted | Confirmed documentation. Existing snippet redirects Noon output to stderr and emits {}. No live Codex hook run. |
| Claude permission-prompts | claude --version -> 2.1.302; claude --help: none means nobody answers, anything that would prompt denied; mode still decides everything else. headless reference requires >=2.1.259 and says PermissionRequest hooks/rules still apply | Corrected unsupported read-only/no-tools guarantees; explicit manual mode in inspection profiles; documentation/help only. |
| acceptEdits | claude --help lists mode; https://code.claude.com/docs/en/permissions and headless reference: auto-approves edits plus common filesystem commands | Confirmed with caveat: more than Edit, not sandbox. |
| allowedTools patterns | https://code.claude.com/docs/en/headless#auto-approve-tools gives Bash(git diff *) and git log * examples; https://code.claude.com/docs/en/permissions describes subcommand wildcard | Corrected examples to current documented space-star syntax. Pre-approval is not exclusive tool restriction; Git args/config may have side effects. |
| Read/Grep/Glob | permissions reference read-only tools in working/additional directories require no approval; https://code.claude.com/docs/en/cli-reference --tools notes Glob/Grep default availability differs on macOS/Linux/WSL | Corrected absolute availability/read-only promise. Permissions permit reads, tool availability varies. |
| Codex profiles stdin/sandbox | codex --version -> 0.163.0; codex exec --help: '-' reads stdin, sandbox possible values read-only/workspace-write/danger-full-access; https://developers.openai.com/codex/noninteractive | Confirmed help/docs for both profiles; shell sandbox not promise about hooks/MCP. |
| Fresh worktree and unattended Codex | gh api repos/openai/codex/contents/codex-rs/exec/src/lib.rs?ref=rust-v0.163.0: ConfigOverrides approval_policy Some(AskForApproval::Never), AutoReview exception; get_git_repo_root check at lines 1013–1018, not persisted interactive trust check | Confirmed versioned source, not live agent. Fresh Git worktree meets repository check, but project-config and hook trust remain separate; auth/policy/tools can fail. No bypass flags added. |
| Gemini argument prompt | https://geminicli.com/docs/cli/headless/: headless mode when providing query with -p/--prompt; tool not installed | Confirmed documentation only; no read-only/tool-disable guarantee or live result. |
| Ollama stdin prompt/no tool execution | https://docs.ollama.com/cli plus gh-fetched https://github.com/ollama/ollama/blob/4d2e25016476e2744863900795fd226266b76a86/cmd/cmd.go RunHandler: io.ReadAll(os.Stdin), prepends prompt, interactive=false; plain generation path | Confirmed source/docs only; Ollama not installed/live-probed; running server/model required. |
| PR and issue untrusted-input tools | claude 2.1.302 --help and https://code.claude.com/docs/en/cli-reference: --tools empty disables built-ins but not MCP; --disallowedTools mcp__* denies MCP; strict-mcp-config excludes configured servers; safe-mode disables customizations with managed-policy exceptions | Corrected both scripts using a quoted empty argument, explicit MCP denial and safe-mode. Docs explain remaining CLI/auth/network/managed-hook/script access and AGENT_CMD override responsibility. No permission bypass; documented tool restriction, not OS sandbox. |
| gh-webhook signatures | gh api at 115d4d6768b55c74ee7d7c61d4775d83fdc323d2: https://github.com/cli/gh-webhook/blob/115d4d6768b55c74ee7d7c61d4775d83fdc323d2/webhook/forward.go defines --secret/-S; forwards bytes.NewReader(ev.Body), copies headers. create_webhook.go passes secret in configuration | Confirmed client source compatibility with exact-body HMAC; live service delivery unresolved/explicitly untested. README gives supported secret flag and fallback for older extension; no invented flag or disabled verification. |

Built-in profile finding (not changed; separately authorized implementation required): internal/config/jobs.go's claude command omits explicit permission mode. Current vendor default may be auto; permission-prompts none alone is not a read-only guarantee.

Modified-file contract extension: append relevant profile findings to NOON-33 as explicitly requested by this task; no Docker/example-container files changed.

Regression failure targeted: dirty failing trees could bypass verification or Stop callbacks could block forever. TestExampleVerifyGate uses built Noon/Git/real jq, isolated HOME/XDG/state/TMPDIR and clean Git configuration; no network/real agents. Also catches success counter retention, unbounded feedback, missing jq, unsafe session IDs.

AC#1: backlog task NOON-32 --plain -> evidence table present; four affected READMEs include 2026-10-16, vendor versions/sources and documentation/help versus live distinction; no unresolved delivery claim marked live-confirmed.
AC#2: task cli:test -- -run '^TestExampleVerifyGate$' -count=1 -v -> PASS, not SKIP (real jq pinned via mise/task init). Covers dirty failure exit 2, dirty success exit 0/counter reset, clean bypass, MAX_ATTEMPTS escape, missing jq message.
AC#3: first-party headless and CLI reference passages quoted above establish headless Stop execution and explicit built-in/MCP tool restriction. Documentation evidence only; no synthetic payload claimed as vendor integration.
AC#4: task cli:test -- -run '^TestExamplesValidate$' -count=1 -v -> PASS. One direct general review of git diff -- examples/ai against this table completed; corrected overbroad Codex table guarantee and tightened test environment/output bound. Independent review launch unavailable: registry/scope conflict; no child ran. No second general review.

Delivery: implementation 7c8e2a2; merged into main as 874afb7 after concurrent documentation commit 3f636ee, preserving that work. task ci passed on implementation and merged code (security/vulnerability scan, formatting/vet/staticcheck, tests, race, smoke). LSP examples_test.go clean; sh -n passed for all three changed scripts; focused gate rerun passed after review corrections. No tests removed, skipped or weakened; no core/design contract changed. NOON-33 received profile findings. Owned worktree/branch docs/noon-32 removed via Worktrunk after creation receipt and idle shell pane verification; exact Herdr workspace wE9 closed by post-remove hook. No owned checkout retained. Remaining blocker: none. No remaining item-scoped implementation step; final ledger commit and claim release complete delivery.

Review 2026-10-17: fixed a regression. verify-gate exited 2 (blocking Stop) when jq was missing or session_id was invalid, with no counter, so Claude could loop forever. Before this change, a missing jq still reached the bounded counter path. Setup errors now exit 1 (non-blocking, per the hooks reference); the README and TestExampleVerifyGate expectations were updated to match. Claude 2.1.302 help confirms --safe-mode, --tools, --strict-mcp-config, and --disallowedTools. TestExampleVerifyGate/TestExamplesValidate pass.

## Summary

Verified vendor contracts against dated first-party docs, versioned help/source; corrected tool-access promises and explicitly disabled review tools. Added real-binary deterministic verify-gate regression and pinned jq. Focused tests and task ci pass; 7c8e2a2 merged as 874afb7; owned worktree cleaned. Live model/webhook delivery remains explicitly untested, not claimed verified.
