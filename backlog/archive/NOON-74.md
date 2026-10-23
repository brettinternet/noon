---
id: NOON-74
title: Integrate worktree setup and teardown through Worktrunk hooks
status: draft
labels: [integration, worktrees]
rank: ad653594d65359490
created: 2026-10-16T19:20:00Z
updated: 2026-10-17T17:37:00Z
---

Draft, not ready work. Promote when a concrete worktree setup or teardown job needs a verified adapter, then supply outcome, scope, non-goals, modified-file contract, dependencies, and executable acceptance commands.

Problem: a new worktree often needs project preparation, while removal may need owned-resource cleanup. Worktrunk already owns these lifecycle events; Noon should offer job execution and history without taking over worktree management.

Candidate scope: documented, verified Worktrunk hook recipes that invoke existing named Noon jobs with structured worktree/branch/event context. Distinguish synchronous prerequisites from optional detached work. Use foreground execution when setup must finish before work starts or teardown must finish before the directory disappears. Detached setup requires explicit checkout enablement and approval; never automatically enable a new worktree.

Safety: teardown must operate only on resources demonstrably owned by that checkout. A directory-exit event is not a worktree-removal event and must not stop shared services. Hook failures must have documented blocking/nonblocking behavior. Preserve existing project/user hooks and require explicit configuration.

Non-goals: Noon creating/deleting worktrees or branches, automatic service management, shared-resource reference counting, replacing Worktrunk, or installing hooks without consent. Distinct from DRAFT-4, which considers execution isolation for agent jobs.

Promotion questions: which current Worktrunk events and ordering satisfy preparation/removal; synchronous authorization model; metadata contract; failed setup recovery; proof of resource ownership and teardown ordering.

Dependencies/context: existing foreground runs, NOON-7, and NOON-22 provide the integration primitives; no dependency on NOON-30. Follow decision-4 and inspect .config/wt.toml before proposing changes.

Acceptance direction when promoted: disposable owned worktrees demonstrate that blocking setup completes first, required teardown completes before removal, failures are visible, and unrelated resources/hooks remain untouched. State exact commands and cleanup ownership rules.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Resolved as an example: examples/integrations/worktrunk runs foreground noon run setup/teardown from Worktrunk pre-start/pre-remove hooks with a per-worktree Compose database. Verified with Worktrunk 0.80.0: failing pre-start leaves the worktree and exits nonzero; failing pre-remove blocks removal.
