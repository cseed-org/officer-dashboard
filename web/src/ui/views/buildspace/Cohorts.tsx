/**
 * Cohort switcher, "New cohort" and the cohort settings menu (docs/launchpad-cohorts.md).
 * Everyone reads every cohort; directors and up create, rename, archive, restore and delete.
 */
import { useState } from "preact/hooks";
import type { LaunchpadCohort } from "../../../lib/contracts";
import { api, fieldErrors } from "../../../lib/api/client";
import { pacificToday } from "../../../lib/calendar";
import { fmtShort } from "../../../lib/format";
import { setParams } from "../../../lib/nav";
import { Drawer } from "../../components/Drawer";
import { RowMenu } from "../../components/people";
import { errorMessage, Notice } from "../../components/states";

export function CohortBar({
  cohorts,
  cohortId,
  canManage,
  changed,
}: {
  cohorts: LaunchpadCohort[];
  cohortId: string | null;
  canManage: boolean;
  /** After a create/update/delete: the message to show and the cohort to select (undefined keeps it). */
  changed: (message: string, select?: string | null) => void;
}) {
  const [dialog, setDialog] = useState<null | "new" | "edit">(null);
  const current = cohorts.find((c) => c.id === cohortId) ?? null;
  const run = async (fn: () => Promise<unknown>, message: string, select?: string | null) => {
    try {
      await fn();
      changed(message, select);
    } catch (e) {
      alert(errorMessage(e));
    }
  };
  const empty = current && !current.counts.projects && !current.counts.builders && !current.counts.invited;
  return (
    <div class="lp-cohort-bar" data-cohort-bar>
      {cohorts.length > 0 && (
        <label class="lp-cohort-select">
          <span class="visually-hidden">Cohort</span>
          <select
            class="select"
            aria-label="Cohort"
            data-cohort-select
            value={cohortId ?? ""}
            onChange={(e) => setParams({ cohort: e.currentTarget.value, person: null, project: null })}
          >
            {cohorts.map((c) => (
              <option value={c.id} key={c.id}>
                {c.name}
                {c.status === "archived" ? " (archived)" : ""}
              </option>
            ))}
          </select>
        </label>
      )}
      {current && (
        <span class="bs-muted lp-cohort-dates">
          {fmtShort(current.startsOn)}
          {current.endsOn ? ` – ${fmtShort(current.endsOn)}` : " onward"}
        </span>
      )}
      {canManage && current && (
        <RowMenu
          label={`${current.name} settings`}
          items={[
            { label: "Rename or change dates", onSelect: () => setDialog("edit") },
            current.status === "active"
              ? {
                  label: "Archive cohort",
                  onSelect: () => {
                    if (!confirm(`Archive ${current.name}? It becomes read-only: builder portals stop accepting updates and its builders drop out of weekly follow-ups. You can restore it later.`)) return;
                    void run(() => api("launchpadCohortUpdate", { params: { id: current.id }, body: { status: "archived" } }), `${current.name} archived.`);
                  },
                }
              : {
                  label: "Restore cohort",
                  onSelect: () => void run(() => api("launchpadCohortUpdate", { params: { id: current.id }, body: { status: "active" } }), `${current.name} restored.`),
                },
            {
              label: empty ? "Delete cohort" : "Delete cohort (only when empty)",
              danger: true,
              disabled: !empty,
              onSelect: () => {
                if (!confirm(`Delete ${current.name}? This can’t be undone.`)) return;
                void run(() => api("launchpadCohortDelete", { params: { id: current.id } }), `${current.name} deleted.`, null);
              },
            },
          ]}
        />
      )}
      {canManage && (
        <button type="button" class="btn" data-new-cohort onClick={() => setDialog("new")}>
          New cohort
        </button>
      )}
      <Drawer open={!!dialog} onClose={() => setDialog(null)} id="lp-cohort-dialog" kind="dialog" title={dialog === "edit" ? "Edit cohort" : "New cohort"} eyebrow="Buildspace + Launchpad">
        {dialog && (
          <CohortForm
            cohort={dialog === "edit" ? current ?? undefined : undefined}
            saved={(c, message) => {
              setDialog(null);
              changed(message, c.id);
            }}
          />
        )}
      </Drawer>
    </div>
  );
}

function CohortForm({ cohort, saved }: { cohort?: LaunchpadCohort; saved: (c: LaunchpadCohort, message: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  return (
    <form
      class="bs-form motion-list"
      data-cohort-form
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        const body = {
          name: String(f.get("name")),
          startsOn: String(f.get("startsOn")),
          endsOn: String(f.get("endsOn") || "") || null,
        };
        setBusy(true);
        setError("");
        setFields({});
        try {
          const res = cohort
            ? await api("launchpadCohortUpdate", { params: { id: cohort.id }, body })
            : await api("launchpadCohortCreate", { body });
          saved(res.cohort, cohort ? "Cohort saved." : `${res.cohort.name} created. Invite builders from the Invites tab.`);
        } catch (err) {
          setFields(fieldErrors(err));
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Cohort name
        <input class="input" name="name" required maxLength={100} placeholder="Winter 2027" defaultValue={cohort?.name} aria-invalid={fields.name ? true : undefined} />
      </label>
      <label>
        Starts on
        <input class="input" type="date" name="startsOn" required defaultValue={cohort?.startsOn ?? pacificToday()} />
      </label>
      <label>
        Ends on · optional
        <input class="input" type="date" name="endsOn" defaultValue={cohort?.endsOn ?? ""} />
      </label>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Saving…" : cohort ? "Save cohort" : "Create cohort"}
      </button>
    </form>
  );
}
