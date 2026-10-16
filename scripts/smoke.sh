#!/bin/sh
set -eu

noon_binary="$(pwd)/bin/noon"
noon_smoke_dir="$(mktemp -d "${TMPDIR:-/tmp}/noon-smoke.XXXXXX")"
printf '%s\n' 'noon CLI smoke fixture' > "$noon_smoke_dir/.noon-smoke-owner"
printf 'Smoke fixture retained at %s\n' "$noon_smoke_dir"
cd "$noon_smoke_dir"

noon() {
  env -i PATH="$PATH" HOME="$noon_smoke_dir" XDG_CONFIG_HOME="$noon_smoke_dir/config" "$noon_binary" "$@"
}

noon --version
noon
noon config check
noon --json status
test ! -e "$noon_smoke_dir/.local/state/noon"
test ! -e noon.yaml
noon init
cp noon.yaml original.yaml
noon init
cmp noon.yaml original.yaml
noon --json config check
if noon --json --config missing.yaml config check > failure.json; then
  printf '%s\n' 'missing explicit config incorrectly succeeded' >&2
  exit 1
else
  test "$?" -eq 1
fi
grep -q '"error":{"code":"config","message":"' failure.json
if noon --json --bogus > usage.json; then
  printf '%s\n' 'unknown flag incorrectly succeeded' >&2
  exit 1
else
  test "$?" -eq 2
fi
grep -q '"error":{"code":"usage","message":"' usage.json
noon --help > help.txt
