# Examples

Each directory is a self-contained Noon project: a `noon.yaml`, the scripts it runs, and a README
explaining the scenario. Copy what you need into your repository, or inspect one in place:

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
| [local-overrides](basics/local-overrides/) | beginner | `noon.yaml` vs private `.git/noon.yaml` vs user config; toggles and replacements |
| [crontab-import](basics/crontab-import/) | beginner | preview-first `noon import crontab` and what it refuses |

## Local development

| Example | Level | What it shows |
| --- | --- | --- |
| [go-project](local-dev/go-project/) | intermediate | test/race/vet/vulncheck cadence, cache warming, `cooldown` |
| [bun-app](local-dev/bun-app/) | intermediate | quick checks vs nightly e2e, a bundle-size budget gate |
| [housekeeping](local-dev/housekeeping/) | intermediate | machine-wide chores from one personal project: fetch all repos, Docker prune, disk alerts |
| [db-backups](local-dev/db-backups/) | intermediate | Postgres/SQLite backups, retention, weekly restore drills, catch-up after sleep |
| [git-hooks](local-dev/git-hooks/) | intermediate | post-commit/merge/checkout queueing, idempotency keys, event payloads |
| [shell-entry](local-dev/shell-entry/) | intermediate | a check-in job when you `cd` into a project, with failure notifications |
| [pipelines](local-dev/pipelines/) | advanced | build → test → package chained by `on_success` hooks; recursion depth |

## AI workflows

| Example | Level | What it shows |
| --- | --- | --- |
| [agent-profiles](ai/agent-profiles/) | intermediate | `prompt`/`prompt_file` jobs, read-only vs editing profiles for Claude, Codex, Gemini, Ollama |
| [daily-standup](ai/daily-standup/) | intermediate | commits + PRs since `NOON_LAST_SUCCESS_AT` summarized into a dated note |
| [research-digest](ai/research-digest/) | intermediate | HN/arXiv reading list and a release radar for your tools |
| [local-llm](ai/local-llm/) | intermediate | private jobs with Ollama: model updates, warm-up, notes and failure digests |
| [verify-loop](ai/verify-loop/) | intermediate → advanced | agent-facing descriptions, MCP, Claude/Codex `Stop` hooks, a blocking completion gate |
| [failure-triage](ai/failure-triage/) | advanced | `on_failure` queues an agent that diagnoses the failed run's log |
| [background-agents](ai/background-agents/) | advanced | editing agents in throwaway worktrees: weekly dependency upgrades and a task inbox |
| [pr-review](ai/pr-review/) | advanced | signed GitHub webhooks → AI PR review and issue triage, side effects opt-in |

## Integrations

| Example | Level | What it shows |
| --- | --- | --- |
| [notifications](integrations/notifications/) | beginner → intermediate | desktop, ntfy, and Slack hooks; per-job opt-out; recovery alerts |
| [docker-agent](integrations/docker-agent/) | advanced | agent jobs in Docker with mounted user config, secrets, and a writable reports volume |
| [compose.yaml](compose.yaml) | intermediate | minimal production Compose service ([Docker](../docs/docker.md)) |
| [jobs.yaml](jobs.yaml) | reference | every job field in one file ([Configuration](../docs/configuration.md#jobs)) |

## Patterns used throughout

- **Failure is the alarm.** Report-style jobs exit non-zero when something needs attention and
  let an `on_failure` hook notify you.
- **Triggers are wake-ups; scripts reconcile.** A request that arrives while a run of the same job
  is already waiting is merged into that run, and its payload is not kept. Jobs that must handle
  every event (PR review, failure triage) list what still needs work and record what they did.
- **Gather deterministically, summarize with a model.** AI scripts collect facts themselves and
  give the model no tools, so untrusted input (logs, PR text, feeds) cannot make it act.
- **Unattended means non-interactive.** Agent profiles deny or pre-approve permissions; none can
  hang a scheduled run waiting for a prompt.
- **Side effects are opt-in.** Posting comments, applying labels, pushing branches, and deleting
  data are off by default or limited to manual jobs.
- **Secrets stay out of YAML.** Tokens and webhook URLs come from the scheduler's environment.
- **Approval is not a sandbox.** `noon enable` approves job definitions, not the scripts or prompt
  files they reference. Review those like any code you run.
