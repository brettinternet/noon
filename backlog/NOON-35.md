---
id: NOON-35
title: Smoke-test event-driven example scripts against the built binary
status: done
type: task
priority: medium
labels: [integration, examples, reviewed]
rank: a653594d653594d44
refs: [examples/README.md]
files: [integration/examples_test.go, mise.toml]
created: 2026-10-16T22:10:00Z
updated: 2026-10-17T02:00:00Z
---

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

## Acceptance criteria

- [x] `task cli:test -- -run '^TestExampleScripts$' -count=1 -v` exits 0 with PASS, not SKIP. Both subtests execute shipped script bodies against the built binary with no external agent or network.
- [x] The same command verifies two offline failures coalesce to one triage run, both run IDs and distinct job-log evidence reach reports, hook-only output is excluded, and rerunning triage prints no undiagnosed failures with no new agent invocation or changed report.
- [x] The same command verifies succeeded build/test/package ordering, a real archive/checksum, the test run JSON in package's event envelope, and a fixture-only fourth-stage hook refused with code recursion without creating another run or changing package success.
- [x] `task cli:test -- -run '^TestExampleScripts$' -count=3 -v` passes without leaked schedulers or fixed sleeps. Record one deliberate local history/log contract mutation that makes the focused test fail, restore only that owned mutation, and rerun successfully; no mutation tooling is added.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Refinement: keep actual shipped triage/package scripts under test, override job.env (not only inherited env), provide real jq through existing setup, and test the recursion boundary in a fixture-only hook so packaging can still succeed. DRAFT-8's existing hook-composition validation is covered here; no new workflow engine is implied.

Implementation is limited to integration/examples_test.go; jq already pinned by NOON-32. For required AC#4 mutation experiment only, temporarily change internal/app/history.go RunsView JSON field from runs to entries, run focused regression expecting failure, then restore this exact owned change before delivery. No core change will be committed.

Mutation correction: RunsView tag does not control CLI rendering (CLI emits its own map), so that mutation passed and was restored. The actual experiment will temporarily rename runs to entries only in status-filtered CLI history output in internal/cli/history.go; unfiltered test observation remains intact, exercising the shipped triage consumer. Review found and fixed a consumed completion-channel cleanup hang and missing per-report rejection of the other job evidence.

AC#1: task cli:test -- -run '^TestExampleScripts$' -count=1 -v -> PASS both subtests, no skips, shipped scripts with fake external work only.
AC#2: same command -> PASS two failures/one offline due triage run, per-run reports and distinct evidence excluding hook chatter and other-job evidence; repeat makes no calls or report changes.
AC#3: same command -> PASS ordered successful stages, real archive listing and checksum verification, package event data.run identity and log, recursion refusal with no fourth run and package still successful.
AC#4: task cli:test -- -run '^TestExampleScripts$' -count=3 -v -> PASS all three repetitions. Renaming runs to entries for status-filtered history in internal/cli/history.go caused failure-triage to FAIL (triage status failed), while pipelines passed. Restored exact mutation; repeated and single focused commands PASS. No schedulers remain.
Delivery: fe48f97 test(examples): exercise triage and pipeline scripts; task check:staged PASS, LSP clean, task ci PASS on implementation commit (security, vet, staticcheck, all tests, race, smoke). Fast-forward merged into main. One independent review completed; both concrete test findings fixed before final checks. Only integration/examples_test.go and this record changed; no shipped defect or documentation contract change. No existing tests weakened/deleted/skipped.
Cleanup: verified session receipt and idle wEB:p1 shell; Worktrunk removed owned test-example-scripts worktree and branch, post-remove workspace closure verified. Unrelated feat/tui-design retained, not session-owned. No blockers or resumable work remains.

## Summary

Added offline integration coverage for shipped failure triage and pipeline scripts. All acceptance checks, mutation experiment, independent review fixes, and full CI passed. Implementation fe48f97 merged to main; owned worktree/branch/workspace cleaned up.
