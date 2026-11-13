# Buildspace + Launchpad draft

Buildspace + Launchpad keeps the builder register, projects, mentor assignments and activity together at `/buildspace-launchpad`. All existing active officers, directors and leadership can read and edit it. External mentor contacts have no sign-in rights just because they are assigned: a mentor who needs dashboard access must already be an explicitly allowlisted officer. A future mentor portal should scope access to assigned projects and redact private notes before adding a new role.

## Working model

Each builder belongs to one project. Track and the one or two assigned mentors follow that project; partners are its other builders. A project can temporarily have zero mentors and is flagged until someone assigns one. Tracks are exactly Software, Hardware, Creative and Extended Virtual Reality (XR). Mentors reuse External Contacts of type mentor/both. A mentor with no assignment stays visible in the Mentors table, so officers can spot spare capacity. Project creation and builder registration are separate, and projects can exist before their builders join.

Builder records can link to a local Members mirror ID and a unique Discord user ID. The draft uses explicit IDs when linking existing members and Finance requests. Moving a builder with activity to another project is refused to preserve history; a future transfer flow needs a decision on historical membership. Project edits replace the full form and currently use last-write-wins semantics.

The activity register supports weekly check-ins, mentor notes, general requests, purchase needs and absences. Entries can describe a whole project or a builder on it; weekly updates require a builder, and mentor check-ins require a currently assigned mentor. Person details include that person's entries plus project-wide activity, while project details include all activity. Entries are append-only, except that open, unlinked requests/purchase needs can be marked resolved. A linked Finance purchase reads its live title and status; Finance owns that lifecycle. Buildspace + Launchpad does not change the purchase's permissions or ledger.

The People, Projects and Mentors tabs support search, track, mentor, stage and attention filters. The People table includes clickable partners and compact labeled counts for open requests, open purchases and absences this week (including project-wide entries plus the person’s own activity). Table is the directory view; grouped cards use track, mentor or project; the board always shows projects by stage, with stage edits available in the project drawer. Shared projects appear under each of their mentors. URL parameters preserve the tab, layout, grouping, filters and selected person/project. Summary counts show the whole cohort and act as filters.

“Awaiting this week” means a builder who has joined and has no dated weekly entry between the start of the current check-in week and today, including the week in progress; it is a follow-up signal, not an attendance verdict. Once the due time passes on the due day, the badge reads “Past due” until the next week starts. Absences do not erase the follow-up signal. The absence filter covers dated absences this week. No automatic reminders are sent.

## Check-in weeks and the due day

Leadership sets the weekly due day from the Buildspace + Launchpad page itself (**Check-in schedule**, next to Add builder); every officer can open it to read the current rule, and the server refuses writes from anyone below leadership. Choices are a due weekday and an optional due time (empty means end of day). Every change is audited as `buildspace_launchpad.schedule.updated` with the before/after schedule. The default, migrated rule is the original convention: due Sunday at end of day, so weeks run Monday–Sunday.

- **Club time zone.** “Today”, the current week and the due moment use the club time zone from Settings (previously hard-coded Pacific). Weeks are calendar dates in that zone, so DST changes never move a boundary; a due time on a DST night resolves like a wall clock (a skipped 02:30 becomes 03:30, never earlier).
- **A week is the dates up to and including its due date.** With a Wednesday due day, weeks run Thursday–Wednesday. A dated weekly entry counts for the week containing its date. The due time affects only the “Past due” signal and calendar titles, not which week an entry belongs to, because entries are date-only; a check-in dated on the due day still counts for that week.
- **Changing the setting mid-week never moves the current deadline.** The new rule starts the day after the current week's due date. Its first week runs from then to the first new due weekday at least six days later, so the transition week lasts 7–13 days instead of demanding two check-ins in a few days. Example: on Tuesday Nov 3, switching from Sunday to Wednesday keeps this week as Nov 2–8, then Nov 9–18 (10 days), then Thursday–Wednesday weeks. Saving again before the change takes effect replaces the pending change; choosing the current rule again cancels it. Past weeks never change, because rules only ever take effect after the current week.
- **Everything follows the same rules:** the “Awaiting this week” summary and filter, each builder's This week status, the drawer notice, absences this week, Discord ingest (its response reports the assigned `week`), seeding, and the `buildspace-weekly` calendar items.

Rules are versioned rows in `buildspace_week_rules` (migration `0019_buildspace_schedule.sql`, after #49's `0018`): each governs dates from its `effective_from` until the next row. The week arithmetic lives in `server/internal/store/buildspace-launchpad_schedule.go`, with unit tests for different due days, year ends, transitions, DST due moments and the club-zone “today” on a fall-back night, and API tests for leadership-only writes, auditing, a mid-week change, ingest assignment and calendar items.

`dashboard seed` inserts six projects, twelve builders and three mentor contacts, covering all tracks and stages. Weekly demo dates use the week when seeding; they age naturally. This is explicit sample-data seeding, never automatic production migration. Demo names are independent from real member records.

## API

All session endpoints require officer or above and use the existing origin protection and audit log.

| Method / path | Body / response |
| --- | --- |
| `GET /api/buildspace-launchpad` | Full small-cohort register: projects, people, mentor summaries, activity, current `weekStart`/`weekDue` and `today` in the club time zone, and `schedule` (current rule, pending change and its first week, `dueAt`, `pastDue`). |
| `PUT /api/buildspace-launchpad/projects/{id}` | Full project form: `name`, `description`, `track`, `stage`, `links` (HTTP/S only), `mentorIds` (0–2 distinct contacts). Creates or replaces. |
| `PUT /api/buildspace-launchpad/people/{id}` | `name`, `projectId`, `joinedOn` (date), optional nullable `memberId`, `discordId`. Creates or replaces. |
| `POST /api/buildspace-launchpad/activity` | `projectId`, optional `personId`, `mentorId`, `purchaseId`, `kind` (weekly/mentor/request/purchase/absence), `date`, `body`, `status` (open/resolved/on-track/needs-help). Returns `{ok, week: {start, due}}`. |
| `PUT /api/buildspace-launchpad/schedule` | **Leadership only**, audited. `dueWeekday` (0 = Sunday … 6), optional nullable `dueTime` (`HH:MM`, 24-hour; null = end of day). Returns the schedule as in the list response. |
| `POST /api/buildspace-launchpad/activity/{id}/resolve` | No body. Only unlinked general requests and purchase needs. |
| `POST /api/ingest/buildspace-launchpad` | Bearer service token with **only** the needed `ingest:buildspace` scope; weekly payload below. No session login required. Returns `{ok, week}`: the check-in week the entry was assigned to. |

Contracts live in both contract files; the contract checker exercises every route, including repeated ingestion and revoked-token rejection. Reads return the whole register because the intended cohort is small; pagination/retention is a follow-up if history grows substantially.

## Discord bot handoff

1. Register builders and save each Discord **user ID**, not the display name.
2. A director creates an expiring service token in Officers → Tokens with `ingest:buildspace`, then stores it in the bot's secret environment as `BUILDSPACE_INGEST_TOKEN`. Set `DASHBOARD_URL` to the dashboard's HTTPS origin. The existing token revocation screen can disable ingestion immediately.
3. Wire the adapter stub at `scripts/buildspace-launchpad-discord-stub.mjs` into the bot's weekly slash command or dedicated-channel handler. The bot itself is not implemented or deployed by this draft. Take author identity and message ID from Discord's trusted event, not from user-supplied text. Compute the check-in date in the club time zone (Settings); the response's `week` says which check-in week it counted for.
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
- Should excused absences suppress follow-up? (The weekly deadline is now a leadership setting.)
- Which mentor notes are private if mentors later gain restricted access?
- Should member and Finance linking use search pickers rather than explicit IDs?
- Are absence date ranges, entry corrections/deletion and cohort/archive management needed next?

## Home calendar sources

The Home calendar registry includes `buildspace-absences`, `buildspace-weekly` and `buildspace-mentor`. All three are **off by default**, including for new accounts and feed requests without an explicit source list. Enable them in Calendar → Options → Sources or toggle their legend entries. Existing saved source choices remain unchanged.

Absence and mentor check-in entries appear on their reported date, linking to their project drawer. Titles show the event kind, project, and builder when present; note bodies and absence reasons are omitted. All existing officers already have access to the full Buildspace + Launchpad register, and these sources follow that same cohort visibility policy.

Weekly due items follow the check-in schedule: one per builder on each week's due date (titles include the due time when set), including the longer transition week after a change. A dated weekly entry for that builder in the same week removes the due item. Builders not yet joined and projects marked Shipped are excluded. Due items link to the builder drawer, remain date-only across viewer time zones, and send no reminders.

The loaders filter the requested exclusive date range in SQL; the registry controls metadata, validation and widget toggles without another frontend source switch. Migration `0017_buildspace.sql` follows dashboard `0015` and calendar `0016`.

## Motion (V0.6)

Buildspace + Launchpad uses the shared V0.5 motion system (`docs/motion-performance-v05.md`) rather than one-offs: tokens from `tokens.css`, the page-content rise from the Gate, the `Async` skeleton crossfade, the first-eight `.motion-list` stagger (People/Projects/Mentors tables, card groups, board lanes, drawer timelines and partner lists, and the manual-entry/project/builder forms, so conditionally revealed fields enter too), the native drawer edge slide with backdrop fade, and the shared `Tabs` component for People/Projects/Mentors, whose single indicator slides with transform only (they are now a real tablist with arrow-key navigation). Drawer sections switch with the shared `.tab-panel` enter; the schedule popover uses `pop-in` like the calendar Options popover.

Two small shared helpers live in `web/src/ui/components/motion.ts`:

- `useCrossfade` swaps whole views (tab, Table/Cards/Board, card grouping). The outgoing view is cloned, inert and `aria-hidden` with ids stripped, into the same `.async-outgoing` overlay that `Async` uses, and fades for 180ms while the incoming view owns layout, so removing the overlay never resizes the page.
- `useFlip` glides keyed `data-flip` items from their previous to their new position after filter changes and board stage moves (a project whose stage is saved in the drawer slides to its new lane). It reads each rect once per commit relative to the view, then writes; it skips above 60 items and is cancelled when the view itself is swapped.

Everything is transform/opacity, starts at non-zero opacity, and is disabled under `prefers-reduced-motion` (no overlay, no FLIP, no CSS animation). Browser checks in `web/tests/buildspace-launchpad.spec.ts` cover the tablist and indicator, overlay inertness and stable height, the paintable first frame and eight-item cap, FLIP on filters and stage moves, the drawer slide, form field entry, reduced motion, the schedule popover permissions and save/cancel flow, and no horizontal overflow at desktop and phone widths in both themes.

### Performance

Measured with `web/scripts/performance.mjs` (`PERF_ROUTES=/buildspace-launchpad`, director, the standard throttled profile) against main `7ba453c`, alternating before/after run by run on the shared Raspberry Pi. The LCP element is the intro line “A little progress, every week.”. It previously waited for `/api/buildspace-launchpad`; the intro and its buttons now render before the register loads (the schedule button holds the same box while disabled), so it paints as soon as sign-in resolves.

| Variant | LCP median (min–max) | CLS median | JS bytes |
| --- | ---: | ---: | ---: |
| main `7ba453c` | 2.080s (2.076–2.188) | 0 | 76,747 |
| this branch | 1.716s (1.676–1.784) | 0 | 79,108 |

Main was already over the 2s budget on this page; the branch meets LCP < 2s and CLS < 0.05 in these three lab samples, which are not a field guarantee. An earlier cut that kept the intro inside `Async` measured 2.172s median, against 2.176s for its own main baseline (`30b1a4b`) in the same batch: the motion itself costs no measurable LCP. Raw samples: [perf.json](evidence/buildspace-launchpad-v06/perf.json).

Screenshots (built site, leadership): [people desktop light](evidence/buildspace-launchpad-v06/people-1440-light.png), [people phone dark](evidence/buildspace-launchpad-v06/people-390-dark.png), [board desktop dark](evidence/buildspace-launchpad-v06/board-1440-dark.png), [drawer phone light](evidence/buildspace-launchpad-v06/drawer-390-light.png), [schedule desktop light](evidence/buildspace-launchpad-v06/schedule-1440-light.png), [schedule phone dark](evidence/buildspace-launchpad-v06/schedule-390-dark.png); all four theme/width combinations of each are in that folder.

## Rename compatibility

Migration 0023 renames the five tables and recreates the three explicit indexes under `buildspace_launchpad_*`, preserving rows and foreign keys. API aliases retain old clients and ingest tokens (`ingest:buildspace`). Calendar source IDs and item IDs remain stable for saved preferences and ICS subscriptions; titles and links use the new name and route. The legacy page redirects with 301 and keeps query parameters.
