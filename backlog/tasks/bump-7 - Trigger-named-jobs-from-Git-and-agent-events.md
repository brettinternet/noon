---
id: BUMP-7
title: Trigger named jobs from Git and agent events
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-4
documentation:
  - docs/design.md
modified_files:
  - internal/events/**
  - internal/app/**
  - internal/cli/**
  - internal/store/**
  - internal/config/**
  - integration/**
  - docs/integrations.md
  - examples/hooks/**
priority: medium
type: feature
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a Git hook or agent lifecycle hook can submit bounded structured context to a named existing job and inspect the outcome.
Scope: bump trigger list/submit, event IDs and deduplication, cooldowns/rate limits, recorded origin, bounded versioned JSON payloads, command-hook argv with JSON stdin and separate diagnostics, time/output limits, recursion prevention. Provide opt-in post-commit/post-merge and generic agent completion examples. Events are data, not shell interpolation. Hook failures remain visible without recursively spawning failure hooks.
Non-goals: automatic Git hook installation, transcript scraping, network listener, in-process Go plugins, marketplace/discovery, trusting a self-declared actor as authorization.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestEventTrigger' -count=1 -v exits 0: repeated IDs deduplicate, cooldown/rate limits are observable, oversized/malformed payloads fail, unknown targets do not execute.
- [ ] #2 task cli:test -- -run '^TestCommandHook' -count=1 -v exits 0 with argv/JSON isolation, enforced timeout/output limits, visible failure, and no recursive event loop.
- [ ] #3 task cli:test -- -run '^TestGitHookIntegration' -count=1 -v exits 0 in a temporary Git repo with opt-in hook dispatch and no modification of real user hooks.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
