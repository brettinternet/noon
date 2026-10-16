---
id: BUMP-6
title: Import user crontabs with preview and explicit compatibility
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-1
documentation:
  - docs/design.md
modified_files:
  - internal/cronfile/**
  - internal/cli/**
  - internal/config/**
  - docs/configuration.md
  - examples/**
priority: medium
type: feature
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: existing simple crontabs become reviewable Bump jobs without scheduling anything during import.
Scope: explicit file/stdin input, five-field user crontab lines, comments, supported environment assignments/macros, deterministic normalized job IDs, line-specific errors, preview by default and exclusive explicit output creation. Document supported and rejected cron features, including timezone and percent/shell semantics, instead of silently approximating them.
Non-goals: reading/modifying system crontabs automatically, system-user columns, implicit @reboot execution, hot-reading crontabs as a second authoritative manifest.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestCronImport' -count=1 -v exits 0 with supported entries round-tripping into valid YAML, environment/shell semantics preserved, and unsupported syntax rejected with line numbers.
- [ ] #2 task cli:test -- -run '^TestCronImportSafety' -count=1 -v exits 0 proving preview starts no processes, modifies no files, and explicit output never overwrites an existing file.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
