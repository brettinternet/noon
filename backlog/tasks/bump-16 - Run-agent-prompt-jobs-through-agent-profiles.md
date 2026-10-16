---
id: BUMP-16
title: Run agent prompt jobs through agent profiles
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
labels:
  - config
  - runner
dependencies:
  - BUMP-2
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
- User config only: `agents: {NAME: {command: [ARGV...], prompt: stdin|argument}}`; `prompt` defaults to `stdin`. `default_agent` is allowed in user config and `.bump.yaml`, and an explicit `--config` file accepts all keys. A key outside its allowed files names the allowed location.
- Built-in profiles, replaceable by defining the same name: `claude` = `[claude, -p, --permission-prompts, none]` and `codex` = `[codex, exec, -]`, both on stdin. Unattended runs must never wait on permission prompts; document that users who want edits add flags such as `--permission-mode acceptEdits` in their own profile.
- Effective agent: the job's `agent`, else `default_agent`. A prompt job with neither is a validation error naming both fixes. An unknown agent lists the known names.
- Execution: argv is the profile command plus, in argument mode, the prompt as the final element. In stdin mode the prompt bytes are the child's stdin. Never template, expand, or trim the prompt. An unreadable, missing, or oversized `prompt_file` fails the run before starting, with exit code 125.
- Add `BUMP_AGENT=NAME` to the environment of prompt runs.
- The digest includes `prompt`, `prompt_file`, and `agent` (the name only; profiles are user-owned and excluded).
- List: COMMAND shows `claude: Review open PRs…` (first line, truncated) or `claude: prompts/review.md`. List JSON adds `prompt`, `prompt_file`, and the effective `agent`. `bump config --json` adds `agents` (name to command) and `default_agent`.
- Add a prompt job to examples/jobs.yaml with `agent: claude`. Document profiles in docs/configuration.md, using only flags verified against each CLI's `--help` on the implementing machine.

Non-goals: model SDKs or API keys, prompt templating, conversation resume, TTY sessions, agent-specific output parsing, verifying that agent CLIs are installed (doctor does that later).

Tests:
- internal/cli `TestPromptJobs` uses a fake agent script on PATH that records its argv and stdin to files; no real agent CLI is ever invoked. Cover inline and file prompts in stdin and argument modes, byte-exact delivery including newlines and a literal `$HOME`, override of the built-in `claude` profile from user config, and a missing prompt file that exits 125 without starting the agent.
- Extend `TestLoadJobs`: two action fields, `agent` on a run job, unknown agent, `agents` outside user config, and a prompt over 64 KiB.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestPromptJobs$' -count=1 -v` exits 0 and prints `--- PASS: TestPromptJobs`, covering every case listed under Tests without invoking a real agent CLI
- [ ] #2 `task cli:test -- -run '^TestLoadJobs$' -count=1 -v` exits 0 including the new prompt and agent validation cases
- [ ] #3 `task cli:build && bin/bump --config examples/jobs.yaml --json list` exits 0 and the example prompt job reports `"agent":"claude"`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
