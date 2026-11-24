---
id: NOON-97
title: Configure the scheduler listener in user settings so the background service serves it
status: done
type: feature
priority: medium
labels: [http, config, ops, security]
refs:
  - docs/design.md
  - docs/service.md
  - docs/configuration.md
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
files:
  - internal/config/**
  - internal/cli/serve.go
  - internal/cli/serve_test.go
  - internal/app/doctor.go
  - internal/scheduler/serve.go
  - integration/serve_listener_settings_test.go
  - README.md
  - docs/cli.md
  - docs/configuration.md
  - docs/service.md
  - docs/webhooks.md
  - docs/design.md
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/dashboard.go
  - internal/cli/dashboard_test.go
created: 2026-11-23T20:47:44Z
updated: 2026-11-24T07:25:01Z
---

Why: the per-user service (`noon service install`) runs `noon serve` with only `--state-dir`, so webhooks, the remote API, and the planned web viewer (decision-6) can run in the background only in Docker or by hand. Flags captured in the unit would vanish when the documented "re-run install after each upgrade" step omits them.

Outcome: a `serve:` block in user config sets the listener, and `noon serve` reads it at startup. The unchanged service unit then serves the listener in the background, and the setting survives reinstalls and upgrades.

```yaml
# ~/.config/noon/config.yaml
serve:
  listen: 127.0.0.1:8787
  api_token_file: ~/.config/noon/api-token
```

Settled contract:
- Keys: `serve.listen` (host:port) and `serve.api_token_file` (path). Paths resolve like `state_dir` (relative to the config file's directory) and expand a leading `~`. NOON-98 adds `serve.web_token_file`.
- Allowed only in user config. In `noon.yaml`, `.git/noon.yaml`, `.git/noon.d/*.yaml`, or a `--config` file it is an error naming user config (decision-6 item 2). A cloned or pulled repository can never open a port.
- Precedence per key: flag > user config. There are no environment variables. `--api-token-file` (flag or setting) without a listen address (flag or setting) stays a usage error (exit 2) from `serve`, and `noon config check` reports it as a config error.
- Token validation is unchanged (read once at startup, at least 32 bytes, startup fails with exit 1 before scheduling). The token is never logged or echoed.
- `noon service install` writes the same unit as today. The unit has no listener flags; the setting is read at runtime. Changing the setting takes effect after `noon service restart`.
- `noon config` shows `Listener: ADDR` (plus `API` when a token file is set) or `Listener: off`. `noon doctor` fails a check when the token file is unreadable or too short, and warns when the token file is readable by group or others, or when the address isn't loopback. Docs generate tokens with `umask 077; openssl rand -base64 32 > FILE`.

Scope: internal/config (key, file placement, resolution), internal/cli serve/config/doctor wiring, docs.

Tests (critical only): placement rejection in every non-user file (security boundary), precedence, and one built-binary integration test proving `serve` with no flags listens from user config.

Non-goals: environment variables for the listener; applying listener changes without a restart; listener flags on `service install`; TLS; the web viewer (NOON-98).

Dependencies: none. Follows decision-1, decision-2, decision-6.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/config -- -count=1` exits 0. A `serve:` block in `noon.yaml`, `.git/noon.yaml`, a `.git/noon.d/*.yaml` fragment, or a `--config` file fails validation naming user config; in user config a relative `api_token_file` resolves against the config directory and a leading `~` expands.
  Evidence: AC#1: task cli:test PKG=./internal/config -- -count=1 → ok. TestServeSettingsAreUserOnly rejects serve in noon.yaml, .git/noon.yaml, .git/noon.d fragment, and --config naming user config; relative api_token_file resolves to config dir; ~ expands.
- [x] `task cli:test PKG=./integration -- -run '^TestServeListenerSettings$' -count=1 -v` exits 0. With the built binary and isolated HOME/XDG/state: user config `serve.listen: 127.0.0.1:0` plus a 32-byte token file makes `bin/noon serve` with no flags answer `/healthz` and an authenticated `GET /v1/projects`; `--listen` overrides the setting; `serve.api_token_file` without any listen address exits 2; a repository `noon.yaml` containing `serve:` makes `noon config check` exit 1 and opens no listener; `noon service install --print` output is byte-identical with and without the setting.
  Evidence: AC#2: task cli:test PKG=./integration -- -run '^TestServeListenerSettings$' -count=1 -v → PASS (2.35s): no-flag serve answers /healthz + authenticated GET /v1/projects (401 without token); --listen overrides serve.listen; lone serve.api_token_file exits 2; repo noon.yaml serve: → config check exit 1, serve opens no listener; service install --print byte-identical.
- [x] `task cli:test PKG=./internal/cli -- -count=1` exits 0. `noon config` prints `Listener: 127.0.0.1:8787 (API)` for the setting and `Listener: off` without it; `noon doctor` exits nonzero naming the token file when it is unreadable or under 32 bytes, and warns when the token file is group- or world-readable or the address is not loopback. The token string appears in no command output.
  Evidence: AC#3: task cli:test PKG=./internal/cli -- -count=1 → ok (48.7s). TestListenerSettingsInConfigAndDoctor: noon config prints 'Listener:      127.0.0.1:8787 (API)' / 'Listener:      off' (padded to align with sibling Project:/Timezone: columns); doctor fails naming token file when missing or 31 bytes, warns for 0644 token and 0.0.0.0 address; token absent from all output.
- [x] README, docs/cli.md, docs/configuration.md (where-keys table and settings), docs/service.md (background listener, restart to apply), docs/webhooks.md, and docs/design.md describe only implemented behavior. `task ci` exits 0.
  Evidence: AC#4: README, docs/cli.md, docs/configuration.md (where-keys row + ### Listener), docs/service.md (## Background listener, restart to apply), docs/webhooks.md, docs/design.md (webhooks, remote API, security table) updated. task ci → exit 0 on a6b783e.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Worktree: .worktrees/noon-97-serve-settings branch noon-97-serve-settings (session 8f507c94-c783-4fe6-bf1c-c432d6b0bd66).

Commit a6b783e (fast-forwarded to main). Serve settings live in internal/config/serve.go (new file under internal/config/**); token reading moved from scheduler to config.ReadTokenFile so doctor and serve share it. serve.api_token_file without serve.listen is a config error for Load (config check, doctor) but serve itself reads settings via ServeSettings and applies the pairing rule after flags (exit 2). noon config Listener line is padded like its sibling lines. Self-review: no defects found. DoD: task ci exit 0 on a6b783e; diff within contract; no tests weakened.
