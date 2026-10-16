---
id: NOON-30
title: Support project-entry shell hooks and shell completion
status: To Do
assignee: []
created_date: '2026-10-16 19:19'
labels:
  - shell
  - integration
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
- [ ] #1 `task cli:test -- -run '^(TestShellEntry|TestShellCompletion)$' -count=1 -v` exits 0 and prints PASS for both named tests, covering the critical entry, authorization, payload-safety, completion-selection, and no-side-effect regressions above through public interfaces.
- [ ] #2 `task shell:smoke` builds the binary and exits 0 after real Bash, Zsh, and Fish checks of generated integration and completion: initial load/reload queues nothing; project entry queues exactly once; same-project movement does not; a completion query yields expected commands/flags/job names without mutating state. Existing hooks and exit status are preserved. Missing supported shells fail with an actionable prerequisite message rather than silently skipping coverage.
- [ ] #3 `task cli:build && noon_ac_home=$(mktemp -d) && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon shell --help` exits 0 and documents explicit shell selection, completion-only setup, and optional --on-enter; help/init are verified by the focused tests to leave config/state untouched.
- [ ] #4 `CGO_ENABLED=0 mise exec go -- go build -o /dev/null ./cmd/noon` exits 0. README.md and docs/shell.md document tested Bash/Zsh/Fish setup/removal, project-entry and offline behavior, cooldown guidance, completion safety, and the distinction between queued work and parent-shell environment changes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
