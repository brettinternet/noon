#!/bin/sh
set -eu
# The test stage's hook passed its run summary; show where this package came from.
if [ -n "${NOON_EVENT_FILE:-}" ] && command -v jq >/dev/null 2>&1; then
  jq -r '"after \(.data.run.job) run \(.data.run.id): \(.data.run.status)"' "$NOON_EVENT_FILE"
fi
version=$(git describe --tags --always --dirty)
mkdir -p dist
tar -czf "dist/app-$version.tar.gz" bin/
(cd dist && shasum -a 256 "app-$version.tar.gz" > "app-$version.tar.gz.sha256")
printf 'packaged dist/app-%s.tar.gz\n' "$version"
