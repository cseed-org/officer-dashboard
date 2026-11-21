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

All active officers with an account email in People/Officers receive reader access on all
four calendars. Email/active changes increment a roster generation checked within a minute;
ACLs also reconcile daily. Non-roster reader access is removed. Owner access is preserved.

Meetings support title, description, location, start/end, all-day, recurrence, reminders,
invitees and send-invites. Invitees use `{kind,ref}` with kind user/subteam/email. User and
subteam references expand against active officer emails when sent. External defaults to
sending invites, Internal defaults off; the flag controls Google sendUpdates all/none,
including cancellation. No Meet link is created. New manual recurrence uses weekly RRULE;
pulled Google daily/weekly/monthly/yearly rules, exclusions and instance overrides are retained.

Tentative meetings use `startsAt:null` plus inclusive `tentativeFrom`/`tentativeTo` dates.
Google receives an all-day `[Tentative]` entry, ending the day after tentativeTo. Setting a
fixed start/end clears the range and replaces the same Google event. Fixed all-day meetings
use date strings and an exclusive endsAt. Dashboard drafts carry `[Draft]`; cancelled or
deleted source events are removed. See [programs](programs.md) for recurring source sessions.

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
