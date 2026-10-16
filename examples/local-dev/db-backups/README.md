# Local database backups

**Level:** intermediate · **Shows:** `missed: run_once` catch-up, restore drills, retention, secrets outside YAML

| Job | When | Does |
| --- | --- | --- |
| `pg-backup` | 3am, or once on wake | Custom-format `pg_dump`, keep newest 14 |
| `pg-restore-check` | Sunday 11am | Create `app_dev_restore_check`, restore newest dump, report `CHECK_SQL`, drop only the database it created |
| `sqlite-backup` | hourly | `VACUUM INTO` snapshot + `integrity_check`, keep newest 48 |

```sh
noon run pg-backup
noon run pg-restore-check
noon history pg-restore-check --status failed
```

Credentials: `env` values are committed with `noon.yaml`, so use libpq's `~/.pgpass` (mode 0600)
or a `PGSERVICEFILE`. Put machine paths in a private `.noon.yaml` replacement if they differ per
developer (see [local overrides](../../basics/local-overrides/)). Job `env` overrides inherited
variables: edit/replace the job definitions, not just your shell environment.

Use matching-major PostgreSQL clients/server and SQLite with `VACUUM INTO` support (3.27+).
The restore user needs database create/drop privileges. An existing scratch database makes the
drill **fail without touching it**; concurrent drills likewise fail safely. Restore and SQL errors
fail the job. A successful query result is printed, not interpreted as an application-specific
row-count assertion. Keep database names short enough that `<name>_restore_check` fits PostgreSQL's
63-byte identifier limit (use ASCII names).

Backups use `PREFIX-YYYYMMDDTHHMMSS-TOKEN.dump` (or `.db`), with UTC timestamps and a six-character
random alphanumeric token. Same-second runs get separate names. Retention orders names descending
(timestamp first; ties by token), not filesystem modification time. `KEEP` must be a positive
integer without leading zeros. Only matching regular files are pruned; prefixes/suffixes are
literal. Other names, legacy timestamp-only backups, partials, directories, and symlinks survive.

Each invocation reserves a private `.noon-backup.TOKEN` staging directory in the destination directory. A
successful dump must also pass `pg_restore --list`; SQLite must return exactly `ok` from
`integrity_check`. Only then is output published with a no-overwrite hard link, before retention.
Failures remove the owned staging output and directory and leave existing backups intact. SIGKILL/power loss can
leave a hidden partial; it is never retained or selected for restore. Full restore drills, not just
archive listing, establish that a Postgres backup is usable.

Use trusted/private backup directories on a filesystem supporting hard links; these examples do
not defend against another user maliciously replacing paths during execution. Paths can contain
spaces/apostrophes, but not newlines; database/file prefixes must not contain `/`. SQLite waits up
to five seconds for conflicting locks, then fails without publishing or pruning. A writer may
prevent a snapshot: `VACUUM INTO` is not a promise of immediate success under every write workload.
Schedule retries as appropriate.

## Opt-in verification

From the repository root, with Docker running a Linux engine:

```sh
task examples:db-backups        # real host clients; macOS evidence when run on macOS
task examples:db-backups:linux  # real Debian clients inside postgres:17-bookworm
```

The host target requires PostgreSQL 17 clients and SQLite on `PATH`; for a mise-installed
PostgreSQL, use `mise exec postgres@17.11 -- task examples:db-backups`. The Linux target needs Go
for cross-compilation and network access to install SQLite in its disposable Debian container.
Both print actual OS/client/server versions. Neither target is part of `task ci`.

The harness copies this example, replaces its job environment with isolated paths, seeds real
rows, and executes jobs through built Noon. It checks retention, restore contents and cleanup,
invalid inputs, partial dump failure, SQLite contention, and publication failures. A uniquely
labelled PostgreSQL container is removed only after checking ownership. Local fixtures (including
Noon history/logs and ownership markers) are deliberately retained at the printed canonical
OS-temporary path for diagnosis; no user database, backup directory, HOME, or Noon state is used.
