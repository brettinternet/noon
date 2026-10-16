---
id: NOON-36
title: Use NOON_PREVIOUS_STATUS in the notification examples
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
labels:
  - examples
dependencies:
  - NOON-31
documentation:
  - docs/hooks.md
  - examples/integrations/notifications/README.md
modified_files:
  - examples/integrations/notifications/**
priority: low
type: task
ordinal: 3700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the notifications example reports status changes using the NOON_PREVIOUS_STATUS fact from NOON-31 instead of querying history itself.

Why: `examples/integrations/notifications/bin/noon-recovery` runs `noon history NAME --json` and jq inside every hook to find the previous outcome, which is the per-hook logic NOON-31 removes. Once NOON-31 ships, the example should show the supported pattern, consistent with the change-only wrappers in docs/hooks.md: notify on the first failure and on recovery, stay quiet while the status is unchanged, and treat no history as succeeded so a first success is quiet.

Scope: replace `noon-recovery` with a `noon-on-change` wrapper that uses NOON_PREVIOUS_STATUS, and update the example noon.yaml and README (scripts table, Needs column, hook explanation).

Non-goals: changing Noon, docs/hooks.md, or the other notification scripts beyond references.

Dependencies: NOON-31 must be Done.

Modified-file contract: the modified_files list plus this task record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `rg -n "noon history|jq" examples/integrations/notifications/bin/noon-on-change` prints nothing, and `rg -n "noon-recovery" examples` prints nothing.
- [ ] #2 In a disposable project whose job fails, fails, succeeds, succeeds, with `noon-on-change` configured as both hooks and a stub `noon-notify` on PATH that appends its status to a file, the file contains exactly two lines: `failed` then `recovered`. The command and output are recorded in Implementation Notes.
- [ ] #3 `task cli:test -- -run "^TestExamplesValidate$" -count=1 -v` exits 0 and prints `--- PASS: TestExamplesValidate`.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
