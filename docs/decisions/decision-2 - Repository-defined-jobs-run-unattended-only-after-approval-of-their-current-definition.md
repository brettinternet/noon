---
id: decision-2
title: >-
  Repository-defined jobs run unattended only after approval of their current
  definition
date: '2026-10-16 01:14'
status: accepted
---
## Context

A per-user scheduler (decision-1) runs commands from many checkouts without anyone watching. A clone,
`git pull`, branch switch, or agent-created worktree can add or change jobs in a committed
`noon.yaml`. Running `task test` by hand after a pull is a choice; a new job firing every minute at
3am is not. direnv re-asks when `.envrc` changes; mise trusts by path. Agents add another writer.

## Decision

1. `noon enable` opts one canonical checkout into background runs and approves the current
   definition of every job that needs approval. Nothing is enabled implicitly: not clones,
   worktrees, discovery, `init`, status, or MCP. `noon disable` stops background runs and keeps
   history.
2. Jobs from `noon.yaml` and from an explicit `--config` file need approval. Jobs from `.noon.yaml`
   and user config are the user's own and do not.
3. Approval pins a SHA-256 digest of every field that affects what runs or when. A changed job
   waits as `needs approval` while unchanged jobs keep running. `noon enable` re-approves.
4. Digests cover definitions, not the contents of scripts or prompt files they reference. Docs say
   so plainly.
5. `noon add` approves only the job it writes. Manual `noon run` needs no approval. MCP clients
   cannot approve.

## Consequences

- Teams that change shared jobs make each developer re-run `noon enable`; status and the dashboard
  must make that impossible to miss.
- A committed `.noon.yaml` would bypass approval; docs require it to stay ignored.
- This protects against unnoticed repository changes, not against a local agent with shell access.
- Docker deployments enable the project explicitly before `noon serve`.
