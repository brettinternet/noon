---
id: NOON-33
title: Build and run the docker-agent example
status: To Do
assignee: []
created_date: '2026-10-16 22:09'
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
priority: medium
type: task
ordinal: 3400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: examples/integrations/docker-agent builds, starts under Compose, and runs agent tooling as the non-root image user, with any needed fixes applied.

Why: the example was written without building it. Open questions that can break it for anyone who copies it: whether `npm install -g @anthropic-ai/claude-code` works on the Alpine base and `claude` runs as UID 10001; whether Alpine 3.23 names the package `github-cli`; whether the `/home/noon` tmpfs is writable by UID 10001 under `read_only: true`; whether `env_file: .env` should be optional so a fresh copy starts; whether the user-config hook `/workspace/scripts/ntfy.sh` is executable from the read-only project mount; and whether `noon enable` and `noon serve` pick up agent profiles from `XDG_CONFIG_HOME=/config`.

Scope: build and run the example, fix the Dockerfile, compose.yaml, config, scripts, and README as needed.

Non-goals: publishing images, adding the example to CI, changing the root Dockerfile or examples/compose.yaml, calling a real model API beyond version checks.

Dependencies: requires Docker with a Linux engine and network access for package installs.

Modified-file contract: the modified_files list plus this task record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task docker:build && docker build -t noon-agent:local examples/integrations/docker-agent` exits 0.
- [ ] #2 For each of `claude --version`, `gh --version`, `git --version`, and `jq --version`, `docker run --rm --read-only --tmpfs /tmp --tmpfs /home/noon --entrypoint sh noon-agent:local -c "<command>"` exits 0 and prints a version.
- [ ] #3 From a disposable copy of the example with an empty `.env`, `docker compose up -d --build` starts the service; `docker compose exec noon noon config` lists the `claude` profile from `/config`; `docker compose exec noon noon status` shows the project enabled and the scheduler online; `docker compose exec noon touch /home/noon/probe /reports/probe` exits 0; `docker compose down --volumes` removes everything. Outputs are recorded in Implementation Notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
