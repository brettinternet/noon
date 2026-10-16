# Agent verify loop

**Level:** intermediate → advanced · **Shows:** agent-facing `description`, MCP, Claude Code/Codex `Stop` hooks, a blocking completion gate

Coding agents claim "done" too early. This example gives them one well-described `verify` job and
three ways to make sure it runs.

| Approach | Files | Guarantees |
| --- | --- | --- |
| **Instructions + MCP** | [`AGENTS.snippet.md`](AGENTS.snippet.md), [`mcp.json`](mcp.json) | Agent can discover, run, and read results; relies on it following instructions |
| **Queue on stop** | [`claude-settings.queue.json`](claude-settings.queue.json), [`codex-hooks.json`](codex-hooks.json) | A run always starts after each session; you see the result later. Not a gate |
| **Blocking gate** | [`claude-settings.gate.json`](claude-settings.gate.json), [`scripts/verify-gate.sh`](scripts/verify-gate.sh) | Claude cannot finish while verify fails (up to 3 attempts) |

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
the last 60 lines so Claude fixes the problem. The snippets are named so they do not activate
inside this repository; copy them to the paths shown.

## What agents see

```sh
noon list --json | jq '.jobs[] | {name, description, approval}'
```

The MCP server exposes `list_jobs`, `run_job`, `get_run`, `list_runs`, `cancel_run`, and
`status`. It cannot add, edit, enable, or approve jobs, so an agent can only run what you
approved. Without MCP, `noon skill` prints a shell-only skill document you can install yourself.
