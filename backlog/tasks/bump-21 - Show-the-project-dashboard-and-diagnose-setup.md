---
id: BUMP-21
title: Show the project dashboard and diagnose setup
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:03'
labels:
  - cli
dependencies:
  - BUMP-20
  - BUMP-17
  - BUMP-11
documentation:
  - docs/design.md
  - docs/development.md
modified_files:
  - internal/app/**
  - internal/cli/**
  - internal/service/**
  - internal/store/**
  - README.md
priority: high
type: feature
ordinal: 1400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: bare `bump` answers "what will run, what ran, and what needs me?" on one screen, and `bump doctor` finds every setup problem that would stop jobs from running.

Read first: docs/design.md#experience (the dashboard transcript is the target), #enable-and-approve, and #scheduler-lifetime.

Scope:
- One app operation builds the dashboard; the CLI only renders it. JSON: `{"schema_version":1,"project":{"path","name","timezone","enabled","config_error"},"scheduler":{"state":"running"|"offline","pid","heartbeat_at"},"service":{"installed"},"jobs":[...list job objects...],"queued":[...run objects...],"notices":[{"code","message"}],"next":"bump enable"}`.
- Human layout: a header line `NAME · PATH · scheduler running|offline · TIMEZONE`; the JOB/SCHEDULE/NEXT/LAST table (NEXT shows `paused`, `disabled`, or `needs approval` where relevant); up to three notice lines; exactly one `Next:` line. Fit 80 columns by truncating the widest column. No color.
- Notice codes, in priority order: `config_error`, `not_enabled` (scheduled jobs exist), `needs_approval`, `scheduler_offline` (project enabled), `queued`.
- `next` follows the same priority: fix config, `bump enable`, `bump service install` (or `bump serve` when the service is installed but not running), `bump run JOB`, or `bump add` when there are no jobs.
- `bump doctor` checks, each `ok`, `warn`, or `fail` with a one-line fix:
  - Config: config valid (fail); timezone valid (fail).
  - State and scheduler: state directory writable, or its nearest existing parent writable when absent, without creating it (fail); database schema compatible (fail); project enabled when scheduled jobs exist (warn); approvals pending (warn); scheduler heartbeat fresh while the project is enabled (warn); service installed on macOS or Linux (warn).
  - Per job: argv[0] resolvable for list `run` jobs using the job's PATH and cwd, `/bin/sh` present for string jobs, and the agent profile's argv[0] for prompt jobs (fail); `cwd` exists (fail); `prompt_file` exists and is at most 64 KiB (fail).
  - `.bump.yaml` ignored by Git when it exists (warn).
- Doctor exits 1 if any check fails, otherwise 0. JSON: `{"schema_version":1,"ok":bool,"checks":[{"name","status","message","hint"}]}`. Doctor never writes anything.

Non-goals: fixing problems automatically, network checks, agent login checks, colors, a watch mode.

Tests:
- internal/cli `TestDashboard` (JSON) for an empty project, not enabled, needs approval, scheduler offline vs. a fresh heartbeat, a recorded config error, and a queued run; each case asserts `notices` codes and `next`.
- internal/cli `TestDoctor`: a job whose command is missing fails with exit 1 and names the job; a healthy enabled project with a fresh heartbeat exits 0; neither run creates files in the project or state directories.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestDashboard|TestDoctor)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `task cli:build && bump_ac_home=$(mktemp -d) && bump_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: [definitely-not-a-command-xyz]\n' > "$bump_ac_project/bump.yaml" && env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump -C "$bump_ac_project" doctor; echo "exit=$?"` prints a failing check naming job `t` and the missing command, then `exit=1`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
