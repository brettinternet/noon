#!/bin/sh
# git fetch --prune every repository directly under CODE_DIR. Continues past failures,
# then exits 1 if any repository failed so the run is marked failed.
set -u
: "${CODE_DIR:?set CODE_DIR}"

failed=0
for repo in "$CODE_DIR"/*/; do
  [ -e "$repo/.git" ] || continue
  if git -C "$repo" fetch --all --prune --quiet 2>&1; then
    printf 'ok      %s\n' "$repo"
  else
    printf 'FAILED  %s\n' "$repo"
    failed=1
  fi
done
exit "$failed"
