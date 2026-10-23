---
id: NOON-75
title: Trigger refresh jobs on login wake and network restoration
status: draft
labels: [integration, service]
rank: af711dc47711dc426
created: 2026-10-16T19:20:00Z
updated: 2026-10-16T19:20:00Z
---

Draft, not ready work. Promote after selecting an actual refresh/sync use case and supported OS event sources; then add the ready-task outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: fixed schedules and project entry do not always refresh data promptly after login, sleep, or connectivity loss. OS event adapters could request existing jobs when work becomes useful again.

Candidate scope: explicitly installed macOS/Linux integrations for selected login, resume-from-sleep, and network-restored events. Prefer OS facilities or small command adapters that queue existing named jobs, preserving approvals, cooldowns, coalescing, event payloads, and run history. Existing scheduler missed-run recovery already handles elapsed schedules; avoid duplicating that work.

Safety: no automatic installation, listeners, services, trust, or polling loops from inspection. Network availability is only a signal, not proof that a remote endpoint is reachable. Bound event storms and clearly distinguish receiving an OS event, accepting a queued run, and successful execution.

Non-goals: a cross-platform system-monitoring daemon, network credential management, arbitrary retry orchestration, or replacing launchd/systemd supervision.

Promotion questions: exact event APIs on each OS; whether login is already satisfied by the current service; offline dispatch and retry needs; noisy network transitions; stale events after sleep; installation/removal ownership; system versus user session permissions.

Dependencies/context: NOON-7, NOON-11, and NOON-19 are completed foundations. No dependency on NOON-30. Follow decision-1, decision-4, docs/service.md, and the existing missed-run policy.

Acceptance direction when promoted: deterministic local event injection verifies metadata, deduplication/coalescing, approval rejection, scheduler-offline queuing, and clean removal. Add real OS smoke checks only for supported event types; no real external network dependency in automated tests.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
