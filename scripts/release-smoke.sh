#!/bin/sh
set -eu

fail() {
  printf 'release:smoke: %s\n' "$*" >&2
  exit 1
}

binary=${BINARY:-bin/noon}
case "$binary" in
  /*) ;;
  *) binary="$(pwd)/$binary" ;;
esac
[ -x "$binary" ] || fail "binary is missing or not executable: $binary"

version_output=$("$binary" --version)
expected_version=${VERSION:-}
if [ -n "$expected_version" ]; then
  case "$version_output" in
    "noon version $expected_version ("*) ;;
    *) fail "expected version $expected_version, got '$version_output'" ;;
  esac
fi

smoke_dir=$(mktemp -d "${TMPDIR:-/tmp}/noon-release-smoke.XXXXXX")
printf 'Smoke fixture retained at %s\n' "$smoke_dir"
home="$smoke_dir/home"
config="$smoke_dir/config"
xdg_state="$smoke_dir/xdg-state"
state="$smoke_dir/noon-state"
project="$smoke_dir/project"
mkdir -p "$home" "$config" "$xdg_state" "$project"
cat > "$project/noon.yaml" <<'EOF'
version: 1
jobs:
  release-smoke:
    schedule: every day at 2am
    run: [sh, -c, "printf ran > release-smoke-job-ran"]
EOF

command -v lsof >/dev/null 2>&1 || fail 'lsof is required to verify no network listener is opened'
listener_snapshot() {
  snapshot=$1
  if lsof -nP -a -c noon -iTCP -sTCP:LISTEN > "$snapshot.raw" 2> "$snapshot.err"; then
    sort "$snapshot.raw" > "$snapshot"
  else
    lsof_status=$?
    if [ "$lsof_status" -ne 1 ] || [ -s "$snapshot.err" ]; then
      cat "$snapshot.err" >&2
      fail 'lsof could not inspect Noon TCP listeners'
    fi
    : > "$snapshot"
  fi
}
listener_snapshot "$smoke_dir/listeners.before"

cd "$project"
env -i \
  PATH="$PATH" \
  HOME="$home" \
  XDG_CONFIG_HOME="$config" \
  XDG_STATE_HOME="$xdg_state" \
  NOON_STATE_DIR="$state" \
  NOON_PROJECT="$project" \
  "$binary" > "$smoke_dir/inspection.txt" 2>&1 || {
    cat "$smoke_dir/inspection.txt" >&2
    fail 'no-argument inspection failed'
  }
grep -q 'release-smoke' "$smoke_dir/inspection.txt" || fail 'inspection did not read the fixture job'
test ! -e "$project/release-smoke-job-ran" || fail 'no-argument inspection executed a job'
test ! -e "$state" || fail 'no-argument inspection created Noon state'
test ! -e "$home/Library/LaunchAgents/io.github.brettinternet.noon.plist" || fail 'no-argument inspection installed a launchd service'
test ! -e "$config/systemd/user/noon.service" || fail 'no-argument inspection installed a systemd service'

listener_snapshot "$smoke_dir/listeners.after"
cmp -s "$smoke_dir/listeners.before" "$smoke_dir/listeners.after" || fail 'no-argument inspection changed Noon TCP listeners'
printf 'Release smoke passed: %s\n' "$version_output"
