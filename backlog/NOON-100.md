---
id: NOON-100
title: Link hook notifications to the run page with a public viewer URL
status: todo
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
updated: 2026-11-24T03:55:41Z
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

- [ ] `task cli:test PKG=./internal/config -- -count=1` exits 0: `serve.public_url` accepts absolute http(s) URLs and ignores a trailing slash; a relative URL, another scheme, a query, a fragment, any non-user config file, or a missing `web_token_file` fails validation.
- [ ] `task cli:test PKG=./internal/app -- -count=1` exits 0: with `public_url` set, a failure hook sees `NOON_RUN_URL=https://noon.example.com/runs/ID` for its own run ID in both scheduled and foreground runs; without it, the variable is absent from the hook environment; jobs never receive it.
- [ ] docs/hooks.md, docs/configuration.md, docs/web.md, and examples/integrations/notifications describe only implemented behavior, and `noon config` shows `Public URL:` when set. `task ci` exits 0.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
