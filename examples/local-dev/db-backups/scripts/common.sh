# Shared by the example scripts; backup directories must be trusted/private.
export LC_ALL=C

validate_keep() {
  case "$1" in ''|*[!0-9]*|0*) echo 'KEEP must be a positive integer without leading zeros' >&2; return 1 ;; esac
}

# Literal prefix/suffix, timestamp plus mktemp token; newest names sort first.
# Newlines in paths are unsupported (spaces and apostrophes are supported).
backup_files() {
  for file in "$1/$2"-*"$3"; do
    [ -f "$file" ] && [ ! -L "$file" ] || continue
    name=${file##*/}
    stamp=${name#"$2"-}
    stamp=${stamp%"$3"}
    case "$stamp" in
      [0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]-[a-zA-Z0-9][a-zA-Z0-9][a-zA-Z0-9][a-zA-Z0-9][a-zA-Z0-9][a-zA-Z0-9]) printf '%s\n' "$file" ;;
    esac
  done | sort -r
}

prepare_backup() {
  validate_keep "$KEEP"
  mkdir -p "$BACKUP_DIR"
  # Use an absolute path so SQLite and command operands cannot interpret options.
  BACKUP_DIR=$(cd "$BACKUP_DIR" && pwd -P)
  staging=$(mktemp -d "$BACKUP_DIR/.noon-backup.XXXXXX")
  partial="$staging/output"
  trap '[ ! -e "$partial" ] || unlink "$partial"; rmdir "$staging"' EXIT
  trap 'exit 1' HUP INT TERM
  dest="$BACKUP_DIR/$1-$(date -u +%Y%m%dT%H%M%S)-${staging##*.}$2"
}

publish_backup() {
  # Hard linking is atomic and refuses to replace an existing file. Both paths
  # are on the same filesystem. Reject directories and symlinks as well.
  [ ! -e "$dest" ] && [ ! -L "$dest" ] || { echo "backup already exists: $dest" >&2; return 1; }
  ln "$partial" "$dest"
  printf 'wrote %s\n' "$dest"
}
