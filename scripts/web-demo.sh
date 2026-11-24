#!/bin/sh
# Serve the web viewer from an isolated sandbox with jobs that pass, fail, and run long.
set -eu

noon_binary="$(pwd)/bin/noon"
noon_demo_dir="${TMPDIR:-/tmp}"
noon_demo_dir="${noon_demo_dir%/}/noon-web-demo"
noon_demo_config="$noon_demo_dir/config/noon"
mkdir -p "$noon_demo_config" "$noon_demo_dir/state"

if [ ! -s "$noon_demo_config/web-token" ]; then
  (umask 077; openssl rand -base64 32 > "$noon_demo_config/web-token")
fi

# Port 8788 leaves the documented 8787 free for a real scheduler.
cat > "$noon_demo_config/config.yaml" <<EOF
serve:
  listen: 127.0.0.1:8788
  web_token_file: $noon_demo_config/web-token
jobs:
  ok:
    run: ["sh", "-c", "echo hello; sleep \$((\$(date +%s) % 5 + 1))"]
    schedule: every 1m
  flaky:
    run: ["sh", "-c", "echo working; printf '\\\\033[31mboom\\\\033[0m\\\\n'; exit 1"]
    schedule: every 2m
  slow:
    run: ["sh", "-c", "for i in \$(seq 60); do echo tick \$i; sleep 1; done"]
    schedule: every 5m
EOF

noon() {
  env HOME="$noon_demo_dir" XDG_CONFIG_HOME="$noon_demo_dir/config" \
    XDG_STATE_HOME="$noon_demo_dir/state" "$noon_binary" "$@"
}
noon --user enable > /dev/null
printf 'Sandbox: %s\nToken:   %s\n' "$noon_demo_dir" "$(cat "$noon_demo_config/web-token")"
noon serve
