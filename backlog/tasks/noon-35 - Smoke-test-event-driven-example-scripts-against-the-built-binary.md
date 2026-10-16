---
id: NOON-35
title: Smoke-test event-driven example scripts against the built binary
status: To Do
assignee: []
created_date: '2026-10-16 22:10'
labels:
  - integration
  - examples
dependencies: []
documentation:
  - examples/README.md
modified_files:
  - integration/examples_test.go
priority: low
type: task
ordinal: 3600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the integration suite fails when a Noon CLI or JSON change breaks the shipped example scripts that parse Noon output, not only when their YAML stops validating.

Why: TestExamplesValidate (integration/examples_test.go) only runs `config check`. The example scripts depend on Noon output contracts: `noon history --json` fields (`id`, `job`, `status`, `finished_at`), `noon logs` flags (`--section`, `--tail`, `--limit-bytes`), hook stdin JSON (`run.id`, `run.job`, `run.status`), and request merging plus the recursion limit. A rename or behavior change would make examples/ai/failure-triage diagnose nothing and examples/local-dev/pipelines stop chaining, while the examples test stays green. The reconcile pattern in failure-triage exists because a request made while a run of the same job is waiting joins that run (internal/store/queue.go); that behavior is what the example must keep handling.

Scope: one integration test that copies examples/ai/failure-triage and examples/local-dev/pipelines into temporary projects, substitutes fake job commands and a fake agent (no network, no real agent CLIs), runs the built binary with an isolated environment and a short-lived scheduler, and asserts the observable outcomes in the criteria. Fix an example only if the test exposes a real bug, and justify the extra path in Implementation Notes.

Non-goals: covering pr-review or anything needing GitHub or network, testing every example, shell linting, real agents.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run "^TestExampleScripts$" -count=1 -v` exits 0 and prints `--- PASS: TestExampleScripts`.
- [ ] #2 The test asserts that two failing jobs, run while no scheduler is active, leave one queued triage run; that after the scheduler runs, that single triage run wrote one report per failure; and that a second triage run writes nothing and prints "no undiagnosed failures".
- [ ] #3 The test asserts that `noon run build` records succeeded build, test, and package runs in that order, that package received the test run JSON through `NOON_EVENT_FILE`, and that a request from the package stage is refused with the recursion guard.
- [ ] #4 Implementation Notes record a deliberate local mutation (for example changing `finished_at` in the triage jq filter) that makes the test fail, then revert it.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
