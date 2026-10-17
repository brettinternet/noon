---
id: NOON-41
title: Preserve invocation context in copyable next-step commands
status: Done
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-17 01:36'
labels:
  - cli-ux
  - devex
dependencies: []
modified_files:
  - internal/cli/**
  - internal/app/**
  - docs/cli.md
  - docs/design.md
priority: high
type: bug
ordinal: 8400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: copying a suggested command operates on the same intended project, config and state instead of silently inspecting or mutating another checkout.

Verified reproduction: run -C 'project space' with a custom --state-dir, then logs NAME -n 1 prints next: noon logs 1 --after-cursor 12 without either override. Copying it from the original invocation directory returns No runs yet. run's candidate guidance likewise drops -C. Queue follow hints and dashboard/service guidance embed bare noon commands.

Scope: audit action-specific Next, Hint, Follow it and log cursor commands. Preserve the selectors required by the suggested operation, use safely quoted paths/values (including spaces and apostrophes), and preserve timezone where a scheduling suggestion requires it. Retain log selection intent where relevant to resuming a filtered/sectioned read. Apply this to machine-readable next/hint fields too. Do not blindly append project/config/timezone to serve or service commands that reject them; state-dir is the relevant service selector. Never propagate output mode or secrets/payloads unnecessarily.

Non-goals: changing project discovery, approval or scheduler behavior, storing shell commands as runtime instructions, automatic follow-up execution.
Modified-file contract: internal/cli/**, internal/app/** only for guidance representation/rendering, docs/cli.md, docs/design.md; explain expansion.
Dependencies: none. Coordinate wording with NOON-39/NOON-40 without requiring them; existing hints already demonstrate the bug.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestDashboard|TestValueFlagsAreNotMistakenForJSON|TestInspectionDoesNotWriteOrExecute)$' -count=1 -v` exits 0, with a focused public-interface regression that copies a generated inspection command from a different cwd and proves it reads the intended project/state. Include a path containing shell metacharacters and assert they remain data, never execution.
- [x] #2 `task cli:build` and `task smoke` exit 0. In an isolated project with a manual printf job, an explicit config file and a custom state directory, run the job, request bounded/filtered logs, and execute the printed resume command from the original cwd. It addresses the same run/state and preserves relevant log selection; record commands and observed output.
- [x] #3 Manually inspect dashboard/run/queue guidance under -C, --config, --state-dir and environment-selected overrides. Project commands target the intended project; service/serve suggestions carry only supported selectors and are checked with --help or service install --print, never by starting a real service. `task test` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented shared CLI guidance rendering for explicit flag/environment selectors, absolute shell quoting, supported service selectors/environment clearing, and filtered log resume commands (JSON resume alongside numeric next). Doctor hints remain complete. No runtime behavior or dependencies changed.
AC#1: task cli:test -- -run TestDashboard -count=1 -v plus the exact task AC command selecting TestDashboard, TestValueFlagsAreNotMistakenForJSON and TestInspectionDoesNotWriteOrExecute -> exit 0. Extended TestDashboard copies generated log arguments through sh from another cwd, verifies intended run/state/filtering, and checks apostrophes, command substitution and semicolon paths remain data. Also verifies environment-selected service guidance with service install --print.
AC#2: task cli:build; task smoke -> exit 0. Isolated script /tmp/noon-41-acceptance.iI41b6/check.sh -> exit 0. Explicit project/config/state paths include spaces and apostrophes, timezone UTC. Ran printf check then logs check --section job --match ^keep --context 0 --after-cursor 0 --limit-bytes 11. First page 1:keep-one, cursor 9; copied resume from original cwd returned 3:keep-two, cursor 25. JSON resume from another cwd returned the same page. Transcript: /tmp/noon-41-manual.log.
AC#3: inspected dashboard, run candidates, queued Follow it and scheduler guidance with explicit selectors and environment overrides. Project commands retained selectors; service suggestions carried only state-dir and cleared unsupported environment variables. Generated service install --print succeeded; no service started. task test -> exit 0.
Review: reviewer launch rejected by model scope before any child started. One local review found and fixed rewriting a log regex containing noon run, now covered by regression. Quoted history job arguments and prevented doctor hint truncation. LSP diagnostics clean. Existing tests assert newly required state selector; none deleted, skipped or weakened. Initial full suite had one transient TestLogsFollow timing failure; unchanged focused and later full suites passed.
Implementation/docs within file contract; backlog metadata is required delivery bookkeeping. Next: commit, CI, merge, finalize, owned worktree cleanup, release claim.

Delivery: implementation commit 6371314f502fc0ef695a2516af96307bc4530715 passed task ci (security, formatting/vet/staticcheck, tests, race, smoke) on the committed tree. Fast-forward merged into main. Verified creation receipt and Worktrunk listing before removal; owned fix/noon-41-guidance worktree/branch removed with Worktrunk, corresponding Herdr workspace wF4 disappeared. Other pre-existing worktrees retained untouched. No push. No remaining item blocker or resumable implementation step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Copyable CLI guidance retains explicit invocation context with shell-safe paths; service commands receive supported state selection only; log resumes retain filters and sections, including a JSON resume command. All acceptance checks and CI passed; committed and merged locally; owned worktree cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
