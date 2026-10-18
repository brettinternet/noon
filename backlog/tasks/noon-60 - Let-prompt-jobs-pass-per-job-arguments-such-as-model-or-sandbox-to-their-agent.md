---
id: NOON-60
title: Let prompt jobs pass per-job arguments such as model or sandbox to their agent
status: To Do
assignee: []
created_date: '2026-10-18 16:33'
labels:
  - agents
  - config
  - runner
dependencies:
  - NOON-59
documentation:
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
modified_files:
  - internal/config/**
  - internal/app/run.go
  - internal/cli/root.go
  - internal/cli/run_test.go
  - noon.schema.json
  - docs/configuration.md
  - docs/design.md
  - docs/agents.md
  - examples/ai/agent-profiles/**
priority: medium
type: feature
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a prompt job can choose a cheaper model, a lower effort level, or a narrower sandbox for its agent in the job definition itself, so a shared noon.yaml stays runnable on every machine that has the named agent and does not require each user to define matching custom profile names.

Why: agent profiles are machine choices in user config, so today a shared job that wants `claude --model haiku` must name a profile such as `claude-cheap` that other machines lack. agent-cron sets model per job; Codex scheduled-task guidance recommends the narrowest sandbox per task. Claude (`--model`, `--effort`), Codex (`-m`, `-s/--sandbox`), and Pi (`--model`, `--thinking`, `--tools`) all take these as arguments.

Settled contract:
- New job field `agent_args`: non-empty list of strings, prompt jobs only. A job that sets it must name `agent` explicitly; `default_agent` is not used, because arguments are vendor-specific and a machine default must not feed Claude flags to Codex. Values are argv, never templated or expanded.
- `agent_args` is part of the execution digest when set; jobs without it keep their digests (same rule as `requests`). A pulled change to agent_args therefore waits for approval. Arguments can widen permissions just as `run` can; docs say approval is not a sandbox.
- Profiles gain optional `prompt_args` (string list) for arguments that must come last. Effective argv: profile `command`, then the NOON-59 session flag and ID when configured, then job `agent_args`, then profile `prompt_args`, then the prompt when `prompt: argument`.
- Built-ins move their tails into `prompt_args` so the effective argv without agent_args is byte-identical to today: codex `[codex, exec]` + `[-]`; gemini `[gemini]` + `[-p]`; opencode `[opencode, run]` + `[--]`; claude and pi have none. Custom profiles whose command ends in `--` or a value-taking flag must move that tail to `prompt_args` before their jobs use agent_args; docs/configuration.md says so. `noon config` shows prompt_args.
- noon.schema.json adds `agent_args` with the prompt-only and explicit-agent rules.

Non-goals: a portable model or effort vocabulary mapped per vendor, `noon add` flags for agent_args, per-job environment beyond existing `env`, verifying that an agent accepts the arguments, profiles defined in shared config.

Dependencies: NOON-59 (shares the argv assembly and profile parsing; implement after it to avoid conflicting argv rules).

Modified-file contract: the modified_files list plus this task record. Updating an existing expectation for the intentional built-in profile shape change is allowed and must be noted in Implementation Notes; never call real agent CLIs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run "^TestPromptJobs$" -count=1` exits 0; with record-agent fakes, agent_args appear after the profile command (and session flag when configured) and before prompt_args and the prompt for both stdin and argument profiles, and the effective argv of built-in-shaped codex, gemini, and opencode profiles without agent_args equals the current argv.
- [ ] #2 `task cli:test -- -run "^(TestLoadJobs|TestJobDigest)$" -count=1` exits 0; validation rejects agent_args on run jobs, without an explicit job agent, as an empty list, or with non-string items, with file and line in the error; the digest changes when agent_args changes and is unchanged for jobs that omit it.
- [ ] #3 `task cli:test -- -run "^TestNoopExitCodeSchemaRange$" -count=1` exits 0 and `noon.schema.json` declares agent_args; `task cli:test -- -run "^TestExamplesValidate$" -count=1` exits 0 with at least one examples/ai/agent-profiles job using agent_args.
- [ ] #4 `rg -n "agent_args" docs/configuration.md docs/design.md docs/agents.md` and `rg -n "prompt_args" docs/configuration.md` exit 0; docs show the argv order, the explicit-agent rule, digest coverage, and the custom-profile migration note.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
