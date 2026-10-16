---
id: NOON-32
title: Verify vendor-specific claims in the AI examples
status: To Do
assignee: []
created_date: '2026-10-16 22:09'
labels:
  - integration
  - examples
dependencies: []
documentation:
  - docs/triggers.md
  - docs/agents.md
  - examples/README.md
modified_files:
  - examples/ai/**
priority: medium
type: docs
ordinal: 3300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: every vendor CLI flag, hook contract, and extension behavior the AI examples rely on is confirmed against the installed tool or current vendor documentation, and examples that differ are corrected.

Why: the examples library (commits 7389425 and 3f2c196) was partly written from memory and never run with real agents. Users copy these files verbatim, and a wrong flag or exit code makes an unattended agent job hang on a prompt, fail, or silently skip its completion gate. Unverified claims:
- `examples/ai/verify-loop/scripts/verify-gate.sh` and `claude-settings.gate.json`: Claude Code Stop hooks block stopping on exit 2 and feed stderr back to the agent; stdin carries `session_id` and `stop_hook_active`; `$CLAUDE_PROJECT_DIR` expands in the command; the per-hook `timeout` field and its unit.
- `examples/ai/verify-loop/codex-hooks.json`: copied from docs/triggers.md (verified 2026-10-16); confirm it is still current.
- `examples/ai/agent-profiles/user-config.yaml`: `claude --allowedTools` patterns such as `Bash(git log:*)`; that Read, Grep, and Glob run without prompting under `-p --permission-prompts none`; `codex exec --sandbox read-only|workspace-write -` with the prompt on stdin; `gemini -p` taking the prompt as an argument; `ollama run MODEL` reading the prompt from stdin.
- `examples/ai/background-agents/scripts/agent-worktree.sh`: `codex exec --sandbox workspace-write -` runs unattended in a freshly created worktree without a trust prompt.
- `examples/ai/pr-review/README.md`: whether `gh webhook forward` (cli/gh-webhook) can sign forwarded deliveries with a secret Noon accepts. If it cannot, document the limitation instead.

Scope: verify each claim, correct the examples and their READMEs, and add a "verified on DATE against SOURCES" note in the style of docs/triggers.md to the READMEs whose claims were checked.

Non-goals: changing Noon code or built-in agent profiles, adding examples or agents, verifying non-AI examples.

Dependencies: none. Running real agents costs API usage; use the smallest prompts that exercise each behavior.

Modified-file contract: the modified_files list plus this task record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Implementation Notes list every claim under Why with its result (confirmed or corrected), the evidence (vendor documentation URL or the exact `--help` line), and the date.
- [ ] #2 In a disposable Git repository whose `verify` job fails, a headless `claude -p` session using the gate settings is blocked by `verify-gate.sh` (exit 2, failure output fed back), and with `verify` passing the session ends normally; the commands and observed output are recorded in Implementation Notes.
- [ ] #3 `rg -n "[Vv]erified" examples/ai/verify-loop/README.md examples/ai/agent-profiles/README.md examples/ai/pr-review/README.md` prints a verification note in each file.
- [ ] #4 `task cli:test -- -run "^TestExamplesValidate$" -count=1 -v` exits 0 and prints `--- PASS: TestExamplesValidate`.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
