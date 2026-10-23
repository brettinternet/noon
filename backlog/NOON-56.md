---
id: NOON-56
title: Document checkout state, late catch-up runs, and delayed run_job requests for agents
status: done
type: docs
priority: medium
labels: [docs, agents, reviewed]
rank: aa6b29aca6b29ac70
refs: [docs/agents.md, examples/ai/background-agents/README.md, docs/design.md]
files: [docs/agents.md]
created: 2026-10-18T16:24:00Z
updated: 2026-10-18T17:52:00Z
---

Outcome: docs/agents.md tells prompt-job authors and MCP clients three implemented facts they currently have to discover from code or other docs.

1. Checkout state. A scheduled prompt job runs in its `cwd` (default the project root) in the live checkout, including uncommitted changes and whatever branch is checked out. Noon never creates worktrees or changes branches (docs/design.md#execution). Point editing jobs to examples/ai/background-agents (worktree isolation) and examples/ai/container-agent (a real isolation boundary); state that neither approval nor a worktree is a sandbox.
2. Late catch-up runs. With the default `missed: run_once`, a run after sleep or downtime starts as `NOON_TRIGGER=catch_up` with `NOON_SCHEDULED_AT` set to the first missed occurrence, possibly hours late. Show one short guard: `missed: skip` for time-sensitive jobs, or a `run` wrapper that compares `NOON_SCHEDULED_AT` with the current time and exits with the job `noop_exit_code`. Prompt jobs cannot use `noop_exit_code` directly; say so.
3. Delayed MCP requests. `run_job` already accepts `key`, `in`, and `at` (internal/mcp/server.go runJobArgs), and the HTTP API accepts `{key,in,at,data}` (docs/cli.md), but the docs/agents.md tool table lists only `data`. Fix the row and add one example of an agent asking for a follow-up, e.g. `run_job {"name":"check-ci","in":"2h"}`, noting the project must be enabled, shared jobs need approval, private jobs do not, and execution needs a running scheduler. Explain that `in`/`at` one-shots stay independent of due-run merging.

Prior art: Claude Desktop scheduled tasks (worktree toggle; warns a 9am task can run at 11pm), Codex scheduled tasks (local checkout versus dedicated worktree), Claude Code /loop one-shot follow-ups.

Non-goals: code changes, worktree creation or branch handling in core, a start window (draft-10), new environment variables, changes to the shipped `noon skill` text.

Dependencies: none.

Modified-file contract: docs/agents.md plus this task record. Record the starting commit SHA before implementation; compare scope against that SHA, not a moving main branch. Justify other paths in Implementation Notes first.

## Acceptance criteria

- [x] ``rg -n '^\| `run_job` .*`in`.*`at`' docs/agents.md`` exits 0, and docs/agents.md contains an `in` example for run_job; every field named matches runJobArgs in internal/mcp/server.go. The text distinguishes project enablement, shared-job approval, and scheduler availability from queue acceptance.
- [x] rg -n "background-agents" docs/agents.md and rg -n "uncommitted" docs/agents.md each exit 0; the text states Noon never creates worktrees or changes branches and that worktrees are not a sandbox.
- [x] rg -n "catch_up" docs/agents.md and rg -n "NOON_SCHEDULED_AT" docs/agents.md each exit 0; the guard shown uses only implemented fields and variables, and states prompt jobs cannot use noop_exit_code directly.
- [x] `git diff --check` exits 0. `git diff --name-only START_SHA -- . ':!backlog'` (replace START_SHA with the literal starting commit recorded before implementation) prints only docs/agents.md, including after committing on main.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Refinement: use single quotes for a copyable run_job search without backtick escaping. Use a recorded starting SHA for scope checks because this repository defaults to implementation in the current checkout; main moves on commit. Queue requirements checked against docs/agents.md and docs/design.md.

Start SHA: 7f67ee05f9c9bd8289b9f2c410bbd58e4bdf383d. Worklease claim on backlog-md noon/backlog NOON-56; implementing in worktree .worktrees/noon-56-agent-docs.

Commit 799236b on main (docs/agents.md only; worktree branch rebased onto b0bf5c2, fast-forwarded, worktree removed).
AC#1: rg -n '^\| `run_job` .*`in`.*`at`' docs/agents.md -> exit 0 (line 73); in example {"name":"check-ci","in":"2h","key":...} at line 85; fields key/in/at/data/name match runJobArgs; text separates enablement, shared-job approval (private none), running scheduler from queueing.
AC#2: rg -n background-agents docs/agents.md -> exit 0; rg -n uncommitted docs/agents.md -> exit 0; states Noon never creates worktrees or changes branches, neither approval nor a worktree is a sandbox.
AC#3: rg -n catch_up / rg -n NOON_SCHEDULED_AT docs/agents.md -> exit 0; guard uses missed, noop_exit_code, run, NOON_TRIGGER, NOON_SCHEDULED_AT only; states prompt jobs can't use noop_exit_code. Guard executed: 2020 catch_up -> exit 78; recent catch_up -> runs (0); schedule trigger -> runs (0); example noon.yaml accepted by noon list.
AC#4: git diff --check -> exit 0; git diff --name-only 7f67ee05f9c9bd8289b9f2c410bbd58e4bdf383d -- . ':!backlog' -> docs/agents.md only (after commit on main).
DoD: task ci -> exit 0 on 799236b. No tests touched. Review: one pass, no defects.

Post-completion review: no actionable defects. Verified run_job fields (name/project/key/in/at/data) and at formats against runJobArgs in internal/mcp/server.go; separate one-shot vs due-run merge against docs/design.md and docs/cli.md; catch_up trigger and NOON_SCHEDULED_AT = first missed occurrence formatted RFC 3339 UTC (plan.go uses state.NextAt; serve.go formats time.RFC3339 UTC, so the BSD date -f pattern with Z matches); noop_exit_code rejected on prompt jobs (config/jobs.go:631); 'weekdays at 9am' parses; claude --permission-prompts none matches the built-in profile. BSD date branch checked: 2020-01-01T00:00:00Z -> 1577836800.

## Summary

docs/agents.md now documents live-checkout execution and isolation options, late catch_up runs with a NOON_SCHEDULED_AT guard, and run_job key/in/at with a delayed follow-up example.
