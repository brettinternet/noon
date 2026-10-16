---
id: NOON-16
title: Run agent prompt jobs through agent profiles
status: Done
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 03:18'
labels:
  - config
  - runner
dependencies:
  - NOON-2
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/config/**
  - internal/runner/**
  - internal/app/**
  - internal/cli/**
  - examples/**
  - docs/configuration.md
  - README.md
priority: high
type: feature
ordinal: 500
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a job can be a prompt for an agent CLI the user already has. The built-in `claude` and `codex` profiles run unattended, profiles are configurable in user config, and prompts reach the agent byte for byte on stdin or as the last argument.

Read first: docs/design.md#agent-prompts and #execution, decision-4.

Scope:
- Job fields: `prompt` (inline string, at most 64 KiB), `prompt_file` (relative to the project root, read when the run starts, at most 64 KiB), `agent` (profile name). Exactly one of `run`, `prompt`, `prompt_file`. `agent` is valid only on prompt jobs.
- User config only: `agents: {NAME: {command: [ARGV...], prompt: stdin|argument}}`; `prompt` defaults to `stdin`. `default_agent` is allowed in user config and `.noon.yaml`, and an explicit `--config` file accepts all keys. A key outside its allowed files names the allowed location.
- Built-in profiles, replaceable by defining the same name: `claude` = `[claude, -p, --permission-prompts, none]` and `codex` = `[codex, exec, -]`, both on stdin. Unattended runs must never wait on permission prompts; document that users who want edits add flags such as `--permission-mode acceptEdits` in their own profile.
- Effective agent: the job's `agent`, else `default_agent`. A prompt job with neither is a validation error naming both fixes. An unknown agent lists the known names.
- Execution: argv is the profile command plus, in argument mode, the prompt as the final element. In stdin mode the prompt bytes are the child's stdin. Never template, expand, or trim the prompt. An unreadable, missing, or oversized `prompt_file` fails the run before starting, with exit code 125.
- Add `NOON_AGENT=NAME` to the environment of prompt runs.
- The digest includes `prompt`, `prompt_file`, and `agent` (the name only; profiles are user-owned and excluded).
- List: COMMAND shows `claude: Review open PRs…` (first line, truncated) or `claude: prompts/review.md`. List JSON adds `prompt`, `prompt_file`, and the effective `agent`. `noon config --json` adds `agents` (name to command) and `default_agent`.
- Add a prompt job to examples/jobs.yaml with `agent: claude`. Document profiles in docs/configuration.md, using only flags verified against each CLI's `--help` on the implementing machine.

Non-goals: model SDKs or API keys, prompt templating, conversation resume, TTY sessions, agent-specific output parsing, verifying that agent CLIs are installed (doctor does that later).

Tests:
- internal/cli `TestPromptJobs` uses a fake agent script on PATH that records its argv and stdin to files; no real agent CLI is ever invoked. Cover inline and file prompts in stdin and argument modes, byte-exact delivery including newlines and a literal `$HOME`, override of the built-in `claude` profile from user config, and a missing prompt file that exits 125 without starting the agent.
- Extend `TestLoadJobs`: two action fields, `agent` on a run job, unknown agent, `agents` outside user config, and a prompt over 64 KiB.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^TestPromptJobs$' -count=1 -v` exits 0 and prints `--- PASS: TestPromptJobs`, covering every case listed under Tests without invoking a real agent CLI
- [x] #2 `task cli:test -- -run '^TestLoadJobs$' -count=1 -v` exits 0 including the new prompt and agent validation cases
- [x] #3 `task cli:build && bin/noon --config examples/jobs.yaml --json list` exits 0 and the example prompt job reports `"agent":"claude"`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend existing config/job validation with user-owned agent profiles and prompt actions. 2. Connect byte-exact prompt delivery to foreground execution and expose effective agents in list/config output. 3. Add critical fake-agent regression coverage and update examples/docs using locally verified CLI flags. 4. Run all acceptance commands and task ci, perform one focused independent review, commit, merge to main, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Modified-file contract expansion: docs/design.md is permitted solely to align its agent-prompts and execution status/contracts with implemented NOON-16 behavior, as explicitly required by DoD #5. No unrelated design changes.

AC#1: task cli:test -- -run '^TestPromptJobs$' -count=1 -v -> exit 0; PASS including exact inline/file stdin/argument delivery, literal $HOME/newlines, profile override/default, missing/oversized/FIFO preflight with no child start. Fake agents only.
AC#2: task cli:test -- -run '^TestLoadJobs$' -count=1 -v -> exit 0; PASS including action conflicts, agent-on-run, unknown agents, profile source restrictions and 64 KiB limit.
AC#3: task cli:build && bin/noon --config examples/jobs.yaml --json list -> exit 0; review example reports agent=claude. CLI execution used env -i with isolated temporary HOME per development instructions.
Review: one independent reviewer pass found canonical digest mismatch and FIFO preflight hang. Fixed both; TestJobDigest now checks independently computed canonical SHA-256 and default/explicit equivalence; TestPromptJobs FIFO case passes immediately with exit 125 and no child start. No tests deleted, skipped, or weakened. All affected LSP diagnostics clean.
Delivery: c50603f (feat: run agent prompt jobs through profiles) fast-forward merged to main. task check:staged and task ci passed on that final implementation commit (security, vet, staticcheck, full tests, race, build, smoke). Docs/examples updated; Claude/Codex flags verified with local --help. Owned worktree and branch removed via Worktrunk; matching Herdr workspace wBR removed. No retained worktree, blocker, or remaining item step; no push performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented foreground prompt jobs with user-owned agent profiles, exact stdin/argument delivery, safe preflight, canonical digests, list/config output and documentation. All acceptance checks and task ci passed; independent review defects fixed. Committed c50603f, merged to main, and cleaned up owned worktree.
<!-- SECTION:FINAL_SUMMARY:END -->
