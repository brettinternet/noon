#!/bin/sh
set -eu
: "${PGDATABASE:?set PGDATABASE}" "${BACKUP_DIR:?set BACKUP_DIR}" "${KEEP:=14}"
. "$(dirname "$0")/common.sh"
prepare_backup "$PGDATABASE" .dump
pg_dump --format=custom --no-owner --file "$partial"
pg_restore --list "$partial" > /dev/null
publish_backup
"$(dirname "$0")/prune.sh" "$BACKUP_DIR" "$PGDATABASE" .dump "$KEEP"
