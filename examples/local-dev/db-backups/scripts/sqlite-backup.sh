#!/bin/sh
set -eu
: "${SQLITE_DB:?set SQLITE_DB}" "${BACKUP_DIR:?set BACKUP_DIR}" "${KEEP:=48}"
. "$(dirname "$0")/common.sh"
[ -f "$SQLITE_DB" ] || { echo "missing SQLite database: $SQLITE_DB" >&2; exit 1; }
name=$(basename "$SQLITE_DB" .db)
prepare_backup "$name" .db
# VACUUM INTO writes an absent path inside our private staging directory.
# Escape SQL literals; wait at most five seconds for a conflicting writer.
quoted=$(printf '%s' "$partial" | sed "s/'/''/g")
sqlite3 -bail -cmd '.timeout 5000' "$SQLITE_DB" "VACUUM INTO '$quoted'"
result=$(sqlite3 -bail "$partial" 'PRAGMA integrity_check')
[ "$result" = ok ] || { echo "integrity_check failed: $result" >&2; exit 1; }
publish_backup
"$(dirname "$0")/prune.sh" "$BACKUP_DIR" "$name" .db "$KEEP"
