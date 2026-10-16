# Background agents in worktrees

**Level:** advanced · **Shows:** unattended editing agents, Git worktree isolation, check-then-commit, a file-based task queue

| Job | When | Does |
| --- | --- | --- |
| `deps-upgrade` | Monday 6am | Upgrade dependencies, fix fallout, commit on `agent/deps-upgrade-<time>` |
| `inbox` | every 30m, 8am–6pm weekdays | Run the oldest `agent-inbox/*.md` task the same way |

Every run:

1. creates `.worktrees/<name>-<time>` on a new `agent/...` branch from `origin/main`,
2. runs the agent there (default `codex exec --sandbox workspace-write -`, prompt on stdin),
3. removes the worktree if nothing changed, otherwise runs `CHECK_CMD`,
4. commits on success and removes the worktree (the branch stays), or keeps the worktree for
   inspection on failure,
5. pushes and opens a draft PR only when `OPEN_PR=1`.

Your own checkout is never modified. Review results with `git log agent/deps-upgrade-...` or
`noon logs deps-upgrade`.

```sh
cp -R examples/ai/background-agents/scripts examples/ai/background-agents/prompts .
cp examples/ai/background-agents/noon.yaml .
printf '.worktrees/\nagent-inbox/\n' >> .gitignore
mkdir -p agent-inbox
$EDITOR agent-inbox/add-export-json.md   # one task per file
noon run inbox                           # try it in the foreground first
noon enable && noon service install
```

Things to know:

- **One run per project at a time.** Noon's scheduler starts at most one run per project, so a 40
  minute agent run delays this project's other scheduled jobs. If that matters, put agent jobs in
  their own small Noon project and point `cwd` at the repository's absolute path.
- The sandbox flag is the agent's own isolation. Approval (`noon enable`) is not a sandbox; the
  agent runs with your account's permissions. Keep `OPEN_PR` off until you trust the output.
- Inbox task files are untracked inputs, not part of the approved job definition. Anyone who can
  write `agent-inbox/` can instruct the agent, so keep it local and ignored.
- `timeout` bounds a runaway agent: Noon sends TERM to the whole process group, then KILL.
