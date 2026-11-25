#!/bin/sh
# Sourced only by the app entrypoint. Backup credentials are never resolved here.
for key in DISCORD_CLIENT_SECRET DISCORD_WEBHOOK_URL DISCORD_BOT_TOKEN SMTP_USERNAME SMTP_PASSWORD GOOGLE_OAUTH_CLIENT_SECRET GOOGLE_SERVICE_ACCOUNT_JSON CLOUDFLARE_API_TOKEN DEV_AUTH_KEY; do
  eval "file=\${${key}_FILE:-}"
  eval "direct=\${${key}:-}"
  if [ -n "$file" ]; then
    if [ -n "$direct" ]; then
      echo "Set either $key or ${key}_FILE, not both." >&2
      exit 1
    fi
    [ -f "$file" ] && [ -r "$file" ] || { echo "Cannot read ${key}_FILE." >&2; exit 1; }
    value=$(cat -- "$file")
    [ -n "$value" ] || { echo "Empty ${key}_FILE." >&2; exit 1; }
    export "$key=$value"
  fi
done
unset key file direct value
