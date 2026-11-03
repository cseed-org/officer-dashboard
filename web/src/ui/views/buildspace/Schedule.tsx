import { useRef, useState } from "preact/hooks";
import type { BuildspaceSchedule } from "../../../lib/contracts";
import { api } from "../../../lib/api/client";
import { fmtShort } from "../../../lib/format";
import { errorMessage, Notice } from "../../components/states";

export const WEEKDAYS = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];
type Rule = BuildspaceSchedule["current"];

/** Club wall-clock "18:00" → "6:00 PM"; no zone conversion (it is already club time). */
export const clock = (t: string) => {
  const [h, m] = t.split(":").map(Number) as [number, number];
  return `${h % 12 || 12}:${String(m).padStart(2, "0")} ${h < 12 ? "AM" : "PM"}`;
};
export const ruleLabel = (r: Rule) =>
  `${WEEKDAYS[r.dueWeekday]}s, ${r.dueTime ? clock(r.dueTime) : "end of day"}`;
const range = (w: { start: string; due: string }) =>
  `${fmtShort(w.start)} – ${fmtShort(w.due)}`;

export function scheduleSummary(s: BuildspaceSchedule) {
  const pending = s.pending
    ? ` From ${fmtShort(s.pending.effectiveFrom)}, due ${ruleLabel(s.pending)}.`
    : "";
  return `Weekly check-ins are due ${ruleLabel(s.current)} (${s.timezone}); this week runs ${range(s.week)}.${pending}`;
}

/**
 * The due-day setting lives on the Buildspace page. Every officer can read it; leadership
 * edits it. A change never moves the current week's deadline: it starts the day after.
 */
export function ScheduleMenu({
  schedule,
  canEdit,
  saved,
}: {
  schedule: BuildspaceSchedule | null;
  canEdit: boolean;
  saved: () => void;
}) {
  // Same box before the register loads, so the intro row never shifts.
  if (!schedule)
    return (
      <button type="button" class="btn" disabled>
        Check-in schedule
      </button>
    );
  return (
    <ScheduleDetails schedule={schedule} canEdit={canEdit} saved={saved} />
  );
}
function ScheduleDetails({
  schedule,
  canEdit,
  saved,
}: {
  schedule: BuildspaceSchedule;
  canEdit: boolean;
  saved: () => void;
}) {
  const menu = useRef<HTMLDetailsElement>(null);
  const target = schedule.pending ?? schedule.current;
  const [day, setDay] = useState(target.dueWeekday);
  const [time, setTime] = useState(target.dueTime ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const close = () => {
    menu.current?.removeAttribute("open");
    menu.current?.querySelector("summary")?.focus();
  };
  const changed =
    day !== target.dueWeekday || (time || null) !== (target.dueTime ?? null);
  return (
    <details
      class="bs-schedule"
      ref={menu}
      onToggle={(e) => {
        if (e.currentTarget.open) {
          setDay(target.dueWeekday);
          setTime(target.dueTime ?? "");
          setError("");
        }
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && menu.current?.open) {
          e.preventDefault();
          close();
        }
      }}
    >
      <summary class="btn">Check-in schedule</summary>
      <div
        class="bs-schedule-panel"
        role="group"
        aria-labelledby="bs-schedule-title"
      >
        <h2 id="bs-schedule-title" class="bs-schedule-title">
          Weekly check-in deadline
        </h2>
        <p>
          Due <strong>{ruleLabel(schedule.current)}</strong> in{" "}
          {schedule.timezone}. This week: {range(schedule.week)}
          {schedule.pastDue && " · deadline passed"}.
        </p>
        {schedule.pending && schedule.pendingFirstWeek && (
          <p class="bs-schedule-pending">
            Changing to <strong>{ruleLabel(schedule.pending)}</strong>. The
            first week under it runs {range(schedule.pendingFirstWeek)}.
          </p>
        )}
        {canEdit ? (
          <form
            class="bs-form bs-schedule-form"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              try {
                await api("buildspaceSchedulePut", {
                  body: { dueWeekday: day, dueTime: time || null },
                });
                close();
                saved();
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <label>
              Due day
              <select
                class="select"
                value={day}
                disabled={busy}
                onChange={(e) => setDay(Number(e.currentTarget.value))}
              >
                {WEEKDAYS.map((d, i) => (
                  <option value={i}>{d}</option>
                ))}
              </select>
            </label>
            <label>
              Due time · optional
              <input
                class="input"
                type="time"
                value={time}
                disabled={busy}
                aria-describedby="bs-schedule-hint"
                onInput={(e) => setTime(e.currentTarget.value)}
              />
            </label>
            <p class="bs-muted small" id="bs-schedule-hint">
              Leave the time empty for end of day. Changes start after the
              current week ends ({fmtShort(schedule.week.due)}), so this week’s
              deadline never moves; the first week under a new day lasts 7–13
              days.
            </p>
            {error && <Notice tone="negative">{error}</Notice>}
            <div class="action-row">
              <button
                type="button"
                class="btn btn--text"
                disabled={busy}
                onClick={close}
              >
                Cancel
              </button>
              <button class="btn btn--primary" disabled={busy || !changed}>
                {busy ? "Saving…" : "Save schedule"}
              </button>
            </div>
          </form>
        ) : (
          <p class="bs-muted small">Leadership can change the due day.</p>
        )}
      </div>
    </details>
  );
}
