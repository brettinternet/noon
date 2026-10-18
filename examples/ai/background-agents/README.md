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

The wrapper applies agent edits in the new worktree; this is not a security boundary for
custom agents, repository hooks, or check commands; [container-agent](../container-agent/) adds
one. Review results with `git log agent/deps-upgrade-...` or
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

An empty `agent-inbox/` exits 78, configured as `noop_exit_code`: `noon run inbox` returns 0
but history records `noop`, neither hook fires, and the last meaningful result is unchanged.
Inspect idle polls with `noon history inbox --status noop`; no agent is invoked.

Things to know:

- **One run per project at a time.** Noon's scheduler starts at most one run per project, so a 40
  minute agent run delays this project's other scheduled jobs. If that matters, put agent jobs in
  their own small Noon project and point `cwd` at the repository's absolute path.
- The sandbox flag is the agent's own isolation. Approval (`noon enable`) is not a sandbox; the
  agent runs with your account's permissions. Keep `OPEN_PR` off until you trust the output.
- Inbox task files are untracked inputs, not part of the approved job definition. Anyone who can
  write `agent-inbox/` can instruct the agent, so keep it local and ignored.
- `timeout` bounds a runaway agent: Noon sends TERM to the whole process group, then KILL.

## Verification (2026-10-16)

**Documentation/source/help only; no authenticated agent run.** Codex **0.163.0**
(`codex --version`, `codex exec --help`) and the
[noninteractive reference](https://developers.openai.com/codex/noninteractive) confirm
`exec --sandbox workspace-write -` and stdin prompts. The matching
[exec source](https://github.com/openai/codex/blob/rust-v0.163.0/codex-rs/exec/src/lib.rs)
sets approval policy to Never by default (with AutoReview/configuration exceptions) and
checks for a Git repository, not a preexisting interactive trust entry. A fresh Git worktree
satisfies that repository check; no `--skip-git-repo-check` or permission bypass is needed.
This is not proof that every operation succeeds unattended: auth, dependencies, sandbox
restrictions and organization policy still apply. Failures leave the worktree for inspection.

Project-config trust and [hook trust](https://developers.openai.com/codex/hooks) are separate:
non-managed hooks require prior review/trust, and project configuration may be ignored when
untrusted. Do not bypass trust to make a background run succeed. Review the base repository,
agent configuration and check command first, and test in a disposable checkout before scheduling.
The wrapper's check and Git commit run outside the agent sandbox, so trusted repository hooks
may execute with your account's permissions. See [profile verification](../agent-profiles/#verification-2026-10-16).
