---
id: NOON-43
title: Make init local protect its private manifest from Git
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
labels:
  - cli-ux
  - devex
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/manage.go
  - README.md
  - docs/cli.md
  - docs/configuration.md
priority: high
type: bug
ordinal: 10400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: init --local honors its advertised private-file default instead of leaving a sensitive manifest ready to accidentally commit.

Verified reproduction in a fresh temporary Git repository: noon init --local prints Created .../.noon.yaml and a generic ignore reminder; git status --short reports ?? .noon.yaml and git check-ignore .noon.yaml exits 1. The flag help promises Create private .noon.yaml instead; add it to .gitignore, but initialize in internal/cli/root.go never writes an ignore file.

Scope: implement the promised ignore protection for a local manifest within a Git checkout, preserving existing .gitignore contents and avoiding duplicate rules on repeated init. Resolve the selected project/worktree correctly, do not affect unrelated projects, preserve existing config bytes/permissions, and surface actionable failures without falsely claiming the file is protected. Respect existing effective ignore rules. Outside Git, initialize safely and state that ignore protection was not applicable; do not create a repository. Keep private file permissions at 0600.

Non-goals: untracking already tracked files, staging/committing anything, rewriting global Git excludes, changing shared init or add/import semantics.
Modified-file contract: internal/cli/root.go, internal/cli/root_test.go; internal/cli/manage.go only to reuse existing Git/ignore helpers; README.md, docs/cli.md, docs/configuration.md as needed. Explain expansion.
Dependencies: none. This fulfills the existing help contract, not a change to background approval policy.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestInitPreservesExistingConfig$' -count=1 -v` exits 0 after extending this existing regression for local init: preserve existing manifest and ignore contents, idempotent protection, selected project/nested checkout behavior, and an actionable ignore-write failure without a false protected-success claim.
- [ ] #2 `task cli:build` exits 0. In a fresh temporary Git repository with isolated HOME, `bin/noon -C DIR init --local` followed by `git -C DIR check-ignore .noon.yaml` exits 0; `git -C DIR status --short --untracked-files=all` does not offer .noon.yaml for addition. A second init leaves manifest contents and effective ignore rules unchanged; record private file mode.
- [ ] #3 Repeat local init outside Git and shared init in separate temporary fixtures: neither initializes Git or overwrites existing files; shared init does not unexpectedly alter ignore rules. `task test` and `task smoke` exit 0, with docs/help matching actual behavior.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
