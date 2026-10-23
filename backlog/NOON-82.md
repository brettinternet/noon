---
id: NOON-82
title: Continue the previous run agent session for follow-up jobs
status: draft
type: feature
priority: low
labels: [agents, runner]
blocked_by: [NOON-59]
rank: ae82fa0be82fa0b9c
refs: [docs/agents.md]
created: 2026-10-18T16:33:00Z
updated: 2026-10-18T16:38:00Z
---

Draft, not ready work. Depends on NOON-59 (recorded session IDs). Promote only when a concrete recurring job needs conversational continuity that a prompt instructing the agent to read NOON_LAST_SUCCESS_AT from its environment, the event file, or repository files (Noon does not expand environment variables inside prompt text) cannot provide. Then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: some recurring agent work is one long thread, such as following one PR until it merges or continuing a research question day by day. Each Noon prompt run starts a fresh agent session, so context must be rebuilt every time.

Prior art: Codex in-chat scheduled tasks return to the same conversation and keep context; Claude Code /loop runs inside one session; both bound this with expiry or stop conditions.

Candidate direction: an opt-in job setting that resumes the job previous recorded session (latest retained run with an `agent_session_id`) through a profile-declared resume flag (`claude --resume ID`, `pi --session-id ID`), with a separately defined fresh-session fallback. NOON-59 records a requested ID, not proof of a retained transcript; Pi --session-id creates a new session when the ID is absent. Do not report that as resumed continuity.

Promotion questions: distinguish no prior recorded ID from a recorded ID whose transcript is missing, pruned, stored under another cwd/user, or never created; decide whether each case fails or explicitly starts fresh; persist enough agent/profile/cwd identity to reject incompatible candidates after config changes rather than reusing any latest non-null ID; which run supplies the session (latest succeeded only, or any finished run); behavior after failure, timeout, cancellation, or a definition digest change; growth limits for long sessions and a reset path; concurrency with `overlap: queue` and `requests: each`; how resumption appears in history and run JSON; whether working-directory scoping of agent sessions breaks across `cwd` changes; approval digest coverage.

Non-goals: Noon-managed transcripts or memory, summarization, attachable terminal sessions (draft-1), agents choosing which session to resume.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Refinement: retain NOON-59 as the only required dependency; NOON-60 argument support is related, not required for continuity. Installed Pi 1.3.0 dist/main.js confirms --session-id resumes a matching project session or creates a missing one. Promotion must distinguish these outcomes and define compatibility after agent/cwd changes.
