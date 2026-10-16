# Machine housekeeping

**Level:** intermediate · **Shows:** machine-wide chores from one personal project, report-only jobs, failure as an alert

Noon is repo-local, but one per-user scheduler runs every enabled project. Put chores that are
about your machine rather than a codebase in a small personal repository:

```sh
mkdir -p ~/chores && cp -R examples/local-dev/housekeeping/. ~/chores/
cd ~/chores && git init
$EDITOR noon.yaml          # set CODE_DIR
noon run disk-space        # try one
noon enable
noon service install       # survive logout and reboot
```

| Job | What it changes |
| --- | --- |
| `repos-fetch` | Remote-tracking refs only |
| `gone-branches` | Nothing; prints branches safe to review for deletion |
| `docker-prune` | Deletes unused Docker data older than 7 days |
| `brew-outdated` | Homebrew metadata only |
| `disk-space` | Nothing; exits 1 above the threshold |

Report-only jobs are the safe default for unattended runs. Read their output with
`noon logs gone-branches`. For alerting jobs such as `disk-space`, set a user-level
`on_failure` hook from [notifications](../../integrations/notifications/).
