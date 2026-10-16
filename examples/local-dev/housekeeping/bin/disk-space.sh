#!/bin/sh
# disk-space.sh THRESHOLD [PATH]: exit 1 when the volume holding PATH (default $HOME) is more than
# THRESHOLD percent full. $HOME rather than / because macOS mounts / as a small read-only system volume.
set -eu
threshold=${1:-90}
target=${2:-$HOME}
used=$(df -P "$target" | awk 'NR == 2 {gsub("%", "", $5); print $5}')
printf '%s volume %s%% used (threshold %s%%)\n' "$target" "$used" "$threshold"
[ "$used" -le "$threshold" ]
