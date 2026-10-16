#!/bin/sh
set -eu
: "${PGDATABASE:?set PGDATABASE}" "${BACKUP_DIR:?set BACKUP_DIR}" "${KEEP:=14}"
mkdir -p "$BACKUP_DIR"

stamp=$(date +%Y%m%dT%H%M%S)
dest="$BACKUP_DIR/$PGDATABASE-$stamp.dump"
# Write to a temporary name so a failed or interrupted dump never looks like a backup.
pg_dump --format=custom --no-owner --file "$dest.partial"
mv "$dest.partial" "$dest"
printf 'wrote %s (%s)\n' "$dest" "$(du -h "$dest" | cut -f1)"

"$(dirname "$0")/prune.sh" "$BACKUP_DIR" "$PGDATABASE" .dump "$KEEP"
