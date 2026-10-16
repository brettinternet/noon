#!/usr/bin/env bash
# Opt-in destructive checks, confined to a labelled disposable server and fixtures.
set -euo pipefail

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*"; }

checks() {
  local fixture=$1 binary=$2
  cd "$fixture/project"
  export HOME="$fixture/home" XDG_CONFIG_HOME="$fixture/config" XDG_STATE_HOME="$fixture/state"
  export NOON_STATE_DIR="$fixture/state/noon"
  mkdir -p "$HOME" "$fixture/pg" "$fixture/sqlite space's" "$fixture/tools" "$fixture/data"
  export PGDATABASE=app_dev PGUSER=postgres
  local original_path=$PATH
  local pg_dir="$fixture/pg" sqlite_dir="$fixture/sqlite space's" keep=2 query='select count(*) from users'
  config() {
    export FIXTURE="$fixture" PG_BACKUPS="$pg_dir" SQLITE_BACKUPS="$sqlite_dir" TEST_KEEP="$keep" TEST_QUERY="$query"
    awk '
      /PGHOST:/ { print "      PGHOST: " ENVIRON["PGHOST"]; print "      PGPORT: \"" ENVIRON["PGPORT"] "\""; next }
      /BACKUP_DIR:.*postgres/ { print "      BACKUP_DIR: " ENVIRON["PG_BACKUPS"]; next }
      /BACKUP_DIR:.*sqlite/ { print "      BACKUP_DIR: " ENVIRON["SQLITE_BACKUPS"]; next }
      /KEEP:/ { print "      KEEP: \"" ENVIRON["TEST_KEEP"] "\""; next }
      /CHECK_SQL:/ { print "      CHECK_SQL: " ENVIRON["TEST_QUERY"]; next }
      /SQLITE_DB:/ { print "      SQLITE_DB: " ENVIRON["FIXTURE"] "/data/app.db"; next }
      { print }
    ' noon.original.yaml > noon.yaml
  }
  noon() {
    env -i PATH="$fixture/tools:$original_path" HOME="$HOME" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" \
      XDG_STATE_HOME="$XDG_STATE_HOME" NOON_STATE_DIR="$NOON_STATE_DIR" PGUSER="$PGUSER" \
      "$binary" "$@"
  }
  reject() { if "$@" > "$fixture/rejected.log" 2>&1; then fail "unexpected success: $*"; fi; }
  count() { local n=0 f; for f in "$1"/*"$2"; do [ -f "$f" ] && [ ! -L "$f" ] && n=$((n+1)); done; printf '%s' "$n"; }
  snapshot() { local f; for f in "$1"/*"$2"; do [ ! -f "$f" ] || cksum "$f"; done; }
  no_partials() { local f; for f in "$1"/.noon-backup.*; do [ ! -e "$f" ] || fail "leaked partial: $f"; done; }
  no_scratch() { [ "$(psql -XAt -d postgres -c "select count(*) from pg_database where datname='app_dev_restore_check'")" = 0 ] || fail 'scratch database leaked'; }

  uname -a
  if command -v sw_vers >/dev/null; then sw_vers; fi
  if [ -f /etc/os-release ]; then grep PRETTY_NAME /etc/os-release; fi
  pg_dump --version; pg_restore --version; psql --version; sqlite3 --version
  psql -XAt -d postgres -c 'select version()'
  createdb app_dev
  psql -X -v ON_ERROR_STOP=1 -c "create table users(id integer primary key, name text); insert into users values (1,'Ada'),(2,'Linus'),(3,'Grace');"
  sqlite3 "$fixture/data/app.db" 'create table users(id integer primary key); insert into users values(1),(2),(3);'
  config

  # Force all four real dumps to share a timestamp, independently of host speed.
  printf '#!/bin/sh\nprintf "20261016T120000\\n"\n' > "$fixture/tools/date"
  chmod +x "$fixture/tools/date"
  local i
  for i in 1 2 3 4; do noon run pg-backup; done
  [ "$(count "$pg_dir" .dump)" = 2 ] || fail 'Postgres retention count'
  for f in "$pg_dir"/*.dump; do pg_restore --list "$f" >/dev/null; done
  pass 'Postgres KEEP+2 real dumps, same-second unique publication, KEEP retained'
  snapshot "$pg_dir" .dump > "$fixture/before"
  keep=0; config; reject noon run pg-backup
  snapshot "$pg_dir" .dump > "$fixture/after"; cmp "$fixture/before" "$fixture/after"
  keep=2; config
  pass 'invalid KEEP preserves backups'

  noon run pg-restore-check > "$fixture/restore.log"
  grep -q 'check returned 3$' "$fixture/restore.log" || fail 'restored row count'
  no_scratch
  createdb app_dev_restore_check
  psql -X -v ON_ERROR_STOP=1 -d app_dev_restore_check -c 'create table sentinel(value integer); insert into sentinel values(42)'
  reject noon run pg-restore-check
  [ "$(psql -XAt -d app_dev_restore_check -c 'select value from sentinel')" = 42 ] || fail 'sentinel changed'
  # This harness owns the sentinel, not the restore script.
  dropdb app_dev_restore_check
  query='select * from nonexistent_table'; config; reject noon run pg-restore-check; no_scratch
  query='select count(*) from users'; pg_dir="$fixture/empty"; mkdir "$pg_dir"; config
  reject noon run pg-restore-check; no_scratch
  printf corrupt > "$pg_dir/app_dev-20261016T120000-ABC123.dump"
  reject noon run pg-restore-check; no_scratch
  pg_dir="$fixture/pg"; config
  pass 'restore seeded rows, sentinel preservation, empty/corrupt/query failures and scratch cleanup'

  # A failing dump has already written bytes; it must not publish or prune.
  printf '#!/bin/sh\nwhile [ "$1" != --file ]; do shift; done\nprintf partial > "$2"\nexit 1\n' > "$fixture/tools/pg_dump"
  chmod +x "$fixture/tools/pg_dump"
  snapshot "$pg_dir" .dump > "$fixture/before"
  reject noon run pg-backup; no_partials "$pg_dir"
  snapshot "$pg_dir" .dump > "$fixture/after"; cmp "$fixture/before" "$fixture/after"
  unlink "$fixture/tools/pg_dump"
  pass 'failed dump leaves prior backups unchanged and no partial or published output'

  # Exercise literal matching with regex metacharacters and all ignored types.
  local retention="$fixture/retention" prefix='app.[x]' suffix='.dump'
  mkdir "$retention"
  for i in 01 02 03 04; do printf good > "$retention/$prefix-20261016T1200$i-ABC123$suffix"; done
  printf lookalike > "$retention/appAx-20261016T120005-ABC123.dump"
  printf lookalike > "$retention/$prefix-20261016T120005-ABC123Xdump"
  printf partial > "$retention/$prefix-20261016T120005-ABC123.dump.partial"
  printf unrelated > "$retention/notes"
  mkdir "$retention/$prefix-20261016T120006-ABC123.dump"
  ln -s notes "$retention/$prefix-20261016T120007-ABC123.dump"
  reject ./scripts/prune.sh "$retention" "$prefix" "$suffix" -1
  reject ./scripts/prune.sh "$retention" "$prefix" "$suffix" abc
  [ -f "$retention/$prefix-20261016T120001-ABC123.dump" ] || fail 'invalid KEEP deleted backup'
  ./scripts/prune.sh "$retention" "$prefix" "$suffix" 2
  [ ! -e "$retention/$prefix-20261016T120001-ABC123.dump" ] || fail 'old backup retained'
  [ ! -e "$retention/$prefix-20261016T120002-ABC123.dump" ] || fail 'old backup retained'
  [ -f "$retention/$prefix-20261016T120003-ABC123.dump" ] && [ -f "$retention/$prefix-20261016T120004-ABC123.dump" ]
  [ -f "$retention/appAx-20261016T120005-ABC123.dump" ]
  [ -f "$retention/$prefix-20261016T120005-ABC123Xdump" ]
  [ -f "$retention/$prefix-20261016T120005-ABC123.dump.partial" ]
  [ -d "$retention/$prefix-20261016T120006-ABC123.dump" ]
  [ -L "$retention/$prefix-20261016T120007-ABC123.dump" ] && [ -f "$retention/notes" ]
  pass 'retention preserves literal lookalikes, unrelated files, partials, directories and symlinks'

  unlink "$fixture/tools/date"
  noon run sqlite-backup
  snapshot "$sqlite_dir" .db > "$fixture/before"
  # Keep a real SQLite connection holding an exclusive write transaction. The
  # FIFO lets us release it deterministically; readiness is signalled after BEGIN.
  mkfifo "$fixture/writer.fifo"
  sqlite3 -bail "$fixture/data/app.db" < "$fixture/writer.fifo" > "$fixture/writer.log" 2>&1 &
  writer=$!
  exec 3> "$fixture/writer.fifo"
  printf 'BEGIN EXCLUSIVE; INSERT INTO users VALUES(4);\n.output %s\n.print locked\n' "$fixture/writer.ready" >&3
  local deadline=$((SECONDS+10))
  until [ -s "$fixture/writer.ready" ]; do
    [ "$SECONDS" -lt "$deadline" ] || fail 'writer readiness timeout'
    sleep 0.05
  done
  reject noon run sqlite-backup
  snapshot "$sqlite_dir" .db > "$fixture/after"; cmp "$fixture/before" "$fixture/after"
  no_partials "$sqlite_dir"
  # Backup starts while writer is active, then succeeds after the lock releases.
  noon run sqlite-backup > "$fixture/sqlite-concurrent.log" 2>&1 &
  backup=$!
  deadline=$((SECONDS+10))
  until compgen -G "$sqlite_dir/.noon-backup.*" > /dev/null; do
    [ "$SECONDS" -lt "$deadline" ] || fail 'backup start timeout'
    sleep 0.05
  done
  printf 'COMMIT;\n.quit\n' >&3
  exec 3>&-
  wait "$writer"; writer=''
  wait "$backup"; backup=''
  for i in 2 3 4; do noon run sqlite-backup; done
  [ "$(count "$sqlite_dir" .db)" = 2 ] || fail 'SQLite retention count'
  for f in "$sqlite_dir"/*.db; do
    [ "$(sqlite3 "$f" 'pragma integrity_check')" = ok ]
    [ "$(sqlite3 "$f" 'select count(*) from users')" = 4 ]
  done
  snapshot "$sqlite_dir" .db > "$fixture/before"
  printf '#!/bin/sh\nprintf "20261016T120000\\n"\n' > "$fixture/tools/date"
  chmod +x "$fixture/tools/date"
  # Force the same name as an existing backup while still reserving a real
  # exclusive temporary file. Neither database tool is mocked for collisions.
  printf '#!/bin/sh\nfile=${2%%XXXXXX}ABC123\nmkdir "$file" || exit 1\nprintf "%%s\\n" "$file"\n' > "$fixture/tools/mktemp"
  chmod +x "$fixture/tools/mktemp"
  printf sentinel > "$sqlite_dir/app-20261016T120000-ABC123.db"
  snapshot "$sqlite_dir" .db > "$fixture/before"
  reject noon run sqlite-backup; no_partials "$sqlite_dir"
  snapshot "$sqlite_dir" .db > "$fixture/after"; cmp "$fixture/before" "$fixture/after"
  printf sentinel > "$pg_dir/app_dev-20261016T120000-ABC123.dump"
  snapshot "$pg_dir" .dump > "$fixture/before"
  reject noon run pg-backup; no_partials "$pg_dir"
  snapshot "$pg_dir" .dump > "$fixture/after"; cmp "$fixture/before" "$fixture/after"
  unlink "$fixture/tools/mktemp"
  pass 'Postgres and SQLite name collisions preserve existing backup bytes'
  pass 'SQLite concurrent writer, bounded contention failure, valid snapshots, quoted paths, retention and publication failure preservation'
  pass 'all database backup checks'
}

if [ "${1:-}" = --checks ]; then
  writer='' backup=''
  cleanup_checks() {
    local status=$? child
    for child in "$writer" "$backup"; do
      if [ -n "$child" ]; then kill "$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true; fi
    done
    exit "$status"
  }
  trap cleanup_checks EXIT
  trap 'exit 1' HUP INT TERM
  checks "$2" "$3"
  exit
fi

mode=${1:-host}
[ "$mode" = host ] || [ "$mode" = linux ] || fail 'usage: db-backups-smoke.sh [host|linux]'
repo=$(pwd -P)
# Fixture HOME must not change the Docker context used by ownership cleanup.
export DOCKER_CONFIG="${DOCKER_CONFIG:-$HOME/.docker}"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/noon-db-backups.XXXXXX")
fixture=$(cd "$fixture" && pwd -P)
owner="noon-db-backups-$(basename "$fixture")-$$"
printf '%s\n' "$owner" > "$fixture/.owner"
printf 'Fixture retained: %s (owner %s)\n' "$fixture" "$owner"
mkdir "$fixture/project"
cp -R examples/local-dev/db-backups/. "$fixture/project/"
cp "$fixture/project/noon.yaml" "$fixture/project/noon.original.yaml"
container=''
cleanup() {
  local status=$?
  if [ -n "$container" ]; then
    if [ "$(docker inspect --format '{{ index .Config.Labels "noon.db-smoke-owner" }}' "$container")" = "$owner" ]; then
      docker container stop --timeout 2 "$container" >/dev/null
      docker container rm -v "$container" >/dev/null
      printf 'Cleaned owned PostgreSQL container %s\n' "$container"
    else
      printf 'Ownership mismatch; retained container %s\n' "$container" >&2
      status=1
    fi
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
# Loopback-only port and a throwaway trust-authenticated server, never user PG*.
container=$(docker run -d --name "$owner" --label "noon.db-smoke-owner=$owner" \
  -e POSTGRES_HOST_AUTH_METHOD=trust -p 127.0.0.1::5432 postgres:17-bookworm)
export PGHOST=127.0.0.1 PGUSER=postgres PGDATABASE=postgres
export PGPORT=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort}}' "$container")
deadline=$((SECONDS+60))
until docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do
  [ "$SECONDS" -lt "$deadline" ] || fail 'PostgreSQL readiness timeout'
  sleep 0.2
done
if [ "$mode" = linux ]; then
  CGO_ENABLED=0 GOOS=linux go build -o "$fixture/noon" ./cmd/noon
  docker exec "$container" bash -c 'apt-get update -qq && apt-get install -y -qq sqlite3'
  docker cp "$fixture/." "$container:/tmp/noon-db-fixture"
  docker cp scripts/db-backups-smoke.sh "$container:/tmp/noon-db-fixture/check.sh"
  docker exec -e PGHOST=127.0.0.1 -e PGPORT=5432 "$container" \
    bash /tmp/noon-db-fixture/check.sh --checks /tmp/noon-db-fixture /tmp/noon-db-fixture/noon
else
  # Fail rather than silently substituting container clients for host evidence.
  case "$(pg_dump --version)" in *' 17.'*) ;; *) fail 'host pg_dump must match PostgreSQL server major 17' ;; esac
  env -i PATH="$PATH" PGHOST="$PGHOST" PGPORT="$PGPORT" \
    bash scripts/db-backups-smoke.sh --checks "$fixture" "$repo/bin/noon"
fi
