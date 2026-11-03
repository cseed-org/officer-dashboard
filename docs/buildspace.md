# Buildspace draft

Buildspace keeps the builder register, projects, mentor assignments and activity together at `/buildspace`. All existing active officers, directors and leadership can read and edit it. External mentor contacts have no sign-in rights just because they are assigned: a mentor who needs dashboard access must already be an explicitly allowlisted officer. A future mentor portal should scope access to assigned projects and redact private notes before adding a new role.

## Working model

Each builder belongs to one project. Track and the one or two assigned mentors follow that project; partners are its other builders. A project can temporarily have zero mentors and is flagged until someone assigns one. Tracks are exactly Software, Hardware, Creative and Extended Virtual Reality (XR). Mentors reuse External Contacts of type mentor/both. A mentor with no assignment stays visible in the Mentors table, so officers can spot spare capacity. Project creation and builder registration are separate, and projects can exist before their builders join.

Builder records can link to a local Members mirror ID and a unique Discord user ID. The draft uses explicit IDs when linking existing members and Finance requests. Moving a builder with activity to another project is refused to preserve history; a future transfer flow needs a decision on historical membership. Project edits replace the full form and currently use last-write-wins semantics.

The activity register supports weekly check-ins, mentor notes, general requests, purchase needs and absences. Entries can describe a whole project or a builder on it; weekly updates require a builder, and mentor check-ins require a currently assigned mentor. Person details include that person's entries plus project-wide activity, while project details include all activity. Entries are append-only, except that open, unlinked requests/purchase needs can be marked resolved. A linked Finance purchase reads its live title and status; Finance owns that lifecycle. Buildspace does not change the purchase's permissions or ledger.

The People, Projects and Mentors tabs support search, track, mentor, stage and attention filters. Table is the directory view; grouped cards use track, mentor or project; the board always shows projects by stage, with stage edits available in the project drawer. Shared projects appear under each of their mentors. URL parameters preserve the tab, layout, grouping, filters and selected person/project. Summary counts show the whole cohort and act as filters.

Weeks start Monday in Pacific time. “Awaiting this week” means a builder who has joined and has no dated weekly entry between Monday and today, including the current week in progress; it is a follow-up signal, not an overdue attendance verdict. Absences do not erase the follow-up signal. The absence filter covers dated absences this week. No automatic reminders are sent.

`dashboard seed` inserts six projects, twelve builders and three mentor contacts, covering all tracks and stages. Weekly demo dates use the week when seeding; they age naturally. This is explicit sample-data seeding, never automatic production migration. Demo names are independent from real member records.

## API

All session endpoints require officer or above and use the existing origin protection and audit log.

| Method / path | Body / response |
| --- | --- |
| `GET /api/buildspace` | Full small-cohort register: projects, people, mentor summaries, activity, Pacific `weekStart` and `today`. |
| `PUT /api/buildspace/projects/{id}` | Full project form: `name`, `description`, `track`, `stage`, `links` (HTTP/S only), `mentorIds` (0–2 distinct contacts). Creates or replaces. |
| `PUT /api/buildspace/people/{id}` | `name`, `projectId`, `joinedOn` (date), optional nullable `memberId`, `discordId`. Creates or replaces. |
| `POST /api/buildspace/activity` | `projectId`, optional `personId`, `mentorId`, `purchaseId`, `kind` (weekly/mentor/request/purchase/absence), `date`, `body`, `status` (open/resolved/on-track/needs-help). |
| `POST /api/buildspace/activity/{id}/resolve` | No body. Only unlinked general requests and purchase needs. |
| `POST /api/ingest/buildspace` | Bearer service token with **only** the needed `ingest:buildspace` scope; weekly payload below. No session login required. |

Contracts live in both contract files; the contract checker exercises every route, including repeated ingestion and revoked-token rejection. Reads return the whole register because the intended cohort is small; pagination/retention is a follow-up if history grows substantially.

## Discord bot handoff

1. Register builders and save each Discord **user ID**, not the display name.
2. A director creates an expiring service token in Officers → Tokens with `ingest:buildspace`, then stores it in the bot's secret environment as `BUILDSPACE_INGEST_TOKEN`. Set `DASHBOARD_URL` to the dashboard's HTTPS origin. The existing token revocation screen can disable ingestion immediately.
3. Wire the adapter stub at `scripts/buildspace-discord-stub.mjs` into the bot's weekly slash command or dedicated-channel handler. The bot itself is not implemented or deployed by this draft. Take author identity and message ID from Discord's trusted event, not from user-supplied text. Compute the check-in date in Pacific time.
4. Submit this payload, keeping the original message ID and identical body/date/status for retries:

```json
{
  "discordId": "123456789012345678",
  "externalId": "discord-message-unique-id",
  "kind": "weekly",
  "date": "2026-11-03",
  "body": "Tested our prototype with three students. Next: simplify onboarding.",
  "status": "on-track"
}
```

The API derives builder and project from `discordId`; caller-supplied person/project IDs cannot override it. Unlinked authors get 400. Only weekly entries are accepted. Identical retries succeed without another entry; reusing a message ID with different content gets 409. Message edits need a deliberate future correction policy, rather than silently rewriting the activity history. Retry network/5xx failures with backoff; surface 400/401/403/409 to the operator. The bot adapter intentionally leaves scheduling, Discord events, command registration and retry persistence to the actual bot integration.

Manual check-ins use the drawer's Add entry section and share the same timeline. Bot secrets are never sent to the browser. Ingestion tokens can write weekly updates for any registered Discord ID, so scope the token to this bot and keep it out of client-side code.

## Open decisions for iteration

- Is one active project per builder correct, and how should project transfers preserve history?
- What is the weekly deadline, and should excused absences suppress follow-up?
- Which mentor notes are private if mentors later gain restricted access?
- Should member and Finance linking use search pickers rather than explicit IDs?
- Are absence date ranges, entry corrections/deletion and cohort/archive management needed next?
