---
id: NOON-12
title: Run the scheduler in production containers
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - ops
dependencies:
  - NOON-5
  - NOON-17
documentation:
  - docs/design.md
  - docs/development.md
  - docs/docker.md
  - >-
    backlog/decisions/decision-1 -
    One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md
modified_files:
  - Dockerfile
  - .dockerignore
  - examples/compose.yaml
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
  - scripts/**
  - docs/docker.md
  - README.md
priority: medium
type: feature
ordinal: 1800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the image runs `noon serve` as a well-behaved production container: correct signals, reaped children, state persisted on a volume, a read-only repository mount, and a documented Compose setup.

Read first: docs/design.md#docker and #scheduler-lifetime, decision-1, docs/docker.md, Dockerfile, and the `docker:*` targets in Taskfile.dist.yaml.

Scope:
- Dockerfile: keep the non-root UID 10001, `NOON_STATE_DIR=/state`, `ENTRYPOINT ["noon"]`, and the static build. Keep the default `CMD` empty, so a bare `docker run` shows the dashboard and starts nothing. Keep `/state` owned by UID 10001 in the image, so a new named volume inherits writable ownership. Pin the base images by digest.
- examples/compose.yaml: one service with `image: ${NOON_IMAGE:-noon:dev}` (no image is published yet; a comment points to `task docker:build`), `init: true`, `read_only: true`, `tmpfs: [/tmp]`, the project directory (`.`) bind-mounted read-only at `/workspace`, a named volume at `/state`, `working_dir: /workspace`, and `entrypoint: ["/bin/sh", "-c", "noon enable && exec noon serve"]`. It must override the entrypoint: a `command:` would be appended to `ENTRYPOINT ["noon"]` and run `noon sh -c ...`. Comments explain that `noon enable` trusts the deployed config at each start.
- Add `task docker:integration` and run it in Linux CI after `docker:smoke`. Use a disposable project fixture and named resources with a unique prefix, removed at the end. Scenarios after the first may use `docker run` with equivalent options. It must:
  - start the scheduler from examples/compose.yaml itself (`docker compose --project-directory FIXTURE -f examples/compose.yaml -p PREFIX up -d` with `NOON_IMAGE=noon:dev`) and confirm `noon serve` is running in the container;
  - in the served project, queue a job with `docker exec ... noon run NAME --detach`, and observe `succeeded` in `noon history --json`;
  - run with `--network none`, and have a schedule in a non-UTC timezone report the expected next time;
  - check that a job leaving a background grandchild produces no zombie (`ps` shows no `Z` state);
  - `docker stop` while a job is running: the container exits within 15s, and after restarting it, history shows the run `interrupted`, never `succeeded`;
  - recreate the container on the same volume and confirm history persists.
- docs/docker.md: production Compose usage, volume ownership for UID 10001, extending the image with job tools (a `FROM` example adding git), keeping agent credentials and the Docker socket out by default, and the approval semantics of `noon enable` in containers.

Non-goals: Kubernetes, multi-arch publishing changes, bundling agent CLIs, launching a container per job, health-check endpoints (the webhook task adds `/healthz`).

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task docker:smoke` exits 0
- [ ] #2 `task docker:integration` exits 0 on a Linux Docker host, exercising every bullet under the target's scope, and leaves no container, volume, or image with the fixture prefix behind (`docker ps -a --filter name=PREFIX -q` and `docker volume ls --filter name=PREFIX -q` print nothing)
- [ ] #3 `.github/workflows/ci.yaml` runs `task docker:integration` on Linux after `task docker:smoke`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
