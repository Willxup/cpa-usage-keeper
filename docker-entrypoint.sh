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

case "${BACKUP_ENABLED:-true}" in
  false|FALSE|False|0)
    ;;
  *)
    ensure_writable_dir "${BACKUP_DIR:-/data/backups}"
    ;;
esac

case "${LOG_FILE_ENABLED:-true}" in
  false|FALSE|False|0)
    ;;
  *)
    ensure_writable_dir "${LOG_DIR:-/data/logs}"
    ;;
esac

exec su-exec app "$@"
