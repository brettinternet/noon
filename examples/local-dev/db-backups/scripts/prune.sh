#!/bin/sh
# prune.sh DIR PREFIX SUFFIX KEEP: retain newest matching regular backups.
set -eu
. "$(dirname "$0")/common.sh"
validate_keep "$4"
backup_files "$1" "$2" "$3" | {
  count=0
  while IFS= read -r file; do
    count=$((count + 1))
    if [ "$count" -gt "$4" ]; then
      # Recheck type before removal; never follow symlinks.
      [ -f "$file" ] && [ ! -L "$file" ] || continue
      printf 'prune %s\n' "$file"
      unlink "$file"
    fi
  done
}
