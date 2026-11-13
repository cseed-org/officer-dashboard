# Review + QA — PR #63 `v09-settings-stats` @ 30dc934

## What ran
- `cd server && go test ./...` — all packages pass; `TestMyHostingStats` re-run uncached (`-count=1`): PASS.
- `npm run contract-check` — passed, 0 schema errors (`meStats` validated with a user who has a last event).
- `web/tests/settings.spec.ts` on desktop + phone (astro dev :14363 → dev-tagged Go server :18763 on a fresh seed, `--retries=0`): **6/6 passed**.
- Manual browser QA per role (seed data): officer (Ava) sees 1 event / 25 attendees / "Coming soon" / link tile "BuildHer: Intro to Web Apps · Oct 16, 2026" → `/events?id=event-buildher-1`. Director and leadership see 0 / 0 / "Coming soon" / empty state. Leadership still gets the club-links and club-location forms; officer/director get the read-only views. Profile form, theme/density, time zone, calendar, sign out and the tour link are all present. No horizontal overflow at 390 px.
- Admin tokens/users and audit log live on the Officers page on `main` too, so they are not missing from Settings (no regression).

## "Hosted" definition: defensible
`events.host_user_id = me` AND `status IN ('published','completed')` AND `starts_at <= now`. This is the only per-user host field (`host_subteam_id` is a team, not a person), so it's the right column. Drafts, cancelled events and future events are excluded, and the test covers all three. Deleted events are hard-deleted (`DELETE FROM events`), and `event_guests` cascades, so they drop out of both counts automatically. Zero case: count 0, sum `coalesce`d to 0, `lastEvent` null (tested). Many events: a correlated count per event summed, which is correct. The unique `(event_id,email)` index stops double-counting within one event; a person who attends two events counts twice, which is reasonable for "attendees across events".

## Findings

**F-1 — should-fix — `web/src/ui/views/SettingsView.tsx:41-42`**
If `/api/me/stats` fails, "Events hosted" and "Attendees" show **0** with their normal sub-text ("that you hosted"), so it looks like real data. Verified by forcing a 500: tiles read `0 | that you hosted`, `0 | checked in at your events`; only the last tile says "Could not load".
*Fix:* when `stats.status === 'error'`, show `'—'` with sub "Could not load" on all three data tiles. Optionally add a retry, e.g. make the last tile's sub a button that calls `stats.reload`.

**F-2 — should-fix — `web/tests/settings.spec.ts:62-78`**
The new spec signs in as **director**, who hosts nothing in the seed, and then accepts "either a link or the empty state". As a result, the link branch (title, date, `href`) is never tested end to end, and the test would still pass if `lastEvent` were always null.
*Fix:* the seed is deterministic, so sign in as `officer` (Ava) and assert `1`, `25`, the text "BuildHer: Intro to Web Apps", and `href="/events?id=event-buildher-1"`. Keep a second, short check that director sees the "not hosted" empty state and `0`.

**F-3 — nit — `server/internal/store/mystats.go:29-38`**
The function runs two separate reads. If the only hosted event is deleted or cancelled between them, the second `QueryRow` returns `sql.ErrNoRows` and the endpoint answers 500. This is very unlikely.
*Fix:* treat `sql.ErrNoRows` as "no last event", or fold everything into one statement (for example a CTE plus a window function or a subquery for the latest row).

**F-4 — nit — `web/src/ui/components/StatRow.tsx` via `SettingsView.tsx:45`**
The event title is passed as a tile `value`, so it goes through `CountUp`. A title containing a number (e.g. "Hack Night 3") would count up from "Hack Night 0" while the page animates in. Reduced motion is respected, so this is cosmetic only.
*Fix:* add an opt-out on `StatTile` (e.g. `countUp={false}`), or render the title some other way. Since `StatTile` is shared, keep the change small.

**F-5 — nit — `web/src/styles/components.css:274` / `SettingsView.tsx:45`**
At phone width the title is cut to "BuildHer: In…" with no way to see the rest; the DOM text is complete, so screen readers are fine.
*Fix:* add `title={lastEvent.name}`, or allow two lines with `-webkit-line-clamp:2` instead of `nowrap`.

**F-6 — nit — `SettingsView.tsx:51,83,132,136,145`**
- Heading structure: the group headings ("Account", "Preferences"…) are `h2`, the same level as each Section's `h2`, so the screen-reader outline stays flat. Consider wrapping each group in a `<section aria-labelledby="g-…">`, or making the group label a non-heading eyebrow.
- "Session" group → "session" section is a repeated heading; drop the group label for a single-section group.
- "club location" (a leadership club setting) appears under **Preferences** because it lives inside `TimezonePanel`. It would sit better under **Club**, but only if it can be split off with little churn.

**F-7 — nit (PR description) — data caveat**
Luma-imported events have no `host_user_id` (the Luma import never sets it), so they count for nobody. Please state this in the PR alongside the chosen definition, so leadership isn't surprised by low numbers. There is also no index on `events.host_user_id`, which is fine at the club's event volume.

## Verdict
**Changes needed (small).** F-1 and F-2 should be fixed before merge. The rest are optional polish. The scope matches the brief: shared `StatRow` reused, Discord tile is a placeholder only, one endpoint, contracts updated and contract-check green, existing settings work for officer, director and leadership, the tour replay entry is there, and the page works at phone width.
