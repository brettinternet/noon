---
id: NOON-35
title: Smoke-test event-driven example scripts against the built binary
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
updated_date: '2026-10-16 22:21'
labels:
  - integration
  - examples
dependencies: []
documentation:
  - examples/README.md
modified_files:
  - integration/examples_test.go
  - mise.toml
priority: medium
type: task
ordinal: 3350
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a small offline integration regression fails when Noon output/queue changes break shipped failure-triage or pipeline scripts, not just their YAML.

Why: TestExamplesValidate only checks config. triage.sh consumes history fields and bounded job-only logs; pipeline hooks forward stdin JSON into event-file data. Due-now requests coalesce into an already-due queued run, so triage must reconcile multiple failures rather than trust a single event. The regression catches lost diagnostics and broken downstream context.

Scope: extend integration/examples_test.go with TestExampleScripts subtests, reusing noonBinary, isolatedEnvironment, and scheduler helpers from integration/serve_test.go. Copy the shipped examples to disposable projects and execute their actual triage.sh, package.sh, and hook commands. Fake only external agents/build work/desktop notification; do not replace the script logic or jq with a mock.

Fixture requirements:
- Real jq, Git, tar, and the checksum tool used by package.sh must be available. Pin jq through existing mise setup if not already supplied; missing prerequisites fail clearly, not skip. Create a local Git commit and harmless bin artifact so the real packaging script runs.
- Isolate HOME/XDG/state and prepend the built Noon binary/fake tools to PATH. Replace hard-coded job.env where necessary, disable schedule fields, explicitly enable the fixture, and start only a short-lived owned scheduler. Poll outcomes with deadlines and always stop/wait for it.
- Start two failures while the scheduler is offline. Confirm one due queued triage run. After dispatch, verify one report for each failed run ID and that the fake agent received the distinct job log evidence, not hook-only markers. A second triage run must make no agent calls or report changes.
- For the pipeline, run build with the shipped hooks and real packaging script. Check succeeded build/test/package order and that package sees the test run inside the event envelope's data.run. Add a fixture-only package hook requesting a fourth job at depth 3; verify error code recursion, no fourth run, and unchanged package success. Do not rewrite package.sh simply to test the already-covered core recursion rule.
- Only fix shipped examples when the regression exposes a real defect; record the evidence and exact path in notes before expanding the test-file-only change.

Non-goals: real agents/network/GitHub, testing every example, changing core queue semantics, a generic example-testing framework, mutation-testing infrastructure.

Dependencies: none; these test current shipped contracts, not NOON-31 or DRAFT-9. NOON-32 may also add jq; reuse it if present.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestExampleScripts$' -count=1 -v` exits 0 with PASS, not SKIP. Both subtests execute shipped script bodies against the built binary with no external agent or network.
- [ ] #2 The same command verifies two offline failures coalesce to one triage run, both run IDs and distinct job-log evidence reach reports, hook-only output is excluded, and rerunning triage prints no undiagnosed failures with no new agent invocation or changed report.
- [ ] #3 The same command verifies succeeded build/test/package ordering, a real archive/checksum, the test run JSON in package's event envelope, and a fixture-only fourth-stage hook refused with code recursion without creating another run or changing package success.
- [ ] #4 `task cli:test -- -run '^TestExampleScripts$' -count=3 -v` passes without leaked schedulers or fixed sleeps. Record one deliberate local history/log contract mutation that makes the focused test fail, restore only that owned mutation, and rerun successfully; no mutation tooling is added.
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
Refinement: keep actual shipped triage/package scripts under test, override job.env (not only inherited env), provide real jq through existing setup, and test the recursion boundary in a fixture-only hook so packaging can still succeed. DRAFT-8's existing hook-composition validation is covered here; no new workflow engine is implied.
<!-- SECTION:NOTES:END -->
