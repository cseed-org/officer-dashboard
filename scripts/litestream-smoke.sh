#!/bin/sh
# Run inside the release image as its normal non-root user. No remote replica or credentials.
set -eu

version=$(litestream version)
echo "Litestream version: $version"
test "$version" = "0.3.14"
work=/data/litestream-smoke
mkdir -p "$work"
# Release binaries exclude fixtures. A migrated empty database exercises the
# same WAL snapshot path without introducing sample accounts into the image.
DB_PATH="$work/dashboard.db" /app/dashboard migrate
cat > "$work/litestream.yml" <<EOF
dbs:
  - path: $work/dashboard.db
    replicas:
      - type: file
        path: $work/replica
        sync-interval: 1s
EOF

pid=''
trap 'if [ -n "$pid" ]; then kill -TERM "$pid" 2>/dev/null || true; wait "$pid" || true; fi' EXIT
litestream replicate -config "$work/litestream.yml" > "$work/replicate.log" 2>&1 &
pid=$!
attempt=0
while [ "$attempt" -lt 20 ]; do
  attempt=$((attempt + 1))
  if ! kill -0 "$pid" 2>/dev/null; then
    cat "$work/replicate.log"
    exit 1
  fi
  snapshot=$(find "$work/replica" -type f -name '*.snapshot.lz4' -print -quit 2>/dev/null || true)
  if [ -n "$snapshot" ]; then
    kill -TERM "$pid"
    wait "$pid"
    pid=''
    echo 'Litestream 0.3.14 starts, replicates a snapshot and stops cleanly.'
    exit 0
  fi
  sleep 1
done
cat "$work/replicate.log"
echo 'Litestream did not produce a local snapshot.' >&2
exit 1
