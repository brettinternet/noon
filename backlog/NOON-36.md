---
id: NOON-36
title: Use NOON_PREVIOUS_STATUS in the notification examples
status: done
type: task
priority: low
labels: [examples, reviewed]
blocked_by: [NOON-31]
rank: a5f417d05f417d040
refs: [docs/hooks.md, examples/integrations/notifications/README.md]
files: [examples/integrations/notifications/**, integration/examples_test.go]
created: 2026-10-16T22:10:00Z
updated: 2026-10-17T02:00:00Z
---

Outcome: the notifications example uses NOON_PREVIOUS_STATUS from NOON-31 instead of querying history, reporting the first unhealthy result and one recovery.

Scope: replace noon-recovery with noon-on-change; update example YAML/README/script table and remove stale references. Keep noon-notify as the sibling executable selected relative to the wrapper; do not add configurable dispatch just for testing.

Policy stays in this wrapper: succeeded is healthy; failed and timed_out are one unhealthy group. Unknown history defaults to healthy, so the first success is silent and the first failure notifies. Repeated failures, including failed to timed_out and back, are silent. A return to succeeded invokes noon-notify with NOON_STATUS=recovered. Explain that both hooks are required, history pruning can reset the baseline, and notification delivery is not retried/deduplicated by Noon.

Extend integration/examples_test.go with one focused regression using the shipped wrapper copied beside a capture-only noon-notify stub in a temporary bin directory. A PATH-only stub is insufficient because the wrapper resolves its sibling. Use isolated HOME/XDG/state and a disposable Noon project; never call desktop or network notifiers.

Non-goals: core Noon changes, docs/hooks.md (owned by NOON-31), changing other notification providers, raw-status-change alerts during one outage.

Dependencies: NOON-31. Reuse any existing example helpers; do not add a dependency on unrelated example tests.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.

## Acceptance criteria

- [x] `rg -n 'noon history|jq' examples/integrations/notifications/bin/noon-on-change` and `rg -n 'noon-recovery' examples` each produce no matches (expected exit 1, not an execution error); the new wrapper exists and is executable.
- [x] `task cli:test -- -run '^TestExampleNotifications$' -count=1 -v` exits 0 and prints PASS. Through the built binary, a first success is silent; a fresh job failing, failing, succeeding, succeeding records exactly failed then recovered in the sibling stub. A bounded direct wrapper check of failed/timed_out transitions confirms that changing failure subtype stays silent and recovery still notifies.
- [x] `task cli:test -- -run '^TestExamplesValidate$' -count=1 -v` exits 0 and prints PASS. The README explains the healthy/unhealthy policy, both-hook configuration, unset previous status, and retained-history/delivery limitations consistently with NOON-31.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Replace notification wrapper and update example documentation. 2. Extend existing integration tests with isolated sibling capture stub and verify transition policy. 3. Run acceptance checks and CI, commit, merge to main, finalize task and clean owned worktree.

## Notes

Refinement: a stub only on PATH would not intercept noon-recovery's dirname-relative noon-notify. Stub the sibling in the disposable copy instead. Align the example with NOON-31's first-failure/recovery goal rather than alerting on failed/timed_out changes within one outage.

AC#1: rg -n "noon history|jq" examples/integrations/notifications/bin/noon-on-change and rg -n "noon-recovery" examples -> both exit 1, no matches; test -x wrapper -> exit 0.
AC#2: task cli:test -- -run "^TestExampleNotifications$" -count=1 -v -> exit 0, PASS; isolated binary runs captured exactly failed then recovered, first success silent; bounded failure-subtype transitions captured only recovery.
AC#3: task cli:test -- -run "^TestExamplesValidate$" -count=1 -v -> exit 0, PASS. README reviewed against docs/hooks.md for grouping, both hooks, unknown history, pruning, and delivery limitations.
Single scoped review found no concrete defects. LSP diagnostics clean. No tests removed, skipped, or weakened; changes stay within contract. No core contract changed, so design docs remain accurate.
Implementation commit 1d7546045ee9019f763d985d05710fe3d700b4cd passed task check:staged and task ci (unit/integration, race, vet, Staticcheck, secrets, vulnerabilities, build, smoke). Fast-forward merged into main. Owned worktree and branch fix/noon-36-notifications removed by Worktrunk; matching Herdr workspace removed. Existing feat/tui-design checkout left untouched. No blocker or next implementation step.

## Summary

Replaced history-query recovery wrapper with noon-on-change using NOON_PREVIOUS_STATUS: one notification per outage and one recovery, with sibling notifier resolution. Updated examples/docs and added isolated binary regression. All acceptance checks and full CI passed; merged 1d75460 to main and cleaned owned worktree.
