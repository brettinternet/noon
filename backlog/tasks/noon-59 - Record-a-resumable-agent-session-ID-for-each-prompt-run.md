---
id: NOON-59
title: Record a resumable agent session ID for each prompt run
status: Done
assignee: []
created_date: '2026-10-18 16:32'
updated_date: '2026-10-18 17:08'
labels:
  - agents
  - runner
  - store
  - config
dependencies: []
documentation:
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
modified_files:
  - internal/config/**
  - internal/store/**
  - internal/app/**
  - internal/cli/run.go
  - internal/cli/root.go
  - internal/cli/run_test.go
  - internal/scheduler/**
  - internal/mcp/server_test.go
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
  - docs/cli.md
priority: medium
type: feature
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every prompt run whose agent profile supports caller-chosen session IDs records the session ID requested for that execution, so a person or agent can open or resume the exact conversation behind a scheduled run (`claude --resume ID`, `pi --session-id ID`) instead of reading only the log.

Why: Claude Code routines return a session URL per run so the run can be inspected and continued; Noon runs only show stdout/stderr. Claude Code (`--session-id <uuid>`) and Pi (`--session-id <id>`, creating it if missing) both accept a caller-chosen ID, so Noon can choose and record it without parsing agent output.

Settled contract:
- Agent profiles gain an optional `session_id_flag` string (one flag token starting with `-`, not `-` or `--`, with no whitespace or NUL; omission disables the feature). Built-ins: `claude` and `pi` set `--session-id`; `codex`, `gemini`, and `opencode` do not (Codex cannot accept a caller-chosen ID for a new `exec` session; the other two are unverified). Redefining a built-in replaces it wholesale, so a user override needs its own `session_id_flag`.
- For a prompt run with the flag, Noon generates a random RFC 4122 version 4 UUID when an execution is about to start (foreground creation or queued-to-running dispatch), durably stores it before launching the child, inserts `FLAG UUID` immediately after the profile command and before any prompt argument, and sets `NOON_AGENT_SESSION_ID` for the job and its hooks. `NOON_AGENT_SESSION_ID` is reserved like other `NOON_*` names; inherited values never leak through. Command jobs and profiles without the flag get no argument and no variable. Queued rows remain null until dispatch resolves the actual profile; skipped, missed, expired, and cancelled-before-start rows never acquire an ID. Queue deduplication and read-only RunSpec use by doctor must not allocate or change IDs. Once recorded for an execution the ID is immutable, including start failure and interruption.
- State schema 11 adds a nullable run column; older binaries refuse the newer database through the existing check. Shared run JSON (CLI history/logs/run, MCP, HTTP API, hook stdin `run`) gains `agent_session_id`, null when absent.
- It is a requested ID: Noon does not verify the agent honored it. An agent that rejects the flag fails visibly like any other agent command failure; preserve its normal exit status. Missing agent transcripts, disabled session persistence, or user-supplied conflicting flags can make the requested ID non-resumable; Noon must not claim it verified a conversation.
- The profile field is user config only and is not part of job digests, matching existing profile handling; `noon config` shows it beside the command.
- docs/agents.md shows how to find the ID (`noon history JOB --json` or MCP get_run) and resume from the original job cwd using the same agent installation, user, and session storage. The run project path is not necessarily the job cwd, and this task does not add historical cwd storage. Warn that Pi --session-id creates a fresh session when no match exists; a new empty conversation is not proof of successful resumption.

Non-goals: parsing agent output or JSON streams to discover IDs (for example `codex exec --json`), automatic resume or continuation across runs, attachable terminal sessions (draft-1), storing transcripts, structured results (draft-12), human-output changes beyond what existing renderers show for new JSON fields.

Dependencies: none. NOON-60 builds on the argv order settled here.

Modified-file contract: the modified_files list plus this task record. No new module dependency beyond go.mod as it stands. Never call real agent CLIs; use the existing record-agent fakes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test PKG=./internal/cli -- -run "^TestPromptJobs$" -count=1` exits 0; with a fake profile that sets session_id_flag, a foreground prompt run passes the flag and a version 4 UUID before the prompt argument, the job sees the same value in NOON_AGENT_SESSION_ID, `noon history JOB --json` reports it as agent_session_id, two runs get different IDs, and a profile without the flag gets neither argument nor variable while JSON reports null. `task cli:test PKG=./internal/config -- -run '^TestLoadJobs$' -count=1` exits 0 and rejects invalid session_id_flag values with file/line context.
- [x] #2 `task cli:test PKG=./internal/store -- -run "^TestStore$" -count=1` exits 0; migration to schema 11 preserves existing runs with a null agent_session_id and the existing newer-database refusal still applies. Read-only access to the prior schema does not migrate it; legacy runs remain readable with null IDs.
- [x] #3 `task cli:test PKG=./internal/cli -- -run "^TestHooks$" -count=1` exits 0 and an inherited or job env NOON_AGENT_SESSION_ID never reaches a command job or its hook. For a supported prompt run, the hook environment and stdin run.agent_session_id equal the child's ID, including an agent start failure.
- [x] #4 `rg -n "session_id_flag" docs/configuration.md` and `rg -n "agent_session_id" docs/design.md docs/agents.md` exit 0; docs/design.md lists the field in shared run JSON and the state schema version is 11.
- [x] #5 `task cli:test PKG=./internal/scheduler -- -run '^TestServe' -count=1 -v` exits 0 with executed cases (not a no-tests match). Scheduled and queued prompt executions persist the same ID seen in child argv, shared run JSON, and hooks. A queued run has null before dispatch; dispatch uses the current resolved profile without regenerating a stored execution ID, and cancellation before dispatch leaves it null.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement evidence: CLI CreateRun creates running rows, but scheduler/queue rows can exist long before dispatch; bind the requested ID to the execution, not queue insertion or read-only RunSpec (also called by doctor). Local Claude Code 2.1.302 --help confirms --session-id <uuid>; installed Pi 1.3.0 dist/cli/args.js and dist/main.js confirm --session-id creates a missing session. This verifies flag syntax, not authenticated resume behavior. Keep fake agents in automated acceptance.

AC#1: task cli:test PKG=./internal/cli -- -run "^TestPromptJobs$" -count=1 and task cli:test PKG=./internal/config -- -run "^TestLoadJobs$" -count=1 -> passed; fake-agent UUID/argv/env/history, distinct runs, no-flag nulls, and invalid flag context covered.
AC#2: task cli:test PKG=./internal/store -- -run "^TestStore$" -count=1 -> passed; schema 11 migration, legacy null reads, read-only nonmigration, and newer-schema refusal covered.
AC#3: task cli:test PKG=./internal/cli -- -run "^TestHooks$" -count=1 -> passed; reserved inherited/job env and supported prompt hook env/JSON including start failure covered.
AC#4: rg -n "session_id_flag" docs/configuration.md and rg -n "agent_session_id" docs/design.md docs/agents.md -> passed; design records schema 11.
AC#5: task cli:test PKG=./internal/scheduler -- -run "^TestServe" -count=1 -v -> passed with executed cases; scheduled/queued argv/env/JSON/hooks, current profile dispatch, null pre-dispatch and cancellation covered.
Implementation stays within modified-file contract; no dependencies added, real agents invoked, or tests deleted/skipped/weakened. Final implementation task ci passed. An initial unchanged runner pipe race test failed transiently; two subsequent full CI runs passed. Parent independently reran legacy store migration/refusal and TestServeAgentSessionIDs successfully. Review and final-commit CI/delivery pending.

Delivery: implementation commit 47659998266ae14e112273be0b0a3934ed99bbcb passed task check:staged and task ci on the committed tree, then fast-forward merged into main. One independent general review reported no validated findings. All acceptance and delivery requirements satisfied; no blocker or next implementation step. Worktrunk removed the verified session-owned worktree and branch; its exact Herdr workspace wGS disappeared. No push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented requested per-run UUID session IDs for supported agent profiles, schema 11 persistence and shared JSON, execution/hook environment isolation, current-profile queued dispatch, and resume guidance. All five AC passed, full CI passed on the implementation commit, independent review clean. Merged locally as 4765999; worktree and branch cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
