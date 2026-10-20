---
id: NOON-65
title: Interpolate vars_file values into selected job fields
status: To Do
assignee: []
created_date: '2026-10-20 21:04'
updated_date: '2026-10-20 21:12'
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
modified_files:
  - internal/config
  - internal/app
  - internal/cli
  - internal/scheduler/serve.go
  - noon.schema.json
  - examples
  - docs/configuration.md
  - docs/design.md
  - docs/service.md
  - integration
priority: medium
type: feature
ordinal: 22200
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: users need machine- or deployment-specific schedules and paths without duplicating job manifests (a team noon.yaml where one developer wants `weekdays at 9am`; dotfiles-synced user config across machines). Runtime `env_file` (NOON-64) cannot supply these because it is read when a run starts, after scheduling. `.git/noon.yaml` overrides replace a whole job, so changing one field means copying and maintaining the rest. The user requested scoped interpolation rather than implicit expansion throughout YAML.

Outcome: a manifest may name a `vars_file` whose values are substituted into `${NAME}` expressions in an allowlist of job fields when config loads.

```yaml
# noon.yaml (committed)
vars_file: .noon.vars          # gitignored; optional on disk
jobs:
  sync:
    schedule: ${SYNC_SCHEDULE:-every 1h}
    env_file: .env.secrets     # runtime-only, unchanged
    run: ./sync.sh
```

```dotenv
# .noon.vars (this developer only)
SYNC_SCHEDULE=weekdays at 9am
```

A clone without `.noon.vars` runs `sync` every hour; this checkout runs it weekdays at 9am after re-approval.

Settled contract:
- Field: optional top-level `vars_file`, a non-empty string, accepted in user config, `noon.yaml`, `.git/noon.yaml`, and `--config` files. It is per-document, not a layered setting: it supplies values only to jobs defined in the same file. Values never leak to jobs from another file or project. A private job that replaces a shared one uses the private file's `vars_file`; an enabled-only override keeps the shared job's resolution.
- Path: literal (no interpolation or `~`), absolute or relative to the project root like job `cwd` (so a `.git/noon.yaml` entry resolves per worktree). User config resolves from the user project directory. No implicit `.env` or other discovery.
- File: the existing NOON-64 parser and reading rules (strict non-interpolating Compose subset, `NOON_` names reserved, regular file, nonblocking open, ≤ 64 KiB), unchanged. A missing file is allowed and supplies no values, so committed manifests work on fresh clones through `:-` defaults. Present but unreadable, non-regular, oversized, or malformed files fail config loading with the configured path and, for syntax errors, a 1-based line number, never line content or values. The user config's `vars_file` is read whenever user config is validated (every normal load), so a broken one fails loads like any invalid user config.
- Only source: the vars file. The Noon process environment is never an interpolation source. The shell running `noon list`/`noon enable` and the service running `noon serve` have different environments (docs/service.md), and one scheduler serves many projects. Process-env interpolation would show one schedule in inspection while the scheduler ran another, and approval digests would flap between them. Job `env` values and `env_file` contents are not sources either.
- Allowlist: `schedule`, `cwd`, each `env_file` path, `prompt_file`, `timeout`, `cooldown`, `splay`, and `jitter`, in every job source. Expansion applies to parsed YAML string scalars, before each field's existing validation, path rule, and type checks. Raw YAML is never substituted, so a value containing YAML syntax stays one string. Every other field remains literal text, including `env` values, `run`, hooks, prompts, `agent_args`, keys and job names, and policy fields.
- Syntax: `${NAME}` requires NAME in the vars file; an explicitly empty value is substituted as empty and then validated normally. `${NAME:-default}` uses the literal default when NAME is absent or empty, and also works with no `vars_file`. NAME follows the env-name rule `^[A-Za-z_][A-Za-z0-9_]*$`. `$$` yields a literal `$`; a bare `$NAME` or other lone `$` stays literal. Expansion is single-pass: substituted values and defaults are never reinterpreted. Unterminated or malformed braces, invalid names, and other operators (`:?`, `-`, `:+`, nesting) fail. A missing required variable fails with file, line, and YAML path, naming the variable and the `vars_file` (or saying none is configured). Existing post-expansion validation messages are unchanged. Resolved allowlisted values are not secret: inspection shows them and errors may show them. Docs must say not to put secrets in vars files.
- Compatibility: `$$` and `${` in an existing allowlisted value change meaning; docs note it. `noon add` writes flag values verbatim, so an expression passed through a flag is interpolated on load.
- Not exported: vars-file values are never added to a job's, hook's, or Noon's process environment. To pass the same values to a job, list the file in `env_file` too (same format). It is then also read at run start under NOON-64 rules.
- Approval: the digest is computed from resolved values, as with YAML composition. Changing an effective allowlisted value through the vars file or the manifest makes an approved shared job `needs_approval`. Editing an expression, changing `vars_file`, or changing an unused variable without changing resolved values keeps approval. Every digest path uses the same resolution: `Load`, `LoadWithFileOverride`, and `JobFromSource` for `add` and verify. A job approved by `noon add` must also be approved by the next load.
- Reload: each config load rereads the file, including scheduler reloads. Scheduler change detection stamps every configured vars file, including one that was absent or broken at the previous load. Creating, editing, fixing, or deleting it is therefore picked up without restarting `serve`; an affected shared job waits for approval.
- Editing: `add`, `remove`, and crontab import never write resolved values. They preserve `vars_file` and expressions.
- Doctor: one `vars_file` check per configured file whose message is its configured path: `ok` when loaded and `warn` when absent ("using expression defaults"), so path typos are visible. Read or syntax errors already fail config loading.

Precedent: Docker Compose separates interpolation sources (`.env`, `--env-file`) from runtime injection (service `env_file`), but reusing the name "env file" for both is a known source of confusion. Noon names the config-time file `vars_file` and keeps `env_file` for runtime only. Compose also reads the shell environment, which suits a one-shot CLI. Noon omits it because a long-lived service and an interactive shell must resolve identically. Compose treats a missing `.env` as empty, and so does Noon.

Non-goals: process-environment interpolation (a possible later opt-in); interpolating any non-allowlisted field, including `env` values; shell execution, recursive or nested expansion, `~` expansion, or other operators; changes to the env-file content grammar; multiple vars files or a list form; layering vars across files; automatic discovery; a CLI flag; exporting vars to jobs; new service environment management.

Dependencies: none outstanding; builds on completed NOON-64. Preserves decision-2: approval pins resolved "what and when"; file contents stay unpinned only for runtime `env_file`. Supersedes NOON-64's "YAML never expands `$VARS`" only for the allowlisted fields.

Modified-file contract: internal/config/**; internal/app/** for doctor, inspection, and approval integration; internal/cli/** for add/verify digest parity and critical regressions; internal/scheduler/serve.go for reload stamps; noon.schema.json (`vars_file` string; allowlisted fields keep accepting strings); examples/jobs.yaml plus one self-contained examples/** vars-file example; docs/configuration.md, docs/design.md, docs/service.md; integration/** for reload acceptance coverage; this task record. Justify other paths in Implementation Notes first. Implement first, then extend existing public-interface tests. Acceptance runs isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, NOON_STATE_DIR, and project fixtures in temporary directories.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 task cli:test PKG=./internal/config -- -count=1 exits 0. Public loading tests cover every allowlisted field in every job source: a present value, an empty value, an absent value with and without a default, a default with no vars_file, $$ and bare $NAME literals, single-pass substitution (a value containing ${X} or $$ stays literal), malformed expressions and unsupported operators, and normal validation after expansion. A substituted value containing YAML syntax stays one string. A missing required variable fails with file, line, YAML path, and the variable name.
- [ ] #2 task cli:test PKG=./internal/config -- -count=1 exits 0. Critical regressions: env values, run, hooks, prompts, agent_args, and job names keep literal ${X} and $X text; job env and env_file contents and the process environment (a variable set only in the test process) never supply values. vars_file resolves from the project root (including from .git/noon.yaml and for user jobs), applies only to jobs in its own file, and is unaffected by another file vars_file. A missing file supplies no values. Unreadable, directory, FIFO, oversized, and malformed files fail with the configured path and line, without a sentinel value. add/remove round-trips preserve vars_file and expressions.
- [ ] #3 task cli:test PKG=./internal/cli -- -count=1 exits 0. Isolated CLI regressions through cli.Run: config check and list --json show the resolved schedule; an absent required variable fails config check with exit 1; after enable, a resolved schedule change through the vars file and an inline value change each make the shared job needs_approval. An expression rewrite with identical resolution, and a change to an unused variable, keep it approved. A job added with an expression in --schedule is approved on the next load. doctor reports vars_file ok when present and warn when absent. Vars values appear in no job or hook environment.
- [ ] #4 task cli:test PKG=./integration -- -count=1 exits 0. An isolated built-binary scheduler regression: with serve running and an enabled, approved shared job whose schedule comes from vars_file, creating or editing the file to change that schedule is detected without restarting serve and blocks unattended runs as needs_approval. Re-running enable resumes scheduling at the new schedule.
- [ ] #5 noon.schema.json accepts a non-empty string vars_file and rejects other types. examples/jobs.yaml and a self-contained vars-file example validate without personal files or environment: task cli:test PKG=./integration -- -run TestExamplesValidate -count=1 exits 0. docs/configuration.md covers the field, path rule, per-file scope, missing-file behavior, allowlist, syntax, compatibility note, no process environment, the not-for-secrets warning, approval, reload, and the env_file relationship. docs/design.md and docs/service.md match. task ci exits 0.
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
Refinement: rename config_env_file to vars_file, because Compose uses "env file" for both its interpolation file and runtime injection, a known confusion; Noon keeps env_file runtime-only. Drop the process environment as a source: shell and service environments differ by design, so inspection/approval would diverge from scheduling. Drop env values from the allowlist: env_file already sets job env from a file at run time, and interpolating env would pin secrets in digests and make rotation re-approval. To export, list the same file in env_file. Missing vars_file is empty, Compose .env style, so committed manifests work on fresh clones; doctor warns on absence. Approval digests resolved values only (the YAML-composition precedent), so expression-only edits and unused variables do not churn approval. Code facts: scheduler reloads only when stamped config paths change (internal/scheduler/serve.go configPaths/statPaths), so vars files must be stamped; noon add/verify compute digests via config.JobFromSource, which must resolve identically to Load.
<!-- SECTION:NOTES:END -->
