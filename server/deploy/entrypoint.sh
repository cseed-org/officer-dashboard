#!/bin/sh
# Container entrypoint. Containers have ephemeral disks, so when a replica is configured
# (LITESTREAM_BUCKET) the database is restored from it on boot and replicated continuously
# while the server runs. Without one it is a plain single-node start (a Pi, local Docker).
set -eu

if [ -n "${LITESTREAM_BUCKET:-}" ]; then
  export LITESTREAM_PATH="${LITESTREAM_PATH:-dashboard}" LITESTREAM_REGION="${LITESTREAM_REGION:-auto}" LITESTREAM_ENDPOINT="${LITESTREAM_ENDPOINT:-}"
  # First boot of a new bucket: nothing to restore, the server creates and migrates the file.
  litestream restore -config /etc/litestream.yml -if-db-not-exists -if-replica-exists "$DB_PATH"
  exec litestream replicate -config /etc/litestream.yml -exec "/app/dashboard serve"
fi

echo "LITESTREAM_BUCKET is not set: the database lives only on this container's disk." >&2
exec /app/dashboard serve
