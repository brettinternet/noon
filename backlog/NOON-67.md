---
id: NOON-67
title: Reconcile running scheduler builds during service install and diagnose version mismatch
status: done
type: enhancement
priority: medium
blocked_by: [NOON-66]
rank: ac47711dc47711d84
refs: [docs/service.md]
files: [cmd/noon/**, internal/cli/**, internal/app/**, internal/service/**, internal/scheduler/**, internal/store/**, docs/service.md, docs/cli.md, docs/design.md, README.md]
created: 2026-10-20T22:58:00Z
updated: 2026-10-20T23:41:00Z
---

Outcome: explicit noon service install restarts an outdated running scheduler even when the service unit is unchanged, and doctor explains build mismatches directly.

Context: upgrading the binary behind a stable executable path can leave an older scheduler running indefinitely. The user observed doctor catch this indirectly as scheduler_config failure. NOON-66 supplies explicit restart and upgrade guidance; this follow-up adds running-build reconciliation rather than requiring users to notice the stale process. Existing scheduler records already contain Version but not commit identity.

Scope: expose the running scheduler version and commit through scheduler/service status and compare them with the invoking binary. During explicit service install, changed units retain their update/restart behavior; unchanged units restart when a live service-owned scheduler has a known differing version or commit; matching builds remain undisturbed. Missing or unknown build metadata must be reported honestly with explicit restart guidance rather than guessed. Preserve normal first installation and stopped-service startup behavior. Use established scheduler liveness/identity checks so stale records or an unrelated foreground scheduler cannot drive a service restart. Report scheduler_version_mismatch directly in doctor with both builds and actionable guidance; retain genuine scheduler_config diagnostics independently. Status, doctor, and install --print remain read-only. Document that upgrade-triggered restarts interrupt active jobs.

Non-goals: background automatic restarts, config-edit-triggered restarts, graceful draining, binary download/update, arbitrary executable hashing, or detecting rebuilt binaries whose version and commit are identical. Unknown development identifiers are not proof of a matching known build. Preserve historical state compatibility if metadata persistence changes.

Dependencies: NOON-66 must land first, including resolution of its recorded service-lifecycle decision wording. This task intentionally extends its unchanged-install behavior only for a positively identified running-build mismatch.

Modified-file contract: listed paths plus this task record; justify expansions in Implementation Notes before editing. Implement before extending tests. Critical regressions are a same-path upgrade leaving the old scheduler alive, unnecessary restarts interrupting jobs, stale metadata triggering a restart, unreadable old state, and doctor obscuring a build mismatch. Use isolated state and fake managers only; never operate real services during autonomous verification.

## Acceptance criteria

- [x] task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 exits 0. Public-interface regression checks cover unchanged-unit install restarting on known version or commit mismatch for launchd and systemd, matching builds not restarting, and changed-unit updates restarting once rather than twice; manager failures propagate.
- [x] task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 exits 0. Checks cover absent/unknown build metadata yielding explicit restart guidance without a mismatch-triggered restart, stale or unrelated scheduler records not authorizing a restart, and preserved first-install/stopped-service behavior. Old state remains readable; metadata migrations, if needed, preserve history.
- [x] task cli:test PKG="./internal/app ./internal/cli" -- -count=1 exits 0. Human and JSON service/scheduler status expose running build identity; doctor reports scheduler_version_mismatch independently of scheduler_config with actionable guidance. Status, doctor, and install --print never invoke mutating manager operations or rewrite state.
- [x] task cli:build && git diff --check exits 0. Review docs/service.md and command help against tested behavior: install reconciles units and known running builds, unchanged matching installs remain idempotent, unknown builds need explicit restart, and restarts interrupt active jobs. No promise of automatic background upgrades or graceful draining.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Implementation: scheduler commit identity uses schema migration 12 with read-only fallback for older state. Unchanged install reconciles only known version/commit differences with fresh heartbeat, verified process-start identity, and service-manager PID ownership; changed units update once. Doctor reports build mismatch independently of config errors. No new dependencies or modified-file contract expansion; no tests deleted, skipped, or weakened. Decision-1 remains unchanged: only explicit service operations restart; no automatic background upgrade behavior.
AC#1: task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 -> exit 0. Fake-manager regression covers version/commit mismatch, known match, changed-unit single restart and propagated failure. Native-platform test branches cover launchd on macOS and systemd on Linux (local execution macOS).
AC#2: same focused command -> exit 0. Unknown/missing commit, stale heartbeat, reused PID, unrelated foreground scheduler, first install and stopped startup are covered; legacy read-only state and migration preserve history and commit identity.
AC#3: task cli:test PKG="./internal/app ./internal/cli" -- -count=1 -> exit 0. Human/JSON scheduler and service build identity, independent scheduler_version_mismatch and scheduler_config diagnostics, unchanged database bytes, existing read-only service/print checks pass. Final broader focused rerun also passed.
AC#4: task cli:build && git diff --check -> exit 0. README, service/CLI/design docs and help reviewed against implemented reconciliation and interruption semantics.
Review: one independent reviewer pass found no validated defects; noted stopped-startup coverage gap was filled and focused checks rerun. Full task ci passed before that final regression addition; final-commit CI and delivery pending.

AC#2: task cli:test PKG="./internal/service ./internal/app ./internal/cli ./internal/scheduler ./internal/store" -- -count=1 -> exit 0 on final implementation.
Delivery: implementation commit ae03873a7b76886dd682deeafd99c992138b4928 passed task ci (security, formatting, vet, Staticcheck, full tests, race, smoke), then fast-forward merged into main. task check:staged passed before commit. All acceptance and definition-of-done requirements met. No blockers or remaining implementation steps.
Cleanup: verified session creation receipt and clean worktree, then Worktrunk removed noon-67 checkout and branch. Herdr workspace wJE was verified absent afterward. Unrelated fix-suggested-action checkout/workspace belongs to other work and was preserved. Worklease claim will be released after the final bookkeeping commit and verification.

Release review: clarified unchanged-install warning when no live scheduler existed beforehand (install may have just started the current build); service status prints Build only with a scheduler record; tidied imports; refreshed CHANGELOG. task ci passed.

## Summary

Implemented build-aware explicit service installation and direct doctor mismatch diagnostics. Running version/commit are exposed in human/JSON status. Safe restart guards reject stale, reused-PID, and foreground records; unknown metadata prompts explicit restart. Historical databases remain readable. Merged ae03873 into main; focused checks, full CI, and independent review passed. Task worktree/branch/workspace removed; no push.
