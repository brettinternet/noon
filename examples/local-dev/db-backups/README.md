# Local database backups

**Level:** intermediate · **Shows:** `missed: run_once` catch-up, restore drills, retention, secrets outside YAML

| Job | When | Does |
| --- | --- | --- |
| `pg-backup` | 3am, or once on wake | `pg_dump` to a timestamped file, keep newest 14 |
| `pg-restore-check` | Sunday 11am | Restore newest dump into `app_dev_restore_check`, run `CHECK_SQL`, drop it |
| `sqlite-backup` | hourly | `VACUUM INTO` snapshot + `integrity_check`, keep newest 48 |

```sh
noon run pg-backup
noon run pg-restore-check
noon history pg-restore-check --status failed
```

Credentials: `env` values are committed with `noon.yaml`, so use libpq's `~/.pgpass` (mode 0600)
or a `PGSERVICEFILE`. Put machine paths in a private `.noon.yaml` replacement if they differ per
developer (see [local overrides](../../basics/local-overrides/)).

Dumps are written to `*.partial` and renamed on success, so retention never counts a failed dump.
[`scripts/prune.sh`](scripts/prune.sh) only deletes files matching `PREFIX-<timestamp>SUFFIX`.
