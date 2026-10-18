# Agent profiles and prompt jobs

**Level:** intermediate · **Shows:** `prompt`, `prompt_file`, `agent`, `default_agent`, permission modes and sandbox profiles

A prompt job sends text to an agent CLI. Noon includes profiles named `claude`, `codex`,
`gemini`, `opencode`, and `pi`; they need no user-config entries. Install and authenticate the
chosen CLI separately. Built-in profiles are launch commands, **not read-only sandboxes**.
Permission settings, hooks, and tools remain the agent's responsibility.

Custom profiles live in user config; projects only name them. This example uses distinct names
(`claude-manual`, `codex-read`, and editing profiles) rather than silently overriding built-ins.
The mixed manifest needs those custom profiles too: merge the supplied config before listing
the whole project. For just a built-in job, copy that job alone to your own manifest.

```sh
# Merge agents from user-config.yaml into your existing user config; do not overwrite it.
# User config: ${XDG_CONFIG_HOME:-$HOME/.config}/noon/config.yaml
noon config                       # lists available profiles
noon -C examples/ai/agent-profiles list
noon -C examples/ai/agent-profiles run pi-summary   # requires an authenticated Pi CLI
# Or: run gemini-summary / opencode-summary. These jobs have no schedule.
```

| Job | Profile | Can change files |
| --- | --- | --- |
| `todo-sweep` | `claude-manual` (manual mode) | depends on settings/hooks |
| `docs-review` | `codex-read` (`--sandbox read-only`) | sandboxed shell cannot write; hooks/MCP are separate |
| `release-notes` | `claude-git` (pre-approved `git log/describe/diff`) | depends on settings/hooks and command arguments |
| `doc-comments` | `claude-edit` (`acceptEdits`) | yes, manual only |
| `gemini-summary` | built-in `gemini` | depends on agent configuration; manual only |
| `opencode-summary` | built-in `opencode` | depends on agent configuration; manual only |
| `pi-summary` | built-in `pi` | depends on agent configuration; manual only |

Noon's built-in Gemini command is `gemini -p`, OpenCode is `opencode run --` (prompt appended
as one final argument), and Pi is `pi --print` (prompt on stdin). The request not to use tools in
these sample prompts is an instruction, not an enforced restriction. These examples validate
Noon's configuration without contacting a model; model execution requires your own credentials.

Rules of thumb:

- **Unattended runs need explicit permissions and a timeout.** Claude's
  `--permission-prompts none` denies unresolved prompts; Codex `exec` is noninteractive.
  This does not guarantee progress: authentication, tools, and model services can fail or stall.
- **Schedule inspection work; run editing work by hand** (or in an isolated worktree, see
  [background agents](../background-agents/)).
- Prompt text and the profile *name* are part of the approval digest, so editing an inline
  prompt on a shared job requires `noon enable` again. Prompt *files* are read at run time and
  are not covered: review them like scripts.
- Context comes from the environment. A prompt may say "read the event in `$NOON_EVENT_FILE`" or
  "summarize commits since `$NOON_LAST_SUCCESS_AT`"; Noon does no templating.

## Verification (2026-10-16)

Documentation and installed help, **not model-backed execution**:

- Claude Code **2.1.302**, `claude --help`, [headless](https://code.claude.com/docs/en/headless)
  and [permissions](https://code.claude.com/docs/en/permissions): `--permission-prompts none`
  (requires 2.1.259+) denies only unresolved requests. Manual mode is explicit here because
  an unspecified mode can be auto. Settings, hooks, MCP, and existing allow rules can grant
  more access. Read/Grep/Glob in working directories need no approval; availability varies
  by version/platform. These profiles are **not read-only sandboxes**.
- `acceptEdits` also approves common filesystem commands, not just Edit. Git rules use the
  documented `Bash(git log *)` syntax; `--allowedTools` pre-approves rather than restricts
  the tool set. Git arguments/configuration can have side effects (for example output files
  or external diff helpers). Use these profiles only with trusted input and configuration.
- Codex **0.163.0**, `codex exec --help` and
  [noninteractive mode](https://developers.openai.com/codex/noninteractive): `-` reads stdin;
  `--sandbox read-only|workspace-write` selects the shell sandbox. See
  [background-agent trust notes](../background-agents/#verification-2026-10-16).
- Gemini is not installed here. [Headless reference](https://geminicli.com/docs/cli/headless/)
  documents `-p/--prompt` with a query argument; Noon appends that argument. This is not a
  tool-disable mode or a verified read-only profile.
- Ollama is not installed here. [CLI reference](https://docs.ollama.com/cli) documents
  `ollama run`; [RunHandler source](https://github.com/ollama/ollama/blob/4d2e25016476e2744863900795fd226266b76a86/cmd/cmd.go)
  reads non-TTY stdin and disables interactive mode. This plain generation path does not
  execute model tool calls; it needs a running server and a downloaded model.

Ad hoc, without editing YAML:

```sh
noon add pr-risks "weekdays at 9am" --prompt "List open PRs and their main risks." --agent claude-git
noon -C examples/ai/agent-profiles run release-notes --in 2h      # queue a one-shot for later (needs enable + serve)
```
