# AI daily standup

**Level:** intermediate · **Shows:** script-driven agent pipelines, `NOON_LAST_SUCCESS_AT`, `missed: run_once`

At 8:45 on weekdays the job collects your commits across several repositories plus open PRs and
review requests (via `gh`), asks an agent to turn them into Yesterday / Today / Blockers, and
writes `~/notes/standup/YYYY-MM-DD.md`. The note is also in the run log:

```sh
noon run standup
noon logs standup
```

**Why a script instead of a prompt job?** The facts are gathered deterministically and passed in
the prompt, so the model needs no tools or permissions and cannot wander. Use prompt jobs when the
agent must explore; use a script when you already know the inputs.

`NOON_LAST_SUCCESS_AT` is the start of the last successful run, so Monday's note covers Friday
onward and a failed day is included in the next one. Edit `REPOS`, `STANDUP_DIR`, and `AGENT_CMD`
(e.g. `codex exec -` or `ollama run llama3.2`) in a private `.noon.yaml` copy of the job.
