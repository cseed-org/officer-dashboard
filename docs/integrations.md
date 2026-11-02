# Integrations

## CSV import rules

CSV imports accept UTF-8 (optional BOM), quoted commas/newlines, and CRLF or LF. Header
matching ignores case, spaces, underscores and punctuation. Guest files need `email`;
optional headers are `name` / `full name`, `guest id` / `api id`, `approval status` /
`registration status`, `registered at` / `registration time`, and `checked in at` /
`check-in time`. Reflection files need `email` / `email address` and `timestamp` /
`submitted at`. Timestamps with an offset are preferred. Offset-less ISO dates/times or
`MM/DD/YYYY HH:mm:ss [AM|PM]` are interpreted in the club default time zone (`club_settings.club_timezone`), falling
back to America/Los_Angeles when unset; nonexistent spring
DST times are rejected and ambiguous autumn times choose the earlier instant.

Emails are trimmed/lowercased; duplicate event/email rows upsert. Blank/missing CSV fields and omitted JSON fields preserve existing values, including
check-ins; explicit JSON null clears a nullable guest field.
Pending approval, invited, waitlisted, declined and cancelled guests do not count as
registrations unless they checked in. Reflection rates count distinct respondents
matched to checked-in guests. Bad rows are reported by physical source row and skipped;
`dryRun=true` previews the merged metrics without writes, including for a new event.
Files are limited to 5 MB; JSON
ingest is limited to 5,000 guests by contract.

The club zone applies to both Luma guests and reflection CSV timestamps. Explicit
offsets and Z retain their instant. `/api/ingest/luma` accepts JSON with explicit
offsets only; naive timestamps are rejected. Fiscal and accounting date boundaries
remain Pacific regardless of the club or personal display zone.

## Service tokens and purchasing agents

Directors issue service tokens once, store only SHA-256 hashes, and may revoke them.
`ingest:luma` permits `/api/ingest/luma`; `agent:jobs` permits jobs list/claim/complete.
A failed job returns its request to approved, clears its token claim and re-queues
automatically while fewer than three total claims have been attempted. The default
queued-job poll discovers retries. After the third failed attempt the job stays failed
for a director to resolve manually. Only the current claiming token may complete it. Revoking a token
prevents future use; deactivating a user ends sessions but does not revoke independently
issued service tokens (revoke those separately).

**Agent settlement tolerance.** The purchasing agent's `POST /api/agent/jobs/:id/complete`
may post an `actualTotalCents` of up to the larger of the estimate + 10% and the
estimate + $25 (set by `AGENT_SETTLEMENT_TOLERANCE_PERCENT` / `_CENTS`). Above that it
gets a 409 and the job becomes `held`: its claim is cleared, so later complete, failed or
claim calls from the agent return 409 and can never re-queue it (no second purchase).
Directors get a `request.held` notification with the attempted vs approved amount, and
the request page shows a "held for director review" banner (`settlementHold`). A
director settles it with `POST /api/requests/:id/transition` (`to: 'fulfilled'`), which
clears the hold; sending it back to `approved` cancels the held job instead. The
director's own settlement has no limit.

## Receipts and Google service accounts

Receipts allow PDF, PNG, JPEG, HEIC and WebP, up to 10 MB. Configure
`GOOGLE_SERVICE_ACCOUNT_JSON` and `DRIVE_ROOT_FOLDER_ID` (the older
`GOOGLE_DRIVE_FOLDER_ID` is accepted as an alias). Share the Drive folder with the
service-account identity. The Go adapter uses RS256 JWT exchange, Drive v3
multipart uploads and shared-drive flags, following Google's
[service-account protocol](https://developers.google.com/identity/protocols/oauth2/service-account)
and [upload API](https://developers.google.com/workspace/drive/api/guides/manage-uploads).
The token requests the full `https://www.googleapis.com/auth/drive` scope to access the pre-existing shared folder. Files are
stored under `FY<year>/<request-id>/`, and officers need Drive access to open its links.
Folder lookups search the shared drive that holds the root folder (`corpora=drive` +
`driveId`, or `corpora=allDrives` if the root isn't in a shared drive). When two first
uploads race to create the same folder, both re-list after creating, keep the oldest,
and the loser's empty duplicate is trashed. Trashing needs the service account to be a
**Content manager** of the shared drive; as a Contributor the empty duplicate just stays.

**Lock down the service account.** Because the scope is the full `drive` scope, the key
reaches every file shared with that account. Make the service account a member of the
receipts shared drive **only** (don't share other folders or drives with it), and keep
its key **only** in the server's environment / secret store (`GOOGLE_SERVICE_ACCOUNT_JSON`):
never in the repo, CI variables or chat. Rotate the key if it leaks.

Receipt operations return HTTP 503 if neither a local receipts directory nor complete Drive credentials/root folder are configured. Development selects the local-disk file store.
Uploads precede the attachment metadata transaction, so a failed metadata commit may
leave an unreferenced file for later cleanup. MIME validation checks the declared type;
there is no virus scan or content classification.

## Purchase notifications

`DISCORD_WEBHOOK_URL` enables bounded, best-effort embeds; unset webhooks log events.
Delivery failure does not undo the committed purchase. Live Google/Discord credentials
are not needed for the local test suite; production delivery must be checked with the
organization's actual credentials.

## Discord sign-in and officer invitations

Discord OAuth uses PKCE and `${APP_URL}/auth/callback`. Ordinary login only permits existing active allowlisted users; it never creates a user or grants leadership/ownership. Officer invitation acceptance is a separate flow that creates an officer account. Director+ can issue invitations and send their links via bot DM using `DISCORD_BOT_TOKEN`; a failed DM is surfaced so delivery can be retried. A webhook is only for purchase notifications, not invitation delivery.

## Cloudflare Analytics and membership D1

`CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` connect GraphQL analytics; `CLOUDFLARE_ZONE_ID` scopes zone analytics. Adding `D1_DATABASE_ID` enables the membership integration. Members are mirrored into local SQLite for reads; edits and synchronization use the D1 integration. The UI displays disconnected, loading, partial/error and connected widget states. Configure the token for the intended account, D1 database and analytics resources; credentials stay on the server. See `server/internal/cloudflare/`, `server/internal/cfsync/`, and [API](api.md).

## Luma handoff kit and Notion

Event planning generates a handoff kit for creating the Luma listing; CSV import then reconciles registrations/check-ins and survey reflections. It is not automatic Luma event publishing. The Notion adapter currently returns an explicit placeholder link and placeholder status; see [Notion integration](notion-integration.md). No live Notion credentials are consumed.

## Calendar delivery

Officers save their required profile fields before choosing delivery, then select email invitations or connect Google Calendar. SMTP uses `SMTP_HOST`, `SMTP_PORT` (587), `SMTP_USERNAME`, `SMTP_PASSWORD`, and `SMTP_FROM`. Google uses `GOOGLE_OAUTH_CLIENT_ID` / `GOOGLE_OAUTH_CLIENT_SECRET`, redirect `${APP_URL}/auth/google/callback`, PKCE, and `openid email https://www.googleapis.com/auth/calendar.events` scopes. This is per-user consent, separate from the Drive service account.

The server stores sealed Google tokens, refreshes access, and upserts events; disconnect removes the stored grant and attempts revocation. Event updates and cancellation use delivery records; changing delivery preferences or completing onboarding triggers upcoming-event delivery. Settings supports a test invite. Live provider delivery needs deployment credentials; local tests use fakes.
