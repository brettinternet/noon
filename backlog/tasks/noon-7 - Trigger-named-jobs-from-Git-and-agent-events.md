---
id: NOON-7
title: Trigger jobs from Git and agent hooks with payloads
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - integration
  - security
dependencies:
  - NOON-5
  - NOON-10
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/config/**
  - internal/store/**
  - internal/scheduler/**
  - internal/app/**
  - internal/cli/**
  - internal/mcp/**
  - docs/triggers.md
  - README.md
priority: medium
type: feature
ordinal: 1700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Git hooks, agent hooks, and scripts can trigger named jobs with a JSON payload, without floods, duplicates, or loops.

Read first: docs/design.md#integrations (Triggers, Event files) and #execution, decision-4.

Scope:
- `noon run NAME --data JSON`, `--data @FILE`, or `--data -` (stdin). Foreground and queued runs both accept it. The payload must be valid JSON of at most 256 KiB (otherwise a usage error, exit 2). It is stored before the run starts as `STATE/events/RUN_ID.json` in the design's event-file shape (`source: "cli"`). The migration adds `runs.event_path TEXT` and `runs.depth INTEGER NOT NULL DEFAULT 0`. The job sees `NOON_EVENT_FILE`. Event files follow log retention.
- Job field `cooldown` (duration, at least 1s; in the digest). For queued requests (`--detach`, `--in`, `--at`, MCP `run_job`, and later webhooks), if a run of the job was created within the cooldown, create nothing and return the latest run with `created: false` and `reason: "cooldown"` plus when it ends. Exit 0. Scheduled and foreground runs ignore cooldowns. In the shared queued-request operation the order becomes: key, requirements, cooldown, coalescing, insert. A retried key therefore returns its own run even inside a cooldown.
- Recursion guard. Every run stores `depth`: the requesting process's `NOON_DEPTH` (0 when unset or not a number) for foreground and queued runs, and 0 for scheduled and webhook runs. The run and its hooks get `NOON_DEPTH` = depth + 1, never the scheduler's own value. So A queuing B, which queues A again, reaches the limit even though every hop is detached. A request made with `NOON_DEPTH` of 3 or more, or one that targets the same project and job as the calling `NOON_PROJECT` and `NOON_JOB`, fails with code `recursion` and exit 1. `noon mcp` applies the guard using its own process environment, which it inherits from the agent that launched it.
- MCP `run_job` gains an optional `data` argument (a JSON value) with the same limits, so an agent can pass context.
- Add docs/triggers.md with examples verified on the implementing machine: Git `post-commit` (`noon run tests --detach --key "$(git rev-parse HEAD)"`); `post-merge`; a Claude Code `Stop` hook in `.claude/settings.json` piping hook input with `--data -`; and any other agent hook mechanism whose syntax you confirmed in that agent's current docs. Never install hooks; show the file to create.

Non-goals: installing hooks, debounce, transcript scraping, webhooks (next task), templating payloads into commands.

Tests:
- internal/cli `TestTriggers`: `--data` reaches a foreground job and a queued job (via the serve loop) through `NOON_EVENT_FILE` with the documented shape; invalid and oversized payloads exit 2 and create nothing; cooldown returns `created: false` without a new record; same-job and depth-3 recursion are refused; job A that queues B, where B queues A, is refused at depth 3 across real detached dispatches by the serve loop; `--key` dedupe still applies, and a retried key inside a cooldown returns its own run.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestTriggers$' -count=1 -v` exits 0 and prints `--- PASS: TestTriggers`, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "cat \\"$NOON_EVENT_FILE\\""\n' > "$noon_ac_project/noon.yaml" && echo '{"sha":"abc"}' | env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --data -` exits 0 and prints the event JSON object, whose `source` is `cli` and whose `data` equals `{"sha":"abc"}`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
