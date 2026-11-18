/**
 * The mentor roster (Mentors tab, table layout) and the Auto-assign mentors dialog
 * (docs/launchpad-cohorts.md). Availability and caps are officer settings; portal invites for
 * mentors are director-only. Auto-assign previews a random, even spread and applies exactly
 * the pairs shown, all or none.
 */
import { useEffect, useState } from "preact/hooks";
import type { BuildspaceData, BuildspaceProject, LaunchpadMentor } from "../../../lib/contracts";
import { api, ApiCallError, type ResponseOf } from "../../../lib/api/client";
import { Drawer } from "../../components/Drawer";
import { PeopleChip } from "../../components/PeopleTable";
import { Empty, errorMessage, Loading, Notice } from "../../components/states";
import { pushEntry } from "./history";
import { ACCOUNT_HUE, accountLabel, copyText } from "./launchpad";
import { trackShort } from "./model";

type Preview = ResponseOf<"launchpadAutoAssignPreview">;

export function MentorRoster({
  b,
  mentors,
  canDirector,
  readOnly,
  reload,
  setNotice,
}: {
  b: BuildspaceData;
  mentors: LaunchpadMentor[];
  canDirector: boolean;
  readOnly: boolean;
  reload: () => void;
  setNotice: (s: string) => void;
}) {
  const [error, setError] = useState("");
  const [inviting, setInviting] = useState<LaunchpadMentor | null>(null);
  const save = async (m: LaunchpadMentor, patch: Partial<{ available: boolean; maxProjects: number | null }>) => {
    setError("");
    try {
      await api("launchpadMentorSettings", {
        params: { contactId: m.id },
        body: { available: patch.available ?? m.available, maxProjects: patch.maxProjects !== undefined ? patch.maxProjects : m.maxProjects },
      });
      reload();
      setNotice(`${m.name} updated.`);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const resend = async (m: LaunchpadMentor) => {
    if (!m.account) return;
    setError("");
    try {
      const res = await api("launchpadAccountResend", { params: { id: m.account.id } });
      reload();
      if (res.link) {
        const ok = await copyText(res.link.url);
        setNotice(`New portal invite link for ${m.name}${ok ? " copied to your clipboard" : `: ${res.link.url}`}.`);
      } else setNotice(`A new portal invite is on its way to ${m.name}.`);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="table-wrap bs-wrap" tabIndex={0} role="region" aria-label="Mentor roster">
        <table class="bs-table" data-mentor-roster>
          <thead>
            <tr>
              {["Mentor", "Focus", "Portal", "Available", "Cap", "Load", "Projects this cohort"].map((h) => (
                <th scope="col">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody class="motion-list">
            {mentors.map((m) => {
              const ps = b.projects.filter((p) => p.mentorIds.includes(m.id) && !p.archivedAt);
              const full = m.maxProjects !== null && m.load >= m.maxProjects;
              return (
                <tr key={m.id} data-flip={m.id} data-mentor={m.id} class={!m.available ? "lp-muted-row" : undefined}>
                  <td data-label="Mentor">
                    <strong>{m.name}</strong>
                    <span class="bs-sub">{[m.org, m.email].filter(Boolean).join(" · ")}</span>
                  </td>
                  <td data-label="Focus">{m.topics.join(" · ") || <span class="bs-muted">—</span>}</td>
                  <td data-label="Portal">
                    {m.account ? (
                      <PeopleChip hue={m.account.status === "invited" && m.account.inviteExpired ? "coral" : ACCOUNT_HUE[m.account.status]}>{accountLabel(m.account)}</PeopleChip>
                    ) : (
                      <span class="bs-muted">No account</span>
                    )}
                    {canDirector && !readOnly && (!m.account || m.account.status === "invited") && (
                      <button
                        type="button"
                        class="linkish lp-copy"
                        data-mentor-invite
                        onClick={() => (m.account ? void resend(m) : setInviting(m))}
                      >
                        {m.account ? "Resend" : "Invite"}
                      </button>
                    )}
                  </td>
                  <td data-label="Available">
                    <label class="lp-switch">
                      <input
                        type="checkbox"
                        role="switch"
                        checked={m.available}
                        disabled={readOnly}
                        aria-label={`${m.name} available for new projects`}
                        onChange={(e) => void save(m, { available: e.currentTarget.checked })}
                      />
                      <span>{m.available ? "Yes" : "No"}</span>
                    </label>
                  </td>
                  <td data-label="Cap">
                    <input
                      class="input lp-cap"
                      type="number"
                      min={1}
                      max={50}
                      inputMode="numeric"
                      placeholder="None"
                      disabled={readOnly}
                      aria-label={`${m.name} maximum projects`}
                      defaultValue={m.maxProjects === null ? "" : String(m.maxProjects)}
                      key={`${m.id}-${m.maxProjects}`}
                      onChange={(e) => {
                        const v = e.currentTarget.value.trim();
                        const n = v === "" ? null : Math.round(Number(v));
                        if (n !== null && (!Number.isFinite(n) || n < 1 || n > 50)) {
                          setError("A cap is a whole number from 1 to 50, or blank for none.");
                          return;
                        }
                        if (n !== m.maxProjects) void save(m, { maxProjects: n });
                      }}
                    />
                  </td>
                  <td data-label="Load" class={full ? "bs-alert" : undefined}>
                    {m.load}
                    {m.maxProjects !== null && <span class="bs-muted"> / {m.maxProjects}</span>}
                    {full && <span class="bs-sub">At cap</span>}
                  </td>
                  <td data-label="Projects this cohort">
                    {ps.length ? (
                      ps.map((p) => (
                        <button class="bs-partner" key={p.id} onClick={() => pushEntry({ project: p.id })}>
                          {p.name}
                        </button>
                      ))
                    ) : (
                      <span class="bs-muted">None</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <Drawer open={!!inviting} onClose={() => setInviting(null)} id="lp-mentor-invite" kind="dialog" title={inviting ? `Invite ${inviting.name} to the mentor portal` : "Invite mentor"} eyebrow="Buildspace + Launchpad">
        {inviting && (
          <MentorInviteForm
            m={inviting}
            done={(message) => {
              setInviting(null);
              reload();
              setNotice(message);
            }}
          />
        )}
      </Drawer>
    </>
  );
}

function MentorInviteForm({ m, done }: { m: LaunchpadMentor; done: (message: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [method, setMethod] = useState<"email" | "link" | "both">("email");
  const [link, setLink] = useState<string | null>(null);
  if (link)
    return (
      <div class="stack" data-mentor-invite-result>
        <Notice tone="positive">Invite ready. Share this link with {m.name}; it works once, for 14 days.</Notice>
        {error && <Notice tone="negative">{error}</Notice>}
        <code class="lp-link">{link}</code>
        <div class="action-row">
          <button type="button" class="btn btn--primary" onClick={() => void copyText(link).then((ok) => ok ? done(`Portal invite link for ${m.name} copied.`) : setError("Couldn’t copy the link. Select and copy it below."))}>
            Copy link and close
          </button>
        </div>
      </div>
    );
  return (
    <form
      class="bs-form"
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        const email = String(f.get("email") || "").trim();
        setBusy(true);
        setError("");
        try {
          const res = await api("launchpadMentorInvite", { params: { contactId: m.id }, body: { method, ...(email ? { email } : {}) } });
          if (res.link) setLink(res.link.url);
          else done(`Portal invite emailed to ${res.account.email}.`);
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <p class="flush">
        Mentors see only the projects they are assigned to: the builders, their team updates and notes shared with mentors. Never Finance, member records or officer-only notes.
      </p>
      <label>
        Email
        <input class="input" type="email" name="email" required={!m.email} defaultValue={m.email} maxLength={254} />
      </label>
      <fieldset>
        <legend>How to send</legend>
        {(
          [
            ["email", "Email the link"],
            ["link", "Just give me the link"],
            ["both", "Both"],
          ] as const
        ).map(([v, l]) => (
          <label class="bs-check" key={v}>
            <input type="radio" name="method" checked={method === v} onChange={() => setMethod(v)} />
            {l}
          </label>
        ))}
      </fieldset>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Sending…" : "Send invite"}
      </button>
    </form>
  );
}

/** "Auto-assign mentors": preview → shuffle → apply exactly the shown pairs. */
export function AutoAssignDialog({
  b,
  open,
  onClose,
  applied,
}: {
  b: BuildspaceData;
  open: boolean;
  onClose: () => void;
  applied: (message: string) => void;
}) {
  const [preview, setPreview] = useState<Preview | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [stale, setStale] = useState("");
  const [busy, setBusy] = useState(false);
  const load = async () => {
    if (!b.cohortId) return;
    setLoading(true);
    setError("");
    try {
      setPreview(await api("launchpadAutoAssignPreview", { params: { id: b.cohortId } }));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => {
    if (open) {
      setStale("");
      void load();
    } else setPreview(null);
  }, [open]);
  const project = (id: string): BuildspaceProject | undefined => b.projects.find((p) => p.id === id);
  const mentor = (id: string) => b.mentors.find((m) => m.id === id);
  const stats = (id: string) => preview?.mentors.find((m) => m.id === id);
  return (
    <Drawer open={open} onClose={onClose} id="lp-auto-assign" kind="dialog" size="lg" title="Auto-assign mentors" eyebrow={b.cohorts.find((c) => c.id === b.cohortId)?.name ?? "Buildspace + Launchpad"}>
      <div class="stack" data-auto-assign>
        <p class="flush">
          Every active project in this cohort without a mentor gets one available mentor, spreading projects evenly: the mentor with the fewest projects goes first, a mentor whose focus matches the track wins a tie, and caps are respected. Nothing changes until you apply.
        </p>
        {stale && <Notice tone="attention">{stale}</Notice>}
        {error && <Notice tone="negative">{error}</Notice>}
        {loading && !preview ? (
          <Loading rows={3} label="Planning assignments" />
        ) : preview && !preview.assignments.length ? (
          <Empty title={preview.unassigned.length ? "No mentor has room" : "Every project has a mentor"}>
            {preview.unassigned.length
              ? "Mark more mentors available or raise their caps in the Mentors tab, then try again."
              : "Projects that are archived or Shipped are left alone."}
          </Empty>
        ) : (
          preview && (
            <>
              <div class="table-wrap bs-wrap" tabIndex={0} role="region" aria-label="Proposed assignments">
                <table class="bs-table" data-auto-assign-table>
                  <thead>
                    <tr>
                      {["Project", "Track", "Mentor", "Their projects"].map((h) => (
                        <th scope="col">{h}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody class="motion-list">
                    {preview.assignments.map((a) => {
                      const p = project(a.projectId),
                        m = mentor(a.mentorId),
                        s = stats(a.mentorId);
                      return (
                        <tr key={a.projectId} data-assignment={a.projectId}>
                          <td data-label="Project">
                            <strong>{p?.name ?? a.projectId}</strong>
                          </td>
                          <td data-label="Track">{p && <span class="bs-track" data-track={p.track}>{trackShort(p.track)}</span>}</td>
                          <td data-label="Mentor">
                            {m?.name ?? a.mentorId}
                            {s && !s.hasAccount && <span class="bs-sub bs-alert">No portal account yet</span>}
                          </td>
                          <td data-label="Their projects">{s ? `${s.loadBefore} → ${s.loadAfter}` : ""}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              {preview.unassigned.length > 0 && (
                <Notice tone="attention">
                  No mentor had room for {preview.unassigned.map((id) => project(id)?.name ?? id).join(", ")}. Raise a cap or mark another mentor available.
                </Notice>
              )}
            </>
          )
        )}
        <div class="action-row">
          <button
            type="button"
            class="btn btn--primary"
            data-auto-assign-apply
            disabled={busy || loading || !preview?.assignments.length}
            onClick={async () => {
              if (!preview || !b.cohortId) return;
              setBusy(true);
              setError("");
              try {
                const res = await api("launchpadAutoAssign", { params: { id: b.cohortId }, body: { assignments: preview.assignments } });
                applied(`${res.assigned} ${res.assigned === 1 ? "project" : "projects"} assigned a mentor.`);
                onClose();
              } catch (e) {
                if (e instanceof ApiCallError && e.status === 409) {
                  setStale("Someone changed mentors or projects since this plan was made. Here is a fresh plan; check it and apply again.");
                  void load();
                } else setError(errorMessage(e));
              } finally {
                setBusy(false);
              }
            }}
          >
            {busy ? "Assigning…" : `Apply ${preview?.assignments.length ?? ""} ${preview?.assignments.length === 1 ? "assignment" : "assignments"}`}
          </button>
          <button type="button" class="btn" data-auto-assign-shuffle disabled={loading || busy} onClick={() => void load()}>
            {loading && preview ? "Shuffling…" : "Shuffle"}
          </button>
          <button type="button" class="btn btn--text" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    </Drawer>
  );
}
