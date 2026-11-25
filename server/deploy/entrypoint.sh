#!/bin/sh
set -eu
if [ -n "${BACKUP_SINK_URL:-}" ]; then
  if [ ! -f "$DB_PATH" ]; then
    # D5 forbids keeping the decryption key here. Never replace a lost DB with an empty one.
    if [ ! -f "${DB_PATH}.recovery.age" ]; then
      /app/dashboard-backup download --output "${DB_PATH}.recovery.age"
    fi
    echo 'Restore off-box with scripts/restore-from-d1, install the DB, then restart.' >&2
    exit 1
  fi
  /app/dashboard-backup loop &
  backup_pid=$!
  /app/dashboard serve &
  app_pid=$!
  trap 'kill -TERM "$app_pid" "$backup_pid" 2>/dev/null || true' TERM INT
  set +e
  wait "$app_pid"
  result=$?
  kill -TERM "$backup_pid" 2>/dev/null || true
  wait "$app_pid" 2>/dev/null
  exit "$result"
fi
exec /app/dashboard serve
