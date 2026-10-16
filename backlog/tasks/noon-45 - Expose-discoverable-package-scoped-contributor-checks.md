---
id: NOON-45
title: Expose discoverable package-scoped contributor checks
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-16 23:23'
labels:
  - cli-ux
  - devex
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
- [ ] #1 `task` and `task --list` exit 0 and visibly include cli:test and cli:check with useful descriptions; setup-only internal tasks need not be advertised. Verify directly rather than adding tests for static task configuration.
- [ ] #2 `task --dry test` still selects ./... through mise. The documented package-selector invocation with `-- -run TestLoad -count=1 -v` expands to only ./internal/config, and executing it exits 0 with the matching tests actually run (not merely cached or no-tests-to-run). Record the exact documented command and output.
- [ ] #3 `task test`, `task cli:check`, and `task check:staged` exit 0. Full test and CI targets still cover every package, flags are forwarded as before, and docs/development.md distinguishes full checks from package-scoped iteration without introducing services or runtime dependencies.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
