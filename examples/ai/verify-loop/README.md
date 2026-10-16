# Agent verify loop

**Level:** intermediate → advanced · **Shows:** agent-facing `description`, MCP, Claude Code/Codex `Stop` hooks, a blocking completion gate

Coding agents claim "done" too early. This example gives them one well-described `verify` job and
three ways to make sure it runs.

| Approach | Files | Guarantees |
| --- | --- | --- |
| **Instructions + MCP** | [`AGENTS.snippet.md`](AGENTS.snippet.md), [`mcp.json`](mcp.json) | Agent can discover, run, and read results; relies on it following instructions |
| **Queue on stop** | [`claude-settings.queue.json`](claude-settings.queue.json), [`codex-hooks.json`](codex-hooks.json) | Requests a queued run on a normal response stop; needs approval and a running scheduler. Not a gate |
| **Blocking gate** | [`claude-settings.gate.json`](claude-settings.gate.json), [`scripts/verify-gate.sh`](scripts/verify-gate.sh) | Blocks normal stops on dirty-tree verification failure, up to 3 attempts; not an unconditional completion guarantee |

## Setup

```sh
cp examples/ai/verify-loop/noon.yaml .                     # adjust run: to your check command
claude mcp add noon -- noon mcp                            # or: cp mcp.json .mcp.json
codex mcp add noon -- noon mcp
cat examples/ai/verify-loop/AGENTS.snippet.md >> AGENTS.md
```

Queue on stop (needs `noon enable` and a running scheduler; the hook payload becomes the run's
`NOON_EVENT_FILE`):

```sh
mkdir -p .claude && cp examples/ai/verify-loop/claude-settings.queue.json .claude/settings.json
mkdir -p .codex && cp examples/ai/verify-loop/codex-hooks.json .codex/hooks.json   # trust it in Codex
```

Blocking gate (foreground run, so no enable or scheduler needed; requires `jq`):

```sh
mkdir -p .claude scripts
cp examples/ai/verify-loop/claude-settings.gate.json .claude/settings.json
cp examples/ai/verify-loop/scripts/verify-gate.sh scripts/ && chmod +x scripts/verify-gate.sh
```

The gate skips verification when the working tree is clean, runs `noon run verify` in the
foreground (so the result is recorded in `noon history` like any run), and on failure exits 2 with
the last 60 lines (at most 12,000 bytes) as feedback for Claude to fix the problem. The snippets are named so they do not activate
inside this repository; copy them to the paths shown.

## Verification (2026-10-16)

Checked against Claude Code **2.1.302** (`claude --help`), the first-party
[hooks reference](https://code.claude.com/docs/en/hooks#stop) and
[headless reference](https://code.claude.com/docs/en/headless#start-faster-with-bare-mode).
This is **documentation/help evidence**, not a live vendor session:

- Stop receives JSON stdin with `session_id` and `stop_hook_active`. Exit 2 blocks the
  normal stop and sends stderr back as feedback. Interrupts and API errors do not run Stop.
- The headless reference explicitly says: “Without `--bare`, a `-p` session runs the hooks
  in a project's `.claude/settings.json`”. Do not add `--bare` or `--safe-mode` to a
  gated session; they disable these hooks. Only use headless mode in trusted directories.
- Hook commands expand `CLAUDE_PROJECT_DIR`; the quoted path handles spaces. Hook
  `timeout` is in **seconds**, so 900 is 15 minutes. Timeout/cancellation is not proof
  of verification success.
- `stop_hook_active` means a previous hook continued the turn. This gate deliberately
  retries then; its per-session counter bounds failures instead of bypassing on that flag.
  It allows stopping after MAX_ATTEMPTS blocking failures, even if verification still fails.
- Codex **0.163.0** help and [hooks reference](https://developers.openai.com/codex/hooks)
  document `.codex/hooks.json`, nested command handlers, JSON stdin, and JSON stdout for
  Stop. The snippet sends Noon's status to stderr and emits `{}` on success. Non-managed
  hooks must be reviewed and trusted in Codex; copying a file alone does not enable it.

`task cli:test -- -run '^TestExampleVerifyGate$' -count=1 -v` exercises the shipped
script with a real Noon binary, Git, jq, isolated state, and **synthetic** Stop payloads:
clean bypass, dirty failure/success, counter reset, bounded retries, and missing-jq feedback.
It does not test Claude/Codex integration or call a model.

## What agents see

```sh
noon list --json | jq '.jobs[] | {name, description, approval}'
```

The MCP server exposes `list_jobs`, `run_job`, `get_run`, `list_runs`, `cancel_run`, and
`status`. It cannot add, edit, enable, or approve jobs, so an agent can only run what you
approved. Without MCP, `noon skill` prints a shell-only skill document you can install yourself.
