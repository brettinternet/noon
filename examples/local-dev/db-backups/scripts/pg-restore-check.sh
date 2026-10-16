#!/bin/sh
set -eu
: "${PGDATABASE:?set PGDATABASE}" "${BACKUP_DIR:?set BACKUP_DIR}" "${CHECK_SQL:?set CHECK_SQL}"

latest=$(ls -1t "$BACKUP_DIR"/"$PGDATABASE"-*.dump 2>/dev/null | head -n 1)
[ -n "$latest" ] || { echo "no dumps in $BACKUP_DIR" >&2; exit 1; }

scratch="${PGDATABASE}_restore_check"
trap 'dropdb --if-exists "$scratch"' EXIT
dropdb --if-exists "$scratch"
createdb "$scratch"
pg_restore --no-owner --exit-on-error --dbname "$scratch" "$latest"
result=$(psql --dbname "$scratch" --tuples-only --no-align --command "$CHECK_SQL")
printf 'restored %s into %s; check returned %s\n' "$(basename "$latest")" "$scratch" "$result"
