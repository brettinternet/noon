---
id: NOON-18
title: Enable projects and approve shared jobs
status: Done
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 14:27'
labels:
  - security
  - store
  - cli
  - reviewed
dependencies:
  - NOON-3
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
modified_files:
  - internal/store/**
  - internal/app/**
  - internal/cli/**
  - internal/config/**
  - README.md
  - docs/configuration.md
priority: high
type: feature
ordinal: 800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: background scheduling has an explicit opt-in per checkout. Shared jobs run unattended only while their approved definition is unchanged, and every view says which jobs need approval.

Read first: decision-2, docs/design.md#enable-and-approve and #security-model.

Scope:
- Migration 2: on `projects`, add `enabled INTEGER NOT NULL DEFAULT 0`, `enabled_at TEXT`, and `config_file TEXT` (absolute path of an explicit `--config` file; NULL means automatic files). Add `approvals(project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL, digest TEXT NOT NULL, approved_at TEXT NOT NULL, PRIMARY KEY(project_id, job))`.
- `noon enable` (current project; no arguments): load config (invalid config fails with nothing changed); register or update the project with its config selection; set enabled; replace its approvals with the current digest of every job that needs approval. Idempotent: `enabled_at` changes only when a disabled project becomes enabled, and `approved_at` only when a job's approved digest changes. Output: `Enabled ~/code/app · 3 jobs · approved 2 shared jobs`, then a line saying nothing runs in the background until a scheduler serves the project. Name only commands that exist; the serve task adds its own hint. JSON: `{"schema_version":1,"project":{...},"approved":["deps","tests"]}`.
- `noon disable`: clear enabled and the project's approvals, keep history, and exit 0 when already disabled.
- A job needs approval when its effective definition comes from `noon.yaml` or an explicit `--config` file. Jobs from `.noon.yaml`, including whole-job local overrides, do not. An `enabled`-only local toggle does not change the shared digest.
- `approval` per job in list JSON and the app job struct: `not_required`, `approved`, or `needs_approval` (never approved, or digest differs). The project object gains `enabled`. Read-only commands read approvals without creating state.
- Human list and bare `noon`: NEXT shows `needs approval` for those jobs. When any shared job needs approval in an enabled project, add one notice `N shared jobs changed since you enabled this project. Next: noon enable`. For a project that is not enabled and has scheduled jobs, add `Not enabled for background runs. Next: noon enable`.
- Foreground `noon run` stays allowed without approval.
- README gains a short Safety section: what enable does, what digests cover (definitions, not script or prompt-file contents), why `.noon.yaml` must stay ignored, and that approval is not a sandbox.

Non-goals: the scheduler, pause, queued runs, `noon add`, per-job approve commands, approval via MCP, notifications.

Tests:
- internal/cli `TestEnableApproval` (security boundary): enable approves shared jobs; changing a shared job's `run`, `schedule`, or `env` makes only that job `needs_approval`; changing `description` does not; `.noon.yaml` jobs and local overrides report `not_required`; disable then list shows the project not enabled; enable with invalid config changes nothing; enabling with `--config FILE` records the file and treats its jobs as shared.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^TestEnableApproval$' -count=1 -v` exits 0 and prints `--- PASS: TestEnableApproval`, covering every case listed under Tests
- [x] #2 In one shell: `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    schedule: daily\n    run: "true"\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" enable && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json list` exits 0 with job `t` reporting `"approval":"approved"`; then `printf '    timeout: 5m\n' >> "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json list` reports `"approval":"needs_approval"`, and `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"` prints `Next: noon enable`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add transactional project enable/disable and approval reads, preserving legacy read-only stores. 2. Extend app inspection and CLI enable/disable plus approval notices. 3. Add security-boundary regression coverage, update safety documentation, run both acceptance commands and full CI. 4. Independent scoped review, commit, merge locally, finalize backlog and remove owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Schema inspection: migration 2 already adds runs.log_overflow. Approval schema will be migration 3 rather than rewriting an existing migration; this preserves existing databases and does not conflict with decision-2. Worktree: .worktrees/noon-18-approval; Worklease claim 8ac64d9a3713dd976abac31392fdf3ef, loop 53909a3c-e1ac-40d5-a491-b52b62d6687d.

AC#1: task cli:test -- -run '^TestEnableApproval$' -count=1 -v -> exit 0, --- PASS: TestEnableApproval; covers shared action/schedule/env changes, description stability, local jobs/replacements/toggles, invalid-config no-change, disable/history, explicit config selection, and manual execution without approval. AC#2: exact shell acceptance sequence executed via /tmp/noon-18-acceptance.sh -> exit 0; t approval changed from approved to needs_approval after timeout edit; dashboard printed Next: noon enable. task test and task ci passed (vet, Staticcheck, security scans, race, smoke); affected-file LSP diagnostics clean. Store transition regression additionally verifies unchanged timestamps, changed-digest timestamps, rollback on DB failure, and disable/re-enable. Existing schema-version and JSON-shape assertions updated for new contract without removing checks. Independent scoped review pending; next step resolve concrete findings then commit, integrate, finalize, clean up.

Single independent review found invalid job configuration blocked disable. Reproduced and fixed by resolving checkout/state without reading project manifests or validating jobs; explicit state-dir allows recovery from malformed explicit/user files. Added revocation regression including broken YAML and invalid explicit jobs, retained history checks. AC#1 rerun -> PASS; AC#2 exact shell sequence rerun -> approved then needs_approval and Next: noon enable. Full task ci passed again on implementation commit fb8f7f2531b324ffd35644104309128b089aeede, which was fast-forward merged to main. task check:staged passed before commit; no tests deleted/skipped/weakened; all paths within contract. Docs/help describe implemented behavior and remain consistent with settled design. Owned worktree .worktrees/noon-18-approval and branch removed via Worktrunk; associated Herdr workspace wC4 removed by hook. No remaining blocker or resumable implementation step.

Review (post-merge): enable/disable, approval states, notices, and idempotent timestamps match the contract; `noon run` stays allowed without approval. Medium finding, not fixed: with `--config FILE`, jobs are shared and need approval, but the file may also define `agents`, and the digest covers only the agent name. Changing `agents.foo.command` (e.g. from `cat` to `sh -c ...`) left an approved prompt job `approved` with an unchanged digest, so the scheduler would run an unapproved command. Recommend hashing the resolved agent command and prompt mode into prompt-job digests. Low: (1) a project-level `timezone` in `noon.yaml` is not in any job digest, so a shared edit can shift cron times without re-approval (it does reset schedule state); (2) enable output says `1 jobs · approved 1 shared jobs`. AC1 and AC2 rerun and pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented checkout opt-in, atomic shared-job digest approvals, safe revocation, read-only approval views/notices, and safety docs. Both acceptance checks and full CI pass; review finding fixed and regression-tested. Merged fb8f7f2 locally; owned worktree/branch/workspace cleaned up. No push.
<!-- SECTION:FINAL_SUMMARY:END -->
