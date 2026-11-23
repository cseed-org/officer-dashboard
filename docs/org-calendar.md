# Organization calendar

Four calendars belong to one dedicated organization Google account: Internal Meetings,
External Meetings, Weekly Programs, and Events. Officers can edit any Internal/External
meeting. Programs and Events are locked; `sourceHref` links to their source workflows.
These locks apply to service tokens and MCP. Existing per-officer Google/ICS delivery and
the Home calendar feed continue independently.

Leadership navigates to `GET /api/settings/org-google/connect` to connect the org account.
This reuses the existing Google OAuth credentials, PKCE and AES-GCM token sealer, with
separate user-bound state cookies and the full Calendar scope. Register
`APP_URL/auth/org-google/callback` as an additional Google redirect URI. Leadership-only
status/disconnect retain local data and calendar IDs. Reconnecting the same account reuses
its calendars; changing accounts creates four new calendars without deleting the old ones.
The status response adds `needsReconnect` (the org refresh token was revoked: connect again)
and `lastSyncError` (the last failed sync pass, null when healthy). Connect and disconnect
cancel a running sync pass instead of waiting for it.

`GET /api/orgcal/calendars` gives every officer (and calendar:read/write tokens) two booleans,
because calendar IDs stay set after a disconnect and so cannot show whether sync is live:
`googleConnected` is true while an org account is connected and Google has not revoked it;
`syncProblem` is true when it needs reconnecting or the last sync pass recorded an error.
The account email and error text stay on the leadership status endpoint. While
`googleConnected` is false, meetings save locally and no invites go out until leadership
connects again. The error flags are kept in memory, so after a restart the next pass sets them again.

All active officers with an account email in People/Officers receive reader access on all
four calendars. Email/active changes increment a roster generation checked within a minute;
ACLs also reconcile daily. Non-roster reader access is removed. Owner access is preserved.
ACL sync is best effort with its own backoff: an address Google rejects is logged with the
address and skipped, and meeting/Event/program delivery and pulls continue.

Meetings support title, description, location, start/end, all-day, recurrence, reminders,
invitees and send-invites. Invitees use `{kind,ref}` with kind user/subteam/email. User and
subteam references expand against active officer emails when sent. For people, External
defaults to sending invites and Internal to off; the flag controls Google sendUpdates all/none,
including cancellation. Requests from a calendar:write service token or an MCP agent default
to sendInvites:false on create, and an agent update that omits sendInvites turns it off; set
`sendInvites:true` explicitly to email guests. Events created directly in Google import with
sendInvites:false. No Meet link is created.

Pulls merge Google's attendees into the local invitees: a subteam/user row stays while all of
its addresses still attend, Google-side additions become email rows and removed addresses are
dropped, so RSVP-only changes leave invitees unchanged. The last pulled responseStatus per
attendee is sent back on push, so a dashboard edit keeps guests' RSVPs.

A tentative meeting cannot recur: `rrule` with a tentative range is a 400 on POST and PATCH;
send `rrule:null` to make a recurring meeting tentative.

Locked items link to their source: `/events?id=<id>` for Events and
`/buildspace-launchpad?tab=programs&program=<id>` for programs.

New manual recurrence is one `rrule` line: `RRULE:FREQ=DAILY|WEEKLY|MONTHLY`, optional
`INTERVAL=1..52`, optional `BYDAY` (weekly only, e.g. `MO,WE`; the start must fall on one of
those days) and at most one of `UNTIL=YYYYMMDD[THHMMSSZ]` or `COUNT=1..500`. Biweekly is
`FREQ=WEEKLY;INTERVAL=2`. Monthly repeats on the start's day of month; like Google, months
without that day (for example the 31st) are skipped. Occurrences keep their local time across
DST in the meeting timezone (America/Los_Angeles). Other parts return 400. The rule is pushed
to Google unchanged. Pulled Google daily/weekly/monthly/yearly rules (including WKST),
exclusions and instance overrides are retained and expanded locally.

Listed occurrences of a recurring meeting share the series id and carry their own
`startsAt`/`endsAt` plus the master's `seriesStartsAt`/`seriesEndsAt` (null when not
recurring); edit against the series times. A PATCH that changes a recurring master's
`startsAt`/`endsAt` keeps the series on its original date and applies the new times of day and
duration there, so editing from a later occurrence never drops earlier ones (locally or in
Google). Send `"moveSeriesStart": true` to move the series' first date deliberately.

Tentative meetings use `startsAt:null` plus inclusive `tentativeFrom`/`tentativeTo` dates.
Google receives an all-day `[Tentative]` entry, ending the day after tentativeTo. Setting a
fixed start/end clears the range and replaces the same Google event. Fixed all-day meetings
use date strings and an exclusive endsAt. Dashboard drafts carry `[Draft]`; cancelled or
deleted source events are removed. See [programs](programs.md) for recurring source sessions.
Changing a fixed source event back to tentative withdraws its old per-officer invitation.
The last fixed window is retained for retries and deletion; setting a fixed date again
restores delivery through the existing Google/ICS path.

Writes save their projection and outbox in one SQLite transaction. Failures retain jobs with
exponential backoff from one minute to one hour. Startup/hourly retry resumes jobs;
deterministic Google IDs prevent duplicate inserts. ID/etag metadata identifies later
patches. Revision checks preserve edits committed during an in-flight send.

Every five minutes, paginated incremental pulls use stored sync tokens. HTTP 410 performs
a full resync. Internal/External edits/deletes update local records; pending dashboard
writes win. Locked calendars never import source changes: drift queues a re-push and extra
Google events are removed. The loop uses TryLock, backoff and the server shutdown context.
Disconnected or unconfigured servers make no Google calls.

`GET /api/orgcal/events?from=YYYY-MM-DD&to=YYYY-MM-DD&cal=internal,external,programs,events`
returns `{events,timezone}`. To is exclusive; ranges are at most 366 days. Org timezone is
America/Los_Angeles. Responses retain master IDs for editing; key displayed occurrences by
ID plus start time. Org createdAt/updatedAt timestamps are epoch milliseconds.

MCP tools: orgcal_list, orgcal_create, orgcal_update, orgcal_delete, programs_list.
Service tokens call orgcal endpoints using calendar:read or calendar:write (includes read),
with normal token/user audit attribution. They cannot configure OAuth or modify programs.
Integration tests fake Google. Live OAuth, sharing and invitation receipt need the org
account's sign-in during integration QA.
