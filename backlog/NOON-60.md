---
id: NOON-60
title: Let prompt jobs pass per-job arguments such as model or sandbox to their agent
status: done
type: feature
priority: medium
labels: [agents, config, runner, reviewed]
blocked_by: [NOON-59]
rank: aafa0be82fa0be7f6
refs: [docs/configuration.md, docs/design.md, docs/agents.md]
files: [internal/config/**, internal/app/run.go, internal/cli/root.go, internal/cli/run_test.go, noon.schema.json, docs/configuration.md, docs/design.md, docs/agents.md, examples/ai/agent-profiles/**, internal/cli/approval_test.go]
created: 2026-10-18T16:33:00Z
updated: 2026-10-18T18:07:00Z
---

Outcome: a prompt job can choose a cheaper model, a lower effort level, or a narrower sandbox for its agent in the job definition itself, so a shared noon.yaml stays runnable on every machine that has the named agent and does not require each user to define matching custom profile names.

Why: agent profiles are machine choices in user config, so today a shared job that wants `claude --model haiku` must name a profile such as `claude-cheap` that other machines lack. agent-cron sets model per job; Codex scheduled-task guidance recommends the narrowest sandbox per task. Claude (`--model`, `--effort`), Codex (`-m`, `-s/--sandbox`), and Pi (`--model`, `--thinking`, `--tools`) all take these as arguments.

Settled contract:
- New job field `agent_args`: non-empty list of strings, prompt jobs only. A job that sets it must name `agent` explicitly; `default_agent` is not used, because arguments are vendor-specific and a machine default must not feed Claude flags to Codex. Values are argv, never templated or expanded. Reject NUL in any argv element; empty string elements remain valid literal arguments. Validate explicit agent presence from the source job before default_agent resolution.
- `agent_args` is part of the execution digest when set; jobs without it keep their digests (same rule as `requests`). A pulled change to agent_args therefore waits for approval. Arguments can widen permissions just as `run` can; docs say approval is not a sandbox.
- Profiles gain optional `prompt_args` (string list) for arguments that must come last. Effective argv: profile `command`, then the NOON-59 session flag and ID when configured, then job `agent_args`, then profile `prompt_args`, then the prompt when `prompt: argument`.
- Built-ins move their tails into `prompt_args` so the effective argv without agent_args is byte-identical to today: codex `[codex, exec]` + `[-]`; gemini `[gemini]` + `[-p]`; opencode `[opencode, run]` + `[--]`; claude and pi have none. Custom profiles whose command ends in `--` or a value-taking flag must move that tail to `prompt_args` before their jobs use agent_args; docs/configuration.md says so. `noon config` shows prompt_args. Preserve the requested-only session-ID guarantee from NOON-59: generic agent_args are not parsed as vendor flags, so overriding session/persistence arguments can invalidate resumability. Arguments are visible configuration, not a secret channel.
- noon.schema.json adds `agent_args` with the prompt-only and explicit-agent rules.

Non-goals: a portable model or effort vocabulary mapped per vendor, `noon add` flags for agent_args, per-job environment beyond existing `env`, verifying that an agent accepts the arguments, profiles defined in shared config.

Dependencies: NOON-59 (shares the argv assembly and profile parsing; implement after it to avoid conflicting argv rules).

Modified-file contract: the modified_files list plus this task record. Updating an existing expectation for the intentional built-in profile shape change is allowed and must be noted in Implementation Notes; never call real agent CLIs.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/cli -- -run "^TestPromptJobs$" -count=1` exits 0; with record-agent fakes, agent_args appear after the profile command (and session flag when configured) and before prompt_args and the prompt for both stdin and argument profiles, and the effective argv of built-in-shaped codex, gemini, and opencode profiles without agent_args equals the current argv.
- [x] `task cli:test PKG=./internal/config -- -run "^(TestLoadJobs|TestJobDigest)$" -count=1` exits 0; validation rejects agent_args on run jobs, without an explicit job agent, as an empty list, or with non-string items, with file and line in the error; the digest changes when agent_args changes and is unchanged for jobs that omit it. Extend the existing approval boundary test so editing only agent_args blocks unattended runs until reapproval; omitting agent_args preserves a pre-change digest fixture, not merely two new-code computations. Run `task cli:test PKG=./internal/cli -- -run '^TestEnableApproval$' -count=1 -v` for that approval regression.
- [x] `task cli:test PKG=./internal/config -- -run '^TestAgentArgsSchema$' -count=1 -v` exits 0 with PASS (the regression must be added, not a no-tests match). Reuse the schema-loading approach in jobs_test.go to accept prompt/prompt_file jobs with explicit agent and valid agent_args, and reject run jobs, missing explicit agent, empty lists, and non-string items. `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0 with an examples/ai/agent-profiles job using agent_args.
- [x] `rg -n "agent_args" docs/configuration.md docs/design.md docs/agents.md` and `rg -n "prompt_args" docs/configuration.md` exit 0; docs show the argv order, the explicit-agent rule, digest coverage, and the custom-profile migration note.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Refinement: TestNoopExitCodeSchemaRange validates only noop_exit_code and cannot prove agent_args schema rules; require an actual schema regression that rejects configurations accepted by mistake. resolveAgents fills job.Agent from default_agent, so explicit-agent validation must happen before that fallback. Digest compatibility needs a pre-change fixture, and approval behavior is an authorization boundary rather than a hash-only assertion.

AC#1: task cli:test PKG=./internal/cli -- -run "^TestPromptJobs$" -count=1 -> PASS; fake executables verify both prompt modes, session ordering, and unchanged built-in effective argv.
AC#2: task cli:test PKG=./internal/config -- -run "^(TestLoadJobs|TestJobDigest)$" -count=1 and task cli:test PKG=./internal/cli -- -run "^TestEnableApproval$" -count=1 -v -> PASS. Baseline digest captured before implementation: sha256:da8bab3de841d67cb71e01dc587dbcc75ffc4d6b678a2b08347e1498092be533. Argument-only changes block queued work until reapproval.
AC#3: task cli:test PKG=./internal/config -- -run "^TestAgentArgsSchema$" -count=1 -v -> PASS (test ran); task cli:test PKG=./integration -- -run "^TestExamplesValidate$" -count=1 -> PASS.
AC#4: rg -n "agent_args" docs/configuration.md docs/design.md docs/agents.md and rg -n "prompt_args" docs/configuration.md -> exit 0.
Delivery: e965c27494f1d1573ff329affc205debb581be47 fast-forward merged to main. task check:staged passed; task ci passed again on that final implementation commit. Independent reviewer found no validated defects. LSP jobs.go diagnostics clean. All changed files within contract; existing built-in command expectations intentionally split command and prompt_args while preserving effective argv. No tests deleted, skipped, or weakened; digest assertion now uses the captured constant. No real agent CLIs invoked.
Cleanup: owned noon-60 worktree and branch removed via Worktrunk; associated Herdr workspace wGV verified gone. Unrelated pause-all checkout preserved. No remaining blocker or resumable step.

Post-completion review 2026-10-18: implementation matches the settled contract. Verified argv order (command, session flag/ID inserted at command length, agent_args, prompt_args, prompt), explicit-agent check uses source fields before default_agent resolution, run-job/empty/non-string/NUL rejection, digest key only when set, built-in effective argv unchanged, profiles replace built-ins wholesale (no prompt_args double-append), schema rule, docs and example. Re-ran AC#1-#3 commands: all PASS. One P3 finding, not in the AC contract: inspection views omit agent_args. internal/app/inspect.go Job view has no agent_args field, so noon list --json (confirmed directly on examples/ai/agent-profiles docs-review), MCP list_jobs, and the TUI do not show permission-widening agent arguments outside the YAML. Consider adding agent_args to the inspection view. No source changes made in this review.

Review finding resolved in fe36eb8: inspection Job view includes agent_args (list --json, MCP list_jobs, HTTP API); docs/configuration.md updated. task ci passed.

## Summary

Implemented literal per-job agent_args, explicit-agent validation, digest/approval protection, prompt_args ordering and config output, schema, documentation and examples. Delivered locally in e965c27; all acceptance and CI checks pass.
