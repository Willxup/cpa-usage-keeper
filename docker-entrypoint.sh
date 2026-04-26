#!/bin/sh
set -eu

ensure_writable_dir() {
  dir="$1"
  if [ -z "$dir" ]; then
    return
  fi
  mkdir -p "$dir"
  chown -R app:app "$dir"
}

sqlite_path="${SQLITE_PATH:-/data/app.db}"
sqlite_dir="$(dirname "$sqlite_path")"
ensure_writable_dir "$sqlite_dir"

if [ "${BACKUP_ENABLED:-true}" != "false" ]; then
  ensure_writable_dir "${BACKUP_DIR:-/data/backups}"
fi

exec su-exec app "$@"
