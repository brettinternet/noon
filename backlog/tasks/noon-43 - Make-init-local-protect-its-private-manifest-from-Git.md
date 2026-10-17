---
id: NOON-43
title: Make init local protect its private manifest from Git
status: Done
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-17 02:36'
labels:
  - cli-ux
  - devex
  - reviewed
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
- [x] #1 `task cli:test -- -run '^TestInitPreservesExistingConfig$' -count=1 -v` exits 0 after extending this existing regression for local init: preserve existing manifest and ignore contents, idempotent protection, selected project/nested checkout behavior, and an actionable ignore-write failure without a false protected-success claim.
- [x] #2 `task cli:build` exits 0. In a fresh temporary Git repository with isolated HOME, `bin/noon -C DIR init --local` followed by `git -C DIR check-ignore .noon.yaml` exits 0; `git -C DIR status --short --untracked-files=all` does not offer .noon.yaml for addition. A second init leaves manifest contents and effective ignore rules unchanged; record private file mode.
- [x] #3 Repeat local init outside Git and shared init in separate temporary fixtures: neither initializes Git or overwrites existing files; shared init does not unexpectedly alter ignore rules. `task test` and `task smoke` exit 0, with docs/help matching actual behavior.
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
AC#1: task cli:test -- -run '^TestInitPreservesExistingConfig$' -count=1 -v -> exit 0; extended existing regression covers byte preservation, idempotence, selected nested checkout, effective ignores, nonregular ignore paths and tracked manifests without false success.
AC#2: task cli:build -> exit 0. Isolated env -i HOME/XDG fixtures: bin/noon -C DIR init --local; git -C DIR check-ignore .noon.yaml -> exit 0; git status --short --untracked-files=all lists only .gitignore. Second init plus cmp confirms unchanged manifest/ignore bytes; stat reports mode 600. Fixture: /var/folders/jp/1mwx72h172955139pth4h8800000gn/T/tmp.3kCsz94jG0.
AC#3: isolated repeated local init outside Git and shared init -> exit 0, cmp unchanged, no .git or .gitignore created. Shared init regression also preserves existing ignore rules. task test and task smoke -> exit 0. Configuration documentation now describes implemented behavior; help already promised this behavior. No design contract changed.
Final implementation commit b134d5b56097b3a48ee900eb398d0102ce70044e: task ci passed (security, vet, Staticcheck, tests, race, smoke). Initial Staticcheck error-string finding fixed before final commit. LSP diagnostics clean. One general review of changed behavior found no additional item-scoped defects. Only allowed implementation files changed; no tests deleted, skipped or weakened. task check:staged passed. Fast-forward merged to main; unrelated NOON-42 changes preserved. No push. No remaining implementation blocker or resumable step; owned worktree cleanup follows.

Cleanup complete: Worktrunk removed the verified session-owned fix/noon-43-local-ignore checkout and branch; Herdr workspace wFA disappeared. Other pre-existing worktrees were untouched.

Review: NOON-46 superseded the .gitignore approach; init --local now writes .git/noon.yaml (0600) and fails outside Git without writing files, which meets the outcome. No protectLocalConfig remnants remain. Fixed a stale .noon.yaml test failure message. Covered by TestInitPreservesExistingConfig.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Local init now ensures effective Git ignore protection, preserves existing files, rejects unsafe ignore paths and tracked manifests, and reports outside-Git behavior accurately. Implemented and locally merged as b134d5b; all acceptance checks and CI pass.
<!-- SECTION:FINAL_SUMMARY:END -->
