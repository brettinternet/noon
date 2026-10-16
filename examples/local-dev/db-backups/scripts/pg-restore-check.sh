#!/bin/sh
set -eu
: "${PGDATABASE:?set PGDATABASE}" "${BACKUP_DIR:?set BACKUP_DIR}" "${CHECK_SQL:?set CHECK_SQL}"
. "$(dirname "$0")/common.sh"
latest=$(backup_files "$BACKUP_DIR" "$PGDATABASE" .dump | head -n 1)
[ -n "$latest" ] || { echo "no dumps in $BACKUP_DIR" >&2; exit 1; }

# A fixed name is intentional: createdb is the atomic ownership check. Never
# pre-drop it. Concurrent drills fail safely rather than sharing a database.
scratch="${PGDATABASE}_restore_check"
[ "${#scratch}" -le 63 ] || { echo 'scratch database name exceeds PostgreSQL identifier limit' >&2; exit 1; }
createdb -- "$scratch"
trap 'dropdb -- "$scratch"' EXIT
trap 'exit 1' HUP INT TERM
pg_restore --no-owner --exit-on-error --dbname "$scratch" "$latest"
result=$(psql -X --set ON_ERROR_STOP=1 --dbname "$scratch" --tuples-only --no-align --command "$CHECK_SQL")
printf 'restored %s into %s; check returned %s\n' "$(basename "$latest")" "$scratch" "$result"
