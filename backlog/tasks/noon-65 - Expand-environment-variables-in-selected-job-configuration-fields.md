---
id: NOON-65
title: Expand environment variables in selected job configuration fields
status: To Do
assignee: []
created_date: '2026-10-20 21:04'
updated_date: '2026-10-20 21:06'
labels:
  - config
dependencies: []
references:
  - >-
    https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/
documentation:
  - docs/configuration.md
  - docs/design.md
  - docs/service.md
priority: medium
type: feature
ordinal: 22200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: users need machine- or deployment-specific schedules and paths without duplicating job manifests. Following NOON-64, runtime env files cannot provide schedule values because they are read only when a run starts. The user requested scoped interpolation rather than implicit expansion throughout YAML.

Outcome and scope: support ${VAR} and ${VAR:-default} in job schedule, cwd, env_file (each path), prompt_file, timeout, cooldown, splay, jitter, and env mapping values, in every supported job source. Expand parsed string values before their existing validation; never substitute raw YAML. Keep existing path restrictions, field types, override precedence, and runtime environment precedence.

Contract: add an optional top-level config_env_file string naming an explicit interpolation source. Read it during config loading, before expanding job fields. Its path is literal (no interpolation or tilde expansion), absolute or relative to the project root; user jobs use the user project directory. No implicit .env discovery. Use the existing strict, bounded env-file parser and regular-file safety checks; a configured file is required, and missing, unreadable, oversized, non-regular, or malformed files fail config loading with secret-safe diagnostics. This file uses the existing non-interpolating dotenv subset, not the full Compose grammar.

Source scope: config_env_file supplies interpolation values for jobs defined in the same YAML document, across shared, private, explicit --config, and user job sources; it does not leak into jobs from other documents/projects. Preserve whole-job override semantics. Precedence, highest first: the loading Noon process environment (including explicitly empty values), config_env_file values, then expression defaults. Job env values and job env_file contents are not interpolation sources. Config-file variables are not automatically exported to jobs or hooks or installed into the process environment; jobs may explicitly reference them through env values. Read the config env file afresh on each config load, including scheduler reloads; never promise that an already-running process inherits subsequent shell environment changes. ${VAR} requires a present variable (an explicitly empty value remains empty and undergoes ordinary field validation); ${VAR:-default} uses its literal default when absent or empty. Variable names follow the existing environment-name grammar. Expansion is single-pass; inserted values are never recursively interpreted. Support $$ as a literal dollar escape in eligible fields; bare $VAR stays literal. Malformed braced expressions and unsupported operators fail with field location, without echoing resolved values. Existing validation errors for expanded values must likewise not disclose their contents.

Resolve during config loading for inspection, approval, and execution. Approval must cover both the interpolation definition and resolved execution values: edits to the definition or changes to effective execution values must not bypass approval. Do not write resolved values back into manifests during add/remove/edit operations. Preserve existing env-value hiding in inspection; document that resolved non-secret fields such as schedules and paths remain visible and should not contain secrets. The explicit config file gives the shell and scheduler a shared on-disk source, but process-environment overrides can still differ. The shell and service can inherit different environments; document this explicitly and how the existing service obtains its environment rather than promising live updates to a running process environment.

Non-goals: expanding run strings/argv, hooks, inline prompts, agent arguments/profiles, YAML keys/job names, policy fields (including overlap and missed), or any other field; shell execution, recursive interpolation, tilde expansion, changes to the dotenv content grammar, automatic .env discovery, multiple config env files, a CLI --env-file flag, or new service environment management.

Dependencies: none outstanding; builds on completed NOON-64. Preserve decision-2 approval boundaries. This intentionally supersedes the no-YAML-expansion statement in NOON-64 only for the allowlisted fields, not its env-file content format.

Modified-file contract: internal/config/**; internal/app/** and internal/cli/** only for approval/inspection integration and critical regressions; noon.schema.json; examples/jobs.yaml; docs/configuration.md; docs/design.md; docs/service.md; integration/** only for config-file reload acceptance coverage; examples/** only for a self-contained config-env-file example; this task record. Justify expansion in Implementation Notes before touching other paths. Implement first, then extend existing public-interface tests for parsing, secret-safe errors, approval invalidation, and preservation of literal execution text. Acceptance tests must isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, NOON_STATE_DIR, and project fixtures in temporary directories.

Example (proposed, implemented by this task):
```yaml
config_env_file: .env.noon
jobs:
  sync:
    schedule: ${SYNC_SCHEDULE:-every 1h}
    env_file: .env.secrets
    run: ./sync.sh
```
`.env.noon` can contain `SYNC_SCHEDULE=weekdays at 9am`; `.env.secrets` remains runtime-only. Changing an effective schedule or other resolved execution value through the config env file invalidates shared-job approval. Unused file values do not need to invalidate every job. Preserve expressions and config_env_file when editing manifests.

Precedent: Docker Compose separates .env/CLI --env-file interpolation sources from service env_file runtime injection. Noon intentionally uses an explicit YAML source instead of automatic discovery so inspection and a multi-project background service select the same project-local file.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test PKG=./internal/config -- -count=1 exits 0. Public loading tests cover the allowlisted fields across supported sources, present/missing/empty values, literal defaults, $$ escaping, nonrecursive values, malformed expressions, and normal post-expansion validation. A value containing YAML syntax remains a string and cannot add YAML keys. Missing required variables and invalid resolved values produce located errors without disclosing sentinel secret values.
- [ ] #2 task cli:test PKG=./internal/config -- -count=1 exits 0. Critical regressions prove commands, hooks, prompts, keys, and other excluded fields retain literal ${VAR}/$VAR text; job env and env_file contents never supply interpolation variables; existing runtime env-file semantics and path restrictions are unchanged; editing jobs preserves interpolation expressions instead of persisting environment values.
- [ ] #3 task cli:test PKG=./internal/cli -- -count=1 exits 0. Isolated CLI regressions exercise config check/list/enable and demonstrate that a valid environment-driven schedule is used, an absent required variable fails config loading, and changing either the expression or resolved execution values invalidates shared-job approval. Env values remain absent from human/JSON inspection and diagnostic errors.
- [ ] #4 task cli:test PKG=./integration -- -run TestExamplesValidate -count=1 exits 0. Schema and documentation cover the exact field allowlist, syntax/default/escape behavior, load timing and environment source, shell-versus-service differences, approval behavior, and separation from runtime env files. Examples use defaults so they validate without personal environment variables. task ci exits 0.
- [ ] #5 task cli:test PKG=./internal/config -- -count=1 and task cli:test PKG=./internal/cli -- -count=1 exit 0. Critical regressions exercise config_env_file path resolution and per-document/project isolation, process > file > default precedence (including an empty process override), rejection of invalid/unreadable/missing/non-regular/oversized files without leaking values, and no automatic runtime export or mutation of the process environment. With no process override, editing the file changes the schedule on the next config load and makes an approved shared job need approval; changing an unused variable alone does not. Existing runtime env_file behavior is unchanged.
- [ ] #6 task cli:test PKG=./integration -- -count=1 exits 0. An isolated scheduler regression verifies that a config_env_file change affecting an approved shared schedule is detected without restarting serve and blocks unattended execution pending approval. Schema, self-contained examples, configuration/design/service docs describe config_env_file source scope, explicit required-file behavior, parser subset, precedence, reload timing, secret-safe errors, and separation from job env_file; examples need no personal files or environment.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
