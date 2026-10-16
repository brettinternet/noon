---
id: NOON-41
title: Preserve invocation context in copyable next-step commands
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-16 23:23'
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
- [ ] #1 `task cli:test -- -run '^(TestDashboard|TestValueFlagsAreNotMistakenForJSON|TestInspectionDoesNotWriteOrExecute)$' -count=1 -v` exits 0, with a focused public-interface regression that copies a generated inspection command from a different cwd and proves it reads the intended project/state. Include a path containing shell metacharacters and assert they remain data, never execution.
- [ ] #2 `task cli:build` and `task smoke` exit 0. In an isolated project with a manual printf job, an explicit config file and a custom state directory, run the job, request bounded/filtered logs, and execute the printed resume command from the original cwd. It addresses the same run/state and preserves relevant log selection; record commands and observed output.
- [ ] #3 Manually inspect dashboard/run/queue guidance under -C, --config, --state-dir and environment-selected overrides. Project commands target the intended project; service/serve suggestions carry only supported selectors and are checked with --help or service install --print, never by starting a real service. `task test` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
