#!/bin/sh
set -eu

bump_binary="$(pwd)/bin/bump"
bump_smoke_dir="$(mktemp -d "${TMPDIR:-/tmp}/bump-smoke.XXXXXX")"
printf '%s\n' 'bump CLI smoke fixture' > "$bump_smoke_dir/.bump-smoke-owner"
printf 'Smoke fixture retained at %s\n' "$bump_smoke_dir"
cd "$bump_smoke_dir"

bump() {
  env -i PATH="$PATH" HOME="$bump_smoke_dir" XDG_CONFIG_HOME="$bump_smoke_dir/config" "$bump_binary" "$@"
}

bump --version
bump
bump config check
bump --json status
test ! -e .bump
test ! -e bump.yaml
bump init
cp bump.yaml original.yaml
bump init
cmp bump.yaml original.yaml
bump --json config check
if bump --json --config missing.yaml config check > failure.json; then
  printf '%s\n' 'missing explicit config incorrectly succeeded' >&2
  exit 1
else
  test "$?" -eq 1
fi
grep -q '"error":{"code":"config","message":"' failure.json
if bump --json --bogus > usage.json; then
  printf '%s\n' 'unknown flag incorrectly succeeded' >&2
  exit 1
else
  test "$?" -eq 2
fi
grep -q '"error":{"code":"usage","message":"' usage.json
bump --help > help.txt
