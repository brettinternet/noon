---
id: BUMP-11
title: Install explicit per-project user services
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 00:19'
labels:
  - bootstrap-roadmap
dependencies:
  - BUMP-4
documentation:
  - docs/design.md
modified_files:
  - internal/service/**
  - internal/cli/**
  - integration/**
  - docs/services.md
  - docs/development.md
  - Taskfile.dist.yaml
  - .taskfiles/**
  - scripts/**
priority: medium
type: feature
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can deliberately keep a repo scheduler running across terminal exit/reboot without opaque self-daemonization.
Scope: launchd and systemd user-service install/uninstall/status, canonical project/config/state paths, explicit environment handling, unique identity, idempotency, clear relocation/error handling. Bare bump service only inspects. Service manager supervises the same bump serve process. Uninstall preserves job state and only removes a verified Bump-owned service.
Non-goals: root/system services, implicit enrollment, arbitrary shell rc sourcing, starting services during clone/init/status, a global multi-repo daemon.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test -- -run '^TestServiceDefinition' -count=1 -v exits 0: paths with spaces, canonical project identity, explicit environment, idempotent install, and preserved foreign definitions are covered.
- [ ] #2 task integration:service exits 0 on the native host using a uniquely named temporary user service: install/start/status/stop/uninstall and confirm no fixture-owned service remains. Add the target and document its host prerequisite; never skip unsupported host checks silently.
- [ ] #3 task cli:test -- -run '^TestServiceSafety' -count=1 -v exits 0: bare commands/discovery never install/start a service and uninstall retains project state.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
