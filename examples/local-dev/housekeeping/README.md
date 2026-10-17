# Machine housekeeping

**Level:** intermediate · **Shows:** personal machine-wide chores, report-only jobs, failure as an alert

Keep machine chores in your Noon user project, not in a stand-in repository. Copy the example into
the user config directory (`$XDG_CONFIG_HOME/noon`, else `~/.config/noon`), set `CODE_DIR` in `config.yaml`, and try a
job before enabling schedules:

```sh
config_home="${XDG_CONFIG_HOME:-$HOME/.config}"
umask 077
mkdir -p "$config_home/noon"
chmod 700 "$config_home/noon"
cp -R examples/local-dev/housekeeping/. "$config_home/noon/"
chmod 600 "$config_home/noon/config.yaml"
$EDITOR "$config_home/noon/config.yaml"  # set CODE_DIR
noon --user run disk-space
noon --user enable
noon service install
```

Scripts are relative to the user project root, so `./bin/...` resolves beside `config.yaml`.
`noon service install` runs all enabled projects; it is intentionally used without `--user`.

| Job | What it changes |
| --- | --- |
| `repos-fetch` | Remote-tracking refs only |
| `gone-branches` | Nothing; prints branches safe to review for deletion |
| `docker-prune` | Deletes unused Docker data older than 7 days |
| `brew-outdated` | Homebrew metadata only |
| `disk-space` | Nothing; exits 1 above the threshold |

Report-only jobs are the safe default for unattended runs. Read their output with
`noon --user logs gone-branches`. For alerting jobs such as `disk-space`, set a user-level
`on_failure` hook from [notifications](../../integrations/notifications/).
