---
id: NOON-39
title: Make no-target guidance and mutation feedback consistently useful
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
  - README.md
  - docs/cli.md
  - docs/shell.md
priority: high
type: enhancement
ordinal: 6400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: running a command without a target explains the current state and the next explicit action, rather than appearing broken or guessing a mutation.

Evidence from an empty isolated project: remove, pause and resume exit 0 with no output; run says to edit noon.yaml instead of advertising the private-default add flow; history/logs say only No runs yet. Removing an existing job also succeeds silently. shell init without a shell returns usage error 2, unlike add/import crontab's safe guidance defaults.

Scope: give empty selectors clear feedback and a runnable next step; retain useful candidate listings when populated, with the full target and syntax. Bare shell init should list supported shells and sourcing guidance, return 0, and never guess the shell or source code. Give successful remove an explicit job/file confirmation and say history is retained. Preserve the existing root/status dashboard, latest-run logs default, history limit and service-status default. Guidance is not a wizard and must never auto-select or execute the only job.

Non-goals: interactive prompts, implicit enablement/service startup, bulk actions, new defaults for explicitly operational serve/mcp/tui/enable/disable/init commands.
Modified-file contract: internal/cli/**, README.md, docs/cli.md, docs/shell.md; explain expansion in notes.
Dependencies: none. Context-qualified next commands are independently covered by NOON-41; do not duplicate that implementation.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:build` exits 0. In empty and populated isolated projects, manually run `bin/noon`, `bin/noon run`, `bin/noon add`, `bin/noon remove`, `bin/noon pause`, `bin/noon resume`, `bin/noon cancel`, `bin/noon history`, `bin/noon logs`, `bin/noon service`, `bin/noon import crontab` and `bin/noon shell init`; all return useful inspection/guidance with exit 0, no hang, and no implicit mutation.
- [ ] #2 `task cli:test -- -run '^(TestInspectionDoesNotWriteOrExecute|TestRemoveJobs|TestPauseResume)$' -count=1 -v` exits 0. Extend the existing safety regression to cover empty and single-job selectors: no jobs/hooks run, no manifests or state are created/changed, and no service action occurs. Check human wording manually rather than snapshotting prose.
- [ ] #3 In an isolated fixture, `bin/noon remove NAME` reports the removed job and definition file while retained history remains queryable; `bin/noon remove --json NAME` retains its structured success contract. `task test` and `task smoke` exit 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
