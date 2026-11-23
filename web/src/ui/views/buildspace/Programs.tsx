import { useState } from "preact/hooks";
import { api, fieldErrors } from "../../../lib/api/client";
import { useApi } from "../../../lib/api/useApi";
import { HUES, type Hue } from "../../../lib/colors";
import type { CurrentUser, Program, ProgramTrack } from "../../../lib/contracts";
import { fmtDate, fmtShort } from "../../../lib/format";
import { setParams, useSearch } from "../../../lib/nav";
import { Drawer } from "../../components/Drawer";
import { Badge, DataTable, Field, describe, type Column } from "../../components/kit";
import { PeopleChip } from "../../components/PeopleTable";
import { Empty, ErrorState, Loading, Notice, errorMessage } from "../../components/states";
import { leaveEntry, pushEntry } from "./history";
import {
  WEEKDAYS,
  addDays,
  addMinutes,
  clock,
  laIso,
  laNow,
  minutesOf,
  nextSession,
  programSessions,
  strandedExceptions,
  type ClubNow,
  type Session,
} from "./programSchedule";

/**
 * The Programs tab of Buildspace + Launchpad: weekly programs (buildspace, buildHER, future
 * tracks) with per-session skip/move exceptions. Programs are projected to the Weekly Programs
 * Google calendar by the server; here officers manage the source records. Programs are not
 * cohort data: BuildspaceView renders this tab without the cohort register.
 */

const asHue = (hue: string): Hue => ((HUES as readonly string[]).includes(hue) ? (hue as Hue) : "blue");
const CLOSED = { program: null, pnew: null };

export function ProgramsPanel({ me }: { me: CurrentUser }) {
  const qs = useSearch();
  const programs = useApi("programsList");
  const tracks = useApi("programTracks");
  const [notice, setNotice] = useState("");
  const [managing, setManaging] = useState(false);
  const leadership = me.role === "leadership" || me.isOwner;
  if (programs.status === "loading" || tracks.status === "loading") return <Loading rows={6} label="Loading programs" />;
  if (programs.status === "error") return <ErrorState error={programs.error} onRetry={programs.reload} />;
  if (tracks.status === "error") return <ErrorState error={tracks.error} onRetry={tracks.reload} />;
  const reload = () => {
    programs.reload();
    tracks.reload();
  };
  /** Exception writes answer with the whole program: swap it in place, no refetch. */
  const apply = (p: Program) => programs.set((d) => ({ programs: d.programs.map((x) => (x.id === p.id ? p : x)) }));
  const all = programs.data.programs;
  const trackList = tracks.data.tracks;
  const trackById = new Map(trackList.map((t) => [t.id, t]));
  const filter = qs.get("ptrack") || "";
  const shown = all.filter((p) => !filter || p.trackId === filter);
  const groups = trackList
    .map((t) => ({ track: t, programs: shown.filter((p) => p.trackId === t.id) }))
    .filter((g) => g.programs.length > 0);
  const now = laNow();
  const openId = qs.get("program");
  const openProgram = all.find((p) => p.id === openId) ?? null;
  const creating = qs.get("pnew") === "1";
  // Buildspace's history convention (buildspace/history.ts): opening pushes one entry and
  // closing steps back over it, so Back never reopens a closed drawer.
  const close = () => leaveEntry(CLOSED);
  const columns: Column<Program>[] = [
    {
      key: "name",
      label: "Program",
      primary: true,
      render: (p) => (
        <>
          <button class="cell-title bs-title" data-program-open={p.id} onClick={() => pushEntry({ program: p.id })}>
            {p.name}
          </button>
          {p.description && <span class="cell-sub">{p.description}</span>}
        </>
      ),
    },
    {
      key: "when",
      label: "When",
      render: (p) => (
        <span class="bs-nowrap">
          {WEEKDAYS[p.weekday]}s · {clock(p.startTime)}–{clock(addMinutes(p.startTime, p.durationMin))}
        </span>
      ),
    },
    { key: "location", label: "Location", render: (p) => p.location || <span class="muted">—</span> },
    {
      key: "dates",
      label: "Dates",
      render: (p) => (
        <span class="bs-nowrap">
          {fmtShort(p.startsOn)} – {fmtShort(p.endsOn)}
        </span>
      ),
    },
    {
      key: "next",
      label: "Next session",
      render: (p) => {
        const next = nextSession(p, now);
        if (!next) return <span class="muted">—</span>;
        return (
          <span class="bs-nowrap" data-next-session={next.on}>
            {fmtDate(next.on)} · {clock(next.start)}
            {next.exception?.kind === "move" && <> <Badge tone="info">Moved</Badge></>}
          </span>
        );
      },
    },
  ];
  return (
    <div class="stack" data-programs-panel>
      {notice && <Notice tone="positive">{notice}</Notice>}
      <Notice tone="info">
        Programs sync to the Weekly Programs Google calendar automatically; that calendar is read-only — make changes here.
      </Notice>
      <div class="bs-filters">
        <label>
          <span>Track</span>
          <select class="select" aria-label="Filter by track" value={filter} onChange={(e) => setParams({ ptrack: e.currentTarget.value }, { replace: true })}>
            <option value="">All tracks</option>
            {trackList.map((t) => (
              <option value={t.id}>{t.name}</option>
            ))}
          </select>
        </label>
        <div class="action-row">
          {leadership && (
            <button type="button" class="btn" data-manage-tracks onClick={() => setManaging(true)}>
              Manage tracks
            </button>
          )}
          <button type="button" class="btn btn--primary" data-new-program onClick={() => pushEntry({ pnew: "1" })}>
            New program
          </button>
        </div>
      </div>
      {groups.length === 0 ? (
        <Empty title="No programs yet">
          {filter ? "No programs in this track. Clear the filter or add one." : "Add the first weekly program; it will sync to the Weekly Programs calendar."}
        </Empty>
      ) : (
        groups.map((g) => (
          <section key={g.track.id} class="pg-track" aria-label={`${g.track.name} programs`} data-program-track={g.track.key}>
            <h3 class="subhead">
              <PeopleChip hue={asHue(g.track.hue)}>{g.track.name}</PeopleChip>
            </h3>
            <DataTable rows={g.programs} columns={columns} rowKey={(p) => p.id} caption={`${g.track.name} programs`} bs />
          </section>
        ))
      )}
      <Drawer id="program-form" open={creating} onClose={close} title="New program" eyebrow="Programs">
        {creating && (
          <ProgramForm
            tracks={trackList}
            saved={() => {
              reload();
              close();
              setNotice("Program saved. It syncs to the Weekly Programs calendar.");
            }}
          />
        )}
      </Drawer>
      <Drawer id="program-detail" open={!!openProgram} onClose={close} title={openProgram?.name ?? "Program"} eyebrow="Program" size="lg">
        {openProgram && (
          <ProgramDetail
            key={`${openProgram.id}:${openProgram.updatedAt}`}
            program={openProgram}
            tracks={trackList}
            track={trackById.get(openProgram.trackId) ?? null}
            now={now}
            reload={reload}
            apply={apply}
            close={close}
            setNotice={setNotice}
          />
        )}
      </Drawer>
      <Drawer id="track-manager" kind="dialog" open={managing} onClose={() => setManaging(false)} title="Program tracks">
        {managing && <TrackManager tracks={trackList} saved={() => tracks.reload()} />}
      </Drawer>
    </div>
  );
}

function ProgramForm({ tracks, program, saved, cancel }: { tracks: ProgramTrack[]; program?: Program; saved: () => void; cancel?: () => void }) {
  const [error, setError] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  // Changing the weekday or shortening the dates drops skips and moves that no longer fall on a
  // session (the server prunes them on save), so say so before saving.
  const [schedule, setSchedule] = useState({ weekday: program?.weekday ?? 3, startsOn: program?.startsOn ?? "", endsOn: program?.endsOn ?? "" });
  const stranded = program && schedule.startsOn && schedule.endsOn ? strandedExceptions(program, schedule) : [];
  return (
    <form
      class="stack"
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy) return;
        const f = new FormData(e.currentTarget);
        const startsOn = String(f.get("startsOn"));
        const endsOn = String(f.get("endsOn"));
        setFields({});
        setError("");
        if (endsOn < startsOn) {
          setFields({ endsOn: "The program ends before it starts." });
          return;
        }
        setBusy(true);
        try {
          const body = {
            trackId: String(f.get("trackId")),
            name: String(f.get("name")).trim(),
            weekday: Number(f.get("weekday")),
            startTime: String(f.get("startTime")),
            durationMin: Number(f.get("durationMin")),
            location: String(f.get("location")).trim(),
            startsOn,
            endsOn,
            description: String(f.get("description")).trim(),
          };
          if (program) await api("programUpdate", { params: { id: program.id }, body });
          else await api("programCreate", { body });
          saved();
        } catch (err) {
          setFields(fieldErrors(err));
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <Field id="pg-name" label="Program name" error={fields.name}>
        <input class="input" name="name" required maxLength={200} defaultValue={program?.name} {...describe("pg-name", fields.name)} />
      </Field>
      <div class="grid-fields">
        <Field id="pg-track" label="Track" error={fields.trackId}>
          <select class="select" name="trackId" defaultValue={program?.trackId ?? tracks[0]?.id} {...describe("pg-track", fields.trackId)}>
            {tracks.map((t) => (
              <option value={t.id}>{t.name}</option>
            ))}
          </select>
        </Field>
        <Field id="pg-weekday" label="Weekday" error={fields.weekday}>
          <select
            class="select"
            name="weekday"
            defaultValue={String(schedule.weekday)}
            onChange={(e) => setSchedule((s) => ({ ...s, weekday: Number(e.currentTarget.value) }))}
            {...describe("pg-weekday", fields.weekday)}
          >
            {WEEKDAYS.map((w, i) => (
              <option value={i}>{w}</option>
            ))}
          </select>
        </Field>
      </div>
      <div class="grid-fields">
        <Field id="pg-start" label="Start time · PT" error={fields.startTime}>
          <input class="input" type="time" name="startTime" required defaultValue={program?.startTime ?? "18:00"} {...describe("pg-start", fields.startTime)} />
        </Field>
        <Field id="pg-duration" label="Duration · minutes" error={fields.durationMin}>
          <input class="input" type="number" name="durationMin" required min={1} max={1440} step={1} defaultValue={program?.durationMin ?? 60} {...describe("pg-duration", fields.durationMin)} />
        </Field>
      </div>
      <Field id="pg-location" label="Location" error={fields.location} optional>
        <input class="input" name="location" maxLength={500} defaultValue={program?.location} {...describe("pg-location", fields.location)} />
      </Field>
      <div class="grid-fields">
        <Field id="pg-starts" label="First date" error={fields.startsOn}>
          <input
            class="input"
            type="date"
            name="startsOn"
            required
            defaultValue={program?.startsOn}
            onInput={(e) => setSchedule((s) => ({ ...s, startsOn: e.currentTarget.value }))}
            {...describe("pg-starts", fields.startsOn)}
          />
        </Field>
        <Field id="pg-ends" label="Last date" error={fields.endsOn}>
          <input
            class="input"
            type="date"
            name="endsOn"
            required
            defaultValue={program?.endsOn}
            onInput={(e) => setSchedule((s) => ({ ...s, endsOn: e.currentTarget.value }))}
            {...describe("pg-ends", fields.endsOn)}
          />
        </Field>
      </div>
      <Field id="pg-description" label="Description" error={fields.description} optional>
        <textarea class="textarea" name="description" rows={3} maxLength={10000} defaultValue={program?.description} {...describe("pg-description", fields.description, true)} />
      </Field>
      {stranded.length > 0 && (
        <Notice tone="attention">
          <span data-stranded-exceptions={stranded.length}>
            Saving removes {stranded.length === 1 ? "1 skipped or moved session" : `${stranded.length} skipped or moved sessions`} no longer on the
            schedule ({stranded.map((x) => fmtShort(x.onDate)).join(", ")}).
          </span>
        </Notice>
      )}
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="action-row">
        <button class="btn btn--primary" disabled={busy || !tracks.length}>
          {busy ? "Saving…" : "Save program"}
        </button>
        {cancel && (
          <button type="button" class="btn" onClick={cancel}>
            Cancel
          </button>
        )}
      </div>
    </form>
  );
}

function ProgramDetail({
  program,
  tracks,
  track,
  now,
  reload,
  apply,
  close,
  setNotice,
}: {
  program: Program;
  tracks: ProgramTrack[];
  track: ProgramTrack | null;
  now: ClubNow;
  reload: () => void;
  apply: (p: Program) => void;
  close: () => void;
  setNotice: (s: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  // Errors show inside the drawer: it is a modal, so a page-level notice would sit behind it.
  const [error, setError] = useState("");
  const sessions = programSessions(program, now);
  if (editing) {
    return (
      <ProgramForm
        tracks={tracks}
        program={program}
        cancel={() => setEditing(false)}
        saved={() => {
          reload();
          close();
          setNotice("Program saved. It syncs to the Weekly Programs calendar.");
        }}
      />
    );
  }
  const remove = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api("programDelete", { params: { id: program.id } });
      reload();
      close();
      setNotice(`${program.name} deleted.`);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };
  return (
    <div class="stack" data-program-detail={program.id}>
      <p class="page-sub">
        {track && <PeopleChip hue={asHue(track.hue)}>{track.name}</PeopleChip>} {WEEKDAYS[program.weekday]}s · {clock(program.startTime)}–
        {clock(addMinutes(program.startTime, program.durationMin))} · {program.location || "no location"} · {fmtDate(program.startsOn)} – {fmtDate(program.endsOn)}
      </p>
      {program.description && <p>{program.description}</p>}
      <div class="action-row">
        <button type="button" class="btn btn--small" data-edit-program onClick={() => setEditing(true)}>
          Edit
        </button>
        {confirming ? (
          <>
            <button type="button" class="btn btn--small" data-confirm-delete-program disabled={busy} onClick={remove}>
              {busy ? "Deleting…" : "Confirm delete"}
            </button>
            <button type="button" class="btn btn--small" onClick={() => setConfirming(false)}>
              Keep program
            </button>
          </>
        ) : (
          <button type="button" class="btn btn--small" data-delete-program onClick={() => setConfirming(true)}>
            Delete
          </button>
        )}
      </div>
      {confirming && (
        <Notice tone="attention">Delete {program.name}? Its sessions leave the Weekly Programs calendar. This cannot be undone.</Notice>
      )}
      {error && (
        <div data-program-error>
          <Notice tone="negative">{error}</Notice>
        </div>
      )}
      <section aria-label="Upcoming sessions">
        <h3 class="subhead">Upcoming sessions</h3>
        {sessions.length === 0 ? (
          <Empty title="No upcoming sessions">The program's dates have passed.</Empty>
        ) : (
          <ul class="pg-sessions motion-list">
            {sessions.map((s) => (
              <SessionRow key={s.date} program={program} session={s} today={now.today} apply={apply} setError={setError} />
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function SessionRow({
  program,
  session,
  today,
  apply,
  setError,
}: {
  program: Program;
  session: Session;
  today: string;
  apply: (p: Program) => void;
  setError: (s: string) => void;
}) {
  const [moving, setMoving] = useState(false);
  const [busy, setBusy] = useState(false);
  const skipped = session.exception?.kind === "skip";
  const moved = session.exception?.kind === "move";
  const when = fmtDate(session.on);
  const formId = `mv-${session.date}`;
  /** Runs one exception write; true when it saved. */
  const act = async (fn: () => Promise<{ program: Program }>) => {
    if (busy) return false;
    setBusy(true);
    setError("");
    try {
      apply((await fn()).program);
      return true;
    } catch (err) {
      setError(errorMessage(err));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const skip = () => act(() => api("programExceptionSave", { params: { id: program.id }, body: { onDate: session.date, kind: "skip" } }));
  const undo = () => act(() => api("programExceptionDelete", { params: { id: program.id }, query: { onDate: session.date } }));
  return (
    <li class="pg-session" data-session={session.date} data-session-on={session.on} data-session-state={skipped ? "skipped" : moved ? "moved" : "scheduled"}>
      <div class="pg-session-line">
        <span class="pg-session-when">
          <time dateTime={session.on}>{when}</time> · {clock(session.start)}–{clock(session.end)}
          {session.location ? ` · ${session.location}` : ""}
        </span>
        {moved && session.on !== session.date && <span class="muted small">was {fmtShort(session.date)}</span>}
        {skipped && <Badge tone="negative">Skipped</Badge>}
        {moved && <Badge tone="info">Moved</Badge>}
        <span class="action-row">
          {session.exception ? (
            <button type="button" class="linkish" data-undo-exception disabled={busy} aria-label={`Undo the ${skipped ? "skip" : "move"} of the ${fmtDate(session.date)} session`} onClick={undo}>
              Undo
            </button>
          ) : (
            <>
              <button type="button" class="linkish" data-skip-session disabled={busy} aria-label={`Skip the ${when} session`} onClick={skip}>
                Skip
              </button>
              <button
                type="button"
                class="linkish"
                data-move-session
                disabled={busy}
                aria-label={`Move the ${when} session`}
                aria-expanded={moving}
                aria-controls={formId}
                onClick={() => setMoving((m) => !m)}
              >
                Move
              </button>
            </>
          )}
        </span>
      </div>
      {moving && !session.exception && (
        <MoveForm
          id={formId}
          program={program}
          session={session}
          today={today}
          busy={busy}
          save={async (fn) => (await act(fn)) && setMoving(false)}
          cancel={() => setMoving(false)}
        />
      )}
    </li>
  );
}

function MoveForm({
  id,
  program,
  session,
  today,
  busy,
  save,
  cancel,
}: {
  id: string;
  program: Program;
  session: Session;
  today: string;
  busy: boolean;
  save: (fn: () => Promise<{ program: Program }>) => Promise<unknown>;
  cancel: () => void;
}) {
  const [error, setError] = useState("");
  return (
    <form
      class="pg-move grid-fields"
      id={id}
      aria-label={`Move the ${fmtDate(session.on)} session`}
      data-move-form
      onSubmit={(e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        const date = String(f.get("date"));
        const start = String(f.get("start"));
        const end = String(f.get("end"));
        const location = String(f.get("location")).trim();
        if (date < today) {
          setError("Pick today or a later date.");
          return;
        }
        if (end === start) {
          setError("The session needs an end time after its start.");
          return;
        }
        setError("");
        // An end at or before the start is the next morning (a session that crosses midnight).
        const endDate = minutesOf(end) < minutesOf(start) ? addDays(date, 1) : date;
        void save(() =>
          api("programExceptionSave", {
            params: { id: program.id },
            body: {
              onDate: session.date,
              kind: "move",
              newStartsAt: laIso(date, start),
              newEndsAt: laIso(endDate, end),
              newLocation: location || null,
            },
          }),
        );
      }}
    >
      <Field id={`${id}-date`} label="New date" error={error}>
        <input class="input" type="date" name="date" required min={today} defaultValue={session.on < today ? today : session.on} {...describe(`${id}-date`, error)} />
      </Field>
      <Field id={`${id}-start`} label="Start · PT">
        <input class="input" type="time" name="start" required defaultValue={session.start} id={`${id}-start`} />
      </Field>
      <Field id={`${id}-end`} label="End · PT" hint="Before the start means the next morning.">
        <input class="input" type="time" name="end" required defaultValue={session.end} {...describe(`${id}-end`, undefined, true)} />
      </Field>
      <Field id={`${id}-location`} label="Location" optional>
        <input class="input" name="location" maxLength={500} defaultValue={session.location} id={`${id}-location`} />
      </Field>
      <div class="action-row">
        <button class="btn btn--small btn--primary" disabled={busy}>
          {busy ? "Saving…" : "Save move"}
        </button>
        <button type="button" class="btn btn--small" onClick={cancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

function TrackManager({ tracks, saved }: { tracks: ProgramTrack[]; saved: () => void }) {
  const [error, setError] = useState("");
  const [added, setAdded] = useState("");
  const [busy, setBusy] = useState(false);
  return (
    <div class="stack">
      <p class="muted small">Programs are grouped by track. A track's name and colour are set when it is added; renaming is not available yet.</p>
      <ul class="pg-tracks">
        {tracks.map((t) => (
          <li key={t.id}>
            <PeopleChip hue={asHue(t.hue)}>{t.name}</PeopleChip> <span class="bs-muted">{t.key}</span>
          </li>
        ))}
      </ul>
      <form
        class="stack"
        onSubmit={async (e) => {
          e.preventDefault();
          if (busy) return;
          // currentTarget is null once the event finishes dispatching: keep the form before awaiting.
          const form = e.currentTarget;
          const f = new FormData(form);
          const name = String(f.get("name")).trim();
          setBusy(true);
          setError("");
          setAdded("");
          try {
            await api("programTrackCreate", { body: { key: String(f.get("key")).trim(), name, hue: String(f.get("hue")) } });
            form.reset();
            setAdded(`${name} added.`);
            saved();
          } catch (err) {
            setError(errorMessage(err));
          } finally {
            setBusy(false);
          }
        }}
      >
        <div class="grid-fields">
          <Field id="tr-name" label="New track name">
            <input class="input" id="tr-name" name="name" required maxLength={200} placeholder="Track name" />
          </Field>
          <Field id="tr-key" label="Key" hint="Lowercase letters, digits and dashes.">
            <input class="input" name="key" required maxLength={64} pattern="[a-z0-9\-]+" {...describe("tr-key", undefined, true)} />
          </Field>
        </div>
        <Field id="tr-hue" label="Colour">
          <select class="select" id="tr-hue" name="hue">
            {HUES.map((h) => (
              <option value={h}>{h}</option>
            ))}
          </select>
        </Field>
        {added && <Notice tone="positive">{added}</Notice>}
        {error && <Notice tone="negative">{error}</Notice>}
        <button class="btn btn--primary" disabled={busy}>
          {busy ? "Adding…" : "Add track"}
        </button>
      </form>
    </div>
  );
}
