---
id: DRAFT-16
title: Continue the previous run agent session for follow-up jobs
status: Draft
assignee: []
created_date: '2026-10-18 16:33'
labels:
  - agents
  - runner
dependencies:
  - NOON-59
documentation:
  - docs/agents.md
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Depends on NOON-59 (recorded session IDs). Promote only when a concrete recurring job needs conversational continuity that a prompt referencing `$NOON_LAST_SUCCESS_AT`, the event file, or repository files cannot provide. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: some recurring agent work is one long thread, such as following one PR until it merges or continuing a research question day by day. Each Noon prompt run starts a fresh agent session, so context must be rebuilt every time.

Prior art: Codex in-chat scheduled tasks return to the same conversation and keep context; Claude Code /loop runs inside one session; both bound this with expiry or stop conditions.

Candidate direction: an opt-in job setting that resumes the job previous recorded session (latest retained run with an `agent_session_id`) through a profile-declared resume flag (`claude --resume ID`, `pi --session-id ID`), falling back to a fresh session when none exists.

Promotion questions: which run supplies the session (latest succeeded only, or any finished run); behavior after failure, timeout, cancellation, or a definition digest change; growth limits for long sessions and a reset path; concurrency with `overlap: queue` and `requests: each`; how resumption appears in history and run JSON; whether working-directory scoping of agent sessions breaks across `cwd` changes; approval digest coverage.

Non-goals: Noon-managed transcripts or memory, summarization, attachable terminal sessions (draft-1), agents choosing which session to resume.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
