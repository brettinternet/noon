---
id: NOON-36
title: Use NOON_PREVIOUS_STATUS in the notification examples
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
updated_date: '2026-10-16 22:21'
labels:
  - examples
dependencies:
  - NOON-31
documentation:
  - docs/hooks.md
  - examples/integrations/notifications/README.md
modified_files:
  - examples/integrations/notifications/**
  - integration/examples_test.go
priority: low
type: task
ordinal: 3250
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the notifications example uses NOON_PREVIOUS_STATUS from NOON-31 instead of querying history, reporting the first unhealthy result and one recovery.

Scope: replace noon-recovery with noon-on-change; update example YAML/README/script table and remove stale references. Keep noon-notify as the sibling executable selected relative to the wrapper; do not add configurable dispatch just for testing.

Policy stays in this wrapper: succeeded is healthy; failed and timed_out are one unhealthy group. Unknown history defaults to healthy, so the first success is silent and the first failure notifies. Repeated failures, including failed to timed_out and back, are silent. A return to succeeded invokes noon-notify with NOON_STATUS=recovered. Explain that both hooks are required, history pruning can reset the baseline, and notification delivery is not retried/deduplicated by Noon.

Extend integration/examples_test.go with one focused regression using the shipped wrapper copied beside a capture-only noon-notify stub in a temporary bin directory. A PATH-only stub is insufficient because the wrapper resolves its sibling. Use isolated HOME/XDG/state and a disposable Noon project; never call desktop or network notifiers.

Non-goals: core Noon changes, docs/hooks.md (owned by NOON-31), changing other notification providers, raw-status-change alerts during one outage.

Dependencies: NOON-31. Reuse any existing example helpers; do not add a dependency on unrelated example tests.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `rg -n 'noon history|jq' examples/integrations/notifications/bin/noon-on-change` and `rg -n 'noon-recovery' examples` each produce no matches (expected exit 1, not an execution error); the new wrapper exists and is executable.
- [ ] #2 `task cli:test -- -run '^TestExampleNotifications$' -count=1 -v` exits 0 and prints PASS. Through the built binary, a first success is silent; a fresh job failing, failing, succeeding, succeeding records exactly failed then recovered in the sibling stub. A bounded direct wrapper check of failed/timed_out transitions confirms that changing failure subtype stays silent and recovery still notifies.
- [ ] #3 `task cli:test -- -run '^TestExamplesValidate$' -count=1 -v` exits 0 and prints PASS. The README explains the healthy/unhealthy policy, both-hook configuration, unset previous status, and retained-history/delivery limitations consistently with NOON-31.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: a stub only on PATH would not intercept noon-recovery's dirname-relative noon-notify. Stub the sibling in the disposable copy instead. Align the example with NOON-31's first-failure/recovery goal rather than alerting on failed/timed_out changes within one outage.
<!-- SECTION:NOTES:END -->
