---
id: decision-4
title: 'Commands, agent profiles, and hooks are the extension model; no plugin system'
date: '2026-10-16 01:14'
status: accepted
---
## Context

The product brief asked whether hooks and a plugin system make sense for tool integrations. Go's
plugin ABI is fragile, PATH-discovered plugins and marketplaces need trust and versioning
machinery, and no integration so far needs more than running a program.

## Decision

1. Any executable is an integration: jobs run argv or `/bin/sh -c` strings.
2. Agent CLIs are reached through agent profiles (argv plus stdin or argument prompt delivery) in
   user config, with built-in `claude` and `codex` profiles. No model SDK or credentials in Bump.
3. Outbound integration is lifecycle hooks: `on_success` and `on_failure` commands receive a
   versioned JSON run summary on stdin. Notifications are examples, not built-in providers.
4. Inbound integration is `bump run NAME --detach` from Git and agent hooks, signed webhooks, and
   MCP. Payloads are data files, never interpolated into commands.
5. MCP and HTTP are adapters over `internal/app` operations, never a second scheduler.

## Consequences

- One small, testable contract per direction instead of an SDK.
- Revisit when two real integrations need something these cannot express. Prefer `bump-NAME`
  subprocess executables speaking versioned JSON, never Go plugins.
