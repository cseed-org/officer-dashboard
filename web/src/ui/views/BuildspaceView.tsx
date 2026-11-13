import { useRef, useState } from "preact/hooks";
import {
  BUILDSPACE_STAGES,
  BUILDSPACE_TRACKS,
  type BuildspaceData,
  type BuildspaceProject,
} from "../../lib/contracts";
import { useApi } from "../../lib/api/useApi";
import { fmtShort } from "../../lib/format";
import { setParams, useSearch } from "../../lib/nav";
import { Async, Empty, Notice } from "../components/states";
import { Badge, Tabs } from "../components/kit";
import { plural } from "../../lib/format";
import { StatRow } from "../components/StatRow";
import { Icon } from "../components/Icon";
import { Drawer } from "../components/Drawer";
import { useCrossfade, useFlip } from "../components/motion";
import type { CurrentUser } from "../../lib/contracts";
import { ScheduleMenu } from "./buildspace/Schedule";
import { BuildspaceDetail } from "./buildspace/Detail";
import { ProjectForm, PersonForm } from "./buildspace/Forms";
import {
  missed,
  openActivity,
  projectMentors,
  projectPeople,
  trackShort,
} from "./buildspace/model";

export default function BuildspaceView({ me }: { me: CurrentUser }) {
  const state = useApi("buildspaceList");
  const [notice, setNotice] = useState("");
  // The intro (the page's LCP text) and its actions need no register data, so they
  // paint as soon as sign-in resolves instead of waiting for /api/buildspace.
  return (
    <div class="buildspace">
      <div class="bs-intro">
        <div>
          <span class="brand-eyebrow">THE BUILDER REGISTER</span>
        </div>
        <div class="action-row">
          <ScheduleMenu
            schedule={state.status === "ready" ? state.data.schedule : null}
            canEdit={me.role === "leadership" || me.isOwner}
            saved={() => {
              state.reload();
              setNotice("Check-in schedule saved.");
            }}
          />
          <button class="btn" onClick={() => setParams({ new: "person" })}>
            Add builder
          </button>
          <button
            class="btn btn--primary"
            onClick={() => setParams({ new: "project" })}
          >
            New project
          </button>
        </div>
      </div>
      <Async state={state}>
        {(b) => (
          <BuildspaceContent
            b={b}
            reload={state.reload}
            notice={notice}
            setNotice={setNotice}
          />
        )}
      </Async>
    </div>
  );
}
function BuildspaceContent({
  b,
  reload,
  notice,
  setNotice,
}: {
  b: BuildspaceData;
  reload: () => void;
  notice: string;
  setNotice: (s: string) => void;
}) {
  const qs = useSearch();
  const tab =
    qs.get("tab") === "projects"
      ? "projects"
      : qs.get("tab") === "mentors"
        ? "mentors"
        : "people";
  const view = qs.get("view") || "table",
    group = qs.get("group") || "track";
  // Whole-view swaps (tab, layout, grouping) crossfade; changes within a view use FLIP.
  const viewKey = `${tab}|${view}|${view === "cards" ? group : ""}`;
  const viewRef = useRef<HTMLDivElement>(null),
    outRef = useRef<HTMLDivElement>(null);
  useCrossfade(viewRef, outRef, viewKey);
  useFlip(viewRef, viewKey);
  const pastDue = b.schedule.pastDue;
  const weekBadge = (p: BuildspaceData["people"][number]) => (
    <Badge tone={missed(p, b) ? "attention" : "positive"}>
      {missed(p, b)
        ? pastDue
          ? "Past due"
          : "Awaiting check-in"
        : "Checked in"}
    </Badge>
  );
  const project = (id: string) => b.projects.find((p) => p.id === id)!;
  const search = (qs.get("q") || "").toLowerCase();
  const flag = qs.get("flag") || "";
  const matches = (p: BuildspaceProject, personId?: string) => {
    const people = projectPeople(p, b),
      mentors = projectMentors(p, b);
    const activities = b.activity.filter(
      (a) =>
        a.projectId === p.id &&
        (!personId || !a.personId || a.personId === personId),
    );
    return (
      (!qs.get("track") || p.track === qs.get("track")) &&
      (!qs.get("mentor") || p.mentorIds.includes(qs.get("mentor")!)) &&
      (!qs.get("stage") || p.stage === qs.get("stage")) &&
      (!search ||
        [
          p.name,
          p.description,
          ...people
            .filter((x) => !personId || x.id === personId)
            .map((x) => x.name),
          ...mentors.map((x) => x.name),
        ]
          .join(" ")
          .toLowerCase()
          .includes(search)) &&
      (!flag ||
        (flag === "unmentored" && p.mentorIds.length === 0) ||
        (flag === "missed" &&
          people.some(
            (x) => (!personId || x.id === personId) && missed(x, b),
          )) ||
        (flag === "requests" && activities.some(openActivity)) ||
        (flag === "purchases" &&
          activities.some((a) => a.kind === "purchase" && openActivity(a))) ||
        (flag === "absences" &&
          activities.some(
            (a) =>
              a.kind === "absence" &&
              a.date >= b.weekStart &&
              a.date <= b.today,
          )))
    );
  };
  const projects = b.projects.filter((p) => matches(p));
  const people = b.people.filter((p) => matches(project(p.projectId), p.id));
  const mentors = b.mentors.filter((m) => {
    const assigned = b.projects.filter((p) => p.mentorIds.includes(m.id));
    if (qs.get("mentor") && qs.get("mentor") !== m.id) return false;
    if (!assigned.length)
      return (
        !qs.get("track") &&
        !qs.get("stage") &&
        !flag &&
        (!search ||
          [m.name, m.org, ...m.topics].join(" ").toLowerCase().includes(search))
      );
    return assigned.some((p) => matches(p));
  });
  const detailPerson = b.people.find((p) => p.id === qs.get("person"));
  const detailProject = b.projects.find((p) => p.id === qs.get("project"));
  const close = () =>
    setParams({ person: null, project: null, new: null, edit: null });
  const saved = () => {
    reload();
    close();
    setNotice("Saved to Buildspace + Launchpad.");
  };
  const edit = qs.get("edit");
  const editingPerson = edit === "person" ? detailPerson : undefined;
  const editingProject = edit === "project" ? detailProject : undefined;
  const form = qs.get("new") || edit;
  const reset = () =>
    setParams({ q: null, track: null, mentor: null, stage: null, flag: null });
  const count =
    tab === "people"
      ? people.length
      : tab === "projects"
        ? projects.length
        : mentors.length;
  const stageBadge = (p: BuildspaceProject) => (
    <Badge
      tone={
        p.stage === "At risk"
          ? "attention"
          : p.stage === "Shipped"
            ? "positive"
            : "neutral"
      }
    >
      {p.stage}
    </Badge>
  );
  const mentorNames = (p: BuildspaceProject) =>
    projectMentors(p, b)
      .map((m) => m.name)
      .join(", ") || "No mentor assigned";
  const projectLink = (p: BuildspaceProject) => (
    <button class="bs-title" onClick={() => setParams({ project: p.id })}>
      {p.name}
    </button>
  );
  const requestCount = (projectId: string) => {
    const count = b.activity.filter(
      (a) => a.projectId === projectId && openActivity(a),
    ).length;
    return `${count} open ${count === 1 ? "request" : "requests"}`;
  };
  const activityCounts = (projectId: string, personId: string) => {
    const activity = b.activity.filter(
      (a) =>
        a.projectId === projectId && (!a.personId || a.personId === personId),
    );
    const counts = [
      {
        icon: "message",
        label: "open requests",
        singular: "open request",
        count: activity.filter((a) => a.kind === "request" && openActivity(a))
          .length,
      },
      {
        icon: "wallet",
        label: "open purchases",
        singular: "open purchase",
        count: activity.filter((a) => a.kind === "purchase" && openActivity(a))
          .length,
      },
      {
        icon: "calendar",
        label: "absences this week",
        singular: "absence this week",
        count: activity.filter(
          (a) =>
            a.kind === "absence" && a.date >= b.weekStart && a.date <= b.today,
        ).length,
      },
    ];
    return (
      <span class="bs-activity-counts">
        {counts.map((c) => (
          <span
            class={c.count ? "has-activity" : ""}
            title={`${c.count} ${c.count === 1 ? c.singular : c.label}`}
          >
            <Icon name={c.icon} size={14} />
            <span aria-hidden="true">{c.count}</span>
            <span class="visually-hidden">
              {c.count} {c.count === 1 ? c.singular : c.label}
            </span>
          </span>
        ))}
      </span>
    );
  };
  const card = (p: BuildspaceProject, scope = "") => (
    <article
      class="bs-card"
      key={p.id}
      data-flip={scope + p.id}
      data-track={p.track}
    >
      <div class="bs-card-top">
        <span class="eyebrow">{trackShort(p.track)}</span>
        {stageBadge(p)}
      </div>
      <h3>{projectLink(p)}</h3>
      <p>{p.description}</p>
      <div class="bs-card-foot">
        <span>
          <strong>Builders</strong>{" "}
          {projectPeople(p, b)
            .map((x) => x.name)
            .join(" · ") || "No builders yet"}
        </span>
        <span class={!p.mentorIds.length ? "bs-alert" : ""}>
          <strong>Mentors</strong> {mentorNames(p)}
        </span>
      </div>
      <div class="bs-card-signals">
        <span>
          {projectPeople(p, b).filter((x) => missed(x, b)).length} awaiting
          check-in
        </span>
        <span>{requestCount(p.id)}</span>
      </div>
    </article>
  );
  const groups =
    group === "mentor"
      ? [
          ...b.mentors.map((m) => ({ id: m.id, label: m.name })),
          { id: "none", label: "No mentor assigned" },
        ]
      : group === "project"
        ? projects.map((p) => ({ id: p.id, label: p.name }))
        : BUILDSPACE_TRACKS.map((t) => ({ id: t, label: t }));
  const awaiting = b.people.filter((p) => missed(p, b)).length;
  // Tile figures count builder rows, the same rows the tile's filter shows on the People tab.
  const openRequestBuilders = b.people.filter((x) =>
    b.activity.some(
      (a) =>
        a.projectId === x.projectId &&
        (!a.personId || a.personId === x.id) &&
        openActivity(a),
    ),
  ).length;
  const unmentored = b.people.filter(
    (x) => !project(x.projectId).mentorIds.length,
  ).length;
  const flagHref = (f: string) => {
    const u = new URLSearchParams(qs);
    if (flag === f) u.delete("flag");
    else u.set("flag", f);
    const q = u.toString();
    return `/buildspace${q ? `?${q}` : ""}`;
  };
  return (
    <div class="buildspace">
      <StatRow
        label="Buildspace + Launchpad summary"
        tiles={[
          { label: "Builders", icon: "users", hue: "violet", value: b.people.length, sub: plural(b.projects.length, "project") },
          { label: "Awaiting this week", icon: "clock", hue: "amber", value: awaiting, sub: "no check-in yet", alert: awaiting > 0, href: flagHref("missed"), pressed: flag === "missed" },
          { label: "Open requests", icon: "inbox", hue: "teal", value: openRequestBuilders, sub: "builders with open requests", href: flagHref("requests"), pressed: flag === "requests" },
          { label: "Unmentored", icon: "flag", hue: "coral", value: unmentored, sub: "builders without a mentor", alert: unmentored > 0, href: flagHref("unmentored"), pressed: flag === "unmentored" },
        ]}
      />
      {notice && <Notice tone="positive">{notice}</Notice>}
      <div class="bs-toolbar">
        <Tabs
          label="Buildspace + Launchpad directory"
          idPrefix="bs"
          active={tab}
          onChange={(t) => setParams({ tab: t })}
          tabs={(["people", "projects", "mentors"] as const).map((t) => ({
            key: t,
            label: t[0]!.toUpperCase() + t.slice(1),
            count: b[t].length,
          }))}
        />
        <div class="segmented" aria-label="Layout">
          {["table", "cards", "board"].map((v) => (
            <button
              class="segment"
              aria-pressed={view === v}
              onClick={() => setParams({ view: v })}
            >
              {v[0]!.toUpperCase() + v.slice(1)}
            </button>
          ))}
        </div>
      </div>
      <div class="bs-filters">
        <label class="bs-search">
          <span class="visually-hidden">Search Buildspace + Launchpad</span>
          <input
            class="input"
            type="search"
            placeholder="Search builders, projects, mentors…"
            value={qs.get("q") || ""}
            onInput={(e) =>
              setParams({ q: e.currentTarget.value }, { replace: true })
            }
          />
        </label>
        <label>
          <span>Track</span>
          <select
            class="select"
            value={qs.get("track") || ""}
            onChange={(e) => setParams({ track: e.currentTarget.value })}
          >
            <option value="">All tracks</option>
            {BUILDSPACE_TRACKS.map((t) => (
              <option>{t}</option>
            ))}
          </select>
        </label>
        <label>
          <span>Mentor</span>
          <select
            class="select"
            value={qs.get("mentor") || ""}
            onChange={(e) => setParams({ mentor: e.currentTarget.value })}
          >
            <option value="">All mentors</option>
            {b.mentors.map((m) => (
              <option value={m.id}>{m.name}</option>
            ))}
          </select>
        </label>
        <label>
          <span>Stage</span>
          <select
            class="select"
            value={qs.get("stage") || ""}
            onChange={(e) => setParams({ stage: e.currentTarget.value })}
          >
            <option value="">All stages</option>
            {BUILDSPACE_STAGES.map((s) => (
              <option>{s}</option>
            ))}
          </select>
        </label>
        <label>
          <span>Attention</span>
          <select
            class="select"
            value={flag}
            onChange={(e) => setParams({ flag: e.currentTarget.value })}
          >
            <option value="">Everything</option>
            <option value="requests">Open requests / purchases</option>
            <option value="purchases">Open purchases</option>
            <option value="missed">Awaiting weekly check-in</option>
            <option value="absences">Absences this week</option>
            <option value="unmentored">No mentor assigned</option>
          </select>
        </label>
      </div>
      <div class="bs-result">
        <span role="status">
          {count} {tab} · Week {fmtShort(b.weekStart)} – {fmtShort(b.weekDue)}
        </span>
        <div class="action-row">
          {view === "cards" && (
            <label>
              Group by{" "}
              <select
                class="select"
                aria-label="Group by"
                value={group}
                onChange={(e) => setParams({ group: e.currentTarget.value })}
              >
                <option value="track">Track</option>
                <option value="mentor">Mentor</option>
                <option value="project">Project</option>
              </select>
            </label>
          )}
          <button class="linkish" onClick={reset}>
            Clear filters
          </button>
        </div>
      </div>
      <div
        class="bs-view"
        id="bs-panel"
        role="tabpanel"
        aria-labelledby={`bs-tab-${tab}`}
      >
        <div ref={viewRef} class="bs-view-in">
          {!count ? (
            <Empty title="No matches">
              Try a different filter, or clear your filters to see everyone.
            </Empty>
          ) : view === "board" ? (
            <>
              <div class="bs-board">
                {BUILDSPACE_STAGES.map((s) => (
                  <section class="bs-lane motion-list" key={s}>
                    <h3>
                      {s}
                      <span>
                        {projects.filter((p) => p.stage === s).length}
                      </span>
                    </h3>
                    {projects.filter((p) => p.stage === s).map((p) => card(p))}
                    {!projects.some((p) => p.stage === s) && (
                      <p class="bs-muted">Nothing here yet.</p>
                    )}
                  </section>
                ))}
              </div>
            </>
          ) : view === "cards" ? (
            <div class="bs-groups">
              {groups.map((g) => {
                const belongs = (p: BuildspaceProject) =>
                  group === "mentor"
                    ? g.id === "none"
                      ? !p.mentorIds.length
                      : p.mentorIds.includes(g.id)
                    : group === "project"
                      ? p.id === g.id
                      : p.track === g.id;
                const ps = projects.filter(belongs),
                  pe = people.filter((p) => belongs(project(p.projectId))),
                  ms = mentors.filter(
                    (m) =>
                      (group !== "mentor" || g.id === m.id) &&
                      ps.some((p) => p.mentorIds.includes(m.id)),
                  );
                if (
                  !(tab === "people"
                    ? pe.length
                    : tab === "projects"
                      ? ps.length
                      : ms.length)
                )
                  return null;
                return (
                  <section class="bs-group" key={g.id}>
                    <h3>
                      {g.label}
                      <span>
                        {tab === "people"
                          ? pe.length
                          : tab === "projects"
                            ? ps.length
                            : ms.length}
                      </span>
                    </h3>
                    <div class="bs-cards motion-list">
                      {tab === "projects"
                        ? ps.map((p) => card(p, g.id + ":"))
                        : tab === "people"
                          ? pe.map((p) => (
                              <article
                                class="bs-card"
                                key={p.id}
                                data-flip={g.id + ":" + p.id}
                              >
                                <span class="eyebrow">
                                  {trackShort(project(p.projectId).track)}
                                </span>
                                <h3>
                                  <button
                                    class="bs-title"
                                    onClick={() => setParams({ person: p.id })}
                                  >
                                    {p.name}
                                  </button>
                                </h3>
                                <p>
                                  <strong>Project</strong>{" "}
                                  {project(p.projectId).name}
                                </p>
                                <p>
                                  <strong>Mentors</strong>{" "}
                                  {mentorNames(project(p.projectId))}
                                </p>
                                {weekBadge(p)}
                              </article>
                            ))
                          : ms.map((m) => (
                              <article
                                class="bs-card"
                                key={m.id}
                                data-flip={g.id + ":" + m.id}
                              >
                                <h3>{m.name}</h3>
                                <p>{m.org}</p>
                                {ps
                                  .filter((p) => p.mentorIds.includes(m.id))
                                  .map(projectLink)}
                              </article>
                            ))}
                    </div>
                  </section>
                );
              })}
              {tab === "mentors" &&
                mentors.some(
                  (m) => !b.projects.some((p) => p.mentorIds.includes(m.id)),
                ) && (
                  <section class="bs-group" key="unassigned">
                    <h3>No projects assigned</h3>
                    <div class="bs-cards motion-list">
                      {mentors
                        .filter(
                          (m) =>
                            !b.projects.some((p) => p.mentorIds.includes(m.id)),
                        )
                        .map((m) => (
                          <article
                            class="bs-card"
                            key={m.id}
                            data-flip={"unassigned:" + m.id}
                          >
                            <h3>{m.name}</h3>
                            <p>{m.org}</p>
                            <a
                              href={`/contacts?id=${encodeURIComponent(m.id)}`}
                            >
                              Open mentor contact →
                            </a>
                          </article>
                        ))}
                    </div>
                  </section>
                )}
            </div>
          ) : (
            <div
              class="table-wrap"
              tabIndex={0}
              aria-label="Buildspace + Launchpad directory"
            >
              <table class="bs-table">
                <thead>
                  <tr>
                    {(tab === "people"
                      ? [
                          "Builder / project",
                          "Track",
                          "Partners",
                          "Mentors",
                          "Stage",
                          "This week",
                          "Activity",
                        ]
                      : tab === "projects"
                        ? ["Project", "Track", "Builders", "Mentors", "Stage"]
                        : ["Mentor", "Focus", "Projects", "Builders"]
                    ).map((h) => (
                      <th scope="col">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody class="motion-list">
                  {tab === "people"
                    ? people.map((p) => {
                        const pr = project(p.projectId);
                        return (
                          <tr key={p.id} data-flip={p.id}>
                            <td data-label="Builder / project">
                              <button
                                class="bs-title"
                                onClick={() => setParams({ person: p.id })}
                              >
                                {p.name}
                              </button>
                              <span class="bs-sub">{pr.name}</span>
                            </td>
                            <td data-label="Track">
                              <span class="bs-track" data-track={pr.track}>
                                {trackShort(pr.track)}
                              </span>
                            </td>
                            <td data-label="Partners">
                              {projectPeople(pr, b)
                                .filter((partner) => partner.id !== p.id)
                                .map((partner) => (
                                  <button
                                    class="bs-partner"
                                    aria-label={`Open partner ${partner.name}`}
                                    onClick={() =>
                                      setParams({ person: partner.id })
                                    }
                                  >
                                    {partner.name}
                                  </button>
                                ))}
                              {projectPeople(pr, b).length === 1 && (
                                <span class="bs-muted">Solo builder</span>
                              )}
                            </td>
                            <td
                              data-label="Mentors"
                              class={!pr.mentorIds.length ? "bs-alert" : ""}
                            >
                              {mentorNames(pr)}
                            </td>
                            <td data-label="Stage">{stageBadge(pr)}</td>
                            <td data-label="This week">
                              {weekBadge(p)}
                              {b.activity.some(
                                (a) =>
                                  a.personId === p.id &&
                                  a.kind === "absence" &&
                                  a.date >= b.weekStart &&
                                  a.date <= b.today,
                              ) && <span class="bs-sub">Absence logged</span>}
                            </td>
                            <td data-label="Activity">
                              {activityCounts(pr.id, p.id)}
                            </td>
                          </tr>
                        );
                      })
                    : tab === "projects"
                      ? projects.map((p) => (
                          <tr key={p.id} data-flip={p.id}>
                            <td data-label="Project">
                              {projectLink(p)}
                              <span class="bs-sub">{requestCount(p.id)}</span>
                            </td>
                            <td data-label="Track">
                              <span class="bs-track" data-track={p.track}>
                                {trackShort(p.track)}
                              </span>
                            </td>
                            <td data-label="Builders">
                              {projectPeople(p, b)
                                .map((x) => x.name)
                                .join(", ") || "No builders"}
                            </td>
                            <td
                              data-label="Mentors"
                              class={!p.mentorIds.length ? "bs-alert" : ""}
                            >
                              {mentorNames(p)}
                            </td>
                            <td data-label="Stage">{stageBadge(p)}</td>
                          </tr>
                        ))
                      : mentors.map((m) => {
                          const ps = b.projects.filter((p) =>
                            p.mentorIds.includes(m.id),
                          );
                          return (
                            <tr key={m.id} data-flip={m.id}>
                              <td data-label="Mentor">
                                <strong>{m.name}</strong>
                                <span class="bs-sub">{m.org}</span>
                              </td>
                              <td data-label="Focus">{m.topics.join(" · ")}</td>
                              <td data-label="Projects">
                                {ps.length
                                  ? ps.map(projectLink)
                                  : "No projects assigned"}
                              </td>
                              <td data-label="Builders">
                                {
                                  b.people.filter((p) =>
                                    ps.some((pr) => pr.id === p.projectId),
                                  ).length
                                }
                              </td>
                            </tr>
                          );
                        })}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <div ref={outRef} class="bs-view-out" />
      </div>
      <Drawer
        open={
          !!form ||
          !!detailPerson ||
          !!detailProject ||
          !!qs.get("person") ||
          !!qs.get("project")
        }
        onClose={close}
        id="buildspace-detail"
        size="lg"
        title={
          form
            ? (edit ? "Edit " : "New ") +
              (form === "person" ? "builder" : "project")
            : detailPerson?.name || detailProject?.name || "Record not found"
        }
        eyebrow="Buildspace + Launchpad"
      >
        {form === "project" ? (
          <ProjectForm b={b} project={editingProject} saved={saved} />
        ) : form === "person" ? (
          <PersonForm b={b} person={editingPerson} saved={saved} />
        ) : detailPerson || detailProject ? (
          <BuildspaceDetail
            key={detailPerson?.id || detailProject?.id}
            b={b}
            person={detailPerson}
            project={detailProject || project(detailPerson!.projectId)}
            reload={reload}
          />
        ) : (
          <Empty title="This record could not be found" />
        )}
      </Drawer>
    </div>
  );
}
