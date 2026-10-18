---
id: NOON-56
title: >-
  Document checkout state, late catch-up runs, and delayed run_job requests for
  agents
status: To Do
assignee: []
created_date: '2026-10-18 16:24'
updated_date: '2026-10-18 16:31'
labels:
  - docs
  - agents
dependencies: []
documentation:
  - docs/agents.md
  - examples/ai/background-agents/README.md
  - docs/design.md
modified_files:
  - docs/agents.md
priority: medium
type: docs
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: docs/agents.md tells prompt-job authors and MCP clients three implemented facts they currently have to discover from code or other docs.

1. Checkout state. A scheduled prompt job runs in its `cwd` (default the project root) in the live checkout, including uncommitted changes and whatever branch is checked out. Noon never creates worktrees or changes branches (docs/design.md#execution). Point editing jobs to examples/ai/background-agents (worktree isolation) and examples/ai/container-agent (a real isolation boundary); state that neither approval nor a worktree is a sandbox.
2. Late catch-up runs. With the default `missed: run_once`, a run after sleep or downtime starts as `NOON_TRIGGER=catch_up` with `NOON_SCHEDULED_AT` set to the first missed occurrence, possibly hours late. Show one short guard: `missed: skip` for time-sensitive jobs, or a `run` wrapper that compares `NOON_SCHEDULED_AT` with the current time and exits with the job `noop_exit_code`. Prompt jobs cannot use `noop_exit_code` directly; say so.
3. Delayed MCP requests. `run_job` already accepts `key`, `in`, and `at` (internal/mcp/server.go runJobArgs), and the HTTP API accepts `{key,in,at,data}` (docs/cli.md), but the docs/agents.md tool table lists only `data`. Fix the row and add one example of an agent asking for a follow-up, e.g. `run_job {"name":"check-ci","in":"2h"}`, noting the job must already be approved and that `in`/`at` one-shots stay independent of due-run merging.

Prior art: Claude Desktop scheduled tasks (worktree toggle; warns a 9am task can run at 11pm), Codex scheduled tasks (local checkout versus dedicated worktree), Claude Code /loop one-shot follow-ups.

Non-goals: code changes, worktree creation or branch handling in core, a start window (draft-10), new environment variables, changes to the shipped `noon skill` text.

Dependencies: none.

Modified-file contract: docs/agents.md plus this task record. Justify other paths in Implementation Notes first.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 rg -n "^\| \`run_job\` .*\`in\`.*\`at\`" docs/agents.md exits 0, and docs/agents.md contains an `in` example for run_job; every field named matches runJobArgs in internal/mcp/server.go.
- [ ] #2 rg -n "background-agents" docs/agents.md and rg -n "uncommitted" docs/agents.md each exit 0; the text states Noon never creates worktrees or changes branches and that worktrees are not a sandbox.
- [ ] #3 rg -n "catch_up" docs/agents.md and rg -n "NOON_SCHEDULED_AT" docs/agents.md each exit 0; the guard shown uses only implemented fields and variables, and states prompt jobs cannot use noop_exit_code directly.
- [ ] #4 git diff --name-only main -- . ":!backlog" prints only docs/agents.md.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
