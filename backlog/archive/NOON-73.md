---
id: NOON-73
title: Trigger jobs from debounced file changes
status: draft
labels: [integration, watching]
rank: ad3594d653594d60e
created: 2026-10-16T19:20:00Z
updated: 2026-10-17T17:37:00Z
---

Draft, not ready work. Promote only after selecting a concrete watch-driven workflow and rewriting this record to the ready-task standard: outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: project-entry and Git events miss edits made between commits. Developers need targeted tests, linting, or generation after relevant files change without starting a run per filesystem notification.

Candidate scope: explicitly configured file-change adapters that queue existing approved jobs with structured changed-path context. Reuse authorization, pause, cooldown, coalescing, history, cancellation, and recursion rules. Support include/exclude rules, trailing-edge debounce, atomic-save/rename bursts, bounded payloads, and a pending change while a job is already running. Cooldown alone is not debounce and must not silently lose the final relevant edit.

Safety: no watcher starts from inspection commands. Ignore .git, Noon state/logs, and declared generated outputs by default where appropriate; prevent output-trigger loops. Recheck approval at dispatch. Bound watch resources and handle deleted directories or watcher overflow explicitly.

Non-goals: editor integrations, arbitrary file-content templating into commands, a plugin system, automatic repository trust, or a second scheduler.

Promotion questions: compose an existing watcher CLI versus owning native watchers; explicit foreground watch command versus an opt-in serve capability; recursive/symlink semantics; output-loop prevention; changes during a run; restart/overflow behavior and whether events are durable.

Dependencies/context: NOON-7 already supplies queued payloads and safeguards. NOON-30 is not a prerequisite. Follow decision-4 and docs/triggers.md.

Acceptance direction when promoted: real isolated file writes/renames produce one debounced run with correct paths, excluded/generated changes do not loop, an edit during a run is not lost, and unapproved projects never execute. Include bounded-resource/shutdown checks and locally executable commands.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Resolved as an example, not a feature: examples/local-dev/file-watch composes watchexec with noon run (foreground, --on-busy-update queue) or noon run --detach (joins the waiting run). The design keeps no fsnotify in Noon. Per-event payload delivery is NOON-48.
