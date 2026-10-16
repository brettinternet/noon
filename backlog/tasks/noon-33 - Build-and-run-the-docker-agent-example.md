---
id: NOON-33
title: Build and run the docker-agent example
status: To Do
assignee: []
created_date: '2026-10-16 22:09'
updated_date: '2026-10-16 22:21'
labels:
  - ops
  - integration
  - examples
dependencies: []
documentation:
  - docs/docker.md
  - examples/integrations/docker-agent/README.md
modified_files:
  - examples/integrations/docker-agent/**
  - scripts/docker-agent-smoke.sh
  - Taskfile.dist.yaml
priority: medium
type: task
ordinal: 3400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the docker-agent example builds and executes a harmless queued job and hook as UID 10001 under its documented read-only Compose configuration.

Why: version checks alone do not prove that home, reports, or config work. /home/noon tmpfs ownership and a fresh /reports volume can prevent non-root writes; the image currently creates neither a noon-owned reports directory nor explicit tmpfs ownership. Verify Alpine package availability and supported agent installation rather than assuming them.

Scope: fix the example Dockerfile, compose.yaml, config, scripts, and README, and add a repeatable opt-in smoke command using the existing scripts/Taskfile style. Record base/image and installed tool versions. Define the .env contract explicitly: either an empty credential-free file created by documented setup, or an optional env_file supported by the stated Compose version. Do not require real credentials merely to start.

Smoke safety:
- Use a disposable copy, unique Compose project name, and only project-owned containers/volumes; preserve unrelated Docker resources. Disable scheduled agent jobs in the fixture before enabling it, so the current weekday/time cannot start a real model call.
- Keep the shipped read-only root/project/config mounts and non-root user. Verify UID, writable home/reports/state, and rejected writes to the read-only project; do not solve permission bugs with root, privileged mode, broad world-writable directories, or a Docker socket.
- Inspect the shipped /config profile, then in the disposable fixture replace the agent executable with a harmless local fake and the notification hook with a capture-only command. Run a real Noon prompt job through enable, queue, dispatch, hook, and history. No model/network calls after image installation.
- Restart the same service/volumes and verify retained run history/report output. Finally stop the owned Compose project and remove only its resources, even on failure.

Non-goals: image publishing, adding this optional smoke to task ci, root Dockerfile/examples/compose.yaml changes, proving real model authentication or inference.

Dependencies: none. Docker Linux engine, Compose, and network for image/package installation are explicit prerequisites; a missing prerequisite is a blocker, never a passing skip. Use relevant NOON-32 vendor findings when available, but do not make independent container checks wait on paid-agent verification.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task docker:build && docker build -t noon-agent:local examples/integrations/docker-agent` exits 0. Record Docker/Compose, image/base, and installed CLI versions in Implementation Notes.
- [ ] #2 `task examples:docker-agent` (the opt-in target implemented here) exits 0 after checking claude/gh/git/jq versions, UID 10001, shipped user-config discovery, writable home/reports/state, and rejected project writes using the actual Compose mount options. No real credential is required.
- [ ] #3 The same `task examples:docker-agent` queues and completes a harmless prompt job plus capture-only hook, asserts run/log/output rather than merely online status, restarts and verifies persistence, and tears down its unique owned project. Record commands/results and confirm unrelated Docker resources remain untouched.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: the old docker-run tmpfs check did not reproduce Compose ownership, and status/version output could pass while scheduled agent jobs could not write. The smoke now proves execution and persistence without exposing credentials or accidentally dispatching scheduled model work.
<!-- SECTION:NOTES:END -->
