# Home calendar

The Calendar widget combines club events (including Luma-linked events) and open
design-request deadlines. It appears in the default Home layout and can be added
to existing layouts through **Edit home → Add widget**.

![Before: separate event and design pages. After: one date-ranged feed powers three widget sizes.](evidence/calendar-widget/overview.png)

## Views and options

| Size | Automatic view |
| --- | --- |
| 2×1 | One-day agenda |
| 2×2 | Week strip and agenda |
| 4×2 | Month grid |

Options saves view, sources, colour grouping and weekend visibility to the signed-in
officer's account. Small widgets always use an agenda; other sizes allow explicit
agenda, week or month views. Select a date to see its agenda and Luma links.
Event and design links open the existing detail pages. **+ event** opens the Events
creation form.

Date-only deadlines stay on their calendar date. Events follow the effective time
zone in Settings, including events spanning midnight and daylight-saving changes.
Cancelled events are hidden by the widget; completed design requests are omitted
from the feed.

## API

- `GET /api/calendar/feed?start=2026-11-01&end=2026-12-01&sources=events,design`
  returns items tagged by source and type, plus the effective time zone.
  The end date is exclusive; the range must be 1–93 calendar days.
  Omitting sources selects both; an empty sources value (or `none`) selects none.
- `GET /api/me/calendar-widget` returns the user's options.
- `PUT /api/me/calendar-widget` replaces options with
  `{view, colourBy, showWeekends, events, design}`, where the last two are booleans.
  Writes are audited and do not replace Home layout preferences.

SQLite filters before returning data. Expression and date indexes support the range
queries. The widget caches a bounded set of ranges for 60 seconds, keyed by sources
and time zone; errors offer Retry.

## Current limits

Purchases have no dated deadline field; officer availability/absences have no data
model. Existing Google/ICS integration sends club events out and does not read
personal subscriptions. Those sources need separate data support before they can
appear here. No external calendar credentials or network calls are introduced.

## Screenshots

These show seeded local data at desktop and phone widths, with the calendar in its
large month view. Runtime verification also exercises small and medium views,
options persistence, source filtering and existing detail-page navigation.

| Light | Dark |
| --- | --- |
| ![Desktop light](evidence/calendar-widget/desktop-light.png) | ![Desktop dark](evidence/calendar-widget/desktop-dark.png) |
| ![Phone light](evidence/calendar-widget/phone-light.png) | ![Phone dark](evidence/calendar-widget/phone-dark.png) |
