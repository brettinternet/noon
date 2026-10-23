---
id: NOON-86
title: Let MCP agents propose temporary jobs that run only after human approval
status: draft
type: feature
priority: low
labels: [mcp, security, cli, tui]
blocked_by: [NOON-70]
rank: af417d05f417d05a4
created: 2026-10-22T22:08:00Z
updated: 2026-10-22T22:08:00Z
---

Outcome: an agent without a shell, or connected over remote MCP, can ask Noon for a temporary job (NOON-70), and the job runs only after a human approves its exact definition. MCP gains no unapproved command authority.

Promotion condition: draft until NOON-70 ships and the open questions below are settled in a backlog decision. This changes the MCP row of the docs/design.md trust table ("Can't add, edit, enable, ... jobs").

Context: agents with a shell already create temporary jobs with `noon add --temporary`, and the skill points them there. MCP clients can only queue approved jobs and attach temporary schedules (NOON-69); client identity is metadata only. Agent clients may prompt the user before each tool call, but Noon cannot verify that, so it is not approval. Decision-2 already pins unattended runs to an approved definition digest.

Proposed behavior:
- A new MCP tool (working name `propose_job`) takes NOON-70 fields: name, schedule, `for`/`until` (at most 7 days), `in`/`at`, an action (`run`/`sh`, or `agent` with `prompt`/`prompt_file`), description, timeout, key. It creates a temporary job with approval `needs_approval`, pinned to its digest, requested_by set to the client identity. It never runs unattended until approved. A foreground `noon run NAME` stays allowed for a human, matching shared jobs.
- The result includes the exact approval command (`noon approve NAME`, or whatever the existing approval command becomes) so the agent can relay it in chat. Bare `noon`, MCP `status`, and the TUI show pending proposals with their requester, action, schedule, and end. The TUI can approve or reject the selected one.
- Approval enables the job; rejection removes it. A proposal not approved before its end expires through the normal NOON-70 grace path. Approving never extends the end.
- Pending proposals are capped per project (for example 10). Over the cap, the tool fails with a clear error. Keyed retries dedupe as in NOON-70.
- Over HTTP MCP, the bearer token can only propose, never approve, so a leaked token cannot run commands.
- Optional standing approval: user config opts in to auto-approving proposals that match a narrow policy: agent-prompt jobs only, using named agent profiles, within a maximum lifetime, from stdio MCP only. Raw `run`/`sh` proposals always need a human. Rationale: the agent profile's argv and permission flags are authority the user already granted; the prompt is data.

Open questions:
- Tool shape: a separate `propose_job` tool, or `run_job` accepting an inline definition.
- Ship standing approval with the first version, or later.
- How a human notices a proposal when not in the agent's chat: dashboard only, or a hook such as `on_proposal`.
- Whether proposals from remote MCP are allowed at all, or stdio only.
- Approval command and TUI key naming, consistent with `noon enable`.

Non-goals: MCP editing or approving jobs; proposals for permanent config jobs; lifetimes over 7 days; any JSON API `/v1/` route that creates jobs.

Modified-file contract (provisional): internal/store (proposal state on temporary_jobs), internal/app (proposal, approve, reject operations), internal/mcp, internal/cli (approval command, dashboard notice), internal/tui, internal/config (only if standing approval ships), internal/skill/SKILL.md, README.md, docs/{agents.md,cli.md,design.md,configuration.md}, a new backlog decision.

Dependencies: NOON-70.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
