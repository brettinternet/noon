---
id: BUMP-1
title: 'Define, validate, and list jobs'
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - config
  - cli
dependencies:
  - BUMP-14
  - BUMP-15
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
Outcome: projects declare command jobs in YAML; `bump config check` validates them with file and line; `bump list` and bare `bump` show the effective jobs and their next run times. Nothing executes and nothing is written.

Read first: docs/design.md#jobs-and-configuration and #experience, decision-2 (digests), decision-5. Current code: internal/config/config.go (strict yaml.Node parsing, `Load`, `Input`, `Config`), internal/cli/root.go.

Scope:
- internal/config parses a `jobs` mapping from `bump.yaml`, `.bump.yaml`, and an explicit `--config` file. `jobs` in user config is an error that names the allowed files.
- Fields in this task: `description`, `schedule`, `run`, `cwd`, `env`, `timeout`, `overlap`, `missed`, `enabled`. Any other job field, including later ones such as `prompt`, is an unknown-field error for now.
- Job names match `^[a-z][a-z0-9_-]{0,63}$`. They start with a letter, so a bare number on the command line always means a run ID.
- Keep the existing strictness (no aliases, nulls, or duplicate keys; one document; 1 MiB). Error format: `config PATH:LINE: jobs.NAME.FIELD: message`.
- `run`: a non-empty YAML string (runs as `/bin/sh -c`) or a non-empty list of strings (exact argv). `schedule`: `schedule.Parse` and its suggestion. `timeout`: `schedule.ParseDuration`, at least 1s, or `none`; default 1h. `overlap`: `skip`|`queue`, default `skip`. `missed`: `run_once`|`skip`, default `run_once`. `env`: string values only (hint: quote numbers); names match `[A-Za-z_][A-Za-z0-9_]*`; the `BUMP_` prefix is reserved. `cwd`: relative to the project root or absolute; it need not exist yet.
- Merge by name: a `.bump.yaml` job replaces a shared job entirely, except an entry containing only `enabled`, which toggles the shared job. An `enabled`-only entry with no shared job is an error.
- Provenance per job: `source` (absolute file path), `shared` (from `bump.yaml` or `--config`), `overrides` (a local entry replaced or toggled a shared job).
- Digest: `"sha256:" + hex(SHA-256(canonical JSON))` over explicitly set execution fields only, with sorted keys and unset fields omitted. Exclude `description` and `enabled`. Use the normalized schedule string and normalized durations, so reformatting YAML or editing a description never changes it. Omitting unset fields keeps old approvals valid when later tasks add optional fields.
- Create internal/app with the shared operation: given `config.Input` and `now`, return the project (path, timezone) and jobs sorted by name, each with `next_at` (nil when manual or disabled). Interval jobs show `now + interval` until the scheduler stores their phase in a later task.
- `bump list` (alias `ls`) prints columns JOB, SCHEDULE (`manual` when absent), NEXT (`Fri 09:00 (in 14h)` in the project timezone, `-` when manual, `disabled`), and COMMAND (truncated to fit 80 columns), then one summary line with job and override counts. With no jobs it prints a ready-to-paste YAML example and exits 0.
- Bare `bump` and `bump status`: a project line (name, path, timezone) followed by the same table, and the factual line "Bump does not run jobs yet." Remove the old bootstrap text.
- `bump list --json`: `{"schema_version":1,"project":{"path","timezone"},"jobs":[{"name","description","schedule","next_at","run","cwd","env_keys","timeout","overlap","missed","enabled","source","shared","overrides","digest"}]}`. `schedule` and `next_at` are null when absent; `next_at` is RFC 3339 in the project timezone; `run` is a string or array; `env_keys` lists names only; `timeout` is a duration string or `none`. Never print env values anywhere.
- `bump config check` validates jobs and reports how many it found.
- `bump init` writes a commented example job that is valid once uncommented, replacing the "later release" comment.
- Add examples/jobs.yaml covering every field in this task. Document job fields in docs/configuration.md and update README's status.

Non-goals: execution, state, prompts and agents, hooks, approval checks, the scheduler, YAML writing, cron import.

Tests (critical only):
- internal/config `TestLoadJobs`: table of valid and invalid manifests, including whole-job override, `enabled`-only toggle, toggle with no shared job, `jobs` in user config, an invalid job name (`1tests`), an unknown field reported with file and line, an invalid schedule with its suggestion, a non-string env value, and a `BUMP_` env name.
- internal/config `TestJobDigest`: unchanged by key order, comments, `description`, and `enabled`; changed by `run`, `schedule`, `env`, `cwd`, and `timeout`.
- internal/cli `TestListJSON`: documented shape, no env values in output, and the project tree byte-identical before and after.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestLoadJobs|TestJobDigest)$' -count=1 -v` exits 0 and prints `--- PASS` for both tests, covering every case named in the description
- [ ] #2 `task cli:test -- -run '^TestListJSON$' -count=1 -v` exits 0: output matches the documented shape, contains no env value, and the temp project tree is byte-identical afterwards
- [ ] #3 `task cli:build && bin/bump --config examples/jobs.yaml --json list` exits 0 and prints one JSON object whose `jobs` are sorted by name and each carry a `sha256:` digest
- [ ] #4 `task cli:build && bump_ac_home=$(mktemp -d) && bump_ac_project=$(mktemp -d) && git -C "$bump_ac_project" init -q && env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump -C "$bump_ac_project" list` exits 0 and prints guidance containing an example `jobs:` block
- [ ] #5 `task cli:build && bump_ac_home=$(mktemp -d) && bump_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    schedule: every day\n    run: "true"\n' > "$bump_ac_project/bump.yaml" && env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump -C "$bump_ac_project" config check; echo "exit=$?"` prints an error naming `bump.yaml:3` and suggesting `every day at 9am`, then `exit=1`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
