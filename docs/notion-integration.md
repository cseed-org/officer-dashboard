# Notion integration

**Status: stub, to be finished later.** Search the code for `TODO(notion)`.

The plan is a Notion page per event (agenda, run of show, notes) that the dashboard
creates when an event is drafted. Today nothing talks to Notion.

## What exists

- `server/internal/notion`: the `NotionSync` interface and `Stub`, a no-op that returns
  `https://www.notion.so/placeholder/cseed-event-<event id>` with `Placeholder: true`.
- `api.Config.Notion` defaults to the stub. `POST /api/events` calls
  `DraftEventPage` and stores the link in `events.notion_url`. A failure is logged and does
  not block the event.
- `GET /api/events/:id` returns `notion: { url, placeholder }`. `placeholder` is true for
  stub links (and when there is no link).
- The Events editor and handoff kit show the Notion section as **Coming soon**, with the
  link in a disabled field. The placeholder link is never rendered as a clickable link.

## To finish

1. A client that implements `NotionSync` with the Notion API: an integration token
   (`NOTION_TOKEN`) and the events database id (`NOTION_EVENTS_DATABASE_ID`), selected in
   `cmd/dashboard` when both are set; the stub stays the default.
2. Call Notion outside the database transaction. Create the event first, then draft the
   page (a background job or a follow-up write) and store `notion_url`, audit-logged as
   `event.notion_linked`.
3. Grow the interface with `UpdateEventPage` (name, time, location, description, Luma link
   changes) and `ArchiveEventPage` (delete), and decide whether edits made in Notion flow
   back.
4. Backfill: replace placeholder links on existing events once the client exists.
5. UI: drop "Coming soon", show the link, and add "Open in Notion".
6. Tests: a fake Notion server, idempotent drafting, and a failure that leaves the event
   saved.
