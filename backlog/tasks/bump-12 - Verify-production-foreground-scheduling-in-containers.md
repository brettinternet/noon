---
id: BUMP-12
title: Verify production foreground scheduling in containers
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
  - Dockerfile
  - .dockerignore
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
  - scripts/**
  - integration/**
  - docs/docker.md
  - examples/docker/**
priority: medium
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the same scheduler runs safely as a non-root foreground container process and survives container replacement with persistent state.
Scope: production entrypoint/child reaping, SIGTERM behavior, bind-mounted read-only repo plus separate writable state, UID ownership, offline execution, restart recovery, bounded logs, sample Compose service, and opt-in agent-image extension guidance. Pin reviewed base images and test supported architectures. No Docker socket or credential mounts by default.
Non-goals: Kubernetes/operator, remote fleet management, sandbox-security claims, bundling all agent CLIs, launching containers from every job.
Modified-file contract: changes are limited to the --modified-file paths plus this task's provider-owned status/evidence. Expand scope only with justification in task notes.
Reference: docs/design.md and docs/development.md. Acceptance tests/targets below are deliverables, not currently implemented checks; matching named tests must actually execute, not pass with 'no tests to run'.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task docker:integration exits 0 with a real non-root container: scheduled fixture command runs against read-only repo, state persists across recreation, SIGTERM terminates owned children, and interrupted runs are not falsely successful. Add the target and run it in Linux CI.
- [ ] #2 task docker:offline exits 0 with --network none and embedded timezone data for a non-UTC schedule; no host credentials or Docker socket are mounted. Add the target.
- [ ] #3 task docker:smoke continues to exit 0 for inspection with a read-only root filesystem.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 Relevant focused checks and task ci pass; record command output and platform limitations before Done.
<!-- DOD:END -->
