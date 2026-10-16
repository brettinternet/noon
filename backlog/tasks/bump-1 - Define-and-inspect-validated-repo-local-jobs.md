---
id: BUMP-1
title: Define and inspect validated repo-local jobs
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
labels:
  - bootstrap-roadmap
dependencies: []
documentation:
  - docs/design.md
modified_files:
  - internal/config/**
  - internal/job/**
  - internal/cli/**
  - docs/configuration.md
  - README.md
  - examples/**
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can declare named commands and inspect the exact effective job definitions before anything executes.
Scope: extend version-1 YAML with jobs, argv commands and explicit shell actions, schedule/timeout/overlap validation, explicit cwd/env references, whole-job local overrides, disabled jobs, source provenance, and bump list. Preserve settings precedence and Git boundaries. User settings must not inject jobs into every repo.
Non-goals: execution, persistence, scheduler, cron import, provider-specific agent actions. Do not execute configuration or interpolate secrets into config/list output.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestJobManifest' -count=1 -v exits 0 and actually runs named cases proving valid jobs, invalid commands/schedules, whole-job overrides, disabled jobs, secret redaction, source-relative cwd, and absence of execution.
- [ ] #2 task cli:build && bin/bump --config examples/jobs.yaml --json list exits 0 with deterministic named jobs; bin/bump list in an empty isolated repo exits 0 with no jobs and useful human guidance.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
