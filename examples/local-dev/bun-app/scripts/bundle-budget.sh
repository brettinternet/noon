#!/bin/sh
# Fail when the built JavaScript in BUNDLE_DIR exceeds BUDGET_KB kilobytes.
set -eu
: "${BUNDLE_DIR:?set BUNDLE_DIR}" "${BUDGET_KB:?set BUDGET_KB}"

bun run build >/dev/null
total_kb=$(find "$BUNDLE_DIR" -name '*.js' -type f -exec cat {} + | gzip -c | wc -c | awk '{print int($1 / 1024)}')
printf 'gzipped JS: %s KiB (budget %s KiB)\n' "$total_kb" "$BUDGET_KB"
if [ "$total_kb" -gt "$BUDGET_KB" ]; then
  printf 'over budget by %s KiB\n' "$((total_kb - BUDGET_KB))" >&2
  exit 1
fi
