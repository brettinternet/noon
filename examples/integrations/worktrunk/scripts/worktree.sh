#!/bin/sh
# worktree.sh setup|teardown
# Everything this creates is named after the checkout's absolute path, so teardown removes this
# worktree's resources and nothing else, even when several worktrees run at once.
set -eu
name=$(basename "$PWD" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n' '-' | sed 's/^-*//')
sum=$(printf '%s' "$PWD" | cksum | cut -d ' ' -f 1)
export COMPOSE_PROJECT_NAME="${name:-checkout}-$sum"

case ${1:-} in
  setup)
    # Add project preparation here, such as `mise install` or `bun install --frozen-lockfile`.
    docker compose up --detach --wait db
    port=$(docker compose port db 5432 | sed 's/.*://')
    printf 'COMPOSE_PROJECT_NAME=%s\nDATABASE_URL=postgres://app:app@127.0.0.1:%s/app\n' \
      "$COMPOSE_PROJECT_NAME" "$port" > .env.local
    echo "database $COMPOSE_PROJECT_NAME on 127.0.0.1:$port (written to .env.local)"
    ;;
  teardown)
    docker compose down --volumes --remove-orphans
    echo "removed database $COMPOSE_PROJECT_NAME"
    ;;
  *)
    echo "usage: worktree.sh setup|teardown" >&2
    exit 2
    ;;
esac
