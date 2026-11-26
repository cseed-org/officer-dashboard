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
prevents future use. Deactivating its creator or demoting them below director revokes every
service token they created in the same transaction (`token.revoked_with_user` in the audit log);
tokens also answer 401 whenever their creator is inactive or below director.

**Behind Cloudflare Access.** `/api/ingest/*` and `/api/agent/*` sit behind an Access application
with a Service Auth policy ([Cloudflare setup, step 5](deploy/cloudflare.md#5-create-a-service-token-for-bots)).
Every caller sends the Access service token as `CF-Access-Client-Id` and `CF-Access-Client-Secret`,
plus its dashboard service token as `Authorization: Bearer …`. The Discord check-in bot's
adapter (`scripts/buildspace-discord-stub.mjs`) adds them from `CF_ACCESS_CLIENT_ID` /
`CF_ACCESS_CLIENT_SECRET`. The Luma ingest and the purchasing agent live outside this repo and
must add the two headers themselves. A 401 or redirect from Cloudflare (no JSON `error` body)
means Access refused the service token. The dashboard itself never sees or needs the token.

### Agent settlement tolerance

The purchasing agent's `POST /api/agent/jobs/:id/complete`
may post an `actualTotalCents` of up to the larger of the estimate + 10% and the
estimate + $25 (set by `AGENT_SETTLEMENT_TOLERANCE_PERCENT` / `_CENTS`). Above that it
gets a 409 and the job becomes `held`: its claim is cleared, so later complete, failed or
claim calls from the agent return 409 and can never re-queue it (no second purchase).
Directors get a `request.held` notification with the attempted vs approved amount, and
the request page shows a "held for director review" banner (`settlementHold`). A
director settles it with `POST /api/requests/:id/transition` (`to: 'fulfilled'`), which
clears the hold; sending it back to `approved` cancels the held job instead. Ordinary
directors share the estimate-plus-tolerance settlement ceiling; only leadership can settle
above it.

## Files in a Google Shared Drive

Receipts and design files live in one Google **Shared Drive** (made by a UW advisor). The
dashboard reaches it as a **member Google account**: an owner connects that account once in
Settings → Google Drive file storage, and chooses the folder the dashboard files everything
under (or pins it with `DRIVE_ROOT_FOLDER_ID`). The account needs the drive's **Content
manager** role, so it can add, rename and trash files. Officers need no access to the drive.

**Opening a file never needs a Drive link.** `GET /api/attachments/:id` and
`GET /api/design-files/:id` run the dashboard's own check first (receipts: the requester,
the uploader and directors; design files: officers), then stream the bytes from Drive with
`Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and
`Cache-Control: private, no-store`. Every API response carries only those
`/api/...` URLs; no `drive.google.com` link is stored for new uploads or sent to the
browser. A refused download never reaches Drive. The bytes are checked against the
receipt rules again on the way out (PDF, PNG, JPEG, HEIC or WebP, up to 10 MB, content
matching its type), so a file changed in Drive is not served.

### Folder tree

```text
<root folder>/                                   DRIVE_ROOT_FOLDER_ID, or chosen in Settings
  Finance/FY<year>/<request id>/<receipt>        receipts and quotes (this PR)
  Events/<YYYY-MM-DD> <event name> [<event id>]/
    Design/<design request id>/<file>            design files of an event's request (this PR)
    event.json, summary.md, cover.<ext>          readable event record (event export)
  Design requests/<design request id>/<file>     design files with no event (this PR)
  Programs/<track>/program.json                  readable program record (event export)
  Backups/daily/, Backups/monthly/               encrypted database snapshots (backups)
```

An event folder is named by `files.EventFolderName`: the start date in the club timezone
(`undated` for a tentative or undated event), the event name with `/\:*?"<>|` and control
characters replaced and capped at 80 characters, and the event id in brackets. The folder
is found by that trailing `[<event id>]`, so when an event is renamed or rescheduled its
folder is renamed in place and keeps its contents. Files uploaded before this layout stay
where they are; only new uploads use it.

### How the client behaves

The Go client (`server/internal/files`) uses Drive v3 with the shared-drive flags. List
queries search the shared drive that holds the root folder (`corpora=drive` + `driveId`,
or `corpora=allDrives` when the root is not in a shared drive). When two first uploads
race to create the same folder, both re-list after creating, keep the oldest, and trash
the loser's empty duplicate. Answers of 429, 5xx and 403 `rateLimitExceeded` /
`userRateLimitExceeded` are retried up to five times with jittered exponential backoff
(1 s, 2 s, 4 s, 8 s), honouring `Retry-After` up to 32 s. A 401 fetches a new access token
once. Large files (backups) use resumable uploads in 8 MiB chunks.

### When Drive says no

- **Drive full** (403 `storageQuotaExceeded`, `teamDriveFileLimitExceeded`, ...) or still
  **rate limited** after the retries: the upload is refused with 507 `storage_quota`, nothing
  is recorded, and owners get a Discord alert (at most one an hour per kind, through
  `DISCORD_WEBHOOK_URL`, else the log).
- **Running low:** once a day the dashboard sums the shared drive's file sizes (trashed
  files included, since they count until purged) and alerts at 70 % of
  `DRIVE_QUOTA_BYTES` (default 100 GB). Settings shows that figure with its time, and
  measures again only when an owner presses Refresh or the figure is a day old, because
  each measurement lists the whole drive.
- **Access revoked or expired** (Google answers `invalid_grant`): the account is marked
  disconnected, owners are alerted once, and uploads and downloads answer 503 until an
  owner reconnects. The OAuth consent screen must be **in Production**: in Testing mode
  Google expires refresh tokens after 7 days.
- **Not connected:** uploads answer 503 `not_configured`. Development uses local disk.

Uploads happen before the attachment row is written; when that write fails, the uploaded
file is trashed again. There is no virus scan or content classification.

### The connected account's tokens

The connect flow (`/api/settings/drive/connect` → Google → `/auth/drive-google/callback`)
uses the `GOOGLE_OAUTH_CLIENT_ID` client with state and PKCE, asks for
`openid email https://www.googleapis.com/auth/drive` (Drive only, never the calendar
scopes), and refuses a grant without the Drive scope. The full `drive` scope is needed
because `drive.file` cannot see a shared drive somebody else made. The refresh and access
tokens are sealed with the OAuth client secret, like the org calendar's, in
`drive_google_account`; they never appear in a response, log or audit row. Disconnecting
revokes the grant at Google. Audit actions: `drive.google_connected`, `drive.root_set`,
`drive.google_disconnected`.

### Alternative: a service account

Without a connected account, a service-account key works too: set
`GOOGLE_SERVICE_ACCOUNT_JSON_FILE` (a path; a direct `GOOGLE_SERVICE_ACCOUNT_JSON` value
stops startup) and `DRIVE_ROOT_FOLDER_ID`, and make the service account a Content manager
of the shared drive **only**. It uses RS256 JWT exchange
([service-account protocol](https://developers.google.com/identity/protocols/oauth2/service-account))
with the same `drive` scope, so the key reaches everything shared with it: keep it in the
secret mount, never in the repo, CI or chat, and rotate it if it leaks. A connected
account wins over the service account.

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
