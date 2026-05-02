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

work_dir="${WORK_DIR:-./data}"
ensure_writable_dir "$work_dir"

case "${BACKUP_ENABLED:-true}" in
  false|FALSE|False|0)
    ;;
  *)
    ensure_writable_dir "$work_dir/backups"
    ;;
esac

case "${LOG_FILE_ENABLED:-true}" in
  false|FALSE|False|0)
    ;;
  *)
    ensure_writable_dir "$work_dir/logs"
    ;;
esac

exec su-exec app "$@"
