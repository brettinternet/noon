---
id: DRAFT-12
title: Expose bounded structured job results alongside run logs
status: Draft
assignee: []
created_date: '2026-10-18 16:09'
updated_date: '2026-10-23 04:34'
labels:
  - automation
  - results
dependencies: []
priority: medium
type: feature
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement (2026-10-22 review):

Summary: a job may write a small, versioned, size-capped result (summary plus a few finding or artifact references) tied to its run, readable through history/logs, MCP, HTTP, and hooks. Value: exit codes and logs describe execution, and prompt-job logs are long transcripts. Agents asking "what did last night's review find?" scrape prose, and notification hooks can only send the log tail. A structured result gives humans, hooks, and agents one stable answer per run; it is the main missing piece for agents consuming other runs' output.

Settle delivery: $NOON_RESULT_FILE, a per-run path the job writes JSON to, stored beside the run log and mirroring NOON_EVENT_FILE. Expose the same path to on_success/on_failure hooks so notification examples (examples/integrations/notifications) can send the summary; that gives the first concrete consumer. Candidate second consumer: examples/ai/pr-review or daily-standup.

Guardrails unchanged: the result is the job's own claim and never changes run status (a "success" result with exit 1 is still failed; noop stays noop); Noon never follows paths or URLs in it.

Ordering: rank this ahead of DRAFT-11 within the smart-repo proposals; it has a clearer consumer and smaller contract.
<!-- SECTION:NOTES:END -->
