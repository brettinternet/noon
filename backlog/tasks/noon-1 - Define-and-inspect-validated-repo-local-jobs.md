---
id: NOON-1
title: 'Define, validate, and list jobs'
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:52'
labels:
  - config
  - cli
dependencies:
  - NOON-14
  - NOON-15
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/config/**
  - internal/app/**
  - internal/cli/**
  - examples/**
  - docs/configuration.md
  - README.md
  - scripts/smoke.sh
priority: high
type: feature
ordinal: 300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: projects declare command jobs in YAML; `noon config check` validates them with file and line; `noon list` and bare `noon` show the effective jobs and their next run times. Nothing executes and nothing is written.

Read first: docs/design.md#jobs-and-configuration and #experience, decision-2 (digests), decision-5. Current code: internal/config/config.go (strict yaml.Node parsing, `Load`, `Input`, `Config`), internal/cli/root.go.

Scope:
- internal/config parses a `jobs` mapping from `noon.yaml`, `.noon.yaml`, and an explicit `--config` file. `jobs` in user config is an error that names the allowed files.
- Fields in this task: `description`, `schedule`, `run`, `cwd`, `env`, `timeout`, `overlap`, `missed`, `enabled`. Any other job field, including later ones such as `prompt`, is an unknown-field error for now.
- Job names match `^[a-z][a-z0-9_-]{0,63}$`. They start with a letter, so a bare number on the command line always means a run ID.
- Keep the existing strictness (no aliases, nulls, or duplicate keys; one document; 1 MiB). Error format: `config PATH:LINE: jobs.NAME.FIELD: message`.
- `run`: a non-empty YAML string (runs as `/bin/sh -c`) or a non-empty list of strings (exact argv). `schedule`: `schedule.Parse` and its suggestion. `timeout`: `schedule.ParseDuration`, at least 1s, or `none`; default 1h. `overlap`: `skip`|`queue`, default `skip`. `missed`: `run_once`|`skip`, default `run_once`. `env`: string values only (hint: quote numbers); names match `[A-Za-z_][A-Za-z0-9_]*`; the `NOON_` prefix is reserved. `cwd`: relative to the project root or absolute; it need not exist yet.
- Merge by name: a `.noon.yaml` job replaces a shared job entirely, except an entry containing only `enabled`, which toggles the shared job. An `enabled`-only entry with no shared job is an error.
- Provenance per job: `source` (absolute file path), `shared` (from `noon.yaml` or `--config`), `overrides` (a local entry replaced or toggled a shared job).
- Digest: `"sha256:" + hex(SHA-256(canonical JSON))` over explicitly set execution fields only, with sorted keys and unset fields omitted. Exclude `description` and `enabled`. Use the normalized schedule string and normalized durations, so reformatting YAML or editing a description never changes it. Omitting unset fields keeps old approvals valid when later tasks add optional fields.
- Create internal/app with the shared operation: given `config.Input` and `now`, return the project (path, timezone) and jobs sorted by name, each with `next_at` (nil when manual or disabled). Interval jobs show `now + interval` until the scheduler stores their phase in a later task.
- `noon list` (alias `ls`) prints columns JOB, SCHEDULE (`manual` when absent), NEXT (`Fri 09:00 (in 14h)` in the project timezone, `-` when manual, `disabled`), and COMMAND (truncated to fit 80 columns), then one summary line with job and override counts. With no jobs it prints a ready-to-paste YAML example and exits 0.
- Bare `noon` and `noon status`: a project line (name, path, timezone) followed by the same table, and the factual line "Noon does not run jobs yet." Remove the old bootstrap text.
- `noon list --json`: `{"schema_version":1,"project":{"path","timezone"},"jobs":[{"name","description","schedule","next_at","run","cwd","env_keys","timeout","overlap","missed","enabled","source","shared","overrides","digest"}]}`. `schedule` and `next_at` are null when absent; `next_at` is RFC 3339 in the project timezone; `run` is a string or array; `env_keys` lists names only; `timeout` is a duration string or `none`. Never print env values anywhere.
- `noon config check` validates jobs and reports how many it found.
- `noon init` writes a commented example job that is valid once uncommented, replacing the "later release" comment.
- Add examples/jobs.yaml covering every field in this task. Document job fields in docs/configuration.md and update README's status.

Non-goals: execution, state, prompts and agents, hooks, approval checks, the scheduler, YAML writing, cron import.

Tests (critical only):
- internal/config `TestLoadJobs`: table of valid and invalid manifests, including whole-job override, `enabled`-only toggle, toggle with no shared job, `jobs` in user config, an invalid job name (`1tests`), an unknown field reported with file and line, an invalid schedule with its suggestion, a non-string env value, and a `NOON_` env name.
- internal/config `TestJobDigest`: unchanged by key order, comments, `description`, and `enabled`; changed by `run`, `schedule`, `env`, `cwd`, and `timeout`.
- internal/cli `TestListJSON`: documented shape, no env values in output, and the project tree byte-identical before and after.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestLoadJobs|TestJobDigest)$' -count=1 -v` exits 0 and prints `--- PASS` for both tests, covering every case named in the description
- [x] #2 `task cli:test -- -run '^TestListJSON$' -count=1 -v` exits 0: output matches the documented shape, contains no env value, and the temp project tree is byte-identical afterwards
- [x] #3 `task cli:build && bin/noon --config examples/jobs.yaml --json list` exits 0 and prints one JSON object whose `jobs` are sorted by name and each carry a `sha256:` digest
- [x] #4 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && git -C "$noon_ac_project" init -q && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" list` exits 0 and prints guidance containing an example `jobs:` block
- [x] #5 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    schedule: every day\n    run: "true"\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" config check; echo "exit=$?"` prints an error naming `noon.yaml:3` and suggesting `every day at 9am`, then `exit=1`
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
1. Extend strict config parsing with jobs, merge rules, provenance, and canonical digests. 2. Add shared inspection operation and list/status output; update init, examples, and documentation. 3. Verify critical regression tests, every acceptance command, and task ci; independently review concrete job validation and secret-redaction risks. 4. Commit implementation, merge main, finalize task evidence, and clean up owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Direct implementation after delegation failed before launch (registry/model-scope conflict). Contract expansion: docs/design.md schedule implementation-status sentence must be updated to match implemented job wiring. Existing TestLoadRejectsAmbiguousOrInvalidYAML rejects jobs: {}; this is obsolete under this task. Replace that case with malformed jobs: [] while retaining all strictness assertions and add valid empty jobs coverage.

AC#1: task cli:test -- -run '^(TestLoadJobs|TestJobDigest)$' -count=1 -v -> exit 0; both named tests PASS, including all requested validation/merge/digest cases.
AC#2: task cli:test -- -run '^TestListJSON$' -count=1 -v -> exit 0; shape, sorting, timezone/interval next times, secret redaction, and byte-identical project tree verified.
AC#3: task cli:build && bin/noon --config examples/jobs.yaml --json list -> exit 0; one JSON object with reminder/report/tests sorted and sha256 digests.
AC#4: exact task acceptance command (task cli:build; isolated mktemp HOME/project; git init; env -i ... list) executed in /tmp/noon-1-acceptance-53909a3c.sh because inline shell guard rejects dynamic syntax -> exit 0, guidance includes jobs: block.
AC#5: exact task acceptance command (task cli:build; isolated mktemp HOME/project; malformed schedule fixture; env -i ... config check; echo exit) in same script -> noon.yaml:3: jobs.t.schedule error suggests every day at 9am, exit=1.
Full task ci passed (format, vet, Staticcheck, tests, race, smoke, secrets and vulnerability scans). Independent reviewer is examining validation, digest/provenance, and redaction risks. Implementation uncommitted pending review.

Delivery: implementation commit 6e72f6142591bab43b31dd02a02867b178535513 (feat: validate and list project jobs) passed task check:staged and task ci on the committed tree, then fast-forward merged into main.
Review: one independent reviewer pass completed (run 15412bcc-45e0-4a14-a35f-6011b23f0be3). Confirmed malformed-env YAML decoder leakage and terminal-control injection in unknown-field paths; both fixed with CLI regressions, direct human/JSON/byte-capture probes, focused tests, clean LSP diagnostics, and full task ci. No unresolved findings. No tests deleted, skipped, or weakened: the obsolete jobs:{} rejection was replaced by malformed jobs:[] and valid-empty coverage.
Cleanup: verified session creation receipt and clean merged checkout, checked exact Herdr workspace wBN had only its idle zsh, then Worktrunk removed .worktrees/feat-noon-1 and branch feat/noon-1. Confirmed workspace wBN disappeared and only main remains. No retained checkout/workspace.
No blocker or remaining implementation step. Final backlog metadata commit and Worklease release complete delivery bookkeeping; no push authorized or performed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented strict command-job configuration, whole-job local overrides and toggles, canonical digests, shared read-only inspection, list/ls/status previews, JSON with environment names only, init example and docs. AC1-5 and task ci pass; independent review findings fixed. Commit 6e72f61 merged to main; owned worktree, branch, and Herdr workspace removed.
<!-- SECTION:FINAL_SUMMARY:END -->
