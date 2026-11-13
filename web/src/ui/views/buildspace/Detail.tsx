import { DueDate } from "../../components/DueDate";
import type { PurchaseStatus } from "../../../lib/domain";
import { useState } from "preact/hooks";
import type {
  BuildspaceActivity,
  BuildspaceData,
  BuildspacePerson,
  BuildspaceProject,
} from "../../../lib/contracts";
import { api } from "../../../lib/api/client";
import { setParams } from "../../../lib/nav";
import { href } from "../../../lib/routes";
import { fmtShort } from "../../../lib/format";
import { Badge } from "../../components/kit";
import { Empty, errorMessage, Notice } from "../../components/states";
import {
  kindLabel,
  missed,
  openActivity,
  projectMentors,
  projectPeople,
} from "./model";
export function BuildspaceDetail({
  b,
  person,
  project,
  reload,
  onPerson,
  section,
}: {
  b: BuildspaceData;
  person?: BuildspacePerson;
  project: BuildspaceProject;
  reload: () => void;
  onPerson: (id: string) => void;
  section?: string;
}) {
  const [localTab, setTab] = useState("overview"),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [message, setMessage] = useState("");
  const tab = section ?? localTab;
  const activities = b.activity.filter(
    (a) =>
      a.projectId === project.id &&
      (!person || !a.personId || a.personId === person.id),
  );
  const mentors = projectMentors(project, b),
    people = projectPeople(project, b);
  const timeline = activities.filter((a) =>
    ["weekly", "mentor"].includes(a.kind),
  );
  const items =
    tab === "timeline"
      ? timeline
      : tab === "requests"
        ? activities.filter((a) => a.kind === "request")
        : tab === "purchases"
          ? activities.filter((a) => a.kind === "purchase")
          : activities.filter((a) => a.kind === "absence");
  const [kind, setKind] = useState<BuildspaceActivity["kind"]>("weekly");
  const [builderId, setBuilderId] = useState(person?.id || "");
  const [entryStatus, setEntryStatus] =
    useState<BuildspaceActivity["status"]>("on-track");
  const resolve = async (a: BuildspaceActivity) => {
    setBusy(true);
    setError("");
    try {
      await api("buildspaceActivityResolve", { params: { id: a.id } });
      reload();
      setMessage("Request marked resolved.");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="bs-detail">
      {!section && <div class="bs-detail-meta">
        <span class="bs-track" data-track={project.track}>
          {project.track}
        </span>
        <Badge tone={project.stage === "At risk" ? "attention" : "neutral"}>
          {project.stage}
        </Badge>
        <button
          class="btn"
          onClick={() => setParams({ edit: person ? "person" : "project" })}
        >
          Edit {person ? "builder" : "project"}
        </button>
      </div>}
      {person && !section && (
        <p>
          Building{" "}
          <button
            class="bs-title"
            onClick={() => setParams({ person: null, project: project.id })}
          >
            {project.name}
          </button>{" "}
          · Joined {person.joinedOn}
          {person.memberId && (
            <>
              {" "}
              ·{" "}
              <a href={`/members?id=${encodeURIComponent(person.memberId)}`}>
                Member record
              </a>
            </>
          )}
        </p>
      )}
      {person && !section && (
        <Notice tone={missed(person, b) ? "attention" : "positive"}>
          {missed(person, b)
            ? b.schedule.pastDue
              ? `This week’s check-in is past due (due ${fmtShort(b.weekDue)}).`
              : `Awaiting a weekly check-in this week (due ${fmtShort(b.weekDue)}).`
            : "Weekly check-in received this week."}
        </Notice>
      )}
      {!mentors.length && (
        <Notice tone="attention">
          No mentor assigned. Edit this project to assign one or two mentors.
        </Notice>
      )}
      {!section && <div class="segmented bs-detail-tabs" aria-label="Record sections">
        {[
          "overview",
          "timeline",
          "requests",
          "purchases",
          "absences",
          "add entry",
        ].map((t) => (
          <button
            class="segment"
            aria-pressed={tab === t}
            onClick={() => setTab(t)}
          >
            {t[0]!.toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>}
      {error && <Notice tone="negative">{error}</Notice>}
      {message && <Notice tone="positive">{message}</Notice>}
      <div class="bs-detail-panel tab-panel" key={tab}>
        {tab === "overview" ? (
          <>
            <section>
              <h3>The project</h3>
              <p>{project.description || "No description yet."}</p>
              {project.links.map((l) => (
                <p>
                  <a href={l} target="_blank" rel="noreferrer">
                    {l}
                  </a>
                </p>
              ))}
            </section>
            <section>
              <h3>
                {person ? "Partners" : "Builders"}{" "}
                <span class="bs-count">
                  {people.filter((p) => p.id !== person?.id).length}
                </span>
              </h3>
              <div class="bs-person-list motion-list">
                {people
                  .filter((p) => p.id !== person?.id)
                  .map((p) => (
                    <button
                      key={p.id}
                      onClick={() => onPerson(p.id)}
                    >
                      <span>{p.name}</span>
                      <span class={missed(p, b) ? "bs-alert" : "bs-muted"}>
                        {missed(p, b) ? "Awaiting check-in" : "Checked in"} →
                      </span>
                    </button>
                  ))}
              </div>
            </section>
            <section>
              <h3>Mentors</h3>
              {mentors.map((m) => (
                <p>
                  <a href={`/contacts?id=${encodeURIComponent(m.id)}`}>
                    {m.name}
                  </a>
                  <span class="bs-sub">
                    {m.org} · {m.topics.join(", ")}
                  </span>
                </p>
              ))}
            </section>
            <section>
              <h3>Latest check-in</h3>
              {timeline[0] ? (
                <p>
                  {timeline[0].body}
                  <span class="bs-sub">
                    {timeline[0].date} · {kindLabel[timeline[0].kind]}
                  </span>
                </p>
              ) : (
                <p class="bs-muted">No check-ins yet. Add the first entry.</p>
              )}
            </section>
          </>
        ) : tab === "add entry" ? (
          <form
            class="bs-form motion-list"
            onSubmit={async (e) => {
              e.preventDefault();
              setBusy(true);
              setError("");
              setMessage("");
              const f = new FormData(e.currentTarget);
              const form = e.currentTarget;
              try {
                await api("buildspaceActivityCreate", {
                  body: {
                    projectId: project.id,
                    personId: String(f.get("personId") || "") || null,
                    mentorId: String(f.get("mentorId") || "") || null,
                    kind,
                    date: String(f.get("date")),
                    body: String(f.get("body")),
                    status: String(
                      f.get("status"),
                    ) as BuildspaceActivity["status"],
                    purchaseId: String(f.get("purchaseId") || "") || null,
                  },
                });
                form.reset();
                reload();
                setMessage("Entry added.");
                setTab(
                  kind === "weekly" || kind === "mentor"
                    ? "timeline"
                    : kind === "request"
                      ? "requests"
                      : kind === "purchase"
                        ? "purchases"
                        : "absences",
                );
              } catch (err) {
                setError(errorMessage(err));
              } finally {
                setBusy(false);
              }
            }}
          >
            <label>
              Entry type
              <select
                class="select"
                aria-label="Entry type"
                value={kind}
                onChange={(e) => {
                  const next = e.currentTarget.value as typeof kind;
                  setKind(next);
                  setEntryStatus(
                    next === "request" || next === "purchase"
                      ? "open"
                      : "on-track",
                  );
                }}
              >
                {Object.entries(kindLabel).map(([v, l]) => (
                  <option value={v}>{l}</option>
                ))}
              </select>
            </label>
            <label>
              Builder {kind !== "weekly" && "· optional"}
              <select
                class="select"
                aria-label="Builder"
                name="personId"
                required={kind === "weekly"}
                value={builderId}
                onChange={(e) => setBuilderId(e.currentTarget.value)}
              >
                <option value="">
                  {kind === "weekly" ? "Choose builder" : "Whole project"}
                </option>
                {people.map((p) => (
                  <option value={p.id}>{p.name}</option>
                ))}
              </select>
            </label>
            {kind === "mentor" && (
              <label>
                Mentor
                <select
                  class="select"
                  aria-label="Mentor"
                  name="mentorId"
                  required
                >
                  <option value="">Choose mentor</option>
                  {mentors.map((m) => (
                    <option value={m.id}>{m.name}</option>
                  ))}
                </select>
              </label>
            )}
            <label>
              Date
              <input
                class="input"
                type="date"
                name="date"
                required
                defaultValue={b.today}
              />
            </label>
            <label>
              {kind === "absence" ? "Reason / context" : "Update / notes"}
              <textarea
                class="textarea"
                name="body"
                required
                maxLength={10000}
                rows={4}
              />
            </label>
            <label>
              Status
              <select
                class="select"
                aria-label="Status"
                name="status"
                key={kind}
                value={entryStatus}
                onChange={(e) =>
                  setEntryStatus(e.currentTarget.value as typeof entryStatus)
                }
              >
                <option value="on-track">On track</option>
                <option value="needs-help">Needs help</option>
                <option value="open">Open</option>
                <option value="resolved">Resolved</option>
              </select>
            </label>
            {kind === "purchase" && (
              <>
                <label>
                  Finance request ID · optional
                  <input class="input" name="purchaseId" maxLength={64} />
                </label>
                <p class="bs-muted">
                  Copy the ID from a{" "}
                  <a href={href.finance()}>Finance request</a> URL to keep its
                  live status linked. Leave blank to record a purchase need
                  before submission.
                </p>
              </>
            )}
            <button class="btn btn--primary" disabled={busy}>
              {busy ? "Saving…" : "Add entry"}
            </button>
          </form>
        ) : items.length ? (
          <ol class="bs-timeline motion-list">
            {items.map((a) => (
              <li key={a.id}>
                <div class="bs-timeline-head">
                  <span>{kindLabel[a.kind]}</span>
                  <time dateTime={a.date}>{a.date}</time>
                </div>
                <p>{a.body}</p>
                <div class="bs-activity-meta">
                  <Badge
                    tone={
                      openActivity(a) || a.status === "needs-help"
                        ? "attention"
                        : "neutral"
                    }
                  >
                    {a.purchaseStatus || a.status}
                  </Badge>
                  {a.purchaseNeededBy && (
                    <span class="bs-due">
                      needed by{" "}
                      <DueDate
                        date={a.purchaseNeededBy}
                        status={
                          (a.purchaseStatus ?? "closed") as PurchaseStatus
                        }
                        short
                      />
                    </span>
                  )}
                  <span>
                    {a.personId && a.personId !== person?.id && b.people.some((p) => p.id === a.personId)
                      ? <button class="bs-title" onClick={() => onPerson(a.personId!)}>{b.people.find((p) => p.id === a.personId)?.name || "Former builder"}</button>
                      : a.personId ? (b.people.find((p) => p.id === a.personId)?.name || "Former builder") : "Whole project"}
                    {a.mentorId &&
                      ` · ${mentors.find((m) => m.id === a.mentorId)?.name || "Former mentor"}`}{" "}
                    · {a.source === "discord" ? "Discord" : "Manual"}
                  </span>
                </div>
                {a.purchaseId ? (
                  <a href={href.request(a.purchaseId)}>
                    Open finance request: {a.purchaseTitle} →
                  </a>
                ) : (
                  openActivity(a) && (
                    <button
                      class="btn"
                      disabled={busy}
                      onClick={() => resolve(a)}
                    >
                      Mark resolved
                    </button>
                  )
                )}
              </li>
            ))}
          </ol>
        ) : (
          <Empty title={`No ${tab} yet`}>
            Add an entry to keep the project’s history together.
          </Empty>
        )}
      </div>
    </div>
  );
}
