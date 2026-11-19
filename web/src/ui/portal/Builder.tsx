import { safeUrl } from "../../lib/url";
/**
 * The builder portal: a three-step onboarding (You → Project → Confirm) until the builder has
 * a record, then their own dashboard: this week's check-in, their project, team, mentors,
 * requests/absences and the team timeline (docs/launchpad-cohorts.md).
 */
import { LoadHistory, mergeActivity } from "../components/LoadHistory";
import { useState } from "preact/hooks";
import { BUILDSPACE_STAGES, BUILDSPACE_TRACKS } from "../../core/constants";
import type { PortalBuilderResponse, PortalMe } from "../../lib/contracts";
import { api, fieldErrors } from "../../lib/api/client";
import { useApi } from "../../lib/api/useApi";
import { fmtDate } from "../../lib/format";
import { Drawer } from "../components/Drawer";
import { Async, Empty, errorMessage, Notice } from "../components/states";
import { Card, firstName, PortalTimeline, TrackChip, WeekChip } from "./shared";

type Track = (typeof BUILDSPACE_TRACKS)[number];
type Stage = (typeof BUILDSPACE_STAGES)[number];

const linksFrom = (s: string) =>
  s
    .split("\n")
    .map((x) => x.trim())
    .filter(Boolean);
const badLink = (links: string[]) => links.find((l) => !/^https?:\/\/[^\s]+\.[^\s]+/i.test(l));

export function BuilderHome({ me, refreshMe }: { me: PortalMe; refreshMe: () => void }) {
  const state = useApi("portalBuilder");
  return (
    <Async state={state}>
      {(d) => <>
        {!d.onboarded ? (
          d.readOnly ? (
            <Empty title={`${d.cohort.name} has ended`}>This cohort is archived, so setup is closed. Ask an officer if you still need to join.</Empty>
          ) : (
            <Onboarding
              d={d}
              me={me}
              done={() => {
                state.reload();
                refreshMe();
              }}
            />
          )
        ) : (
          <Dashboard d={d} reload={state.reload} />
        )}
        {d.activityNextCursor && <LoadHistory key={d.activityNextCursor} load={async (signal) => {
          const more = await api("portalBuilder", {query:{activityCursor:d.activityNextCursor!},signal});
          state.set({...more,activity:mergeActivity(d.activity,more.activity)});
        }} />}
      </>}
    </Async>
  );
}

// ───────────────────────────── onboarding ─────────────────────────────

type Draft = {
  name: string;
  pronouns: string;
  designation: string;
  bio: string;
  mode: "new" | "join";
  projectId: string;
  projectName: string;
  description: string;
  track: Track;
  links: string;
};

const STEPS = ["You", "Project", "Confirm"] as const;

function Onboarding({ d, me, done }: { d: PortalBuilderResponse; me: PortalMe; done: () => void }) {
  const [step, setStep] = useState(0);
  const [draft, setDraft] = useState<Draft>({
    name: me.account.name,
    pronouns: "",
    designation: "",
    bio: "",
    mode: d.joinable.length ? "join" : "new",
    projectId: "",
    projectName: "",
    description: "",
    track: "Software",
    links: "",
  });
  const [error, setError] = useState("");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<Draft>) => setDraft((x) => ({ ...x, ...patch }));
  const joined = d.joinable.find((p) => p.id === draft.projectId);
  const validate = (s: number): string => {
    if (s === 0 && !draft.name.trim()) return "Add your name.";
    if (s === 1) {
      if (draft.mode === "join" && !draft.projectId) return "Choose the project you’re joining, or start a new one.";
      if (draft.mode === "new" && !draft.projectName.trim()) return "Give your project a name. You can change it later.";
      if (draft.mode === "new" && badLink(linksFrom(draft.links))) return `“${badLink(linksFrom(draft.links))}” isn’t a full web link (start with https://).`;
    }
    return "";
  };
  const next = () => {
    const e = validate(step);
    setError(e);
    if (!e) setStep((s) => s + 1);
  };
  const submit = async () => {
    setBusy(true);
    setError("");
    setFields({});
    try {
      const profile = { name: draft.name.trim(), pronouns: draft.pronouns.trim(), designation: draft.designation.trim(), bio: draft.bio.trim() };
      await api("portalBuilderOnboarding", {
        body: {
          ...profile,
          project:
            draft.mode === "join"
              ? { mode: "join", projectId: draft.projectId }
              : { mode: "new", name: draft.projectName.trim(), description: draft.description, track: draft.track, links: linksFrom(draft.links) },
        },
      });
      done();
    } catch (e) {
      setFields(fieldErrors(e));
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="portal-onboarding" data-onboarding data-step={STEPS[step]}>
      <header class="portal-hero">
        <span class="eyebrow">{d.cohort.name}</span>
        <h1>Welcome to Launchpad{draft.name.trim() ? `, ${firstName(draft.name)}` : ""}</h1>
        <p class="muted">Set up your builder profile and project. Officers and your mentors see this; you can change it any time.</p>
      </header>
      <ol class="portal-steps" aria-label="Setup steps">
        {STEPS.map((s, i) => (
          <li key={s} aria-current={i === step ? "step" : undefined} data-done={i < step ? "" : undefined}>
            <span class="portal-step-n">{i + 1}</span>
            {s}
          </li>
        ))}
      </ol>
      <form
        class="block portal-form bs-form tab-panel"
        key={step}
        onSubmit={(e) => {
          e.preventDefault();
          if (step < 2) next();
          else void submit();
        }}
      >
        {step === 0 && (
          <>
            <h2 class="flush">About you</h2>
            <label>
              Your name
              <input class="input" required maxLength={200} value={draft.name} onInput={(e) => set({ name: e.currentTarget.value })} aria-invalid={fields.name ? true : undefined} autoComplete="name" />
            </label>
            <label>
              Pronouns · optional
              <input class="input" maxLength={40} placeholder="she/her, he/him, they/them…" value={draft.pronouns} onInput={(e) => set({ pronouns: e.currentTarget.value })} />
            </label>
            <label>
              Your role on the team · optional
              <input class="input" maxLength={80} placeholder="Hardware lead, Designer, Founder…" value={draft.designation} onInput={(e) => set({ designation: e.currentTarget.value })} />
            </label>
            <label>
              A line about you · optional
              <textarea class="textarea" rows={3} maxLength={500} placeholder="What you’re studying, what you’re good at, what you want to learn." value={draft.bio} onInput={(e) => set({ bio: e.currentTarget.value })} />
              <span class="field-hint">{500 - draft.bio.length} characters left</span>
            </label>
          </>
        )}
        {step === 1 && (
          <>
            <h2 class="flush">Your project</h2>
            <div class="choice-row" role="radiogroup" aria-label="Project">
              <label class="choice">
                <input type="radio" name="mode" checked={draft.mode === "join"} disabled={!d.joinable.length} onChange={() => set({ mode: "join" })} />
                <span>
                  <span class="choice-title">Join a project</span>
                  <span class="choice-sub">{d.joinable.length ? "A teammate already set it up" : "No projects in this cohort yet"}</span>
                </span>
              </label>
              <label class="choice">
                <input type="radio" name="mode" checked={draft.mode === "new"} onChange={() => set({ mode: "new" })} />
                <span>
                  <span class="choice-title">Start a new project</span>
                  <span class="choice-sub">Your teammates can join it next</span>
                </span>
              </label>
            </div>
            {draft.mode === "join" ? (
              <fieldset class="portal-joinable">
                <legend>Which project?</legend>
                {d.joinable.map((p) => (
                  <label class="choice" key={p.id} data-joinable={p.id}>
                    <input type="radio" name="projectId" checked={draft.projectId === p.id} onChange={() => set({ projectId: p.id })} />
                    <span>
                      <span class="choice-title">
                        {p.name} <TrackChip track={p.track} />
                      </span>
                      <span class="choice-sub">{p.builders.length ? `With ${p.builders.join(", ")}` : "No builders yet"}</span>
                      {p.description && <span class="choice-sub portal-clamp">{p.description}</span>}
                    </span>
                  </label>
                ))}
              </fieldset>
            ) : (
              <>
                <label>
                  Project name
                  <input class="input" maxLength={200} value={draft.projectName} onInput={(e) => set({ projectName: e.currentTarget.value })} aria-invalid={fields["project.name"] ? true : undefined} />
                </label>
                <label>
                  Track
                  <select class="select" value={draft.track} onChange={(e) => set({ track: e.currentTarget.value as Track })}>
                    {BUILDSPACE_TRACKS.map((t) => (
                      <option key={t}>{t}</option>
                    ))}
                  </select>
                </label>
                <label>
                  What are you building? · optional
                  <textarea class="textarea" rows={4} maxLength={10000} value={draft.description} onInput={(e) => set({ description: e.currentTarget.value })} />
                </label>
                <label>
                  Links · optional, one per line
                  <textarea class="textarea" rows={2} placeholder="https://github.com/your-team/project" value={draft.links} onInput={(e) => set({ links: e.currentTarget.value })} />
                </label>
              </>
            )}
          </>
        )}
        {step === 2 && (
          <>
            <h2 class="flush">Check and finish</h2>
            <dl class="portal-facts">
              <div>
                <dt>Name</dt>
                <dd>
                  {draft.name}
                  {draft.pronouns && <span class="muted"> ({draft.pronouns})</span>}
                </dd>
              </div>
              {draft.designation && (
                <div>
                  <dt>Role</dt>
                  <dd>{draft.designation}</dd>
                </div>
              )}
              <div>
                <dt>Project</dt>
                <dd>{draft.mode === "join" ? `Joining ${joined?.name ?? ""}` : `Starting ${draft.projectName} (${draft.track})`}</dd>
              </div>
              <div>
                <dt>Email</dt>
                <dd>{d.email}</dd>
              </div>
            </dl>
            <p class="small muted flush">
              Your Discord account is linked, so weekly check-ins you post in Discord count too. If your email matches a CSEED membership record, it is linked automatically.
            </p>
          </>
        )}
        {error && <Notice tone="negative">{error}</Notice>}
        <div class="action-row portal-form-foot">
          {step > 0 && (
            <button type="button" class="btn btn--text" onClick={() => { setError(""); setStep((s) => s - 1); }}>
              Back
            </button>
          )}
          <button class="btn btn--primary" disabled={busy} data-onboarding-next>
            {step < 2 ? "Continue" : busy ? "Setting up…" : "Finish setup"}
          </button>
        </div>
      </form>
    </div>
  );
}

// ───────────────────────────── dashboard ─────────────────────────────

function Dashboard({ d, reload }: { d: PortalBuilderResponse; reload: () => void }) {
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<null | "profile" | "project">(null);
  const me = d.me!;
  const p = d.project;
  const names: Record<string, string> = Object.fromEntries([me, ...d.teammates].map((x) => [x.id, x.name]));
  const saved = (m: string) => {
    setEditing(null);
    setNotice(m);
    reload();
  };
  return (
    <div class="portal-dash" data-builder-dashboard>
      <header class="portal-hero">
        <span class="eyebrow">
          {d.cohort.name}
          {p && ` · ${p.name}`}
        </span>
        <h1>Hi {firstName(me.name)}</h1>
        <p class="muted">
          {[me.designation, me.pronouns].filter(Boolean).join(" · ") || "Builder"} · joined {fmtDate(me.joinedOn)}
        </p>
        {!d.readOnly && (
          <div class="action-row">
            <button type="button" class="btn btn--small" data-edit-profile onClick={() => setEditing("profile")}>
              Edit profile
            </button>
          </div>
        )}
      </header>
      {d.readOnly && <Notice tone="attention">{d.cohort.name} has ended and is archived. Everything here is read-only.</Notice>}
      {notice && <Notice tone="positive">{notice}</Notice>}
      <div class="portal-grid">
        <div class="portal-col">
          <Card id="week-h" data="week" title="This week" actions={!d.readOnly && <WeekChip status={d.thisWeek} schedule={d.schedule} />}>
            {d.readOnly ? <p class="flush">Check-ins have ended for this archived cohort.</p> : d.thisWeek === "done" ? (
              <p class="flush">Your check-in is in. Nice. You can add another update any time.</p>
            ) : d.thisWeek === "not-joined" ? (
              <p class="flush">Your first check-in week hasn’t started yet.</p>
            ) : (
              <p class="flush">What did you get done, and what’s next? A few sentences is plenty. Due {fmtDate(d.schedule.week.due)}.</p>
            )}
            {!d.readOnly && <EntryForm kind="weekly" today={d.schedule.today} done={(m) => { setNotice(m); reload(); }} />}
          </Card>
          <Card id="timeline-h" data="timeline" title="Team timeline">
            <PortalTimeline items={d.activity} names={names} empty="Weekly check-ins from you and your teammates show up here." />
          </Card>
        </div>
        <div class="portal-col">
          {p && (
            <Card
              id="project-h"
              data="project"
              title={p.name}
              actions={!d.readOnly && <button type="button" class="btn btn--small" data-edit-project onClick={() => setEditing("project")}>Edit</button>}
            >
              <div class="bs-card-chips">
                <TrackChip track={p.track} />
                <span class="badge badge--neutral">{p.stage}</span>
              </div>
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
            </Card>
          )}
          <Card id="team-h" data="team" title="Team">
            <ul class="portal-people" role="list">
              {[me, ...d.teammates].map((x) => (
                <li key={x.id}>
                  <strong>
                    {x.name}
                    {x.id === me.id && <span class="muted"> (you)</span>}
                  </strong>
                  <span class="muted small">{[x.designation, x.pronouns].filter(Boolean).join(" · ")}</span>
                  {x.bio && <span class="small">{x.bio}</span>}
                </li>
              ))}
            </ul>
          </Card>
          <Card id="mentors-h" data="mentors" title="Mentors">
            {d.mentors.length ? (
              <ul class="portal-people" role="list">
                {d.mentors.map((m) => (
                  <li key={m.id}>
                    <strong>{m.name}</strong>
                    <span class="muted small">{[m.org, m.topics.join(", ")].filter(Boolean).join(" · ")}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p class="muted flush">No mentor yet. The officers are matching mentors to projects.</p>
            )}
          </Card>
          {!d.readOnly && (
            <Card id="ask-h" data="ask" title="Need something or away?">
              <p class="small muted flush">Ask the officers for help (parts, access, intros) or let them know you’ll miss a session. Your team and mentors see these too.</p>
              <EntryForm kind="request" today={d.schedule.today} done={(m) => { setNotice(m); reload(); }} />
            </Card>
          )}
        </div>
      </div>
      <Drawer open={!!editing} onClose={() => setEditing(null)} id="portal-edit" title={editing === "project" ? "Edit project" : "Edit profile"} eyebrow="Launchpad">
        {editing === "profile" && <ProfileForm d={d} saved={saved} />}
        {editing === "project" && p && <ProjectForm p={p} saved={saved} />}
      </Drawer>
    </div>
  );
}

function EntryForm({ kind: initial, today, done }: { kind: "weekly" | "request"; today: string; done: (message: string) => void }) {
  const [kind, setKind] = useState<"weekly" | "request" | "absence">(initial);
  const [status, setStatus] = useState<"on-track" | "needs-help" | "open">(initial === "weekly" ? "on-track" : "open");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <form
      class="bs-form portal-entry"
      data-entry-form={initial}
      onSubmit={async (e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const f = new FormData(form);
        setBusy(true);
        setError("");
        try {
          await api("portalBuilderActivity", { body: { kind, date: String(f.get("date")), body: String(f.get("body")), status: kind === "weekly" ? status : kind === "absence" ? "on-track" : "open" } });
          form.reset();
          done(kind === "weekly" ? "Check-in posted. Your team and mentors can see it." : kind === "absence" ? "Absence noted. Thanks for the heads-up." : "Request sent. Officers, your team and your mentors can see it.");
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      {initial === "request" && (
        <div class="segmented" aria-label="Kind">
          {(
            [
              ["request", "Ask for help"],
              ["absence", "I’ll be away"],
            ] as const
          ).map(([k, l]) => (
            <button type="button" key={k} class="segment" aria-pressed={kind === k} onClick={() => setKind(k)}>
              {l}
            </button>
          ))}
        </div>
      )}
      <label>
        {kind === "weekly" ? "Your update" : kind === "absence" ? "When and why (optional detail)" : "What do you need?"}
        <textarea class="textarea" name="body" required maxLength={10000} rows={kind === "weekly" ? 4 : 3} />
      </label>
      <div class="portal-entry-row">
        <label>
          Date
          <input class="input" type="date" name="date" required defaultValue={today} />
        </label>
        {kind === "weekly" && (
          <label>
            How’s it going?
            <select class="select" value={status} onChange={(e) => setStatus(e.currentTarget.value as typeof status)}>
              <option value="on-track">On track</option>
              <option value="needs-help">Need help</option>
            </select>
          </label>
        )}
      </div>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Posting…" : kind === "weekly" ? "Post check-in" : kind === "absence" ? "Let them know" : "Send request"}
      </button>
    </form>
  );
}

function ProfileForm({ d, saved }: { d: PortalBuilderResponse; saved: (m: string) => void }) {
  const me = d.me!;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <form
      class="bs-form motion-list"
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        setBusy(true);
        setError("");
        try {
          await api("portalBuilderProfile", {
            body: { name: String(f.get("name")), pronouns: String(f.get("pronouns")), designation: String(f.get("designation")), bio: String(f.get("bio")) },
          });
          saved("Profile saved.");
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Your name
        <input class="input" name="name" required maxLength={200} defaultValue={me.name} />
      </label>
      <label>
        Pronouns · optional
        <input class="input" name="pronouns" maxLength={40} defaultValue={me.pronouns} />
      </label>
      <label>
        Your role on the team · optional
        <input class="input" name="designation" maxLength={80} defaultValue={me.designation} />
      </label>
      <label>
        A line about you · optional
        <textarea class="textarea" name="bio" rows={3} maxLength={500} defaultValue={me.bio} />
      </label>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Saving…" : "Save profile"}
      </button>
    </form>
  );
}

function ProjectForm({ p, saved }: { p: NonNullable<PortalBuilderResponse["project"]>; saved: (m: string) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return (
    <form
      class="bs-form motion-list"
      onSubmit={async (e) => {
        e.preventDefault();
        const f = new FormData(e.currentTarget);
        const links = linksFrom(String(f.get("links")));
        const bad = badLink(links);
        if (bad) {
          setError(`“${bad}” isn’t a full web link (start with https://).`);
          return;
        }
        setBusy(true);
        setError("");
        try {
          await api("portalBuilderProject", {
            body: {
              name: String(f.get("name")),
              description: String(f.get("description")),
              track: String(f.get("track")) as Track,
              stage: String(f.get("stage")) as Stage,
              links,
            },
          });
          saved("Project saved. Your team and mentors see the change.");
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <label>
        Project name
        <input class="input" name="name" required maxLength={200} defaultValue={p.name} />
      </label>
      <label>
        What are you building?
        <textarea class="textarea" name="description" rows={5} maxLength={10000} defaultValue={p.description} />
      </label>
      <label>
        Track
        <select class="select" name="track" defaultValue={p.track}>
          {BUILDSPACE_TRACKS.map((t) => (
            <option key={t} selected={t === p.track}>
              {t}
            </option>
          ))}
        </select>
      </label>
      <label>
        Stage
        <select class="select" name="stage" defaultValue={p.stage}>
          {BUILDSPACE_STAGES.map((t) => (
            <option key={t} selected={t === p.stage}>
              {t}
            </option>
          ))}
        </select>
      </label>
      <label>
        Links · one per line
        <textarea class="textarea" name="links" rows={3} defaultValue={p.links.join("\n")} />
      </label>
      <p class="small muted flush">Everyone on the project can edit these details. Mentors are matched by the officers.</p>
      {error && <Notice tone="negative">{error}</Notice>}
      <button class="btn btn--primary" disabled={busy}>
        {busy ? "Saving…" : "Save project"}
      </button>
    </form>
  );
}
