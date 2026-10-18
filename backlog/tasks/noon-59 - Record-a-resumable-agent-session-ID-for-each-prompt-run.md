---
id: NOON-59
title: Record a resumable agent session ID for each prompt run
status: To Do
assignee: []
created_date: '2026-10-18 16:32'
updated_date: '2026-10-18 16:32'
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
Outcome: every prompt run whose agent profile supports caller-chosen session IDs records the agent session it started, so a person or agent can open or resume the exact conversation behind a scheduled run (`claude --resume ID`, `pi --session-id ID`) instead of reading only the log.

Why: Claude Code routines return a session URL per run so the run can be inspected and continued; Noon runs only show stdout/stderr. Claude Code (`--session-id <uuid>`) and Pi (`--session-id <id>`, creating it if missing) both accept a caller-chosen ID, so Noon can choose and record it without parsing agent output.

Settled contract:
- Agent profiles gain an optional `session_id_flag` string (must start with `-`). Built-ins: `claude` and `pi` set `--session-id`; `codex`, `gemini`, and `opencode` do not (Codex cannot accept a caller-chosen ID for a new `exec` session; the other two are unverified). Redefining a built-in replaces it wholesale, so a user override needs its own `session_id_flag`.
- For a prompt run with the flag, Noon generates a random RFC 4122 version 4 UUID when the run row is created (foreground and scheduler paths alike), stores it, inserts `FLAG UUID` immediately after the profile command and before any prompt argument, and sets `NOON_AGENT_SESSION_ID` for the job and its hooks. `NOON_AGENT_SESSION_ID` is reserved like other `NOON_*` names; inherited values never leak through. Command jobs and profiles without the flag get no argument and no variable.
- State schema 11 adds a nullable run column; older binaries refuse the newer database through the existing check. Shared run JSON (CLI history/logs/run, MCP, HTTP API, hook stdin `run`) gains `agent_session_id`, null when absent.
- It is a requested ID: Noon does not verify the agent honored it. An agent that rejects the flag fails visibly like any other start failure.
- The profile field is user config only and is not part of job digests, matching existing profile handling; `noon config` shows it beside the command.
- docs/agents.md shows how to find the ID (`noon history JOB --json` or MCP get_run) and resume from the run directory, since agent sessions are scoped to the working directory.

Non-goals: parsing agent output or JSON streams to discover IDs (for example `codex exec --json`), automatic resume or continuation across runs, attachable terminal sessions (draft-1), storing transcripts, structured results (draft-12), human-output changes beyond what existing renderers show for new JSON fields.

Dependencies: none. NOON-60 builds on the argv order settled here.

Modified-file contract: the modified_files list plus this task record. No new module dependency beyond go.mod as it stands. Never call real agent CLIs; use the existing record-agent fakes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run "^TestPromptJobs$" -count=1` exits 0; with a fake profile that sets session_id_flag, a foreground prompt run passes the flag and a version 4 UUID before the prompt argument, the job sees the same value in NOON_AGENT_SESSION_ID, `noon history JOB --json` reports it as agent_session_id, two runs get different IDs, and a profile without the flag gets neither argument nor variable while JSON reports null.
- [ ] #2 `task cli:test -- -run "^TestStore$" -count=1` exits 0; migration to schema 11 preserves existing runs with a null agent_session_id and the existing newer-database refusal still applies.
- [ ] #3 `task cli:test -- -run "^TestHooks$" -count=1` exits 0 and an inherited or job env NOON_AGENT_SESSION_ID never reaches a command job or its hook.
- [ ] #4 `rg -n "session_id_flag" docs/configuration.md` and `rg -n "agent_session_id" docs/design.md docs/agents.md` exit 0; docs/design.md lists the field in shared run JSON and the state schema version is 11.
- [ ] #5 `task cli:test PKG=./internal/scheduler -- -run "^TestServe" -count=1` exits 0 and covers one scheduled prompt run recording a session ID visible through the shared run JSON and the hook environment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
