---
id: NOON-33
title: Build and run the docker-agent example
status: Done
assignee: []
created_date: '2026-10-16 22:09'
updated_date: '2026-10-17 01:56'
labels:
  - ops
  - integration
  - examples
  - reviewed
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
- [x] #1 `task docker:build && docker build -t noon-agent:local examples/integrations/docker-agent` exits 0. Record Docker/Compose, image/base, and installed CLI versions in Implementation Notes.
- [x] #2 `task examples:docker-agent` (the opt-in target implemented here) exits 0 after checking claude/gh/git/jq versions, UID 10001, shipped user-config discovery, writable home/reports/state, and rejected project writes using the actual Compose mount options. No real credential is required.
- [x] #3 The same `task examples:docker-agent` queues and completes a harmless prompt job plus capture-only hook, asserts run/log/output rather than merely online status, restarts and verifies persistence, and tears down its unique owned project. Record commands/results and confirm unrelated Docker resources remain untouched.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: the old docker-run tmpfs check did not reproduce Compose ownership, and status/version output could pass while scheduled agent jobs could not write. The smoke now proves execution and persistence without exposing credentials or accidentally dispatching scheduled model work.

NOON-32 vendor findings (2026-10-16): installed Claude Code 2.1.302 help confirms --permission-prompts none (requires >=2.1.259), but this denies only unresolved prompts, not tools or permissions granted by settings/hooks/mode. Unspecified mode can be auto; agent-profiles now explicitly select manual for inspection and document that it is not a read-only sandbox. acceptEdits also approves filesystem commands. For genuinely tool-disabled untrusted text, CLI reference documents --tools "" plus --disallowedTools 'mcp__*'; strict-mcp-config and safe-mode limit ordinary configuration, with managed-policy caveats. See https://code.claude.com/docs/en/headless and https://code.claude.com/docs/en/cli-reference. No live model call was performed; container's pinned/installed version must be checked separately. NOON-32 did not change Docker files.

AC#1: task docker:build && docker build -t noon-agent:local examples/integrations/docker-agent -> exit 0. Docker client 29.9.1, Linux arm64 engine 29.5.2 (Colima), Compose v5.6.0. Base Alpine 3.23 digest 85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0; noon:dev image a70b8874087992bd7b8b577540acb416628c29e3973eca483f51b3db194661d8; noon-agent:local f83ac40eb8754bb8ff1c5470b6ca46f9e2fb6218c046dfe76def2ceff54011af. Native Claude 2.1.302, gh 2.83.0, git 2.52.0, jq 1.8.2. Official setup documentation confirms native Alpine dependencies and version pinning (https://code.claude.com/docs/en/setup). Installed binary copied outside tmpfs home; auto-updates disabled.
AC#2: TMPDIR=/Users/brett/.cache/noon-smoke task examples:docker-agent -> exit 0. Colima shares home, not macOS OS temp: documented TMPDIR setup required. Actual shipped Compose read-only root/project/config and UID 10001 retained; home/reports/state writes and rejected project/config/root writes checked. Mounted user-config discovery and exact profile verified. Empty .env; network_mode:none throughout runtime checks.
AC#3: same task examples:docker-agent -> exit 0. Scheduled jobs removed from fixture before enabling; shipped repo-health prompt delivered via stdin to harmless fake, detached run succeeded with exit 0 and hook status ok; report bytes, job logs and hook logs checked. Same-volume restart preserved exact history, logs and reports. Unique project noon-agent-smoke-noon-docker-agent-fa8pug-83941 removed with all its volumes; earlier failing probes also tore down their volumes. Docker queries show no smoke containers/volumes remain. All five preexisting unrelated container IDs still present; no operations targeted them or their volumes. Host fixtures retained for diagnostics.
Validation: task ci -> exit 0; bash -n and git diff --check pass; Bash LSP clean, YAML diagnostics unavailable (actual Compose execution validated YAML). No tests weakened/deleted. Changes stay within allowed paths; README describes implemented credential, mount, native-install and smoke behavior. No new product/design contract.

Delivery: implementation commit 7a45f58 merged into main as 67f5ea3, preserving the concurrent backlog planning commit. task ci passed on merged source. Independent reviewer completed the one general review with no validated findings. task check:staged passed. Verified session creation receipt and clean integrated worktree before Worktrunk removal; branch/worktree deleted and exact Herdr workspace wEF confirmed absent. Unrelated feat/tui-design worktree left untouched. No blockers or resumable implementation work remain; no push performed.

Review 2026-10-17: the image kept a 265 MB duplicate Claude install under /root/.local after cp -L. The Dockerfile now removes installer leftovers in the same layer. TMPDIR=~/.cache/noon-smoke task examples:docker-agent passed all four PASS stages, and claude 2.1.302 still runs.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Built and verified the Docker agent example with a pinned native Claude installation, owned home/reports paths, explicit empty .env setup, and an opt-in network-disabled prompt/hook persistence smoke. All acceptance criteria passed; implementation merged to main; owned worktree, branch, Compose resources, and Herdr workspace cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
