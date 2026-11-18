import { PersonDrawer, usePersonParam } from "../components/PersonDrawer";
import { api } from "../../lib/api/client";
import { LoadHistory, mergeActivity } from "../components/LoadHistory";
import { useMemo, useRef, useState } from "preact/hooks";
import { BUILDSPACE_STAGES, BUILDSPACE_TRACKS } from "../../core/constants";
import type { BuildspaceData, BuildspaceProject } from "../../lib/contracts";
import { useApi } from "../../lib/api/useApi";
import { fmtDate, fmtShort } from "../../lib/format";
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
import { CardGroup, MentorCard, PersonCard, ProjectCard, TRACK_HUE, STAGE_HUE, checkIn } from "./buildspace/Cards";
import { closeAll, leaveEntry, pushEntry } from "./buildspace/history";
import { CohortBar } from "./buildspace/Cohorts";
import { InvitesPanel, type InviteLinks } from "./buildspace/Invites";
import { AutoAssignDialog, MentorRoster } from "./buildspace/Mentors";
import { lifecycleActions } from "./buildspace/lifecycle";
import { ACCOUNT_HUE, accountLabel, isDirector } from "./buildspace/launchpad";
import { PeopleChip } from "../components/PeopleTable";
import {
  absenceThisWeek,
  missed,
  openActivity,
  projectActivities,
  projectById,
  projectMentors,
  projectPeople,
  projectSearchText,
  trackShort,
} from "./buildspace/model";

export default function BuildspaceView({ me }: { me: CurrentUser }) {
  const params = useSearch();
  // The register is one cohort at a time (default: the newest active one); archived projects and
  // builders load only when asked for (docs/launchpad-cohorts.md).
  const query = {
    cohort: params.get("cohort") || undefined,
    archived: params.get("archived") === "1" ? ("1" as const) : undefined,
  };
  const state = useApi("buildspaceList", { query });
  const [notice, setNotice] = useState("");
  const [links, setLinks] = useState<InviteLinks>({});
  const canDirector = isDirector(me);
  const data = state.status === "ready" ? state.data : null;
  const cohort = data?.cohorts.find((c) => c.id === data.cohortId) ?? null;
  const readOnly = cohort?.status === "archived";
  const canAdd = !!data && !!data.cohortId && !readOnly;
  // The intro (the page's LCP text) and its actions need no register data, so they
  // paint as soon as sign-in resolves instead of waiting for /api/buildspace.
  return (
    <div class="buildspace">
      <div class="bs-intro">
        {data && (
          <CohortBar
            cohorts={data.cohorts}
            cohortId={data.cohortId}
            canManage={canDirector}
            changed={(message, select) => {
              setNotice(message);
              if (select !== undefined && select !== (params.get("cohort") || null)) setParams({ cohort: select, person: null, project: null });
              else state.reload();
            }}
          />
        )}
        <div class="action-row">
          <ScheduleMenu
            schedule={state.status === "ready" ? state.data.schedule : null}
            canEdit={me.role === "leadership" || me.isOwner}
            saved={() => {
              state.reload();
              setNotice("Check-in schedule saved.");
            }}
          />
          <button class="btn" disabled={!canAdd} onClick={() => pushEntry({ new: "person" })}>
            Add builder
          </button>
          <button
            class="btn btn--primary"
            disabled={!canAdd}
            onClick={() => pushEntry({ new: "project" })}
          >
            New project
          </button>
        </div>
      </div>
      <Async state={state}>
        {(b) => (
          b.cohortId === null ? (
            <Empty title="No cohort yet">
              {canDirector
                ? "Create the first cohort with New cohort above, then invite builders to it."
                : "A director creates cohorts. Ask one to set up the first cohort."}
            </Empty>
          ) : (
            <BuildspaceContent
              key={b.cohortId}
              b={b}
              reload={state.reload}
              notice={notice}
              setNotice={setNotice}
              canDirector={canDirector}
              readOnly={b.cohorts.find((c) => c.id === b.cohortId)?.status === "archived"}
              links={links}
              addLinks={(l) => setLinks((old) => ({ ...old, ...l }))}
            />
          )
        )}
      </Async>
      {state.status === "ready" && state.data.activityNextCursor && <LoadHistory key={`${state.data.cohortId}|${state.data.activityNextCursor}`} load={async (signal) => {
        const previous = state.data;
        const more = await api("buildspaceList", {query: {...query, activityCursor: previous.activityNextCursor!}, signal});
        state.set({...more, activity: mergeActivity(previous.activity, more.activity)});
      }} /> }
    </div>
  );
}
function BuildspaceContent({
  b,
  reload,
  notice,
  setNotice,
  canDirector,
  readOnly,
  links,
  addLinks,
}: {
  b: BuildspaceData;
  reload: () => void;
  notice: string;
  setNotice: (s: string) => void;
  canDirector: boolean;
  readOnly: boolean;
  links: InviteLinks;
  addLinks: (l: InviteLinks) => void;
}) {
  const qs = useSearch();
  const personDrawer = usePersonParam();
  const [autoAssign, setAutoAssign] = useState(false);
  const [lifecycleError, setLifecycleError] = useState("");
  const showArchived = qs.get("archived") === "1";
  const cohort = b.cohorts.find((c) => c.id === b.cohortId);
  const lifecycleOpts = {
    canDirector,
    readOnly,
    done: (message: string, closed?: boolean) => {
      setLifecycleError("");
      reload();
      setNotice(message);
      if (closed) closeAll();
    },
    fail: (message: string) => setLifecycleError(message),
  };
  const tab =
    qs.get("tab") === "projects"
      ? "projects"
      : qs.get("tab") === "mentors"
        ? "mentors"
        : qs.get("tab") === "invites"
          ? "invites"
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
  const project = (id: string) => projectById(id, b)!;
  const search = (qs.get("q") || "").toLowerCase();
  const flag = qs.get("flag") || "";
  const matches = (p: BuildspaceProject, personId?: string) => {
    // Cheap URL filters reject most rows before names or activity are touched.
    if (qs.get("track") && p.track !== qs.get("track")) return false;
    if (qs.get("mentor") && !p.mentorIds.includes(qs.get("mentor")!)) return false;
    if (qs.get("stage") && p.stage !== qs.get("stage")) return false;
    const people = projectPeople(p, b);
    if (search && !projectSearchText(p, b, personId).includes(search)) return false;
    if (!flag) return true;
    if (flag === "unmentored") return p.mentorIds.length === 0;
    if (flag === "missed") return people.some((x) => (!personId || x.id === personId) && missed(x, b));
    const activities = projectActivities(p.id, b, personId);
    if (flag === "requests") return activities.some(openActivity);
    if (flag === "purchases") return activities.some((a) => a.kind === "purchase" && openActivity(a));
    return flag === "absences" && activities.some((a) => a.kind === "absence" && a.date >= b.weekStart && a.date <= b.today);
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
  // Closing steps back over the entries this page pushed (or drops a deep link in place), so
  // Back never reopens a closed form and leaves no dead entry (R-4, review F-2).
  const close = closeAll;
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
    <button class="bs-title" onClick={() => pushEntry({ project: p.id })}>
      {p.name}
    </button>
  );
  const requestCount = (projectId: string) => {
    const count = projectActivities(projectId, b).filter(openActivity).length;
    return `${count} open ${count === 1 ? "request" : "requests"}`;
  };
  const activityCounts = (projectId: string, personId: string) => {
    const activity = projectActivities(projectId, b, personId);
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
    <ProjectCard readOnly={readOnly} key={p.id} p={p} b={b} scope={scope} onPerson={personDrawer.open} />
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
  // Summary counts depend only on the register, not search, selection or an open drawer.
  const { live, liveProjects, awaiting, openRequestBuilders, unmentored } = useMemo(() => {
    const live = b.people.filter((p) => p.archivedAt === null);
    const liveProjects = b.projects.filter((p) => p.archivedAt === null);
    const awaiting = live.filter((p) => missed(p, b)).length;
    // Tile figures count builder rows, the same rows the tile's filter shows on the People tab.
    const openRequestBuilders = live.filter((x) => projectActivities(x.projectId, b, x.id).some(openActivity)).length;
    const unmentored = live.filter((x) => !projectById(x.projectId, b)!.mentorIds.length).length;
    return { live, liveProjects, awaiting, openRequestBuilders, unmentored };
  }, [b]);
  const flagHref = (f: string) => {
    const u = new URLSearchParams(qs);
    if (flag === f) u.delete("flag");
    else u.set("flag", f);
    const q = u.toString();
    return `/buildspace-launchpad${q ? `?${q}` : ""}`;
  };
  return (
    <div class="buildspace">
      <StatRow
        label="Buildspace + Launchpad summary"
        tiles={[
          { label: "Builders", icon: "users", hue: "violet", value: live.length, sub: plural(liveProjects.length, "project") },
          { label: "Awaiting this week", icon: "clock", hue: "amber", value: awaiting, sub: "no check-in yet", alert: awaiting > 0, href: flagHref("missed"), pressed: flag === "missed" },
          { label: "Open requests", icon: "inbox", hue: "teal", value: openRequestBuilders, sub: "builders with open requests", href: flagHref("requests"), pressed: flag === "requests" },
          { label: "Unmentored", icon: "flag", hue: "coral", value: unmentored, sub: "builders without a mentor", alert: unmentored > 0, href: flagHref("unmentored"), pressed: flag === "unmentored" },
        ]}
      />
      {readOnly && (
        <Notice tone="attention">
          {cohort?.name ?? "This cohort"} is archived, so it is read-only. Builders can still sign in and read their portal.
        </Notice>
      )}
      {notice && <Notice tone="positive">{notice}</Notice>}
      {lifecycleError && <Notice tone="negative">{lifecycleError}</Notice>}
      <div class="bs-toolbar">
        <Tabs
          label="Buildspace + Launchpad directory"
          idPrefix="bs"
          active={tab}
          onChange={(t) => setParams({ tab: t })}
          tabs={(["people", "projects", "mentors", "invites"] as const).map((t) => ({
            key: t,
            label: t[0]!.toUpperCase() + t.slice(1),
            count: t === "invites" ? b.accounts.length : t === "people" ? live.length : t === "projects" ? liveProjects.length : b[t].length,
          }))}
        />
        {tab !== "invites" && <div class="segmented" aria-label="Layout">
          {["table", "cards", "board"].map((v) => (
            <button
              class="segment"
              aria-pressed={view === v}
              onClick={() => setParams({ view: v })}
            >
              {v[0]!.toUpperCase() + v.slice(1)}
            </button>
          ))}
        </div>}
      </div>
      {tab === "invites" ? (
        <div id="bs-panel" role="tabpanel" aria-labelledby="bs-tab-invites" class="tab-panel">
          <InvitesPanel b={b} canDirector={canDirector} readOnly={readOnly} links={links} addLinks={addLinks} reload={reload} setNotice={setNotice} />
        </div>
      ) : (<>
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
        <label class="lp-archived-toggle">
          <input
            type="checkbox"
            data-show-archived
            checked={showArchived}
            onChange={(e) => setParams({ archived: e.currentTarget.checked ? "1" : null })}
          />
          <span>Show archived</span>
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
          {(tab === "projects" || tab === "mentors") && !readOnly && (
            <button type="button" class="btn" data-auto-assign-open onClick={() => setAutoAssign(true)}>
              Auto-assign mentors
            </button>
          )}
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
                  <CardGroup
                    key={g.id}
                    id={g.id}
                    title={g.label}
                    count={tab === "people" ? pe.length : tab === "projects" ? ps.length : ms.length}
                  >
                    {tab === "projects"
                      ? ps.map((p) => card(p, g.id + ":"))
                      : tab === "people"
                        ? pe.map((p) => (
                            <PersonCard readOnly={readOnly} key={p.id} p={p} b={b} scope={g.id + ":"} onPerson={personDrawer.open} />
                          ))
                        : ms.map((m) => (
                            <MentorCard key={m.id} m={m} b={b} scope={g.id + ":"} projects={ps.filter((p) => p.mentorIds.includes(m.id))} />
                          ))}
                  </CardGroup>
                );
              })}
              {tab === "mentors" &&
                mentors.some(
                  (m) => !b.projects.some((p) => p.mentorIds.includes(m.id)),
                ) && (
                  <CardGroup key="unassigned" id="unassigned" title="No projects assigned" count={mentors.filter((m) => !b.projects.some((p) => p.mentorIds.includes(m.id))).length}>
                    {mentors
                      .filter((m) => !b.projects.some((p) => p.mentorIds.includes(m.id)))
                      .map((m) => (
                        <MentorCard key={m.id} m={m} b={b} scope="unassigned:" projects={[]} />
                      ))}
                  </CardGroup>
                )}
            </div>
          ) : tab === "mentors" ? (
            <MentorRoster b={b} mentors={mentors} canDirector={canDirector} readOnly={readOnly} reload={reload} setNotice={setNotice} />
          ) : (
            <div
              class="table-wrap bs-wrap"
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
                          <tr key={p.id} data-flip={p.id} class={p.archivedAt !== null ? "lp-muted-row" : undefined} data-archived={p.archivedAt !== null ? "" : undefined}>
                            <td data-label="Builder / project">
                              <button
                                class="bs-title"
                                onClick={() => personDrawer.open(p.id)}
                              >
                                {p.name}
                              </button>
                              <span class="bs-sub">
                                {pr.name}
                                {p.designation && ` · ${p.designation}`}
                              </span>
                              {p.archivedAt !== null ? (
                                <Badge tone="neutral">Archived</Badge>
                              ) : p.accountStatus ? (
                                <span class="lp-account-chip" data-account-status={p.accountStatus}>
                                  <PeopleChip hue={ACCOUNT_HUE[p.accountStatus]}>
                                    {p.accountStatus === "active" ? "Portal active" : accountLabel({ status: p.accountStatus, inviteExpired: false })}
                                  </PeopleChip>
                                </span>
                              ) : null}
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
                                      personDrawer.open(partner.id)
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
                              {absenceThisWeek(p, b) && <span class="bs-sub">Absence logged</span>}
                            </td>
                            <td data-label="Activity">
                              {activityCounts(pr.id, p.id)}
                            </td>
                          </tr>
                        );
                      })
                    : tab === "projects"
                      ? projects.map((p) => (
                          <tr key={p.id} data-flip={p.id} class={p.archivedAt !== null ? "lp-muted-row" : undefined} data-archived={p.archivedAt !== null ? "" : undefined}>
                            <td data-label="Project">
                              {projectLink(p)}
                              <span class="bs-sub">{requestCount(p.id)}</span>
                              {p.archivedAt !== null && <Badge tone="neutral">Archived</Badge>}
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
      </>)}
      <AutoAssignDialog
        b={b}
        open={autoAssign}
        onClose={() => setAutoAssign(false)}
        applied={(message) => {
          reload();
          setNotice(message);
        }}
      />
      <PersonDrawer
        open={!!qs.get("person") && !form}
        onClose={() => {
          const current = new URLSearchParams(location.search);
          if (!current.has("edit") && !current.has("new")) personDrawer.close();
        }}
        id="buildspace-person"
        personKey={qs.get("person") || ""}
        name={detailPerson?.name || "Builder"}
        status={!detailPerson ? <Empty title="This record could not be found" /> : undefined}
        eyebrow={detailPerson ? "Buildspace + Launchpad builder" : undefined}
        subtitle={detailPerson ? <button class="bs-title" onClick={() => pushEntry({ person: null, project: detailPerson.projectId })}>{project(detailPerson.projectId).name}</button> : undefined}
        actions={detailPerson ? [
          ...(!readOnly && detailPerson.archivedAt === null ? [
            // The weekly follow-up's main action; "Add entry" is otherwise the last, often scrolled-off tab.
            { label: "Log check-in", icon: "plus" as const, tab: "add entry", primary: checkIn(detailPerson, b).missed, data: "log-checkin" },
            { label: "Edit builder", icon: "pen" as const, onClick: () => pushEntry({ edit: "person" }) },
          ] : []),
          ...(detailPerson.memberId ? [{ label: "Member record", icon: "user" as const, href: `/members?person=${encodeURIComponent(detailPerson.memberId)}` }] : []),
          ...lifecycleActions({ kind: "person", record: detailPerson }, b, lifecycleOpts).map((l) => ({
            label: l.label,
            data: `lifecycle-${l.data}`,
            onClick: l.disabled ? () => setLifecycleError(l.reason ?? "Not available.") : l.run,
          })),
        ] : []}
        stats={detailPerson ? [{ label: "Joined", value: <time dateTime={detailPerson.joinedOn}>{fmtDate(detailPerson.joinedOn)}</time> }] : []}
        chips={detailPerson ? (() => {
          const pr = project(detailPerson.projectId), week = checkIn(detailPerson, b);
          return <>
            <PeopleChip hue={TRACK_HUE[pr.track]}>{pr.track}</PeopleChip>
            <PeopleChip hue={STAGE_HUE[pr.stage]}>{pr.stage}</PeopleChip>
            <PeopleChip hue={week.hue} data={{ "data-checkin": week.missed ? "missed" : "done" }}>{week.label}</PeopleChip>
          </>;
        })() : undefined}
        tabs={["overview", "timeline", "requests", "purchases", "absences", "add entry"].map((section) => ({
          key: section, label: section[0]!.toUpperCase() + section.slice(1), render: () => detailPerson ? (
            <BuildspaceDetail
              key={detailPerson.id}
              b={b}
              person={detailPerson}
              project={project(detailPerson.projectId)}
              reload={reload}
              onPerson={personDrawer.open}
              section={section}
              readOnly={readOnly}
            />
          ) : null,
        }))}
      />
      <Drawer
        open={
          !!form || (!qs.get("person") && !!qs.get("project"))
        }
        onClose={() => {
          const current = new URLSearchParams(location.search);
          if (current.has("person") && !current.has("edit") && !current.has("new")) return;
          // Leaving a builder form steps back to the profile it came from; everything else
          // (project, project edit, new record) closes back to the list (R-4, F-2).
          if (current.has("edit") && current.has("person")) leaveEntry({ edit: null });
          else close();
        }}
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
          <ProjectForm key={editingProject?.id ?? "new-project"} readOnly={readOnly} b={b} project={editingProject} saved={saved} />
        ) : form === "person" ? (
          <PersonForm key={editingPerson?.id ?? "new-person"} readOnly={readOnly} b={b} person={editingPerson} saved={saved} />
        ) : detailProject && !qs.get("person") ? (
          <BuildspaceDetail
            key={detailProject.id}
            b={b}
            project={detailProject}
            reload={reload}
            onPerson={personDrawer.open}
            readOnly={readOnly}
            lifecycle={lifecycleActions({ kind: "project", record: detailProject }, b, lifecycleOpts)}
          />
        ) : (
          <Empty title="This record could not be found" />
        )}
      </Drawer>
    </div>
  );
}
