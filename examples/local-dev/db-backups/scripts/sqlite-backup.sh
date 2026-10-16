#!/bin/sh
set -eu
: "${SQLITE_DB:?set SQLITE_DB}" "${BACKUP_DIR:?set BACKUP_DIR}" "${KEEP:=48}"
mkdir -p "$BACKUP_DIR"

name=$(basename "$SQLITE_DB" .db)
dest="$BACKUP_DIR/$name-$(date +%Y%m%dT%H%M%S).db"
# VACUUM INTO produces a consistent, compact copy even while writers are active.
sqlite3 "$SQLITE_DB" "VACUUM INTO '$dest'"
sqlite3 "$dest" 'PRAGMA integrity_check' | grep -qx ok
printf 'wrote %s\n' "$dest"

"$(dirname "$0")/prune.sh" "$BACKUP_DIR" "$name" .db "$KEEP"
