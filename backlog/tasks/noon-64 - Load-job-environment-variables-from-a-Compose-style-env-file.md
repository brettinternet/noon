---
id: NOON-64
title: Load job environment variables from a Compose-style env file
status: Done
assignee: []
created_date: '2026-10-17 17:55'
updated_date: '2026-10-20 20:49'
labels:
  - config
  - runner
dependencies: []
documentation:
  - docs/configuration.md
  - docs/design.md
  - docs/service.md
  - docs/hooks.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
modified_files:
  - internal/config/jobs.go
  - internal/config/edit.go
  - internal/config/envfile.go
  - internal/config/envfile_test.go
  - internal/config/jobs_test.go
  - internal/app/run.go
  - internal/app/doctor.go
  - internal/app/inspect.go
  - internal/cli/run_test.go
  - noon.schema.json
  - examples/jobs.yaml
  - docs/configuration.md
  - docs/design.md
  - docs/service.md
  - docs/hooks.md
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: jobs often need values that must not be committed (API keys, tokens, webhook URLs). A common pattern, popularized by Docker Compose, is a gitignored `.env` file in the checkout that a script or secret manager generates. Today Noon jobs can only get such values through committed or private `env:` YAML, the scheduler's inherited environment (which the service deliberately keeps minimal), or a shell wrapper such as `set -a; . ./.env`. `prompt` and `prompt_file` jobs have no shell wrapper, so they cannot load a file at all. Promoted at the user's request after rejecting a user-config-only design: an approved job already runs arbitrary code as the user, and decision-2 point 4 already accepts that digests cover referenced paths, not file contents, so restricting where `env_file` may appear would block the pattern without adding a security boundary.

Outcome: a job may name one or more env files that are read each time the job starts:

```yaml
jobs:
  sync:
    run: ./sync.sh
    env_file: .env      # gitignored, generated; relative to the project root
    env:
      LOG_LEVEL: debug  # overrides a LOG_LEVEL from the file
  report:
    run: ./report.sh
    env_file: [.env, /Users/me/.config/secrets/report.env]  # later files override earlier
```

```sh
# .env
export API_URL=https://api.example.com   # comment
TOKEN="abc\tdef"
LITERAL='$NOT_EXPANDED'
```

Settled contract:
- `env_file` is an optional job field, accepted in every file that accepts jobs (user config, `noon.yaml`, `.git/noon.yaml`, `--config`). Its value is a path string or a non-empty list of path strings, as in Compose. Each path is non-empty and follows the `cwd` rule: absolute, or relative to the project root regardless of which file defines the job (so `.env` or `./.env` in `.git/noon.yaml` means `<checkout>/.env`, resolved per worktree). YAML never expands `~` or `$VARS`. For user jobs the project root is the user project directory. Absolute paths let private config share one file across all worktrees; a committed absolute path is not portable, which is the user's choice as with `cwd`.
- `config check` validates the field, not the file: a missing or invalid file is a run-time error, as with `prompt_file`.
- The ordered list of paths (a single string normalizes to a one-item list) is an execution field in the approval digest. File contents are not, matching decision-2 point 4; docs say so plainly next to the field.
- Read timing: `RunSpec` opens every listed file, in order, when each run starts (foreground, detached, scheduled, triggered, webhook, and MCP-started runs alike), so regenerating or rotating it needs no restart. Open nonblocking, require a regular file, and read at most 64 KiB per file (reuse the `prompt_file` reading pattern). Every listed file must exist; there is no optional form. No owner or permission-mode checks.
- Failure: a missing, unreadable, non-regular, oversized, or invalid file fails the run before the process starts with exit code 125 and an error naming the offending `env_file` path as configured and, for syntax errors, the 1-based line number. Errors never include line content or values.
- Precedence, lowest to highest: inherited environment < `env_file` entries in list order (a later file overrides an earlier one) < job `env:` < `NOON_*` metadata.
- Hooks already receive the run's environment; they receive the `env_file` values loaded for that run. When the run failed before its environment was built (including an `env_file` failure), hooks get the environment without file values, as other pre-start failures do today. A hook never rereads the file.
- `noon doctor` adds one `job NAME env_file` check per listed file, like the `prompt_file` check, whose message is that file's configured path; it fails (exit 1) for any read or syntax error and never prints values. Command resolution in doctor uses the file's variables when it loads (a file may set `PATH`).
- `app.Job` gains `env_file`: the configured paths as a list of strings (a single string becomes a one-item list), `null` when unset, so `noon list --json`, MCP, and the HTTP API show it. `env_keys` keeps listing only `env:` keys; file keys and values are never printed by Noon. Child stdout/stderr remain raw logs; Noon does not redact what a job prints.

File format: a strict subset of Docker Compose's documented `.env` syntax (docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/#env-file-syntax; reference implementation compose-go `dotenv/parser.go`). Every file Noon accepts must yield the same variables Compose would; anything outside the subset is an error rather than a guess. Implement in Go without a new dependency.
- UTF-8 text; NUL is an error. Lines end in LF; a trailing CR is removed. Blank lines and lines whose first non-whitespace character is `#` are ignored. Leading whitespace is ignored.
- Statement: optional `export` followed by whitespace, then `KEY`, optional spaces or tabs, `=`, optional spaces or tabs, then the value. KEY must match the existing env-name rule (`^[A-Za-z_][A-Za-z0-9_]*$`) and must not start with `NOON_`. A line without `=`, the `:` delimiter, and a bare `KEY` (Compose's inherit form) are errors.
- Unquoted value: runs to end of line; a space followed by `#` starts a comment; trailing whitespace is trimmed; backslashes are literal. `$` is an error (Compose would interpolate). An empty value is the empty string.
- Single-quoted value: literal; a backslash followed by `'` yields `'`, and a backslash followed by any other character yields both characters unchanged. `$` is allowed and literal.
- Double-quoted value: escapes `\n`, `\r`, `\t`, `\"`, and `\\` are decoded; any other backslash sequence is an error. `$` is an error (Compose would interpolate; use single quotes for a literal `$`).
- After a closing quote, only whitespace and an optional `#` comment may follow on that line. An unterminated quote or a value spanning lines is an error.
- Duplicate keys: the last assignment wins, as in Compose.

Scope: job field parsing, validation, and digest (`internal/config/jobs.go`, `edit.go` field round-trip); the env-file parser (`internal/config/envfile.go`); loading in `RunSpec`, which overlays file values on the inherited environment before calling `JobEnvironment`, so `JobEnvironment`'s signature and its other callers (cli/run.go, scheduler/serve.go, hooks.go) stay unchanged (`internal/app/run.go`); doctor check; inspect JSON field; schema; `examples/jobs.yaml` (uses every field); docs: configuration.md (field table, paths list, an "Env files" section with the format and a Compose `.env` example, the digest note), design.md (execution environment order, digest fields, security model sentence), service.md (job environment: point at `env_file` for values the scheduler lacks), hooks.md (hooks see the run's file values).

Tests (critical only): parser table in `internal/config/envfile_test.go` covering every accepted form and every rejected form, asserting errors carry the line number and never the line's value text (parsing; subtle Compose-divergence regressions); `FuzzEnvFileParse` per docs/development.md (free-form parser: no panics, accepted keys always valid, values never contain NUL); `env_file` validation of the string and list forms and digest coverage, including list order, in `internal/config/jobs_test.go` (authorization boundary); precedence and the pre-start 125 failure through `cli.Run` in `internal/cli/run_test.go` (state transition: the process must not start and the run must record 125). Extend existing tests and helpers.

Non-goals: Compose variable interpolation, multi-line values, `:` delimiters, or inherit-only lines; Compose's long form (`{path, required: false}`) and optional files; an `env_file` default at project or user level; owner or permission checks; hook-level or user-level env files; supplying webhook `secret_env` from an env file (webhook secrets still come from the scheduler's startup environment); `noon add`/`import` flags; redacting child output.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` (call it T) and a temp Git repo whose noon.yaml defines `show` (run `touch started; printf '%s|%s|%s|%s|%s\n' "$FROM_FILE" "$BOTH" "$INHERITED" "$TAB" "$LIT"`, `env_file: [.env, T/override.env]` with T written as its absolute path, `env: {BOTH: yaml}`), whose gitignored `.env` contains the lines `export FROM_FILE=file # comment`, `BOTH=file`, `INHERITED=file`, `TAB="a\tb"`, and `LIT='$X'`, and where `T/override.env` contains `INHERITED=second`: `FROM_FILE=shell INHERITED=shell bin/noon run show` exits 0 and prints exactly `file|yaml|second|a<TAB>b|$X` (a real tab) on stdout. Changing the job to `env_file: ./.env` (string form) and rerunning prints `file|yaml|file|a<TAB>b|$X`.
- [x] #2 In the same environment with `env_file: .env`, with user config `on_success` set to a hook that writes `$FROM_FILE` to an absolute path under T, `bin/noon run show` leaves `file` in that path. After `bin/noon enable` and starting `bin/noon serve` in the background with the same environment, `bin/noon run show --detach` produces a run whose `bin/noon logs` output starts `file|`; after changing the `.env` line to `FROM_FILE=rotated` and without restarting serve, a second `--detach` run logs `rotated|`. Stop serve afterwards.
- [x] #3 In the same environment with `env_file: .env`, each of these makes `bin/noon run show` exit 125 without creating `started`, with stderr naming the `env_file` path `.env`, and `bin/noon history show --json` reporting the latest run failed with exit code 125: `.env` absent; `.env` is a directory; `.env` larger than 64 KiB; and `.env` set to each single invalid line `TOKEN=hunter2 $X`, `TOKEN="hunter2$X"`, `TOKEN="hunter2`, `TOKEN="hunter2\q"`, `TOKEN='hunter2'junk`, `TOKEN:hunter2`, `TOKEN hunter2`, `NOON_X=hunter2`, `1TOKEN=hunter2`. For every invalid-line case stderr includes `line 1`, and neither stderr nor `bin/noon doctor --json` output contains `hunter2`; `bin/noon doctor` exits 1 with a failed check named `job show env_file`. With a valid `.env` restored and `env_file: [.env, T/missing.env]`, `bin/noon run show` exits 125 with stderr naming `T/missing.env`, and doctor reports the `.env` check ok and the `T/missing.env` check failed.
- [x] #4 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path under `jobs.show.env_file` for `env_file: ""`, `env_file: []`, `env_file: [""]`, `env_file: 1`, `env_file: [1]`, and `env_file: {path: .env}`, and exits 0 for `env_file: .env` while `.env` is absent, for `env_file: /tmp/x.env`, and for `env_file: [.env, /tmp/x.env]`. With a valid `.env` and `env_file: .env`, `bin/noon list --json` reports `show` with `env_file` [".env"] and `env_keys` ["BOTH"], contains no value from `.env`, and reports `env_file` null for a job without it. After `bin/noon enable` with `env_file: [.env, T/override.env]`, editing only file contents keeps `show` `approved`, while reordering the list or changing a path makes it `needs_approval`.
- [x] #5 `task cli:test PKG=./internal/config -- -count=1` exits 0 and covers: every accepted form in the settled file format (export prefix, spaces around `=`, empty value, unquoted inline comment versus `#` without a preceding space, trailing CR, single-quoted `$` and backslash rules, each double-quoted escape, trailing comment after a closing quote, last duplicate wins) yields the Compose value; every rejected form errors with its line number and without its value text; `FuzzEnvFileParse` seeds run; env_file string and list validation and digest coverage (paths and their order change the digest, contents cannot). `task cli:test PKG=./internal/cli -- -count=1` exits 0 and covers precedence (inherited < env_file entries in order < env < NOON_*) and the pre-start 125 failure through `cli.Run`.
- [x] #6 noon.schema.json accepts a string or a non-empty array of strings for `env_file` and rejects other types; examples/jobs.yaml uses `env_file` and `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior, including path resolution from `.git/noon.yaml`, list order, the format rules, and that the digest covers paths, not contents. `task ci` exits 0.
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
Refinement (promotion): replaced the user-config-only, mode-0600, strict KEY=VALUE design with a per-job env_file at the user's request. Rationale: approved jobs already run arbitrary code as the user and decision-2 point 4 already scopes digests to referenced paths, so placement restrictions added no boundary. Format is a strict subset of Compose .env syntax (verified against compose-go dotenv/parser.go): `$` is rejected outside single quotes so accepted files never depend on interpolation. Own parser chosen over importing compose-go to avoid a dependency and its silent empty-string interpolation. Code facts: RunSpec already reads prompt_file at run start and its errors become exit 125 in internal/cli/run.go and internal/scheduler/serve.go; hooks receive spec.Env, so file values reach hooks without hook changes; app.Job feeds list --json, MCP, and the HTTP API. Follow-ups not in scope: a user-level env file for hooks; webhook secret_env from a file.

Refinement: at the user's request, env_file accepts a string or a non-empty list (later files override earlier) and paths follow the cwd rule (absolute or project-relative) instead of prompt_file's relative-only rule, so private config can share one file across worktrees. Job paths resolve against the project root even in .git/noon.yaml. Compose's {path, required: false} long form stays out of scope until needed.

Modified-file expansion: internal/cli/root_test.go has an exact list JSON field-set assertion. The required new env_file field makes it fail; update that assertion to require env_file and assert null when unset, preserving its strict shape check. Implementation and direct AC#1–4 verification are complete. Subagent execution/review is unavailable because the available model registry and configured modelScope conflict; performing the single proportional review directly.

AC#1: task cli:build; bun /tmp/noon-64-acceptance.pBTy6z/acceptance.ts -> PASS. Isolated temporary Git checkout and HOME/XDG/state; list and string forms produced exact expected output including real TAB and literal $X.
AC#2: same executable acceptance harness, enable/serve/run --detach/logs -> PASS. Success hook received file, detached runs changed from file to rotated without restarting serve; owned serve stopped and awaited.
AC#3: same executable acceptance harness, run/history/doctor -> PASS for absent, directory, oversized and all nine invalid-line files. All runs recorded failed/125 without started; errors named configured paths/line 1 without hunter2. Multiple-file doctor checks reported first ok and missing second failed. CLI regression additionally covers FIFO, unchanged hook snapshot and no partial file values on failure.
AC#4: same executable acceptance harness, config check/list --json/enable -> PASS for all six invalid field shapes and all three valid shapes without file existence. Inspection normalizes paths and keeps env_keys YAML-only; unset is null. Content changes retain approval; path/order changes revoke it.
AC#5: task cli:test PKG=./internal/config -- -count=1; task cli:test PKG=./internal/cli -- -count=1 -> PASS. Parser accepted/rejected forms, line-only secret-safe errors, normalized digest paths/order, round-trip, precedence and durable pre-start failures covered. FuzzEnvFileParse seeds and a 5-second fuzz run passed (229223 executions).
AC#6: TestEnvFileDefinition validates schema valid/invalid forms; task cli:test PKG=./integration -- -run ^TestExamplesValidate$ -count=1 -> PASS. Examples and all scoped docs updated. task ci -> PASS, repeated on final code commit f459cb2649c3d825ab036b016fe809c4e6ec1763; full log /tmp/noon-64-acceptance.pBTy6z/ci-final-code.log.
Review: one direct risk-focused pass checked parser semantics against compose-go dotenv/parser.go, approval fields, run/doctor integration, hook fallback and docs. Corrected paired single-quote backslashes and rejection of non-space/tab leading value whitespace that Compose would trim. No remaining item-scoped defects found. Independent child review unavailable due model registry/modelScope mismatch. LSP jobs.go clean; initial envfile/run diagnostics unknown, supplemented by build/vet/Staticcheck/full tests/race.
Delivery: f459cb2 feat(config): load per-job env files, fast-forward merged into main while preserving concurrent draft-18 commit. Staged checks passed. Verified session-owned worktree receipt; Worktrunk removed feat/noon-64-env-file and its branch, and Herdr workspace wJB disappeared. No retained worktree/workspace, no push, no outstanding blocker or resumable implementation step. Temporary acceptance/CI artifacts retained for evidence; test scheduler stopped. No tests deleted, skipped or weakened; exact JSON-shape test updated to require the new field and null default.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented per-job env_file strings/lists with strict secret-safe Compose-subset parsing, fresh bounded reads on every run, precedence and hook snapshots, approval path digests, doctor and inspection support, schema/examples/docs and critical regressions. All six ACs verified; full CI passes on f459cb2, merged to main; owned worktree, branch and Herdr workspace cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
