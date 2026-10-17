---
id: NOON-37
title: Publish Noon releases and distribute through Homebrew
status: In Progress
assignee: []
created_date: '2026-10-16 22:28'
updated_date: '2026-10-17 00:19'
labels: []
dependencies: []
priority: medium
ordinal: 4400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Noon currently requires a source checkout to install, release publication is not wired up, and ../homebrew-tap contains only Hum and Worklease. Users need versioned binaries installable with brew install brettinternet/tap/noon, following the existing tap conventions.

Scope: versioned GitHub release archives and SHA-256 checksums for macOS/Linux on arm64/amd64, embedded version/commit/build-time metadata, a Noon formula in the sibling tap, a repeatable formula-update procedure, and installation/release documentation. Publication must be explicitly triggered; local validation must not publish anything.

Non-goals: Homebrew core submission, bottles, Windows packaging, automatic service installation/startup, scheduler behavior changes, or unrelated packaging refactors.

Modified-file contract: Noon .github/workflows/ release workflow, release configuration if needed, Taskfile.dist.yaml, .taskfiles/, scripts/ release packaging/verification scripts, README.md, docs/development.md; sibling ../homebrew-tap/Formula/noon.rb and ../homebrew-tap/README.md. Explain any necessary expansion in task notes before editing.

Dependencies: no outstanding backlog prerequisite; release packaging/publication is included here and must precede a usable tap formula. Actual publication and cross-repository pushes require explicit authorization and suitable GitHub permissions; record missing permission as a blocker rather than claiming shipment. Read sibling repository instructions before editing it.

Acceptance commands below include new Task targets to implement, not claims of existing behavior. Run Homebrew install checks in a disposable environment so existing installations and services remain untouched.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 task release:check exits 0 without publishing or updating the tap: it builds all four CGO_ENABLED=0 target archives, verifies their contents and SHA-256 checksums, and checks native binary version/commit/build-time metadata against the supplied release inputs. Document its inputs and output paths.
- [ ] #2 The explicit release workflow publishes versioned archives and checksums matching the locally validated format. Record an authorized release tag and verify its assets with gh release view TAG --repo brettinternet/noon and gh release download TAG --repo brettinternet/noon --dir DEST; downloaded checksums validate. Never publish merely to satisfy a local check without authorization.
- [ ] #3 In a disposable Homebrew environment, brew install brettinternet/tap/noon and brew test brettinternet/tap/noon exit 0; noon --version matches the release. Formula URLs and checksums cover macOS/Linux arm64/amd64 using the existing tap platform pattern. Record platform validation evidence and any unavailable runners explicitly.
- [x] #4 task release:smoke exits 0 and verifies the native release binary in isolated HOME/config/state directories: installation and no-argument inspection do not install/start a service, execute jobs, or open network listeners. Exercise the same safety contract for the installed Homebrew binary in the disposable environment.
- [ ] #5 Noon and tap documentation describe installation, upgrade, supported platforms, explicit service setup, and the repeatable release/formula-update procedure without claiming unpublished functionality. task ci and task check:staged exit 0; record corresponding tap checks required by its repository instructions.
- [ ] #6 Release CI publishes macOS/Linux arm64/x64 archives with unambiguous mise-compatible asset names and archive contents, plus SHA-256 checksums. Publication is gated on successful CI for the exact tagged commit; validation runs do not publish, and reruns never silently replace released assets. task release:check validates the four-platform asset layout locally without credentials or publication.
- [ ] #7 After an authorized stable release, mise exec github:brettinternet/noon@latest -- noon --version and mise exec github:brettinternet/noon@VERSION -- noon --version succeed with fresh isolated mise cache/data/state directories and no custom asset matching or tool options. CI smoke-checks automatic installation on all four supported OS/architecture combinations and asserts the expected version. Document the plain mise install/use command and record per-platform evidence; do not publish a release without authorization.
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
Release CI requirement: use ../hum/.github/workflows/release.yaml or ../worklease/.github/workflows/release.yml as references, adapting only the relevant release pieces. GitHub Releases must be directly consumable by the mise GitHub backend with automatic platform/architecture asset selection; no custom asset matching, asset_pattern, platform overrides, plugin, or registry entry should be necessary. Preserve the existing Homebrew scope. Reuse existing release:check and release:smoke acceptance targets rather than introducing a parallel packaging path.

Local implementation is in .worktrees/feat-noon-release (feat/noon-release); Worklease claim 0a394e8931de1d11c1b63b71da5fa530 owned by loop 1dfbc178-0ec3-4d89-a9cf-fa8665f76254. Scope expansion: docs/design.md records the archive/distribution contract required by Definition of Done #5. Executor edited this before recording the justification; parent records that process deviation explicitly. No application code or dependencies changed. GitHub inspection: no published releases and no configured environments (gh release list; gh api repos/brettinternet/noon/environments total_count=0). Publication remains unauthorized. Independent review in progress.

AC#1: task release:check VERSION=1.2.3 COMMIT=054c650be8c0c0b079497182eafd2f2dc3f5ea9f BUILD_TIME=2026-10-17T00:17:00Z -> exit 0; all four CGO-disabled archive layouts and SHA-256 checksums verified, native metadata matched exactly. Outputs documented in docs/development.md.
AC#4: task release:smoke BINARY=dist/release/v1.2.3/native-noon VERSION=1.2.3 -> exit 0. Disposable macOS arm64 Homebrew installed/tested the generated formula using local archive URLs, then task release:smoke BINARY=/Users/brett/dev/me/bump/.worktrees/noon-homebrew-validation/bin/noon VERSION=1.2.3 -> exit 0. No real installation or service touched. Initial temporary-directory Homebrew prefix hit its sandbox write-policy check; a disposable prefix under .worktrees worked without disabling sandboxing.
Review: one independent pass found Homebrew single-directory stripping and moved-tag publication defects; both fixed. Local brew execution additionally exposed inferred version 64, fixed with explicit formula version. An isolated Git fixture exercised the actual publication step: unchanged tag allowed, moved tag refused before release creation (gh API transport stubbed to fixture Git). actionlint, shellcheck and git diff --check passed. LSP diagnostics unknown; CLI validators passed.
Delivery: Noon implementation 054c650 and sibling tap documentation 28241a2 committed and fast-forward merged to their main branches. task check:staged passed with actual source files staged; task ci passed on 054c650. No tests removed, skipped, or weakened. Tap README documents pending distribution; no fabricated release formula committed.
User explicitly authorized pushing Noon main including existing unpushed commits, configuring the release environment, publishing v0.1.0 after CI, and pushing the tested tap formula. Next: publish and verify release assets, four native mise installs, then generate real-checksum tap formula and finish online brew checks.
<!-- SECTION:NOTES:END -->
