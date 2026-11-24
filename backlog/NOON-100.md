---
id: NOON-100
title: Link hook notifications to the run page with a public viewer URL
status: done
type: feature
priority: low
labels: [config, http, delight]
blocked_by: [NOON-98]
refs:
  - docs/decisions/decision-6 - An-optional-web-viewer-is-an-authenticated-adapter-on-the-scheduler-listener.md
  - docs/hooks.md
files:
  - internal/config/**
  - internal/app/hooks.go
  - internal/app/*_test.go
  - docs/hooks.md
  - docs/configuration.md
  - docs/web.md
  - examples/integrations/notifications/**
  - internal/cli/root.go
created: 2026-11-24T03:55:25Z
updated: 2026-11-24T08:23:23Z
---

Why: lifecycle hooks (docs/hooks.md) already send failure and recovery notifications through ntfy, Slack, or mail scripts, but a notification can only say which job failed. With the web viewer (NOON-98) behind a reverse proxy, the notification could link straight to the failing run's page. Noon can't derive the public address itself.

Outcome: a `serve.public_url` setting in user config gives the viewer's public base URL, and hooks receive `NOON_RUN_URL` pointing to the run page.

```yaml
serve:
  public_url: https://noon.example.com
```

Settled contract:
- `serve.public_url` is allowed only in user config, like the rest of `serve:` (NOON-97). It must be an absolute `http` or `https` URL with no query or fragment; anything else fails `noon config check` and startup. A trailing slash is ignored.
- `public_url` without `serve.web_token_file` fails `noon config check` and startup.
- Every hook, from scheduled and foreground runs alike, gets `NOON_RUN_URL=PUBLIC_URL/runs/RUN_ID` whenever `public_url` is set. The run page reads shared state, so foreground runs appear there too. Without the setting the variable is unset, not empty.
- Jobs don't receive it; only hooks do. No other environment changes.
- `noon config` shows `Public URL:` when set. The viewer uses it for nothing else; `Host` handling is unchanged.

Scope: internal/config (key, validation), hook environment in internal/app/hooks.go, docs/hooks.md, docs/configuration.md, docs/web.md, and examples/integrations/notifications (include the link when present).

Tests (critical only): URL validation (parsing), and one hook-environment test proving `NOON_RUN_URL` is set with the right run ID only when configured.

Non-goals: links in CLI output or MCP results; deriving the URL from requests; per-project URLs; job environment.

Dependencies: NOON-98. Follows decision-4 (hooks are the extension model) and decision-6.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/config -- -count=1` exits 0: `serve.public_url` accepts absolute http(s) URLs and ignores a trailing slash; a relative URL, another scheme, a query, a fragment, any non-user config file, or a missing `web_token_file` fails validation.
  Evidence: task cli:test PKG=./internal/config -- -count=1: ok. TestServeSettingsAreUserOnly covers http/https with path, trailing slash dropped, relative/bare host/ftp/opaque/query/empty query/fragment/empty fragment rejected, serve in noon.yaml/.git/noon.yaml/fragment/--config rejected, public_url without web_token_file fails ServeSettings (startup).
- [x] `task cli:test PKG=./internal/app -- -count=1` exits 0: with `public_url` set, a failure hook sees `NOON_RUN_URL=https://noon.example.com/runs/ID` for its own run ID in both scheduled and foreground runs; without it, the variable is absent from the hook environment; jobs never receive it.
  Evidence: task cli:test PKG=./internal/app -- -count=1: ok. TestHookRunURL: schedule and manual runs get NOON_RUN_URL=PUBLIC_URL/runs/ID for their own ID; unset (inherited stale value removed) without public_url; JobEnvironment strips it. Built binary: foreground noon run hook got https://noon.example.com/runs/1, job env lacked it; noon serve queued run hook got NOON_RUN_URL=https://noon.example.com/runs/1 (trigger=queued).
- [x] docs/hooks.md, docs/configuration.md, docs/web.md, and examples/integrations/notifications describe only implemented behavior, and `noon config` shows `Public URL:` when set. `task ci` exits 0.
  Evidence: Docs updated: docs/hooks.md (NOON_RUN_URL row), docs/configuration.md (key, table, Public URL line), docs/web.md (Notification links), examples/integrations/notifications (README, user-config, noon-ntfy Click header, noon-slack link; verified with jq and sh -n). Built binary: noon config prints 'Public URL:    https://noon.example.com'; config check rejects ftp:// and missing web_token_file (exit 1). task ci exit 0 on 13b5b4d.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on 13b5b4d (fast-forwarded to main).
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Each criterion has evidence recorded with bl ac.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Deviations (internal/app/run.go, internal/cli/run.go, internal/cli/serve.go, internal/scheduler/serve.go) justified in Notes.
- [x] No test was deleted, skipped, or weakened
  Evidence: Only additions: TestHookRunURL and a public_url table in TestServeSettingsAreUserOnly.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: Docs describe implemented behavior only; design.md execution section already strips all hook metadata names, no settled contract change needed there.

## Notes

Worktree .worktrees/noon-100-public-url, branch noon-100-public-url (Worktrunk; no creation receipt since the hook records Pi sessions only). Contract deviations, each required to deliver hook URLs: internal/app/run.go strips NOON_RUN_URL from job env (jobs never receive it); internal/cli/run.go and internal/scheduler/serve.go pass the public URL into app.FinishRunWithHook for foreground and scheduled runs; internal/cli/serve.go passes serve.public_url into ServeOptions (read at startup like the other serve settings).

## Summary

Added serve.public_url (user config only; absolute http(s), no query/fragment, trailing slash dropped, requires serve.web_token_file). Hooks from foreground and scheduled runs get NOON_RUN_URL=PUBLIC_URL/runs/ID; jobs never do. noon config shows Public URL. Docs and ntfy/Slack examples link to the run page. Commit 13b5b4d on main; task ci passed; one self-review pass, no defects found.
