---
id: NOON-7
title: Trigger jobs from Git and agent hooks with payloads
status: done
type: feature
priority: medium
labels: [integration, security, reviewed]
blocked_by: [NOON-5, NOON-10]
rank: a3594d653594d6524
refs: [docs/design.md, docs/development.md, backlog/decisions/decision-4 - Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md]
files: [internal/config/**, internal/store/**, internal/scheduler/**, internal/app/**, internal/cli/**, internal/mcp/**, docs/triggers.md, README.md]
created: 2026-10-16T00:18:00Z
updated: 2026-10-16T18:23:00Z
---

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

## Acceptance criteria

- [x] `task cli:test -- -run '^TestTriggers$' -count=1 -v` exits 0 and prints `--- PASS: TestTriggers`, covering every case listed under Tests
- [x] `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "cat \\"$NOON_EVENT_FILE\\""\n' > "$noon_ac_project/noon.yaml" && echo '{"sha":"abc"}' | env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t --data -` exits 0 and prints the event JSON object, whose `source` is `cli` and whose `data` equals `{"sha":"abc"}`

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Extend config/store with digested cooldown, atomic queued cooldown checks, persisted depth and bounded event files following log retention. 2. Wire CLI, MCP and execution metadata through shared payload validation and recursion guards; preserve queued authorization and key ordering. 3. Add critical TestTriggers and store/MCP regression coverage after implementation; verify hook examples and document implemented behavior. 4. Run both acceptance commands, independent risk-focused verification and task ci; commit, merge to main, finalize provider state and clean up the owned worktree.

## Notes

Worklease claim 0f9def78f7ece00e8e20954e795548d0 acquired by loop 53909a3c-e1ac-40d5-a491-b52b62d6687d. Owned implementation checkout .worktrees/feat-noon-7-triggers on feat/noon-7-triggers, creation commit 9dea7ff871f6d963aacde6f439824eed0b0e39bb; receipt .git/worktrees/feat-noon-7-triggers/agent-creation.json. Modified-file expansion: docs/design.md may be updated to describe settled cooldown, depth and MCP payload contracts consistently with the required DoD.

Implementation and independent verifier passed: atomic event publication, retention/migrations, cooldown ordering, recursion propagation and MCP payload checks; no code defects. Parent corrected Codex Stop example to send Noon status to stderr and emit {} on stdout, as required by current https://developers.openai.com/codex/hooks. Claude format confirmed at https://code.claude.com/docs/en/hooks. All four documented Git/agent hook commands executed successfully in isolated /tmp/noon-7-hooks.wTs9yJ with isolated HOME and no hooks installed; queued runs 2–5, Codex stdout {}. AC#1: task cli:test -- -run ^TestTriggers$ -count=1 -v -> exit 0, --- PASS: TestTriggers (independent verifier). AC#2: combined shell fixture command blocked by dynamic-shell guard; executed the same build, fixture contents and isolated invocation as separate operations: task cli:build; echo {"sha":"abc"} piped to env -i PATH="$PATH" HOME=/tmp/noon-7-ac-home.QECXhP bin/noon -C /tmp/noon-7-ac-project.hbrEH2 run t --data - -> exit 0, {"schema_version":1,"source":"cli","received_at":"2026-10-16T16:02:42.495Z","data":{"sha":"abc"}}. No acceptance behavior changed. Executor task ci passed; final committed-tree CI remains. LSP diagnostics for internal/app/triggers.go clean. All source changes within contract plus previously justified docs/design.md; no tests removed, skipped or weakened.

Delivered commit 601c667ecd4e64825349de6024a9b11b8b14a55d (feat: add job trigger payloads and safeguards), fast-forward merged to main. task ci on committed implementation passed on main: gitleaks, govulncheck, formatting, vet, staticcheck, full tests, race tests, build and smoke. Worktrunk verified owned checkout/branch against creation receipt, removed both with post-remove hook; workspace wCN contained only its idle shell. No blockers or next implementation steps remain. Local temporary acceptance/smoke fixtures retained outside repository; no hooks/services installed.

Review (post-merge): --data literal/@FILE/stdin with 256 KiB and JSON validation, event-file shape/permissions/atomic publish, retention with logs, NOON_EVENT_FILE scrubbed from inherited env, cooldown order (key, requirements, cooldown, coalesce, insert) under one transaction, depth persistence and +1 propagation to jobs and hooks, same-job and depth-3 refusal, MCP data argument, and trigger docs match the contract. No fix needed in this task. Fixed in 579c79b (via NOON-8): QueueRun applied the calling process recursion guard and NOON_DEPTH to webhook requests, which run in the scheduler. Low, not fixed: an event file is written before the run row commits, so a crash in that window leaves events/ID.json and, because SQLite reuses the rolled-back rowid, the next payload run with that ID fails "publish event file" until a payload-free run consumes the ID; hooks for payload-free runs get NOON_EVENT_FILE set to empty while jobs have it unset. AC1 and AC2 rerun and pass; task ci passes.

## Summary

Implemented bounded CLI/MCP event payloads, durable depth propagation and recursion guards, atomic cooldown/key handling, event retention and verified trigger examples. TestTriggers, isolated stdin event acceptance, independent verification and full task ci passed. Implementation 601c667 merged locally to main; owned worktree and branch removed.
