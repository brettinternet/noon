---
id: NOON-45
title: Expose discoverable package-scoped contributor checks
status: Done
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-17 02:37'
labels:
  - cli-ux
  - devex
  - reviewed
dependencies: []
modified_files:
  - Taskfile.dist.yaml
  - .taskfiles/cli.yaml
  - docs/development.md
  - README.md
priority: medium
type: enhancement
ordinal: 12400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: contributors can discover the documented checks with bare task and run a genuinely focused package/test check without bypassing the pinned toolchain.

Evidence: task/ task --list omit cli:test and cli:check because their definitions have no desc, although docs/development.md tells contributors to use them. task --dry cli:test -- -run TestLoad expands to go test ./... -run TestLoad, so a focused parser test still loads every package; the docs do not give a package-scoped Task invocation.

Scope: expose user-facing CLI check/test tasks with descriptions and provide a documented PKG selection input on the existing test task, retaining ./... as the no-flag default and forwarding Go test flags unchanged. Document copy-paste examples for one package, one test and uncached verification. Keep the top-level task/test/ci defaults and pinned mise execution intact; this is a narrow workflow improvement, not another task framework.

Non-goals: release/install work (NOON-37 already owns it), replacing Task/mise, adding a watch daemon, changing CI coverage, tool upgrades or speculative caching.
Modified-file contract: Taskfile.dist.yaml, .taskfiles/cli.yaml, docs/development.md, README.md only if the quick-start changes. Explain expansion.
Dependencies: none. Use PKG=./internal/config for package-scoped checks and retain ./... when PKG is unset.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task` and `task --list` exit 0 and visibly include cli:test and cli:check with useful descriptions; setup-only internal tasks need not be advertised. Verify directly rather than adding tests for static task configuration.
- [x] #2 `task --dry test` still selects ./... through mise. The documented package-selector invocation with `-- -run TestLoad -count=1 -v` expands to only ./internal/config, and executing it exits 0 with the matching tests actually run (not merely cached or no-tests-to-run). Record the exact documented command and output.
- [x] #3 `task test`, `task cli:check`, and `task check:staged` exit 0. Full test and CI targets still cover every package, flags are forwarded as before, and docs/development.md distinguishes full checks from package-scoped iteration without introducing services or runtime dependencies.
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
AC#1: task and task --list -> exit 0; both list cli:check (Check all Go formatting, vet, and Staticcheck) and cli:test (Run Go tests; PKG selects packages, defaults to ./...).
AC#2: task --dry test -> mise exec go -- go test ./...; task --dry cli:test PKG=./internal/config -- -run TestLoad -count=1 -v -> mise exec go -- go test ./internal/config -run TestLoad -count=1 -v. Exact documented execution task cli:test PKG=./internal/config -- -run TestLoad -count=1 -v -> exit 0, seven matching TestLoad tests ran, PASS, config 0.225s (not cached). Anchored single-test example also ran and passed.
AC#3: task test, task cli:check, task check:staged -> exit 0. Defaults and CI still use ./... through mise; CLI_ARGS unchanged. Documentation distinguishes full checks from focused iteration.
Delivery: c87c189 chore: expose package-scoped contributor checks; task ci passed on this final implementation commit (security, formatting/vet/Staticcheck, all tests, race, isolated smoke). Fast-forward merged into main. Initial full test saw scheduler TestServeQueued cancellation EOF; ten uncached focused repetitions, subsequent full suite and race suite passed without test changes.
Review: one direct diff/behavior review corrected the single-test documentation example to an existing test. No remaining scoped defects. YAML LSP diagnostics unavailable; actual Task parsing/execution passed. Changes limited to .taskfiles/cli.yaml and docs/development.md; this provider record is required workflow bookkeeping. No tests added, deleted, skipped, or weakened; no design contract changes. No blocker. Next step: commit completion record and remove verified owned task worktree; preserve unrelated primary CLI edits and existing feat-tui-design checkout.

Post-done review: ambient PKG env var silently narrowed task test/task ci to one package (PKG=./internal/cli task --dry ci -> go test ./internal/cli). Fixed by pinning PKG=./... on the top-level test call in Taskfile.dist.yaml; docs say full targets always run all packages. Verified: PKG=./internal/cli task --dry ci/test -> ./...; task cli:test PKG=./internal/config -- -run TestLoad -count=1 -> ok; task test -> exit 0.
<!-- SECTION:NOTES:END -->
