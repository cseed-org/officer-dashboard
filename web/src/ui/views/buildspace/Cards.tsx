/**
 * V0.11 Buildspace + Launchpad cards in the officer card style (people.css, PR #68): a band in
 * the record's colour, an avatar, the name, tinted chips, then a few signals with icons. A
 * click anywhere on a card opens it (the name button is the keyboard path); the quick actions
 * appear on hover or focus and stay visible on touch screens. Shared by the Cards view (every
 * grouping) and the projects board.
 */
import type { ComponentChildren } from "preact";
import { Fragment } from "preact";
import { useState } from "preact/hooks";
import type { BuildspaceData, BuildspaceProject } from "../../../lib/contracts";
import { personHue, type Hue } from "../../../lib/colors";
import { fmtShort } from "../../../lib/format";
import { navigate } from "../../../lib/nav";
import { pushEntry } from "./history";
import { Icon, type IconName } from "../../components/Icon";
import { PeopleChip } from "../../components/PeopleTable";
import { Avatar, RowMenu, type MenuItem } from "../../components/people";
import {
  absenceThisWeek,
  missed,
  openActivity,
  projectActivities,
  projectById,
  projectMentors,
  projectPeople,
  trackShort,
} from "./model";

type Person = BuildspaceData["people"][number];
type Mentor = BuildspaceData["mentors"][number];

export const TRACK_HUE: Record<BuildspaceProject["track"], Hue> = {
  Software: "blue",
  Hardware: "teal",
  Creative: "coral",
  "Extended Virtual Reality (XR)": "violet",
};
/** Only the stages that need a glance get a colour; the rest stay neutral. */
export const STAGE_HUE: Partial<Record<BuildspaceProject["stage"], Hue>> = {
  "At risk": "coral",
  Shipped: "green",
};

const DAY = 86_400_000;
/** "Today", "Yesterday", "3 days ago", then the short date (club dates are plain ISO days). */
export function daysAgo(date: string, today: string): string {
  const d = Math.round((Date.parse(today) - Date.parse(date)) / DAY);
  if (d <= 0) return "Today";
  if (d === 1) return "Yesterday";
  if (d < 7) return `${d} days ago`;
  return fmtShort(date);
}

/** The weekly check-in state of one builder, with the due date when it is missing. */
export function checkIn(p: Person, b: BuildspaceData) {
  if (!missed(p, b)) return { label: "Checked in", hue: "green" as Hue, missed: false };
  const due = fmtShort(b.weekDue);
  return b.schedule.pastDue
    ? { label: `Past due · ${due}`, hue: "coral" as Hue, missed: true }
    : { label: `Awaiting check-in · due ${due}`, hue: "amber" as Hue, missed: true };
}

const lastActivity = (b: BuildspaceData, projectId: string, personId?: string) =>
  projectActivities(projectId, b, personId).reduce<string | null>((m, a) => (!m || a.date > m ? a.date : m), null);

const openCount = (b: BuildspaceData, projectId: string, personId?: string) =>
  projectActivities(projectId, b, personId).filter(openActivity).length;

/** One signal line: an icon, a label for screen readers and the value. */
function Signal({ icon, label, children, alert, data }: { icon: IconName; label: string; children: ComponentChildren; alert?: boolean; data?: string }) {
  return (
    <div class={`bs-signal${alert ? " is-alert" : ""}`} data-signal={data}>
      <dt>
        <Icon name={icon} size={14} />
        <span class="visually-hidden">{label}</span>
      </dt>
      <dd>{children}</dd>
    </div>
  );
}

/** The card frame: band, quick actions, body; a click outside its own controls opens it. */
function CardShell({
  hue,
  flip,
  data,
  onOpen,
  actions,
  children,
  menuLabel,
}: {
  hue: Hue;
  flip: string;
  data: Record<string, string>;
  onOpen?: () => void;
  actions: MenuItem[];
  menuLabel: string;
  children: ComponentChildren;
}) {
  return (
    <article
      class={`bs-card${onOpen ? " is-openable" : ""}`}
      data-flip={flip}
      data-hue={hue}
      {...data}
      onClick={(e) => {
        if (!onOpen || (e.target as Element).closest("a, button, input, select, label, [role=menu]")) return;
        onOpen();
      }}
    >
      <span class="bs-card-band" aria-hidden="true" />
      {actions.length > 0 && (
        <div class="bs-card-actions">
          <RowMenu label={menuLabel} items={actions} floating />
        </div>
      )}
      {children}
    </article>
  );
}

const memberHref = (id: string) => `/members?person=${encodeURIComponent(id)}`;

export function PersonCard({ p, b, readOnly = false, scope, onPerson }: { p: Person; b: BuildspaceData; readOnly?: boolean; scope: string; onPerson: (id: string) => void }) {
  const pr = projectById(p.projectId, b)!;
  const mentors = projectMentors(pr, b);
  const week = checkIn(p, b);
  const open = openCount(b, pr.id, p.id);
  const last = lastActivity(b, pr.id, p.id);
  const absent = absenceThisWeek(p, b);
  return (
    <CardShell
      hue={personHue(p.id)}
      flip={scope + p.id}
      data={{ "data-person-card": p.id }}
      onOpen={() => onPerson(p.id)}
      menuLabel={`Actions for ${p.name}`}
      actions={[
        { label: "Open profile", onSelect: () => onPerson(p.id) },
        ...(!readOnly ? [{ label: "Edit builder", onSelect: () => pushEntry({ person: p.id, edit: "person" }) }] : []),
        { label: "Open project", onSelect: () => pushEntry({ project: pr.id }) },
        ...(p.memberId ? [{ label: "Member record", onSelect: () => navigate(memberHref(p.memberId!)) }] : []),
      ]}
    >
      <header class="bs-card-id">
        <Avatar name={p.name} id={p.id} size="lg" />
        <div class="bs-card-name">
          <h3>
            <button type="button" class="person-name" onClick={() => onPerson(p.id)}>
              {p.name}
            </button>
          </h3>
          <span class="person-sub">{pr.name}</span>
        </div>
      </header>
      <div class="people-chips bs-card-chips">
        <PeopleChip hue={TRACK_HUE[pr.track]}>{trackShort(pr.track)}</PeopleChip>
        <PeopleChip hue={STAGE_HUE[pr.stage]}>{pr.stage}</PeopleChip>
        <PeopleChip hue={week.hue} data={{ "data-checkin": week.missed ? "missed" : "done" }}>
          {week.label}
        </PeopleChip>
      </div>
      <dl class="bs-signals">
        <Signal icon="users" label="Mentors" alert={!mentors.length} data="mentor">
          {mentors.map((m) => m.name).join(", ") || "No mentor assigned"}
        </Signal>
        <Signal icon="inbox" label="Open requests" alert={open > 0} data="requests">
          {open ? `${open} open ${open === 1 ? "request" : "requests"}` : "No open requests"}
        </Signal>
        <Signal icon="clock" label="Last activity" data="activity">
          {last ? `Active ${daysAgo(last, b.today).toLowerCase()}` : "No activity yet"}
          {absent && <span class="bs-signal-note"> · absence logged</span>}
        </Signal>
      </dl>
    </CardShell>
  );
}

/** Up to four builder avatars, then "+n". */
function Faces({ people }: { people: Person[] }) {
  if (!people.length) return null;
  return (
    <span class="bs-faces" aria-hidden="true">
      {people.slice(0, 4).map((x) => (
        <Avatar key={x.id} name={x.name} id={x.id} size="sm" />
      ))}
      {people.length > 4 && <span class="bs-faces-more">+{people.length - 4}</span>}
    </span>
  );
}

export function ProjectCard({ p, b, readOnly = false, scope = "", onPerson }: { p: BuildspaceProject; b: BuildspaceData; readOnly?: boolean; scope?: string; onPerson: (id: string) => void }) {
  const people = projectPeople(p, b);
  const mentors = projectMentors(p, b);
  const awaiting = people.filter((x) => missed(x, b)).length;
  const open = openCount(b, p.id);
  const last = lastActivity(b, p.id);
  const openProject = () => pushEntry({ project: p.id });
  return (
    <CardShell
      hue={TRACK_HUE[p.track]}
      flip={scope + p.id}
      data={{ "data-track": p.track, "data-project-card": p.id }}
      onOpen={openProject}
      menuLabel={`Actions for ${p.name}`}
      actions={[
        { label: "Open project", onSelect: openProject },
        ...(!readOnly ? [{ label: "Edit project", onSelect: () => pushEntry({ project: p.id, edit: "project" }) }] : []),
      ]}
    >
      <header class="bs-card-id">
        <span class="bs-mark" aria-hidden="true">
          <Icon name="rocket" size={20} />
        </span>
        <div class="bs-card-name">
          <h3>
            <button type="button" class="person-name bs-title" onClick={openProject}>
              {p.name}
            </button>
          </h3>
          <span class="person-sub">{p.track}</span>
        </div>
      </header>
      {p.description && <p class="bs-card-desc">{p.description}</p>}
      <div class="people-chips bs-card-chips">
        <PeopleChip hue={STAGE_HUE[p.stage]}>{p.stage}</PeopleChip>
        {awaiting > 0 && <PeopleChip hue="amber">{awaiting} awaiting check-in</PeopleChip>}
      </div>
      <dl class="bs-signals">
        <Signal icon="user" label="Builders" data="builders">
          <Faces people={people} />
          <span class="bs-signal-list">
            {people.length
              ? people.map((x, i) => (
                  <Fragment key={x.id}>
                    {i > 0 && ", "}
                    <button type="button" class="bs-link" onClick={() => onPerson(x.id)}>
                      {x.name}
                    </button>
                  </Fragment>
                ))
              : "No builders yet"}
          </span>
        </Signal>
        <Signal icon="users" label="Mentors" alert={!mentors.length} data="mentor">
          {mentors.map((m) => m.name).join(", ") || "No mentor assigned"}
        </Signal>
        <Signal icon="inbox" label="Open requests" alert={open > 0} data="requests">
          {`${open} open ${open === 1 ? "request" : "requests"}`}
        </Signal>
        <Signal icon="clock" label="Last activity" data="activity">
          {last ? `Active ${daysAgo(last, b.today).toLowerCase()}` : "No activity yet"}
        </Signal>
      </dl>
    </CardShell>
  );
}

export function MentorCard({ m, b, projects, scope }: { m: Mentor; b: BuildspaceData; projects: BuildspaceProject[]; scope: string }) {
  // Builders on the projects this card lists (the group's and filters' projects), not every project.
  const builders = b.people.filter((x) => projects.some((p) => p.id === x.projectId)).length;
  const contact = `/contacts?person=${encodeURIComponent(m.id)}`;
  return (
    <CardShell
      hue={personHue(m.id)}
      flip={scope + m.id}
      data={{ "data-mentor-card": m.id }}
      onOpen={() => navigate(contact)}
      menuLabel={`Actions for ${m.name}`}
      actions={[{ label: "Open mentor contact", onSelect: () => navigate(contact) }]}
    >
      <header class="bs-card-id">
        <Avatar name={m.name} id={m.id} size="lg" />
        <div class="bs-card-name">
          <h3>
            <a class="person-name" href={contact}>
              {m.name}
            </a>
          </h3>
          <span class="person-sub">{m.org || "Mentor"}</span>
        </div>
      </header>
      {m.topics.length > 0 && (
        <div class="people-chips bs-card-chips">
          {m.topics.map((t) => (
            <PeopleChip key={t} ghost>
              {t}
            </PeopleChip>
          ))}
        </div>
      )}
      <dl class="bs-signals">
        <Signal icon="rocket" label="Projects" data="projects">
          <span class="bs-signal-list">
            {projects.length
              ? projects.map((p, i) => (
                  <Fragment key={p.id}>
                    {i > 0 && ", "}
                    <button type="button" class="bs-link" onClick={() => pushEntry({ project: p.id })}>
                      {p.name}
                    </button>
                  </Fragment>
                ))
              : "No projects assigned"}
          </span>
        </Signal>
        <Signal icon="user" label="Builders" data="builders">
          {builders === 1 ? "1 builder" : `${builders} builders`}
        </Signal>
      </dl>
    </CardShell>
  );
}

/** A group heading in the People style (collapse chevron, title, count), then the card grid. */
export function CardGroup({ id, title, count, children }: { id: string; title: string; count: number; children: ComponentChildren }) {
  const [open, setOpen] = useState(true);
  const head = `bs-group-${id.replace(/[^a-z0-9-]/gi, "-")}`;
  return (
    <section class="bs-group" aria-labelledby={head} data-group={id}>
      <div class="people-group-head bs-group-head">
        <button type="button" class="people-collapse" aria-expanded={open} aria-controls={`${head}-cards`} aria-label={`${open ? "Collapse" : "Expand"} ${title}`} onClick={() => setOpen(!open)}>
          <Icon name="chevron-down" size={14} />
        </button>
        <h3 id={head}>{title}</h3>
        <span class="people-count">{count}</span>
      </div>
      <div id={`${head}-cards`} class="bs-cards motion-list" hidden={!open}>
        {children}
      </div>
    </section>
  );
}
