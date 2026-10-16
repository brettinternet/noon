#!/bin/sh
# List local branches whose upstream no longer exists, per repository. Read-only.
set -eu
: "${CODE_DIR:?set CODE_DIR}"

for repo in "$CODE_DIR"/*/; do
  [ -e "$repo/.git" ] || continue
  gone=$(git -C "$repo" for-each-ref --format='%(refname:short) %(upstream:track)' refs/heads | awk '$2 == "[gone]" {print $1}')
  [ -n "$gone" ] || continue
  printf '%s\n' "$repo"
  printf '%s\n' "$gone" | sed 's/^/  /'
done
