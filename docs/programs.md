# Weekly programs

Generic tracks are seeded with buildspace and buildher (display name buildHER). Any officer
can manage programs, matching Buildspace project editing. Leadership can add tracks.

`GET /api/program-tracks` returns `{tracks}`; leadership POST takes `{key,name,hue}` and
returns `{track}`. `GET /api/programs` returns `{programs}` including exceptions. POST and
PATCH `/api/programs/:id` return `{program}` and accept:

```json
{"trackId":"buildspace","name":"Weekly build session","weekday":2,"startTime":"18:00","durationMin":90,"startsOn":"2026-11-01","endsOn":"2026-12-15","location":"Club room","description":"Bring your project"}
```

PATCH fields are optional. Weekdays are Sunday=0 to Saturday=6. Times use HH:MM in
America/Los_Angeles. Dates are inclusive (at most five years); duration is 1–1440 minutes.
The range must include a scheduled weekday. Changing its weekday/range removes exceptions
that no longer refer to a scheduled session.
DELETE `/api/programs/:id` returns `{ok:true}` and withdraws its recurring master.

POST `/api/programs/:id/exceptions` replaces one exception; DELETE on the same path with
`?onDate=YYYY-MM-DD` restores it. Both return `{program}`. OnDate must be a scheduled session.

```json
{"onDate":"2026-11-24","kind":"skip"}
```

```json
{"onDate":"2026-11-24","kind":"move","newStartsAt":"2026-11-25T18:00:00-08:00","newEndsAt":"2026-11-25T19:30:00-08:00","newLocation":"Other room"}
```

The source, projection and outbox update transactionally. Google receives one WEEKLY master
with UNTIL, skips as EXDATEs and moves as original-instance overrides. Removing a move
restores its original time/location. The dashboard expands the same schedule locally,
including Pacific DST. Calendar REST cannot bypass the locked source workflow.

## Programs tab (web, V0.15)

Programs live on the **Programs** tab of Buildspace + Launchpad
(`web/src/ui/views/buildspace/Programs.tsx`, schedule logic in `programSchedule.ts`). The org
calendar links a locked program item to `/buildspace-launchpad?tab=programs&program=<id>`
(`href.program(id)` in `web/src/lib/routes.ts`). Programs aren't cohort data: the tab doesn't
wait for the cohort register, works when there is no cohort, and hides the cohort picker,
summary tiles and builder/project actions.

- **List.** Programs are grouped by track (buildspace, buildHER and any track leadership adds),
  with a track filter (`?ptrack=<track id>`). Each row shows the name, weekday and Pacific time
  range, location, date range and next session: the earliest session that hasn't ended and isn't
  skipped, by the date it actually happens on (a moved session counts on its new date).
- **Create and edit.** `?pnew=1` opens the form drawer: track, name, weekday, start time (PT),
  duration in minutes, location, first and last date, description. Opening a program
  (`?program=<id>`) shows its details with Edit and Delete; Delete asks to confirm. When an edit
  changes the weekday or dates so that skipped or moved sessions are no longer on the schedule,
  the form says how many will be removed (the server prunes them on save). Cancel returns to the
  details. Opening and closing follow the page's history rule: Back never reopens a closed drawer.
- **Sessions.** The program drawer lists the next 12 sessions that haven't ended, sorted by the
  date and time they happen. A session moved from a past week to a later day stays listed, so
  its move can be undone; a skipped session stays listed with **Undo**. **Skip** and **Move**
  (new date from today on, start, end and location, in Pacific time; an end before the start is
  the next morning) write an exception for that session; **Undo** removes it. Exception writes
  answer with the whole program, which replaces the row in place. Errors show inside the drawer.
- **Tracks.** Leadership sees **Manage tracks**: add a track with a name, key and colour. The API
  has no rename or colour change yet, and no delete.
- A notice on the tab says programs sync to the Weekly Programs Google calendar, which is
  read-only there: changes are made on this tab.

Tests: `web/tests/programs-schedule.spec.ts` unit-tests session order (moves past the next week,
past sessions moved later, today until the end time, sessions crossing midnight, DST).
`web/tests/programs.spec.ts` covers create, filter, skip/undo, move/undo with the new order,
the prune warning, edit, delete, an error shown in the drawer, and leadership adding a track
(one fixed track per viewport, since tracks can't be deleted) on phone, tablet and desktop.
Programs are deleted in `finally`. `SHOTS=1` writes light and dark screenshots to
`docs/evidence/programs/`.

## Tentative Events (web, V0.15)

The Event editor has a **No set date yet** checkbox. Checked, it asks for an earliest and
latest date (a blank latest date means a single day) instead of a start and end, and saving sends
`startsAt: null` with `tentativeFrom`/`tentativeTo`. Unchecking it and saving a start clears the
range. The Events list and detail show the range with a **Tentative** badge; the month and
week views list tentative events whose range overlaps the visible days in a **No set date yet**
strip above the grid, since they have no day or time to sit on. Drafts stay visible there,
outlined like draft chips. Shared date formatters print a placeholder for an empty `startsAt`
instead of throwing.
Event pickers (purchase and design requests, CSV import, contact links) label an undated Event
"Tentative · Dec 1 – Dec 12, 2026" and sort it by the first day of its range. A contact's
tentative Event counts as past once its range ends, and the person drawer's activity shows the
range and "date not set" for an item whose `at` is empty.
