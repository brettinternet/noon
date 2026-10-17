---
id: DRAFT-9
title: Load personal secrets for jobs and hooks from a private env file
status: Draft
assignee: []
created_date: '2026-10-17 17:55'
updated_date: '2026-10-17 17:55'
labels:
  - config
  - security
  - service
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Draft. Review the settled contract below, then promote. It already has outcome, scope, non-goals, modified-file contract, and acceptance direction; turn the acceptance direction into exact commands on promotion.

Why: hooks and jobs that need credentials (chat webhook URLs, ntfy tokens, API keys) and webhook `secret_env` values are told to read them from "the scheduler's environment". docs/hooks.md and examples/integrations/notifications/README.md say to put them in "the service environment for noon service install". But docs/service.md (correctly) says the unit copies only PATH and XDG_CONFIG_HOME, so a service-managed scheduler has no supported way to receive a secret. Today's workarounds: each hook fetches its own secret (keychain, op, pass), `env:` in each repo's .git/noon.yaml, a Linux-only systemd EnvironmentFile drop-in, or Docker --env-file. Provider-owned config (Apprise via Tell) already works and stays the recommended path for notifications. This task covers credentials that must arrive as environment variables.

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

Settled contract:
- `env_file` is an absolute path setting allowed only in user config and an explicit --config file. noon.yaml and .git/noon.yaml reject it with the existing wrong-file error, so repository content can never choose which secrets load. YAML never expands `~` or `$VARS`. No flag or environment variable; `noon config` shows the path, never values.
- The file must be a regular file (symlinks resolved), owned by the current user, with no group or other permission bits. Anything else is an error naming the path and the problem.
- Strict format, so there is no dotenv-dialect ambiguity: UTF-8; blank lines and lines starting with `#` are ignored; every other line is `KEY=VALUE`. KEY matches the existing env-name rule and must not be `NOON_*`. VALUE is literal to end of line: no quotes, escapes, `export`, interpolation, or trimming beyond the line ending. A value starting with `"` or `'` is rejected with a hint (prevents silently keeping quotes copied from dotenv files). Duplicate keys are errors. Maximum 64 KiB. Errors give the line number but never echo line content.
- Precedence for runs and hooks: inherited environment < env_file < job `env:` < NOON_* variables. The file is read fresh when each run or hook starts, so rotating a secret needs no restart. If it is missing or invalid, the run fails before starting with exit 125 and a clear error. Webhook `secret_env` resolves from the same layered environment whenever the scheduler (re)loads config.
- Values are secret by default: never printed by list, status, config, doctor, JSON, MCP, logs, or errors. `noon doctor` checks presence, ownership, mode, and syntax and reports the key count only. `noon config check` validates the setting, not the file contents (consistent with prompt files).
- Every job in every enabled project receives these variables, including approved shared jobs. Docs say so plainly: approving a repository already lets its scripts read your environment and home directory.
- The service unit is unchanged: it already carries XDG_CONFIG_HOME, so the scheduler finds user config and the file path. No secrets are copied into launchd or systemd units.

Scope: user-config key and placement rule; env-file parser; layering in JobEnvironment/RunSpec for foreground runs, scheduled runs, and hooks; webhook secret resolution; doctor check; docs: configuration.md (key table, env file section), hooks.md and examples/integrations/notifications/README.md (replace the "service environment" advice), service.md (how secrets reach service-managed runs), webhooks.md (secret_env sources), docker.md (env_file vs docker --env-file), design.md (execution environment, security model).

Tests (critical only): placement rejection in noon.yaml and .git/noon.yaml (authorization boundary); ownership and mode refusal (security); parser rules, including quote rejection, reserved names, duplicates, and never echoing content (parsing; add a fuzz target per docs/development.md); precedence among inherited, env_file, job env, and NOON_*; a hook receives the same file variables as its run (regression); rotation is picked up by the next run without restart.

Non-goals: secret-manager integrations (keychain, 1Password, Vault); encryption at rest; per-project or per-job env files; reading dotenv dialects (quotes, export, interpolation); copying environment into service units; a `noon service restart` command; changing how Docker passes environment.

Promotion questions: docs/webhooks.md uses `secret_env: NOON_GITHUB_WEBHOOK_SECRET`, which the file cannot supply because NOON_* is reserved; rename that example to GITHUB_WEBHOOK_SECRET (proposed) or allow non-reserved NOON_ names in the file; whether to also accept the setting in .git/noon.yaml for per-repo secrets (currently rejected: repo-private but per-checkout, and .git/noon.yaml env: already covers it); exact doctor wording; whether `noon list --json` should expose env_file key names (current proposal: no).

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
