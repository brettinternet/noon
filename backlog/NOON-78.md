---
id: NOON-78
title: Tie verification evidence to the repository inputs actually checked
status: draft
labels: [automation, verification]
rank: adc47711dc4771194
created: 2026-10-18T16:09:00Z
updated: 2026-10-18T16:09:00Z
---

Draft, not ready work. Priority within the smart-repo proposals: first. Before promotion, settle the evidence boundary and add a concrete modified-file contract and locally executable acceptance commands with expected results.

Problem and outcome: a successful run can predate the latest edit, and a commit SHA alone cannot distinguish two dirty working trees. Humans and agents need to know which repository inputs a verification run covered rather than treating the latest green run as proof about current code. Existing job digests identify definitions, not script contents or checked source.

Scope: associate verification evidence with the inspected revision and a bounded, explicitly defined input fingerprint; expose that evidence through existing run inspection surfaces. Distinguish matching evidence, stale evidence, and unknown evidence. Report the fingerprint's coverage and limitations. Missing Git, non-repository user jobs, legacy runs, unreadable inputs, or changes during execution must not produce a false current-verification claim.

Non-goals: caching or skipping checks, checking out code automatically, a mandatory Git dependency for every job, provenance as authorization, reproducible-build attestation, or a universal dependency graph.

Promotion questions: opt-in versus automatic capture; how jobs identify relevant inputs; treatment of staged, unstaged, untracked, ignored/generated files and submodules; recording before/after state versus an immutable snapshot; checkout versus job cwd; privacy and hashing cost. Before/after equality alone does not prove inputs stayed unchanged throughout a mutable run. A verified-current claim must be conservative about races and external inputs.

Candidate acceptance: changing a covered tracked or untracked input invalidates earlier evidence even when HEAD is unchanged; a missing or ambiguous fingerprint remains unknown; a noop, skipped, queued, or failed run is never verification success; inspection stays read-only and reveals no source content or secrets.

Dependencies: none for an initial provenance slice; structured job outcomes are related but not a prerequisite. Likely surfaces: internal/app, internal/store, existing CLI/MCP/HTTP run representations, focused integration tests, docs/agents.md and docs/design.md. Preserve decision-3 (per-user runtime state).

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
