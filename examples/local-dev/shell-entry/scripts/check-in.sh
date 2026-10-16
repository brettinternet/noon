#!/bin/sh
# Print a short project status; exit 1 when something needs attention before you start.
set -u
attention=0

if git fetch --quiet 2>/dev/null && git rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
  behind=$(git rev-list --count 'HEAD..@{upstream}')
  ahead=$(git rev-list --count '@{upstream}..HEAD')
  printf 'branch %s: %s behind, %s ahead\n' "$(git branch --show-current)" "$behind" "$ahead"
  [ "$behind" -eq 0 ] || { echo "-> pull before starting"; attention=1; }
fi

if [ -f bun.lock ] && [ bun.lock -nt node_modules ]; then
  echo "-> bun.lock changed since the last install: run bun install"
  attention=1
fi

stashes=$(git stash list | wc -l | tr -d ' ')
[ "$stashes" -eq 0 ] || printf 'note: %s stash entries\n' "$stashes"

exit "$attention"
