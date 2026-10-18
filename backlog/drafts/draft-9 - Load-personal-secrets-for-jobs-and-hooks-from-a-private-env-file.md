---
id: DRAFT-9
title: Load personal secrets for jobs and hooks from a private env file
status: Draft
assignee: []
created_date: '2026-10-17 17:55'
updated_date: '2026-10-18 16:38'
labels:
  - config
  - security
  - service
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft, not ready work. Resolve the security and lifecycle questions below before promotion; then turn the proposed contract and acceptance direction into exact local commands with expected results.

Why: hooks and jobs that need credentials (chat webhook URLs, ntfy tokens, API keys) and webhook `secret_env` values are told to read them from "the scheduler's environment". docs/hooks.md and examples/integrations/notifications/README.md say to put them in "the service environment for noon service install". But docs/service.md (correctly) says the unit copies only PATH and XDG_CONFIG_HOME, so installation does not copy arbitrary credentials from the invoking shell. User-managed service overrides and command-owned secret loading remain available workarounds. Today's workarounds: each hook fetches its own secret (keychain, op, pass), `env:` in each repo's .git/noon.yaml, a Linux-only systemd EnvironmentFile drop-in, or Docker --env-file. Provider-owned config (Apprise via Tell) already works and stays the recommended path for notifications. This task covers credentials that must arrive as environment variables.

Outcome: one user-owned file supplies secret environment variables to every run and hook, foreground or scheduled, with no secret in YAML or in the service unit:

```yaml
# ~/.config/noon/config.yaml
env_file: /Users/me/.config/noon/secrets.env
```

```sh
# /Users/me/.config/noon/secrets.env  (mode 0600)
CHAT_WEBHOOK_URL=https://hooks.example.com/T000/B000/xyz
GITHUB_WEBHOOK_SECRET=a-high-entropy-value
```

Proposed contract (not implemented):
- `env_file` is an absolute path setting allowed only in user config and an explicit --config file. noon.yaml and .git/noon.yaml reject it with the existing wrong-file error, so an automatically discovered manifest cannot choose which secrets load. An explicit --config file is operator-selected but may still be repository-controlled; its eligibility and approval rules need the decision below. YAML never expands `~` or `$VARS`. No flag or environment variable; `noon config` shows the path, never values.
- The file must be a regular file (symlinks resolved), owned by the current user, with no group or other permission bits. Anything else is an error naming the path and the problem.
- Strict format, so there is no dotenv-dialect ambiguity: UTF-8; blank lines and lines starting with `#` are ignored; every other line is `KEY=VALUE`. KEY matches the existing env-name rule and must not be `NOON_*`. VALUE is literal to end of line: no quotes, escapes, `export`, interpolation, or trimming beyond the line ending. A value starting with `"` or `'` is rejected with a hint (prevents silently keeping quotes copied from dotenv files). Duplicate keys are errors. Maximum 64 KiB. Errors give the line number but never echo line content.
- Precedence for runs and hooks: inherited environment < env_file < job `env:` < NOON_* variables. The file is read fresh when each run or hook starts, so rotating a secret needs no restart. A pre-run read failure fails the run before starting with exit 125 and a clear error. A later pre-hook read failure must preserve the already-recorded job result and be surfaced separately as a hook error. Webhook credential precedence and rotation are unresolved below; do not reuse job env layering implicitly at the listener authentication boundary.
- Noon-generated metadata and diagnostics must not print values through list, status, config, doctor, JSON, MCP, or errors. Job and hook stdout/stderr remain raw logs: this feature cannot prevent a child from printing its own environment and does not promise log redaction. `noon doctor` checks presence, ownership, mode, and syntax and reports the key count only. `noon config check` validates the setting, not the file contents (consistent with prompt files).
- Every job in every enabled project receives these variables, including approved shared jobs. Docs say so plainly: approving a repository already lets its scripts read your environment and home directory.
- The service unit is unchanged: it already carries XDG_CONFIG_HOME, so the scheduler finds user config and the file path. No secrets are copied into launchd or systemd units.

Scope: user-config key and placement rule; env-file parser; layering in JobEnvironment/RunSpec for foreground runs, scheduled runs, and hooks; webhook secret resolution; doctor check; docs: configuration.md (key table, env file section), hooks.md and examples/integrations/notifications/README.md (replace the "service environment" advice), service.md (how secrets reach service-managed runs), webhooks.md (secret_env sources), docker.md (env_file vs docker --env-file), design.md (execution environment, security model).

Tests (critical only): placement rejection in noon.yaml and .git/noon.yaml (authorization boundary); ownership and mode refusal (security); parser rules, including quote rejection, reserved names, duplicates, and never echoing content (parsing; add a fuzz target per docs/development.md); precedence among inherited, env_file, job env, and NOON_*; runs and hooks receive the expected file variables when the file is unchanged; rotation between run and hook reads is exercised explicitly, including a hook-only read failure that cannot rewrite the job result; rotation is picked up by the next run without restart.

Non-goals: secret-manager integrations (keychain, 1Password, Vault); encryption at rest; per-project or per-job env files; reading dotenv dialects (quotes, export, interpolation); copying environment into service units; a `noon service restart` command; changing how Docker passes environment.

Promotion questions:
- Resolve allowing env_file in an explicit --config file versus the claim of user-owned-only secret selection. Document the trust boundary; do not silently broaden allowed files.
- Webhook authentication currently snapshots the scheduler environment on config reload (internal/scheduler/serve.go webhookSecretSnapshots); job env is not an authentication secret source. Decide whether file edits themselves trigger webhook-secret reload, how a bad rotation fails closed or retains a last-good secret, and keep repository job env out of secret resolution.
- Validate ownership, mode, regular-file type, and bounded content on the same opened descriptor; a path-only check followed by reopening can race replacement. Choose how to test ownership refusal without requiring root.
- docs/webhooks.md uses `secret_env: NOON_GITHUB_WEBHOOK_SECRET`, which the file cannot supply because NOON_* is reserved; rename that example to GITHUB_WEBHOOK_SECRET (proposed) or allow non-reserved NOON_ names in the file; whether to also accept the setting in .git/noon.yaml for per-repo secrets (currently rejected: repo-private but per-checkout, and .git/noon.yaml env: already covers it); exact doctor wording; whether `noon list --json` should expose env_file key names (current proposal: no).

Dependencies: none. Related: NOON-51 and NOON-52 (independent), NOON-50 (user jobs read the same user config).

Modified-file contract (proposed): internal/config/config.go, internal/config/config_test.go, internal/config/envfile.go, internal/config/envfile_test.go, internal/app/run.go, internal/app/hooks.go, internal/app/doctor.go, internal/cli/root.go, internal/cli/run.go, internal/cli/run_test.go, internal/scheduler/serve.go, internal/scheduler/serve_test.go, docs/configuration.md, docs/hooks.md, docs/service.md, docs/webhooks.md, docs/docker.md, docs/design.md, examples/integrations/notifications/README.md, plus this task record.

Acceptance direction: with isolated HOME/XDG/state under mktemp -d and a fake hook, a scheduled and a foreground run plus their hooks see a file variable; job env overrides it; a 0644 or foreign-owned file fails the run with exit 125 and no value in output; env_file in noon.yaml or .git/noon.yaml is a config error; editing the file changes the next run's value without restarting serve; a webhook secret_env supplied only by the file authenticates; `task ci` exits 0.
<!-- SECTION:DESCRIPTION:END -->

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
Refinement evidence: internal/app/hooks.go commits FinishRun before starting a hook, so a later secret-read error cannot change the job outcome. docs/design.md preserves raw child output, not automatic secret redaction. Draft remains unpromoted because explicit-config trust and webhook rotation need a settled contract. NOON-58 uses a protected URL file independently and does not depend on this draft.
<!-- SECTION:NOTES:END -->
