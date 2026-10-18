---
id: DRAFT-14
title: Demonstrate bounded isolated remediation with a verified human handoff
status: Draft
assignee: []
created_date: '2026-10-18 16:09'
labels:
  - automation
  - examples
  - agents
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Priority within the smart-repo proposals: fourth. This is an example/recipe, not a new core workflow engine. Before promotion, select one concrete failure scenario and add a narrow modified-file contract and credential-free acceptance commands with expected results.

Problem and outcome: existing failure-triage, background-agents, container-agent and verify-loop examples demonstrate separate pieces. A user needs a coherent way to turn one actionable failure into a bounded fix attempt, verification against the resulting patch, and an inspectable handoff without silently authorizing publication or endless repair.

Scope: extend the closest existing example rather than duplicate its helpers. Deterministic preflight confirms the failure is still actionable before invoking an agent; irrelevant or already-resolved work uses the existing noop contract. An editing attempt operates in an owned isolated worktree with documented sandbox limits, an explicit verification command, an attempt cap, an overall timeout, and a durable stop/handoff record. Reruns must not silently reset the cap for the same incident. Preserve evidence and failed work for inspection; distinguish no work, verified proposal, and human-required blocker.

Non-goals: native DAGs or job dependencies, an agent framework, automatic merging/publishing, a generalized permissions system, native path conditions, or treating worktrees/job approval as security sandboxes. Posting, pushing, deployment and destructive cleanup must remain separately authorized; cleanup must be limited to verifiably owned resources.

Candidate acceptance: fake-agent fixtures show no actionable failure invokes no agent; successful remediation records the exact patch/revision and passing check; repeated failure, interruption or unavailable authorization reaches a bounded handoff without recursive jobs or unbounded retries; unrelated checkout changes survive; no fixture requires provider credentials or network access. Do not present authenticated-agent behavior as verified unless it was actually exercised.

Dependencies: existing examples and current Noon features suffice; the proposed provenance and structured-result drafts are optional later integrations, not blockers. Likely surfaces: examples/ai/failure-triage, background-agents, container-agent or verify-loop (choose before promotion), examples/README.md, and existing integration example smoke tests. Respect decision-4 and docs/design.md non-goals.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
