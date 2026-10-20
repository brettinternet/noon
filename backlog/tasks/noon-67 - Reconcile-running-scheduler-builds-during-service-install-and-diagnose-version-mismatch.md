---
id: NOON-67
title: >-
  Reconcile running scheduler builds during service install and diagnose version
  mismatch
status: To Do
assignee: []
created_date: '2026-10-20 22:58'
labels: []
dependencies:
  - NOON-66
documentation:
  - docs/service.md
modified_files:
  - cmd/noon/**
  - internal/cli/**
  - internal/app/**
  - internal/service/**
  - internal/scheduler/**
  - internal/store/**
  - docs/service.md
  - docs/cli.md
  - docs/design.md
  - README.md
priority: medium
type: enhancement
ordinal: 24200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: explicit noon service install restarts an outdated running scheduler even when the service unit is unchanged, and doctor explains build mismatches directly.

Context: upgrading the binary behind a stable executable path can leave an older scheduler running indefinitely. The user observed doctor catch this indirectly as scheduler_config failure. NOON-66 supplies explicit restart and upgrade guidance; this follow-up adds running-build reconciliation rather than requiring users to notice the stale process. Existing scheduler records already contain Version but not commit identity.

Scope: expose the running scheduler version and commit through scheduler/service status and compare them with the invoking binary. During explicit service install, changed units retain their update/restart behavior; unchanged units restart when a live service-owned scheduler has a known differing version or commit; matching builds remain undisturbed. Missing or unknown build metadata must be reported honestly with explicit restart guidance rather than guessed. Preserve normal first installation and stopped-service startup behavior. Use established scheduler liveness/identity checks so stale records or an unrelated foreground scheduler cannot drive a service restart. Report scheduler_version_mismatch directly in doctor with both builds and actionable guidance; retain genuine scheduler_config diagnostics independently. Status, doctor, and install --print remain read-only. Document that upgrade-triggered restarts interrupt active jobs.

Non-goals: background automatic restarts, config-edit-triggered restarts, graceful draining, binary download/update, arbitrary executable hashing, or detecting rebuilt binaries whose version and commit are identical. Unknown development identifiers are not proof of a matching known build. Preserve historical state compatibility if metadata persistence changes.

Dependencies: NOON-66 must land first, including resolution of its recorded service-lifecycle decision wording. This task intentionally extends its unchanged-install behavior only for a positively identified running-build mismatch.

Modified-file contract: listed paths plus this task record; justify expansions in Implementation Notes before editing. Implement before extending tests. Critical regressions are a same-path upgrade leaving the old scheduler alive, unnecessary restarts interrupting jobs, stale metadata triggering a restart, unreadable old state, and doctor obscuring a build mismatch. Use isolated state and fake managers only; never operate real services during autonomous verification.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 exits 0. Public-interface regression checks cover unchanged-unit install restarting on known version or commit mismatch for launchd and systemd, matching builds not restarting, and changed-unit updates restarting once rather than twice; manager failures propagate.
- [ ] #2 task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 exits 0. Checks cover absent/unknown build metadata yielding explicit restart guidance without a mismatch-triggered restart, stale or unrelated scheduler records not authorizing a restart, and preserved first-install/stopped-service behavior. Old state remains readable; metadata migrations, if needed, preserve history.
- [ ] #3 task cli:test PKG="./internal/app ./internal/cli" -- -count=1 exits 0. Human and JSON service/scheduler status expose running build identity; doctor reports scheduler_version_mismatch independently of scheduler_config with actionable guidance. Status, doctor, and install --print never invoke mutating manager operations or rewrite state.
- [ ] #4 task cli:build && git diff --check exits 0. Review docs/service.md and command help against tested behavior: install reconciles units and known running builds, unchanged matching installs remain idempotent, unknown builds need explicit restart, and restarts interrupt active jobs. No promise of automatic background upgrades or graceful draining.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
