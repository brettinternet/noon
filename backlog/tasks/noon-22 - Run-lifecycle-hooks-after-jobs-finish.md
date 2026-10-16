---
id: NOON-22
title: Run lifecycle hooks after jobs finish
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:03'
labels:
  - integration
dependencies:
  - NOON-4
  - NOON-17
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-4 -
    Commands-agent-profiles-and-hooks-are-the-extension-model-no-plugin-system.md
modified_files:
  - internal/config/**
  - internal/runner/**
  - internal/store/**
  - internal/scheduler/**
  - internal/app/**
  - internal/cli/**
  - docs/hooks.md
  - docs/configuration.md
  - README.md
priority: medium
type: feature
ordinal: 1600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users get notified, or chain follow-up work, when jobs succeed or fail, using any command, without Noon shipping notification providers.

Read first: docs/design.md#integrations (Hooks) and #security-model, decision-4.

Scope:
- Job fields `on_success` and `on_failure`: a string (`/bin/sh -c`) or a list (argv), validated like `run`. An empty list disables an inherited default. User config (and an explicit `--config` file) may set top-level `on_success`/`on_failure` defaults for every job; project files may not set top-level defaults. Hooks defined on shared jobs are part of the digest.
- When: `on_success` after `succeeded`; `on_failure` after `failed` or `timed_out`. No hook for other statuses, and never a hook for a hook.
- How: for every trigger, through the execution path shared by `noon run` and the scheduler, after the run is recorded as finished and outside the concurrency caps. cwd and environment are those of the job, plus `NOON_STATUS`, `NOON_EXIT_CODE`, and `NOON_LOG_FILE` (absolute path, or unset when no log). stdin is `{"schema_version":1,"project":{"path","name"},"run":RUN}`, where RUN is the shared run JSON object. Timeout 30s, then TERM, a 5s grace, and KILL on the hook's process group.
- Record: append the hook's combined output (first 64 KiB) to the run's log after a marker `[noon: on_failure hook exited 1]` (or `timed out`), and store `hook_status` (`running`, `ok`, `failed`, `timed_out`, or `interrupted`) and `hook_exit_code` on the run (migration). When a hook will run, the transaction that records the run's terminal status also sets `hook_status` to `running`, and `logs --follow` keeps following until it is no longer `running`. Recovery marks a `running` hook whose owner is gone as `interrupted`. A hook failure never changes the run's status or a foreground run's exit code. Run JSON gains `hook: {"event","status","exit_code"}` or null.
- A foreground `noon run` prints one stderr line after the hook, such as `on_failure hook: ok` or `on_failure hook: exit 1`.
- Add docs/hooks.md with tested examples: a macOS notification via `osascript`, Linux `notify-send`, and posting the summary to a chat webhook with `curl --data-binary @-`. Each example reads `$NOON_JOB` and `$NOON_STATUS` or stdin, never interpolating untrusted output into a shell string.

Non-goals: built-in notifiers, retries, hooks on start, hook chains, per-trigger hooks, templating.

Tests:
- internal/cli `TestHooks`, with fake hook scripts that write their stdin and env to files: success runs only `on_success` with valid JSON stdin; failure and timeout run `on_failure`; an empty list disables the user default; a hook that sleeps past the timeout is killed and recorded while the run status stays unchanged; hook output beyond 64 KiB is cut; `logs --follow`, started while the job runs, prints the output of a hook that sleeps 1s before writing, then exits.
- Extend `TestJobDigest`: changing a shared job's hook changes its digest.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestHooks|TestJobDigest)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "exit 3"\n    on_failure: [sh, -c, "cat > hook.json"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t; echo "exit=$?"` prints `exit=3`, and `$noon_ac_project/hook.json` contains the run summary JSON, whose `run.status` is `failed`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
