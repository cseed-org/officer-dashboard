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
