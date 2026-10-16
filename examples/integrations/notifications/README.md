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
