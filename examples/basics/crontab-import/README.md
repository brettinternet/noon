# Migrate a crontab

**Level:** beginner · **Shows:** preview-first `noon import crontab`, refused cron semantics

```sh
cd examples/basics/crontab-import
noon import crontab crontab.txt             # preview only; writes nothing
crontab -l | noon import crontab -          # your real crontab
crontab -l | noon import crontab - --write  # append to .git/noon.yaml (all-or-nothing)
```

[`crontab.txt`](crontab.txt) previews as:

```yaml
jobs:
  # crontab line 7
  backup-dotfiles-sh:
    env:
        BACKUP_DIR: /Users/me/Backups
        PATH: /usr/local/bin:/usr/bin:/bin
    # Cron never catches up; skip missed runs.
    missed: skip
    run: /Users/me/bin/backup-dotfiles.sh
    schedule: 30 2 * * *
    # Cron has no job timeout.
    timeout: none
  # crontab line 9
  brew:
  ...
```

Names come from the command's first word, so rename generic ones (`find`, `brew`) after
importing. Consider dropping `missed: skip` for backups: unlike cron, Noon can catch up once
after the laptop wakes. Add a `timeout` so a hung job cannot block the next run forever.

[`crontab-refused.txt`](crontab-refused.txt) shows what Noon refuses instead of approximating:

```text
noon: line 2: non-empty MAILTO is not supported
line 3: @reboot is not supported; Noon has no boot trigger
line 4: % in a command is not supported because cron turns it into newlines and stdin
Hint: Fix the listed crontab lines; nothing was written.
```

Replace `MAILTO` with an `on_failure` hook ([notifications](../../integrations/notifications/)),
and move `%` commands into a script.
