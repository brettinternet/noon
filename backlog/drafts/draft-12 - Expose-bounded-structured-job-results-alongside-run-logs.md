---
id: DRAFT-12
title: Expose bounded structured job results alongside run logs
status: Draft
assignee: []
created_date: '2026-10-18 16:09'
labels:
  - automation
  - results
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Priority within the smart-repo proposals: second. Before promotion, settle a minimal result contract, retention and failure semantics, then add the modified-file contract and executable acceptance commands with expected results.

Problem and outcome: exit codes and logs describe process execution but make agents parse prose to find a report, proposed patch, or actionable finding. A small structured result should let humans and clients inspect a job's summary and evidence without guessing from log text.

Scope: optional, versioned, bounded job-authored results associated with a specific run; a short summary and a minimal set of finding or artifact references; consistent retrieval through existing CLI and MCP/HTTP run inspection. Results are untrusted claims from the job, not authority or proof that verification succeeded. Existing process status, noop semantics, hook status, and JSON envelopes remain authoritative.

Non-goals: provider SDKs, a notification catalog, artifact hosting, arbitrary filesystem reads through remote clients, a general workflow variable store, or a new status taxonomy.

Promotion questions: result delivery mechanism; necessary fields demonstrated by two existing examples; malformed/oversized/partial results; process failure after writing a result; cancellation; retention and pruning; behavior when a referenced artifact disappears. Prefer retaining small metadata over copying arbitrary files. Do not dereference job-supplied paths or URLs automatically, leak secrets, or let a client escape an enabled project's access boundary.

Candidate acceptance: a real local fixture produces a retrievable result tied to its run ID; absent results preserve existing behavior; malformed and oversized results have an explicit observable outcome; a success claim in result data cannot turn a failed/noop run into verified success; pruned results and missing artifacts are represented honestly.

Dependencies: none; revision-aware verification can later supply provenance without making either first slice depend on the other. Likely surfaces: internal/app, internal/store, runner environment if needed, CLI/MCP/HTTP adapters, focused integration tests, docs/cli.md and docs/agents.md. Keep decision-3 retention and decision-4 command-based extension boundaries.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
