#!/bin/sh
set -eu

fail() {
  printf 'release:check: %s\n' "$*" >&2
  exit 1
}

version=${VERSION:-}
commit=${COMMIT:-}
build_time=${BUILD_TIME:-}
[ -n "$version" ] || fail 'VERSION is required (for example, VERSION=1.2.3)'
[ -n "$commit" ] || fail 'COMMIT is required (the full release commit SHA)'
[ -n "$build_time" ] || fail 'BUILD_TIME is required (UTC RFC 3339, for example, 2026-10-16T12:00:00Z)'
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || fail "invalid stable version: $version"
printf '%s\n' "$commit" | grep -Eq '^[0-9a-f]{40}$' || fail 'COMMIT must be a full lowercase 40-character Git SHA'
printf '%s\n' "$build_time" | grep -Eq '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' || fail 'BUILD_TIME must be UTC RFC 3339 with second precision'

host_goos=$(mise exec go -- go env GOOS)
host_goarch=$(mise exec go -- go env GOARCH)
case "$host_goos/$host_goarch" in
  linux/amd64) native_target='linux amd64 linux x64' ;;
  linux/arm64) native_target='linux arm64 linux arm64' ;;
  darwin/amd64) native_target='darwin amd64 macos x64' ;;
  darwin/arm64) native_target='darwin arm64 macos arm64' ;;
  *) fail "unsupported native host $host_goos/$host_goarch; use macOS or Linux amd64/arm64" ;;
esac

output="dist/release/v$version"
stage=$(mktemp -d "${TMPDIR:-/tmp}/noon-release-check.XXXXXX")
mkdir -p "$stage/bin" "$output"
trap 'rmdir "$stage/bin" "$stage" 2>/dev/null || true' EXIT

build_archive() {
  goos=$1
  goarch=$2
  asset_os=$3
  asset_arch=$4
  native=$5
  archive="noon-v${version}-${asset_os}-${asset_arch}.tar.gz"

  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" mise exec go -- go build \
    -trimpath \
    -ldflags "-s -w -X main.buildVersion=${version} -X main.buildCommit=${commit} -X main.buildTime=${build_time}" \
    -o "$stage/bin/noon" ./cmd/noon
  tar -C "$stage" -czf "$output/$archive" bin/noon
  members=$(tar -tzf "$output/$archive")
  [ "$members" = 'bin/noon' ] || fail "$archive must contain exactly bin/noon; found: $members"

  if [ "$native" = yes ]; then
    mv -f "$stage/bin/noon" "$output/native-noon"
  fi
}

for target in \
  'linux amd64 linux x64' \
  'linux arm64 linux arm64' \
  'darwin amd64 macos x64' \
  'darwin arm64 macos arm64'
do
  # The host binary is built last so it can also be checked and retained for smoke tests.
  # shellcheck disable=SC2086
  set -- $target
  if [ "$1/$2" = "$host_goos/$host_goarch" ]; then
    continue
  fi
  build_archive "$1" "$2" "$3" "$4" no
done

# shellcheck disable=SC2086
set -- $native_target
build_archive "$1" "$2" "$3" "$4" yes

[ -x "$output/native-noon" ] || fail 'native release binary was not built'
short_commit=$(printf '%.7s' "$commit")
expected="noon $version ($short_commit, built $build_time)"
actual=$("$output/native-noon" --version)
[ "$actual" = "$expected" ] || fail "native metadata mismatch: expected '$expected', got '$actual'"

actual=$("$output/native-noon" version)
[ "$actual" = "$expected" ] || fail "version subcommand mismatch: expected '$expected', got '$actual'"

# Build the checksum list from the same four versioned assets the workflow will publish.
(
  cd "$output"
  shasum -a 256 \
    "noon-v${version}-linux-x64.tar.gz" \
    "noon-v${version}-linux-arm64.tar.gz" \
    "noon-v${version}-macos-x64.tar.gz" \
    "noon-v${version}-macos-arm64.tar.gz" > checksums.txt
  [ "$(wc -l < checksums.txt | tr -d ' ')" = 4 ] || exit 1
  shasum -a 256 -c checksums.txt
)

printf 'Validated release assets in %s\n' "$output"
printf 'Native metadata: %s\n' "$actual"
printf 'Archives: noon-v%s-{linux-x64,linux-arm64,macos-x64,macos-arm64}.tar.gz\n' "$version"
printf 'Checksums: %s/checksums.txt\n' "$output"
printf 'Native smoke binary: %s/native-noon\n' "$output"
