---
id: NOON-12
title: Run the scheduler in production containers
status: done
type: feature
priority: medium
labels: [ops, reviewed]
blocked_by: [NOON-5, NOON-17]
rank: a388ee23b88ee23a6
refs: [docs/design.md, docs/development.md, docs/docker.md, backlog/decisions/decision-1 - One-per-user-foreground-scheduler-supervised-by-the-OS-or-container.md]
files: [Dockerfile, .dockerignore, examples/compose.yaml, Taskfile.dist.yaml, .github/workflows/ci.yaml, scripts/**, docs/docker.md, README.md]
created: 2026-10-16T00:18:00Z
updated: 2026-10-16T18:32:00Z
---

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

## Acceptance criteria

- [x] `task docker:smoke` exits 0
- [x] `task docker:integration` exits 0 on a Linux Docker host, exercising every bullet under the target's scope, and leaves no container, volume, or image with the fixture prefix behind (`docker ps -a --filter name=PREFIX -q` and `docker volume ls --filter name=PREFIX -q` print nothing)
- [x] `.github/workflows/ci.yaml` runs `task docker:integration` on Linux after `task docker:smoke`

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Inspect container, scheduler shutdown, and CI contracts. 2. Add production Compose configuration, container integration scenarios, and documentation. 3. Run acceptance commands and task ci, review the scoped diff, commit, merge, and clean up.

## Notes

Implemented digest-pinned images, production Compose example, tagged Go Docker integration test, Linux CI step, and deployment documentation within the modified-file contract. The integration fixture lives under scripts/dockerintegration temporarily so Linux VM Docker engines can bind the shared checkout (host OS temp paths may not be shared). AC#1: task docker:smoke -> exit 0. AC#2: task docker:integration -> exit 0 on Linux Docker engine, prefix noon-integration-d5c5f5134167b6a1271869ce; Compose dispatch, orphan reaping, interrupted shutdown/restart, recreation persistence, offline Asia/Kolkata next time all passed; cleanup assertions found no prefixed containers, volumes, or images. task ci -> exit 0 (security, vet/staticcheck, tests, race, smoke). Independent scoped review pending.

AC#2 rerun: task docker:integration -> exit 0 after review fix, prefix noon-integration-3039bbcf5b464dc4f2765e8f; cleanup assertions passed. AC#3: yq -o=json .jobs.go.steps .github/workflows/ci.yaml piped to jq assertion -> true / exit 0: docker:integration follows docker:smoke and both are Linux-only. go vet -tags dockerintegration ./scripts/dockerintegration -> exit 0. Independent reviewer PASS after fixing validated offline-container timeout leak (killing Docker CLI alone leaves its container running; dedicated cleanup now removes it). No other validated findings. task check:staged passed; implementation commit 3d147beac9ef245c2035ebaa32cca4f98c4de101; task ci passed on that commit. Fast-forward merged into main. Worktrunk removed the verified owned worktree and branch; its exact Herdr workspace was removed. No tests deleted, skipped, or weakened; only new opt-in Docker acceptance tests added. All changed paths are within contract; docs match implemented behavior and existing design contract. No blocker or remaining implementation step; nothing pushed.

Review (post-merge): digest-pinned bases, UID 10001, empty CMD, /state ownership, Compose overriding entrypoint with init/read_only/tmpfs/ro mount/named volume, and the integration scenarios (Compose dispatch, reaping, stop under 15s with interrupted history, recreation persistence, offline non-UTC preview, prefixed cleanup) match the scope; CI runs docker:integration after docker:smoke on Linux. Fixed in 4d39aea (docs only): the documented FROM example adding git produced an image where every git command in /workspace fails with "detected dubious ownership", because the bind-mounted checkout is owned by a host UID and HOME is read-only at runtime; the example now sets git config --system safe.directory /workspace (reproduced and verified with throwaway images on the local Linux engine). Low, not fixed: a scheduler shutdown that lands while a lifecycle hook runs waits up to 35s for it (second signal does not force it), exceeding the 15s stop_grace_period; Docker then SIGKILLs and recovery marks the hook interrupted (see NOON-22 note). Integration target not rerun; no container files changed.

## Summary

Production container scheduling verified with the actual Compose example, digest-pinned images, persistent non-root state, read-only mounts, init child reaping, graceful interrupted runs, and offline timezone previews. Docker smoke/integration, task ci, tagged vet, and scoped independent review passed. Implementation 3d147be merged to main; owned worktree, branch, and workspace removed.
