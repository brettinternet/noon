---
id: NOON-30
title: Support project-entry shell hooks and shell completion
status: Done
assignee: []
created_date: '2026-10-16 19:19'
updated_date: '2026-10-16 20:54'
labels:
  - shell
  - integration
  - reviewed
dependencies:
  - NOON-7
  - NOON-18
documentation:
  - docs/triggers.md
  - docs/configuration.md
  - docs/development.md
modified_files:
  - internal/cli/**
  - internal/app/**
  - internal/config/**
  - integration/**
  - Taskfile.dist.yaml
  - README.md
  - docs/shell.md
  - docs/triggers.md
  - docs/design.md
priority: medium
type: feature
ordinal: 2200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: entering an opted-in project from an interactive shell can queue a named Noon job without waiting for execution, and Bash, Zsh, and Fish users can discover commands, flags, and job names through completion.

Why: project-entry work currently requires hand-written shell glue, and the CLI enables urfave completion internally without a documented, verified end-to-end shell setup. Ship one explicit shell setup experience while keeping completion usable without enabling automation.

Read first: docs/triggers.md, docs/configuration.md (project selection and approvals), docs/design.md#integrations, decision-2, and decision-4. Reuse the existing urfave/cli v3 completion support and shared queued-run operations; do not add a plugin framework or a second queue.

Scope:
- Provide `noon shell init bash|zsh|fish [--on-enter JOB]`, emitting sourceable shell code on stdout. Without --on-enter, install completion only in the calling shell. With it, also register project-entry detection for that named job. A project opts into this integration by defining that job and explicitly enabling the checkout through the existing approval flow. Do not add a new job schema or magic default job name.
- Generating code and bare shell inspection do not execute jobs, create state, modify startup files, install services, or open listeners. Document manual setup and removal. Unsupported shells and invalid arguments are usage errors.
- Use shell-native directory events where supported; Bash may detect PWD changes before the next prompt. Preserve existing prompt/directory hooks and the previous command exit status. Re-sourcing is idempotent. Initialization records the current location without triggering a job.
- Trigger on a transition into a different canonical Noon project, using existing nearest-marker discovery, Git boundaries, symlink resolution, and worktree semantics. Moving among subdirectories of one project or visiting the same project through a symlink is not another entry. Leaving then re-entering qualifies; leaving alone runs nothing. Separate shells maintain separate entry state.
- Queue the selected job through the existing authorization, pause/enabled, cooldown, coalescing, and recursion rules. Missing jobs, unenabled projects, and unapproved or paused jobs produce no unattended execution. The actual entered project must be the target, never an unrelated inherited NOON_PROJECT. Reuse the enabled checkout configuration selection rather than silently queueing under a different configuration.
- Store structured event data identifying project entry, shell, previous/current directory, and canonical project, inside the existing CLI event envelope. Paths are data, never shell source. Do not use a permanent project idempotency key that suppresses later valid visits.
- Return to the prompt without waiting for job execution. Ordinary no-op transitions and successful queueing are quiet; failures must not change cd success or break the prompt. Document diagnostics and the scheduler-offline behavior: accepted work waits for an explicitly started scheduler. Never automatically enable, approve, or start anything.
- Completion covers commands, aliases, flags, and configured job names for existing-job selectors (run, pause, resume, remove, history, logs, cancel), respecting project/config selection and argument position. Do not suggest existing job names as a new add-name or as a flag value. Config errors may suppress job candidates but must not prevent static command/flag completion.
- Loading or invoking completion is strictly read-only: no job/hook execution, state initialization/migration, approval changes, service actions, or network calls. Quote shell data safely, including paths containing spaces or shell metacharacters. Keep shell glue in the shipped binary; require no Python, Node, jq, or external completion framework.

Non-goals: filesystem watchers, project-exit teardown, shell environment activation, service ownership/reference counting, arbitrary shell-command interception, changing Git hooks, automatic startup-file edits, run-ID/history-only job completion, or a generic event-routing language. mise/direnv remain responsible for modifying the parent shell environment.

Dependencies: NOON-7 and NOON-18 are completed prerequisites. NOON-29 is unrelated and must not be changed or blocked by this task.

Critical regressions to cover after implementation:
- A subdirectory move, symlink alias, or repeated shell initialization must not enqueue duplicate work; crossing a real nested-project/worktree boundary and leaving/re-entering must target the correct project.
- An untrusted/changed shared job, paused job, or inherited project override must not cause unauthorized or misdirected execution. Cooldown, coalescing, offline queuing, and recursion protection remain effective.
- Shell metacharacters in paths must survive as event data without executing commands; existing hooks and exit status survive initialization and transitions.
- Completion must never run a selected command or create state, including completion of mutating commands; project-specific candidates must not leak across project/config selection.

Modified-file contract: the modified_files list plus this task record. Justify expansion in Implementation Notes before changing other paths.

Acceptance commands below are future targets to implement, not claims about current behavior. All fixtures must isolate HOME, XDG paths, NOON_* variables, config, and state, and must not source user startup files or install real hooks/services.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestShellEntry|TestShellCompletion)$' -count=1 -v` exits 0 and prints PASS for both named tests, covering the critical entry, authorization, payload-safety, completion-selection, and no-side-effect regressions above through public interfaces.
- [x] #2 `task shell:smoke` builds the binary and exits 0 after real Bash, Zsh, and Fish checks of generated integration and completion: initial load/reload queues nothing; project entry queues exactly once; same-project movement does not; a completion query yields expected commands/flags/job names without mutating state. Existing hooks and exit status are preserved. Missing supported shells fail with an actionable prerequisite message rather than silently skipping coverage.
- [x] #3 `task cli:build && noon_ac_home=$(mktemp -d) && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon shell --help` exits 0 and documents explicit shell selection, completion-only setup, and optional --on-enter; help/init are verified by the focused tests to leave config/state untouched.
- [x] #4 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0. README.md and docs/shell.md document tested Bash/Zsh/Fish setup/removal, project-entry and offline behavior, cooldown guidance, completion safety, and the distinction between queued work and parent-shell environment changes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a shared project-entry operation using canonical discovery, recorded enabled config, and the existing queue authorization path. 2. Ship Bash/Zsh/Fish glue and urfave completion callbacks, preserving hook/status behavior and treating paths as argv data. 3. Add focused security/state-transition regressions and real-shell smoke coverage, then document setup/removal. 4. Run every AC and task ci, perform one independent scoped review, commit and merge locally, finalize task and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Contract expansion: internal/store/queue.go needs an opt-in RequireUnpaused queue input so shell entry can reject paused jobs atomically with insertion. Existing manual queued requests intentionally do not check pauses (only webhooks currently do); preserve that behavior rather than change all clients or rely on a racy preflight check. internal/app/queue.go will forward this option only for shell entry.

AC#1: task cli:test -- -run '^(TestShellEntry|TestShellCompletion)$' -count=1 -v -> exit 0; both named tests PASS. Covers canonical boundaries, initialization/symlink/subdir deduplication, re-entry, coalescing/cooldown, recorded explicit config, pause/approval/disabled/recursion guards, event payload safety, read-only and position-aware completion.
AC#2: task shell:smoke -> exit 0 for real Bash, Zsh, Fish. A temporary PATH shim points to the already installed Fish 4.9.3 package binary because the default mise fish shim has no selected version; no shell was installed or startup file modified. Both modern Bash and /bin/bash 3.2 paths passed. Missing/broken shell prerequisites were confirmed to fail actionably. Fish explicit quoted/newline project completion regression is included.
AC#3: task cli:build && noon_ac_home=$(mktemp -d) && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon shell --help -> exit 0, explicit bash|zsh|fish, completion-only and --on-enter documented. Executed verbatim via temporary acceptance script; focused tests verify help/init leave config/state untouched.
AC#4: CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon -> exit 0. README/docs/shell.md cover setup/removal, offline queueing, cooldown, read-only completion and no parent-shell environment activation. Design/triggers updated.
Verification: task ci -> exit 0 (security, formatting/vet/staticcheck, full tests, race tests, built CLI smoke). LSP diagnostics clean. No existing tests deleted/skipped/weakened.
One scoped independent reviewer pass completed. Its Fish newline-path finding was reproduced, fixed and regression-covered. Follow-up real PTY Bash/Zsh probes confirmed quoted path candidates with spaces/apostrophes/metacharacters; Bash quote retention was corrected with non-evaluating completion-only quote removal and regression coverage. No remaining validated item-scoped defects. Next: commit implementation, integrate current main, rerun final checks, finalize record, remove owned worktree and release Worklease.

Delivery: implementation commit 0c0114969a3ca3260e9ad39888e9159ccff22106 merged into current main as befed898281e2018218a60603f49ce11130021a5, preserving concurrent unrelated work. task ci and task shell:smoke both passed again on the integrated source commit. Owned worktree/branch feat/noon-30-shell removed with Worktrunk after verifying the session creation receipt; its exact Herdr workspace wDM was inspected and confirmed removed by the hook. Unrelated feat/tui-design worktree/workspace preserved. No push performed. No blocker or remaining implementation step; final administrative commit and claim release only.

Post-merge review of implementation 0c01149 integrated in befed89: no actionable defects found. Inspected canonical discovery/token transitions, recorded enabled configuration selection, inherited override isolation without disabling recursion checks, transactional pause/approval/enablement checks before cooldown/coalescing, event JSON and shell quoting, idempotent hook installation/removal and exit-status preservation, and read-only position-aware completion. Reviewed focused and real-shell regressions. Verification: task cli:test -- -run '^(TestShellEntry|TestShellCompletion)$' -count=1 -v passed; task shell:smoke passed for Bash/Zsh/Fish with a temporary PATH shim to the existing Fish 4.9.3 executable (default mise fish shim is unresolved; initial unmodified invocation failed actionably). CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon passed; shell --help under env -i with isolated HOME passed and documents completion-only and optional entry setup. No source changes, no new dependencies, no global shell/tool configuration changes. This review reran focused acceptance checks, not full CI.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented explicit Bash/Zsh/Fish completion and optional approved project-entry queueing. Canonical project/config selection, atomic pause checks, event safety, hook/status preservation, and read-only completion are covered by focused tests, real-shell smoke and terminal probes. Full CI and race suite pass; merged locally and owned worktree/workspace cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
