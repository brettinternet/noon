# Examples

Most directories are self-contained Noon projects: a `noon.yaml`, the scripts it runs, and a README
explaining the scenario. [Machine housekeeping](local-dev/housekeeping/) is the user-project example:
copy its `config.yaml` and scripts into `$XDG_CONFIG_HOME/noon` (else `~/.config/noon`) and use
`noon --user`. Copy what you need into your repository, or inspect a repository project in place:

```sh
noon -C examples/basics/hello list
noon -C examples/basics/hello run hello
```

Experiment without touching your real run history by pointing Noon at a scratch state directory:

```sh
export NOON_STATE_DIR="$(mktemp -d)"
```

Paths such as `/Users/me/...` are placeholders: YAML does no `~` or `$HOME` expansion. Scripts are
POSIX `sh`; several use `jq`, `gh`, or an agent CLI, noted in each README. Every `noon.yaml` here
is validated by `integration/examples_test.go`.

## Basics

| Example | Level | What it shows |
| --- | --- | --- |
| [hello](basics/hello/) | beginner | argv vs shell jobs, the `NOON_*` job environment, a cron schedule |
| [schedules](basics/schedules/) | beginner | every schedule form, `timezone`, `overlap`, `missed`, `timeout`, `enabled` |
| [local-overrides](basics/local-overrides/) | beginner | config layers, toggles, replacements, and OS/architecture constraints |
| [event-requests](basics/event-requests/) | beginner | `merge` vs `each`, preserved/discarded payloads, idempotency keys |
| [crontab-import](basics/crontab-import/) | beginner | preview-first `noon import crontab` and what it refuses |

## Local development

| Example | Level | What it shows |
| --- | --- | --- |
| [go-project](local-dev/go-project/) | intermediate | test/race/vet/vulncheck cadence, cache warming, `cooldown` |
| [bun-app](local-dev/bun-app/) | intermediate | quick checks vs nightly e2e, a bundle-size budget gate |
| [housekeeping](local-dev/housekeeping/) | intermediate | user-project chores: fetch all repos, Docker prune, disk alerts |
| [db-backups](local-dev/db-backups/) | intermediate | Postgres/SQLite backups, retention, weekly restore drills, catch-up after sleep |
| [git-hooks](local-dev/git-hooks/) | intermediate | post-commit/merge/checkout queueing, idempotency keys, event payloads |
| [shell-entry](local-dev/shell-entry/) | intermediate | a check-in job when you `cd` into a project, with failure notifications |
| [file-watch](local-dev/file-watch/) | intermediate | watchexec re-running jobs after edits: foreground vs `--detach`, never losing the last save |
| [pipelines](local-dev/pipelines/) | advanced | build → test → package chained by `on_success` hooks; recursion depth |

## AI workflows

| Example | Level | What it shows |
| --- | --- | --- |
| [agent-profiles](ai/agent-profiles/) | intermediate | built-in Gemini/OpenCode/Pi jobs, custom Claude/Codex permissions, Ollama |
| [daily-standup](ai/daily-standup/) | intermediate | commits + PRs since `NOON_LAST_SUCCESS_AT` summarized into a dated note |
| [research-digest](ai/research-digest/) | intermediate | HN/arXiv reading list and a release radar for your tools |
| [local-llm](ai/local-llm/) | intermediate | private jobs with Ollama: model updates, warm-up, notes and failure digests |
| [verify-loop](ai/verify-loop/) | intermediate → advanced | agent-facing descriptions, MCP, Claude/Codex `Stop` hooks, a blocking completion gate |
| [failure-triage](ai/failure-triage/) | advanced | `on_failure` queues an agent that diagnoses the failed run's log |
| [background-agents](ai/background-agents/) | advanced | editing agents in throwaway worktrees: weekly dependency upgrades and a task inbox |
| [container-agent](ai/container-agent/) | advanced | an editing agent sandboxed in Docker: sees one worktree, scoped credentials, commits without hooks |
| [pr-review](ai/pr-review/) | advanced | signed GitHub webhooks → AI PR review and issue triage, side effects opt-in |

## Integrations

| Example | Level | What it shows |
| --- | --- | --- |
| [remote-access](integrations/remote-access/) | intermediate | authenticated API/HTTP MCP, named Cloudflare Tunnel, optional ngrok |
| [notifications](integrations/notifications/) | beginner → intermediate | desktop, ntfy, and Slack hooks; per-job opt-out; recovery alerts |
| [heartbeat](integrations/heartbeat/) | intermediate | external missing-ping alerts for jobs and scheduler liveness |
| [tell](integrations/tell/) | beginner | inline `on_failure` hooks that send failure pointers through Tell and Apprise |
| [docker-agent](integrations/docker-agent/) | advanced | agent jobs in Docker with mounted user config, secrets, and a writable reports volume |
| [worktrunk](integrations/worktrunk/) | intermediate | Worktrunk hooks running `noon run` to set up and tear down a database per worktree |
| [compose.yaml](compose.yaml) | intermediate | minimal production Compose service ([Docker](../docs/docker.md)) |
| [jobs.yaml](jobs.yaml) | reference | every job field in one file ([Configuration](../docs/configuration.md#jobs)) |

## Patterns used throughout

- **Failure is the alarm.** Report-style jobs exit non-zero when something needs attention and
  let an `on_failure` hook notify you.
- **Triggers are wake-ups by default.** `requests: merge` (used by the reconciliation
  examples) merges a due-now request into an existing queued run and drops the new payload, so jobs
  should reconcile current state. Opt into `requests: each` when each event needs its own run and
  payload; due-now each requests are capped at 20 queued runs per job, and overdue work expires
  after an hour. Jobs that cannot miss events should still reconcile outstanding work.
- **No work is not success.** Polling jobs use `noop_exit_code: 78` when there is nothing to do.
  `noon run` then exits 0 but stores `noop`: neither hook runs, `NOON_LAST_SUCCESS_AT` does not
  advance, and the last meaningful result stays visible in `list`. Inspect polls with
  `noon history --status noop` or `noon logs JOB --status noop`. Verification must still succeed.
- **Gather deterministically, summarize with a model.** AI scripts collect facts themselves and
  give the model no tools, so untrusted input (logs, PR text, feeds) cannot make it act.
- **Unattended means non-interactive.** Agent profiles deny or pre-approve permissions; none can
  hang a scheduled run waiting for a prompt.
- **Side effects are opt-in.** Posting comments, applying labels, pushing branches, and deleting
  data are off by default or limited to manual jobs.
- **Secrets stay out of YAML.** Tokens and webhook URLs come from the scheduler's environment.
- **Approval is not a sandbox.** `noon enable` approves job definitions, not the scripts or prompt
  files they reference. Review those like any code you run.
