---
id: decision-1
title: 'One per-user foreground scheduler, supervised by the OS or container'
date: '2026-10-16 01:14'
status: accepted
---
## Context

Recurring jobs need something alive at the scheduled time. The seeded design proposed one foreground
`noon serve` per project with optional per-project launchd/systemd units. Options considered:

- OS timer per job or a per-minute dispatcher tick (schedx). No resident process, but minute
  granularity, no live status, no webhooks or MCP queue, and per-OS reconciliation of units.
- Self-daemonizing or auto-started background process (hum's model for dev processes). Convenient,
  but it silently stops at reboot or logout, which is the worst failure for a scheduler.
- One scheduler per project. Simple ownership, but N services for N repos, no cross-repo limits,
  and a TUI or status view must discover N processes.
- One scheduler per user state directory serving every enabled project.

## Decision

1. `noon serve` is a foreground scheduler for every enabled project in its state directory. One owner
   per state directory, enforced with a lock; a second refuses with the owner's PID and start time.
2. Persistence across logout and reboot comes from `noon service install`, run once per machine,
   which registers a launchd agent or systemd user unit running `noon serve` with the installer's
   `PATH` captured explicitly. Docker runs the same foreground process. Noon never forks itself into
   the background, and no command other than `service install` registers or starts a service.
3. `noon run NAME` runs in the foreground without a scheduler.
4. There is no IPC socket. Clients coordinate through the SQLite store; the scheduler polls it every
   second and polls config files every two seconds. Commands needing a scheduler record durable
   intent and state clearly when none is running.

## Consequences

- One install covers every repo; global and per-project concurrency limits are possible.
- Per-checkout opt-in is required (decision-2), because one scheduler sees many projects.
- No protocol versioning between client and daemon; schema versioning (decision-3) replaces it.
- Polling costs a little idle CPU. Revisit with a wake-up channel only if latency or energy use is
  measured to matter.
- Revisit self-starting only if users repeatedly cannot install a service (for example, Linux
  without a systemd user session); even then, warn that schedules stop at reboot.
