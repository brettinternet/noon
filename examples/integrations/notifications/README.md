# Notifications

**Level:** beginner → intermediate · **Shows:** user-level hook defaults, per-job overrides and opt-out, hook stdin JSON, recovery alerts

| Script | Sends | Needs |
| --- | --- | --- |
| [`noon-notify`](bin/noon-notify) | macOS / Linux desktop notification with the last log line | `osascript` or `notify-send` |
| [`noon-ntfy`](bin/noon-ntfy) | Phone push with the log tail; high priority on failure | `NTFY_TOPIC` |
| [`noon-slack`](bin/noon-slack) | Slack message from the run JSON on stdin | `SLACK_WEBHOOK_URL`, `jq` |
| [`noon-recovery`](bin/noon-recovery) | Failures, plus "recovered" on the first success after one | `noon-notify`, `jq` |

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

Secrets such as `SLACK_WEBHOOK_URL` and `NTFY_TOPIC` belong in the scheduler's environment
(your shell for `noon serve`, the service environment for `noon service install`), never in YAML.
