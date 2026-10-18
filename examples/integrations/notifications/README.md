# Notifications

**Level:** beginner → intermediate · **Shows:** user-level hook defaults, per-job overrides and opt-out, hook stdin JSON, recovery alerts

| Script | Sends | Needs |
| --- | --- | --- |
| [`noon-notify`](bin/noon-notify) | macOS / Linux desktop notification with the last log line | `osascript` or `notify-send` |
| [`noon-ntfy`](bin/noon-ntfy) | Phone push with the log tail; high priority on failure | `NTFY_TOPIC` |
| [`noon-slack`](bin/noon-slack) | Slack message from the run JSON on stdin | `SLACK_WEBHOOK_URL`, `jq` |
| [`noon-on-change`](bin/noon-on-change) | First unhealthy result, then "recovered" on return to success | Sibling `noon-notify` |

```sh
mkdir -p ~/.local/bin
cp examples/integrations/notifications/bin/* ~/.local/bin/
chmod +x ~/.local/bin/noon-*
$EDITOR ~/.config/noon/config.yaml      # add: on_failure: [/Users/you/.local/bin/noon-notify]
noon run some-failing-job               # hooks run for foreground runs too
noon logs some-failing-job --section hook
```

YAML does no `~` or `$HOME` expansion, so use absolute paths.

How hooks fit together:

- **Defaults** (`on_success`, `on_failure`) come only from user config and apply to every job.
- A job's own hook **replaces** the default; `on_failure: []` turns it off for that job
  ([`noon.yaml`](noon.yaml)).
- `on_failure` fires for `failed` and `timed_out`; cancelled, skipped, and interrupted runs have
  no hook. Hook failures never change the job's result.
- Each hook gets `NOON_JOB`, `NOON_STATUS`, `NOON_EXIT_CODE`, `NOON_LOG_FILE`, `NOON_PROJECT`, and
  the run as JSON on stdin; 30 seconds to finish; output is appended to the run log.

## Pause after repeated failures

Install `bin/noon-pause-after` alongside the other scripts and put `noon` and these scripts
on the scheduler's PATH. The wrapper reads `NOON_CONSECUTIVE_FAILURES`, pauses at
`NOON_PAUSE_AFTER` (default 3; must be a positive integer), then execs the remaining
arguments with the original stdin JSON. Invalid thresholds fail before pausing or notifying.
If the pause itself fails, the wrapper reports it in the run log and still notifies.

```yaml
jobs:
  agent-check:
    schedule: every 1h
    agent: claude
    prompt: Check the repository for actionable failures.
    on_failure: [noon-pause-after, noon-on-change]
    on_success: [noon-on-change]
```

For a non-default state store, explicitly export its **same absolute path** in the scheduler's
environment (or configure it in the service's environment):

```sh
export NOON_STATE_DIR=/home/me/.local/state/noon-agent
export NOON_PAUSE_AFTER=3
noon --state-dir "$NOON_STATE_DIR" serve
```

`noon service install` passes `--state-dir` to the service; this flag is **not** automatically
exported to hook subprocesses. Ensure the service environment also sets `NOON_STATE_DIR` to
that path. Do not infer it from the project directory or log file.

The count includes the current failed/timed-out run and preceding retained failures/timeouts
of the same project/job, ordered by finish time then ID, stopping at the latest success.
Success yields 0. Noop, skipped, missed, expired, cancelled, and interrupted neither count nor
reset. All triggers, including foreground runs, count; definition changes, pause, and resume
do not reset. This is not a lifetime counter: a pre-pruning snapshot can include 50 retained
failures plus the current failure (51). Missed, expired, cancelled, and interrupted consume
regular retention despite not counting, so thresholds above available history are unreliable.

A run dispatched before the hook finishes can still start. After fixing the cause,
`noon run agent-check` can obtain a success even while paused, then `noon resume agent-check`
re-enables background runs. Resume alone does not reset the count: the next failure can
immediately pause again. Hook failure does not change the job result.

## First failure and recovery

Configure **both** `on_failure` and `on_success` with `noon-on-change`, as in
[`noon.yaml`](noon.yaml). Keep `noon-notify` beside the wrapper; it resolves that
sibling directly, not through PATH.

The wrapper uses `NOON_PREVIOUS_STATUS`: `succeeded` is healthy, while `failed`
and `timed_out` form one unhealthy group. It notifies on the first unhealthy
result, stays silent throughout repeated failures (including changes between
failure and timeout), then calls `noon-notify` with `NOON_STATUS=recovered` on
the first success. Further successes stay silent.

An unset previous status defaults to healthy: the first success is silent and
the first failure notifies. Noon supplies the previous eligible result from
retained history; pruning can reset this baseline and make an outage appear new.
This policy belongs to the wrapper. Noon does not retry or deduplicate notification
delivery; a failed notification can be lost even while later failures stay silent.

Secrets such as `SLACK_WEBHOOK_URL` and `NTFY_TOPIC` belong in the scheduler's environment
(your shell for `noon serve`, the service environment for `noon service install`), never in YAML.
