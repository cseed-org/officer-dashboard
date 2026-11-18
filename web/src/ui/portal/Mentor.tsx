import { safeUrl } from "../../lib/url";
/**
 * The mentor portal: cards for the projects this mentor is assigned to (with the signals that
 * need a look), a project detail with its builders, co-mentors and timeline, and "Add note".
 * The server scopes everything to assigned, non-archived projects (docs/launchpad-cohorts.md).
 */
import { LoadHistory, mergeActivity } from "../components/LoadHistory";
import { useState } from "preact/hooks";
import type { PortalMe, PortalMentorProject, PortalMentorResponse } from "../../lib/contracts";
import { api } from "../../lib/api/client";
import { useApi } from "../../lib/api/useApi";
import { setParams, useSearch } from "../../lib/nav";
import { plural } from "../../lib/format";
import { Drawer } from "../components/Drawer";
import { Async, Empty, errorMessage, Notice } from "../components/states";
import { Card, firstName, PortalTimeline, TrackChip, WeekChip } from "./shared";

export function MentorHome({ me }: { me: PortalMe }) {
  const state = useApi("portalMentor");
  return <Async state={state}>{(d) => <>
    <MentorDashboard d={d} me={me} reload={state.reload} />
    {d.activityNextCursor && <LoadHistory key={d.activityNextCursor} load={async (signal) => {
      const more = await api("portalMentor", {query:{activityCursor:d.activityNextCursor!},signal});
      state.set({...more,projects:more.projects.map((p) => ({...p,activity:mergeActivity(d.projects.find((old) => old.id===p.id)?.activity ?? [],p.activity)}))});
    }} />}
  </>}</Async>;
}

function MentorDashboard({ d, me, reload }: { d: PortalMentorResponse; me: PortalMe; reload: () => void }) {
  const qs = useSearch();
  const [notice, setNotice] = useState("");
  const open = d.projects.find((p) => p.id === qs.get("project"));
  const attention = d.projects.filter((p) => p.flags.awaiting || p.flags.needsHelp || p.flags.openRequests).length;
  return (
    <div class="portal-dash" data-mentor-dashboard>
      <header class="portal-hero">
        <span class="eyebrow">{d.mentor.org || "Mentor"}</span>
        <h1>Hi {firstName(d.mentor.name || me.account.name)}</h1>
        <p class="muted">
          {d.projects.length ? `You’re mentoring ${plural(d.projects.length, "project")}` : "No projects assigned yet"}
          {attention > 0 && ` · ${attention} could use a look`}
        </p>
      </header>
      {notice && <Notice tone="positive">{notice}</Notice>}
      {!d.projects.length ? (
        <Empty title="No projects yet">The officers will match you with a project soon. You’ll see its builders and weekly updates here.</Empty>
      ) : (
        <div class="portal-cards motion-list" data-mentor-projects>
          {d.projects.map((p) => (
            <ProjectCard key={p.id} p={p} onOpen={() => setParams({ project: p.id })} />
          ))}
        </div>
      )}
      <Drawer open={!!open} onClose={() => setParams({ project: null })} id="mentor-project" size="lg" title={open?.name ?? "Project"} eyebrow={open ? `${open.cohortName} · ${open.track}` : undefined}>
        {open && (
          <ProjectDetail
            p={open}
            today={d.schedule.today}
            schedule={d.schedule}
            added={(m) => {
              setNotice(m);
              reload();
            }}
          />
        )}
      </Drawer>
    </div>
  );
}

function ProjectCard({ p, onOpen }: { p: PortalMentorProject; onOpen: () => void }) {
  const flags = [
    p.flags.needsHelp && { label: "Asked for help", tone: "attention" },
    p.flags.awaiting > 0 && { label: `${p.flags.awaiting} awaiting check-in`, tone: "attention" },
    p.flags.openRequests > 0 && { label: plural(p.flags.openRequests, "open request"), tone: "info" },
  ].filter(Boolean) as { label: string; tone: string }[];
  const last = p.activity[0];
  return (
    <article class="block portal-project-card" data-mentor-project={p.id} data-attention={flags.length ? "" : undefined}>
      <div class="portal-block-head">
        <h2>
          <button type="button" class="bs-title" onClick={onOpen}>
            {p.name}
          </button>
        </h2>
        <span class="badge badge--neutral">{p.stage}</span>
      </div>
      <div class="bs-card-chips">
        <TrackChip track={p.track} />
        <span class="muted small">{p.cohortName}</span>
      </div>
      <p class="small flush">{p.builders.map((b) => b.name).join(", ") || <span class="muted">No builders yet</span>}</p>
      {flags.length > 0 ? (
        <ul class="portal-flags" role="list">
          {flags.map((f) => (
            <li key={f.label} class={`badge badge--${f.tone}`}>
              {f.label}
            </li>
          ))}
        </ul>
      ) : (
        <p class="small muted flush">All caught up this week.</p>
      )}
      {last && (
        <p class="small muted flush portal-clamp">
          Latest: {last.body}
        </p>
      )}
      <button type="button" class="btn btn--small" onClick={onOpen}>
        Open project
      </button>
    </article>
  );
}

function ProjectDetail({ p, today, schedule, added }: { p: PortalMentorProject; today: string; schedule: PortalMentorResponse["schedule"]; added: (m: string) => void }) {
  const names = Object.fromEntries(p.builders.map((b) => [b.id, b.name]));
  return (
    <div class="stack" data-mentor-detail>
      <section class="stack stack--tight">
        <p class="flush portal-pre">{p.description || <span class="muted">No description yet.</span>}</p>
        {p.links.length > 0 && (
          <ul class="portal-links" role="list">
            {p.links.filter((l) => safeUrl(l)).map((l) => (
              <li key={l}>
                <a href={safeUrl(l)!} target="_blank" rel="noreferrer noopener">
                  {l.replace(/^https?:\/\//, "")}
                </a>
              </li>
            ))}
          </ul>
        )}
      </section>
      <Card id="mentor-builders-h" title="Builders">
        {p.builders.length ? (
          <ul class="portal-people" role="list">
            {p.builders.map((b) => (
              <li key={b.id} data-builder={b.id}>
                <strong>{b.name}</strong>
                <span class="muted small">{[b.designation, b.pronouns].filter(Boolean).join(" · ")}</span>
                <WeekChip status={b.thisWeek} schedule={schedule} />
                {b.bio && <span class="small">{b.bio}</span>}
              </li>
            ))}
          </ul>
        ) : (
          <p class="muted flush">No builders on this project yet.</p>
        )}
        {p.coMentors.length > 0 && <p class="small muted flush">Also mentoring: {p.coMentors.map((m) => m.name).join(", ")}</p>}
      </Card>
      <Card id="mentor-note-h" title="Add a note">
        <NoteForm p={p} today={today} added={added} />
      </Card>
      <Card id="mentor-timeline-h" title="Timeline">
        <PortalTimeline items={p.activity} names={names} showVisibility empty="Builders’ weekly check-ins and your notes show up here." />
      </Card>
    </div>
  );
}

function NoteForm({ p, today, added }: { p: PortalMentorProject; today: string; added: (m: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [visibility, setVisibility] = useState<"team" | "mentors">("mentors");
  return (
    <form
      class="bs-form portal-entry"
      data-mentor-note
      onSubmit={async (e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const f = new FormData(form);
        setBusy(true);
        setError("");
        try {
          await api("portalMentorActivity", {
            body: {
              projectId: p.id,
              personId: String(f.get("personId") || "") || null,
              date: String(f.get("date")),
              body: String(f.get("body")),
              status: String(f.get("status")) as "on-track" | "needs-help",
              visibility,
            },
          });
          form.reset();
          added(visibility === "team" ? "Note shared with the team." : "Note saved for mentors and officers.");
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Note
        <textarea class="textarea" name="body" required rows={4} maxLength={10000} placeholder="What you covered, what they should try next, anything the officers should know." />
      </label>
      <div class="portal-entry-row">
        <label>
          About
          <select class="select" name="personId">
            <option value="">Whole project</option>
            {p.builders.map((b) => (
              <option key={b.id} value={b.id}>
                {b.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Date
          <input class="input" type="date" name="date" required defaultValue={today} />
        </label>
        <label>
          Status
          <select class="select" name="status">
            <option value="on-track">On track</option>
            <option value="needs-help">Needs help</option>
          </select>
        </label>
      </div>
      <fieldset>
        <legend>Who can see it</legend>
        <label class="bs-check">
          <input type="radio" name="visibility" checked={visibility === "mentors"} onChange={() => setVisibility("mentors")} />
          Mentors and officers only
        </label>
        <label class="bs-check">
          <input type="radio" name="visibility" checked={visibility === "team"} onChange={() => setVisibility("team")} />
          Share with the builders too
        </label>
      </fieldset>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Saving…" : "Add note"}
      </button>
    </form>
  );
}
