# Agent profiles and prompt jobs

**Level:** intermediate · **Shows:** `prompt`, `prompt_file`, `agent`, `default_agent`, read-only vs editing profiles

A prompt job sends text to an agent CLI. *Which* CLI and *what it may do* is a profile in your
user config; the project only names the profile. That keeps machine paths and permissions out of
the repository.

```sh
mkdir -p ~/.config/noon
cp examples/ai/agent-profiles/user-config.yaml ~/.config/noon/config.yaml   # or merge by hand
noon config                       # lists available profiles
noon -C examples/ai/agent-profiles list
noon run release-notes            # streams the agent's output; stored as a normal run log
```

| Job | Profile | Can change files |
| --- | --- | --- |
| `todo-sweep` | `claude` | no |
| `docs-review` | `codex` (`--sandbox read-only`) | no |
| `release-notes` | `claude-git` (allowlisted `git log/describe/diff`) | no |
| `doc-comments` | `claude-edit` (`acceptEdits`) | yes, manual only |

Rules of thumb:

- **Unattended runs must never wait for a prompt.** Every profile here either denies or
  pre-approves; none can hang a scheduled job.
- **Schedule read-only work; run editing work by hand** (or in an isolated worktree, see
  [background agents](../background-agents/)).
- Prompt text and the profile *name* are part of the approval digest, so editing an inline
  prompt on a shared job requires `noon enable` again. Prompt *files* are read at run time and
  are not covered: review them like scripts.
- Context comes from the environment. A prompt may say "read the event in `$NOON_EVENT_FILE`" or
  "summarize commits since `$NOON_LAST_SUCCESS_AT`"; Noon does no templating.

Ad hoc, without editing YAML:

```sh
noon add pr-risks "weekdays at 9am" --prompt "List open PRs and their main risks." --agent claude-git
noon run release-notes --in 2h      # queue a one-shot for later (needs enable + serve)
```
