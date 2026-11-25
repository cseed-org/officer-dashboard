#!/bin/sh
set -eu
umask 0077
if [ -n "${DEPLOY_IMAGE:-}" ]; then
  printf '%s\n' "$DEPLOY_IMAGE" | grep -Eq '^[A-Za-z0-9./:_-]+@sha256:[a-f0-9]{64}$' || { echo 'DEPLOY_IMAGE must be pinned by digest.' >&2; exit 1; }
fi
# Even a legacy mixed env file must not pass sink credentials into the app process.
for key in BACKUP_SINK_URL BACKUP_BEARER_SECRET BACKUP_ACCESS_CLIENT_ID BACKUP_ACCESS_CLIENT_SECRET BACKUP_MEMBERSHIP_API_TOKEN BACKUP_MEMBERSHIP_ACCOUNT_ID BACKUP_MEMBERSHIP_DATABASE_ID BACKUP_AGE_RECIPIENT; do
  unset "$key" "${key}_FILE"
done
unset key
. /app/load-secrets.sh
if [ "${BACKUP_REQUIRED:-}" = 1 ] && [ ! -f "$DB_PATH" ]; then
  echo 'Database missing. The backup service stages ciphertext; restore off-box before restarting.' >&2
  exit 1
fi
exec /app/dashboard serve
