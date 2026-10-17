---
id: NOON-39
title: Make no-target guidance and mutation feedback consistently useful
status: Done
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-17 00:30'
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
- [x] #1 `task cli:build` exits 0. In empty and populated isolated projects, manually run `bin/noon`, `bin/noon run`, `bin/noon add`, `bin/noon remove`, `bin/noon pause`, `bin/noon resume`, `bin/noon cancel`, `bin/noon history`, `bin/noon logs`, `bin/noon service`, `bin/noon import crontab` and `bin/noon shell init`; all return useful inspection/guidance with exit 0, no hang, and no implicit mutation.
- [x] #2 `task cli:test -- -run '^(TestInspectionDoesNotWriteOrExecute|TestRemoveJobs|TestPauseResume)$' -count=1 -v` exits 0. Extend the existing safety regression to cover empty and single-job selectors: no jobs/hooks run, no manifests or state are created/changed, and no service action occurs. Check human wording manually rather than snapshotting prose.
- [x] #3 In an isolated fixture, `bin/noon remove NAME` reports the removed job and definition file while retained history remains queryable; `bin/noon remove --json NAME` retains its structured success contract. `task test` and `task smoke` exit 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect selector and output conventions; improve empty guidance and remove confirmation without changing operational defaults. 2. Extend the existing inspection safety regression and update CLI/shell docs. 3. Run all acceptance commands and task ci, review once, commit, merge locally, finalize task and clean up the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: task cli:build -> exit 0. env -i PATH="$PATH" /bin/bash /tmp/noon-39-acceptance.sh -> exit 0; manually inspected all twelve requested bare commands in empty and single-job fixtures, useful output with no hangs, manifest changes, job/hook execution, state creation or service mutation. HOME/XDG isolated, NOON_* absent, service inspection used a fake manager. Fixture retained at /var/folders/jp/1mwx72h172955139pth4h8800000gn/T/noon-39.Anwp4gUWJ2.

AC#2: task cli:test -- -run '^(TestInspectionDoesNotWriteOrExecute|TestRemoveJobs|TestPauseResume)$' -count=1 -v -> exit 0. Extended existing safety regression for empty/single-job selectors in human and JSON modes; filesystem snapshots catch jobs/hooks, manifests/state and service mutations. Shell guidance case moved from invalid-usage coverage to successful inspection coverage as required; invalid shell/options remain rejected.

AC#3: isolated acceptance script explicitly added/ran/removed retained; human removal named retained and .noon.yaml and confirmed retained history. history retained returned succeeded run 1; logs 1 returned retained-output after removal. remove --json only-job returned removed/file/schema_version unchanged. task test and task smoke -> exit 0.

Delivery: implementation commit 7eaa80f merged fast-forward into main. task check:staged and task ci passed on that commit (security, vet, Staticcheck, full tests, race, smoke); affected Go LSP diagnostics clean. One proportional general diff review found no concrete remaining defects. Changes stay within contract plus this required backlog record. No tests deleted/skipped/weakened; existing shell expectation updated for the requested new behavior. CLI/shell docs updated; README and design already describe this safe no-target contract. No blockers or remaining implementation steps; finalize ledger commit and remove owned worktree, then release Worklease claim.

Cleanup: verified creation receipt for this session/loop and Worktrunk listing; checked owned Herdr workspace wEW contained only its idle shell. wt remove fix/noon-39-guidance --foreground --format=json removed worktree and merged branch; post-remove hook closed wEW, verified absent. No owned checkout/workspace retained. Unrelated pre-existing worktrees left untouched.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented useful empty selectors, explicit remove/pause/resume candidate syntax, safe shell setup guidance, and remove confirmation with retained history. Verified all acceptance commands and full task ci; merged 7eaa80f locally without pushing.
<!-- SECTION:FINAL_SUMMARY:END -->
