#!/bin/sh
# prune.sh DIR PREFIX SUFFIX KEEP: delete all but the newest KEEP files named PREFIX*SUFFIX in DIR.
set -eu
dir=$1 prefix=$2 suffix=$3 keep=$4
ls -1t "$dir" | grep -E "^${prefix}-[0-9T]+${suffix}\$" | tail -n "+$((keep + 1))" | while IFS= read -r name; do
  printf 'prune %s\n' "$name"
  rm -f -- "$dir/$name"
done
