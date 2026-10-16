# Bump design

Status: initial direction, not a finished scheduler. Only the CLI/configuration
bootstrap in README.md is implemented. Backlog.md owns delivery order and scope.

## Product boundary

A local-first scheduler for repository work: agent prompts, checks, reports,
and HTTP calls. One Go binary, useful CLI first, optional TUI later, no required
cloud account, browser, Python, Node, message broker, or LLM provider.

Start with macOS and Linux; containers are Linux deployments of the same binary.
Windows support is not part of the initial execution contract. Bump schedules and
supervises work; it is not a fleet manager, CI replacement, workflow DAG platform,
terminal multiplexer, secret manager, or agent runtime.

Defaults should be unsurprising: current repo, local timezone, no overlapping run
of a job, skip missed scheduled runs, bounded logs, no implicit network listener,
no commands executed by discovery. Every mutation is explicit and inspectable.

## CLI contract

Bare `bump` is a useful summary, never a hidden service start or an automatic TUI.
Bare command groups inspect their scope rather than fail for lack of arguments.
Interactive prompting is an optional convenience; noninteractive commands never
hang waiting for input. Missing targets on destructive operations must never
mean all targets. Provide examples and a next action instead.

Proposed surface (not implemented unless listed in README):

| Invocation | Behavior |
| --- | --- |
| `bump`, `bump status` | Repo summary: next jobs, running work, last failures, scheduler state |
| `bump init` | Minimal valid repo config, never overwrite |
| `bump list` | Jobs and next times; empty is a successful helpful result |
| `bump run` | List runnable jobs; `bump run NAME` executes one now |
| `bump add` | Explain/show examples; explicit fields create a job |
| `bump schedule` | List durable ad-hoc schedules; named jobs can run once later |
| `bump logs` | Latest run in this repo, or a helpful empty result |
| `bump history` | Recent runs |
| `bump pause`, `bump resume`, `bump cancel`, `bump remove` | Missing target shows candidates, never mutates all |
| `bump serve` | Foreground repo scheduler until interrupted |
| `bump service` | Inspect optional launchd/systemd installation |
| `bump trigger` | List named event targets; explicit target submits an event |
| `bump mcp` | stdio MCP adapter; scheduler lifetime remains independent |
| `bump tui` | Explicit terminal interface over the same application operations |

Prefer a small vocabulary over aliases for everything. Commands work without
being in a Git repo. No surprising selector inference based on a single job.
Global flags work before/after subcommands. Humans get compact output; agents get
`--json` with versioned envelopes and no logs/help mixed into action output.
Help/version/completion remain standard text. Failure is nonzero with one useful
diagnostic; no success-shaped not-implemented commands.

## Scheduler lifetime: explicit service, not magic

**Decision:** implement a foreground `bump serve` per project first. Later add
`bump service install` / uninstall / status for launchd and systemd user services.
Use the same foreground process in Docker, with the container runtime supervising
it. Install/start must always be explicit; `bump`, `init`, config discovery,
and MCP connection must not silently register persistent execution.

Why not only native timers like schedx? Native timers are good for cheap periodic
ticks, but Bump also needs run ownership, queues, cancellation, event delivery,
and attachable sessions. One long-running process owns these without reconstructing
them every minute. Native service managers supervise Bump rather than duplicate
its schedule semantics. Do not add a second tick-only execution model initially.

One scheduler owns one canonical project/state identity, enforced by a lock.
Two repositories must not accidentally share the same writable state store.
Reads can inspect persisted state offline; mutating live execution uses a local
authenticated-by-user IPC boundary. A second owner refuses with a useful message.

`bump run NAME` should be usable without a resident scheduler, through the same
execution/storage logic and ownership checks. When a scheduler exists, submit to it;
never launch a second competing owner. Durable ad-hoc schedules survive the client,
but cannot execute while the scheduler/host is down. Say so explicitly.

## Config and job ownership

Keep the implemented settings precedence in configuration.md. Future manifests
extend that schema, not a competing config system:

```yaml
# PROPOSED: not accepted by the bootstrap.
version: 1
timezone: Local
jobs:
  check:
    schedule: "0 9 * * 1-5"
    command: [task, test]
    timeout: 10m
  review:
    schedule: "every 6h"
    command: [./scripts/review]
    overlap: skip
```

Explicit argv is the baseline. Shell scripts are supported through an explicit
shell action, never inferred from punctuation. Agent prompts initially live in
repo files consumed by existing CLI commands; no provider credentials or model
SDK in Bump's core. HTTP actions may start as commands, with a bounded HTTP adapter
only when credential handling, timeouts, and delivery semantics justify it.

User config supplies settings and reusable integration defaults, **not jobs
silently injected into every repo**. A future explicit user/global job scope
gets its own store and selector. Repo config owns declared jobs; CLI/MCP-created
temporary jobs live in local durable state and never rewrite committed YAML.
Names colliding across declared/ad-hoc sources fail clearly. Editing shared files
requires an explicit command; local overrides remain reviewable.

Local YAML overrides replace a complete named job rather than deep-merging
half an execution command with another source. Disabled jobs can shadow shared
definitions without deleting them. Resolve paths against the file that defines
them. Config reload is validate-first and atomic; a bad edit preserves the last
valid running snapshot and reports the error. Running jobs keep their snapshot.

Cron compatibility means a documented five-field schedule and cron-file **import**
first, not pretending to implement every system crontab directive. Require an
explicit import path; never scan/install system crontabs or execute imports.
Support standard shell-command user crontab entries, environment assignments,
and well-defined macros; reject unsupported directives with line numbers.
Preview the normalized YAML before writing. Preserve local timezone semantics.

## Execution and durability

Use a narrow application layer shared by CLI/MCP/TUI, with schedule, store, and
executor implementations separated only where exercised. Plan for embedded
SQLite transactions (no external database), a run/event ledger, bounded per-run
logs, and migrations. Pick the Go driver during the storage task after checking
static Linux/macOS/container builds; avoid inventing a custom database.

Persist accepted schedule occurrences and run IDs before dispatch. A crash can
leave an interrupted run, so recover honestly; **do not promise exactly-once
external effects**. Do not replay ambiguous side effects automatically. Commands
needing retries must be idempotent or opt in knowingly.

Every run records project, job/config revision, trigger identity, scheduled/start/
finish times, exit/result, cwd, and optional session locator; redact credentials.
A resumed scheduler distinguishes interrupted, skipped, failed, and completed work.

Scheduling rules need executable tests:
- Five-field cron in explicit/local timezone, with documented DST behavior.
- Fixed intervals use durations; distinguish them from wall-clock cron.
- One-shot delayed/absolute schedules are durable and finite.
- Missed-run default is skip; opt-in coalesce executes at most once, not a burst.
- Overlap default is skip; queue is bounded; replacement/cancellation is explicit.
- Per-project limits protect a checkout even across differently named jobs.
- Timeout/cancel targets the whole owned process group, with graceful then forced
  termination; never kill a recycled PID based only on persisted state.

Do not infer shell rc files, copy all secrets, create worktrees, or change branches.
Jobs needing isolation explicitly run in their own checkout/container. Agent
spending limits are not the same as process timeouts; Bump can bound runs/time,
while provider budgets remain the agent's responsibility.

## Attachability

Bump does not own a terminal UI. A normal foreground subprocess gives reliable
exit status and logs. A tmux/Herdr adapter may create an attachable session, but
must report readiness, identity, completion, cancellation, and exit outcome.

A detached launcher exiting zero means only **launched**, not **completed**.
Never let that bypass overlap protection. Keep human attachment separate from
scheduler ownership. Start with one adapter only after the execution contract
exists, and do not require tmux/Herdr for ordinary commands.

## Events and integrations

Start with named event targets, then explicit command hooks:
- Local Git hooks invoke the trigger CLI, carrying bounded structured context.
- Agent lifecycle hooks emit explicit events such as completed/needs-input.
- Remote events enter through an optional authenticated webhook listener.
- File events are opt-in; debounce them and avoid observing Bump's own output.

No automatic Git hook installation or transcript scraping. Git hooks are local;
remote CI/review events require webhooks or polling. Do not interpolate webhook
payloads into shell strings. Authenticate/sign incoming events, bound size,
deduplicate replay IDs, apply cooldown/rate limits, and prevent recursive event
loops. Default to loopback; no HTTP server until explicitly enabled.

Hooks are ordinary configured executables with argv and versioned JSON on stdin;
stdout is reserved for a bounded structured result and stderr for diagnostics.
Bound time and output, and record failures without recursively firing the same
failure hook forever. Avoid Go's plugin ABI, automatic PATH plugin discovery,
installation marketplaces, or a general plugin SDK before two real adapters
demonstrate the shared contract. A small subprocess protocol can grow later.

## MCP and agent authority

Use the official Go MCP SDK when implementing the adapter. stdio is the initial
transport. MCP is a client of shared operations, not a second scheduler and not
the owner of persistent jobs. Disconnecting an agent does not cancel accepted
durable work. Transport servers do not auto-start a scheduler.

Start with list/status/history/logs, trigger a **named existing job**, and create/
inspect/cancel bounded one-shot schedules. Agents need stable IDs, idempotency
keys, readable validation, and structured errors. Recurrence, arbitrary executable
creation, hooks, and shared-config edits require explicit operator-enabled policy.
Default agent-created work should expire and have run/time limits. The agent's
claimed identity is metadata, not proof of authorization.

A repository manifest is code-adjacent input, not trusted instruction. Never
execute commands merely because an MCP client lists a project or reads config.
No prompt text or command payload may grant itself wider scheduling authority.

## Docker

Use the same binary and foreground ownership model. Bootstrap image includes a
non-root user, CA roots, a shell, and embedded timezone data; it is not an agent
sandbox. Extend it to install the job's tools. Bind repo/config deliberately and
persist state separately. No Docker socket by default. Do not mount host agent
credentials unless required and explicitly authorized. Container boundaries alone
do not make untrusted agent execution safe.

Production scheduler delivery must verify signal forwarding, child reaping,
non-root ownership, read-only repo mounts, writable state, restart recovery, and
running with no network. Remote access remains SSH/container tooling by default;
do not expose unauthenticated MCP or webhook endpoints.

## Inspiration, not dependencies

- [Supercronic](https://github.com/aptible/supercronic): crontab simplicity,
  inherited execution environment, graceful lifecycle, overlap awareness.
- [schedx](https://github.com/Alireza29675/schedx): repo manifests, one-shot jobs,
  JSON CLI, explicit reconciliation.
- [Dagu](https://github.com/dagucloud/dagu): declarative jobs, run history,
  validation, MCP; deliberately skip the DAG/platform/web UI scope.
- [RunWisp](https://github.com/runwisp/runwisp): TUI visibility, missed-run policy,
  bounded logs, operational evidence.
- [herdr-sched](https://github.com/husniadil/herdr-sched): named event triggers,
  cooldowns, policy-gated agent mutations.
- [mcp-cron](https://github.com/jolks/mcp-cron): durable schedules through MCP.

These references informed the design, not a maintenance or security endorsement.
No source code from these external projects was copied.
