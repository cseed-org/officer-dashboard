import type {
  BuildspaceActivity,
  BuildspaceData,
  BuildspacePerson,
  BuildspaceProject,
  LaunchpadMentor,
} from "../../../lib/contracts";
export const openActivity = (a: BuildspaceActivity) =>
  ["request", "purchase"].includes(a.kind) &&
  (a.purchaseStatus
    ? !["denied", "cancelled", "fulfilled", "reimbursed", "closed"].includes(
        a.purchaseStatus,
      )
    : a.status === "open");

interface BuildspaceIndex {
  projectsById: Map<string, BuildspaceProject>;
  peopleByProject: Map<string, BuildspacePerson[]>;
  mentorsByProject: Map<string, LaunchpadMentor[]>;
  activityByProject: Map<string, BuildspaceActivity[]>;
  activityByPerson: Map<string, BuildspaceActivity[]>;
  checkedInPeople: Set<string>;
  cohortArchived: boolean;
}
const indexes = new WeakMap<BuildspaceData, BuildspaceIndex>();

/** Index a register response once; a reload supplies a new object and therefore a fresh index. */
export function buildspaceIndex(b: BuildspaceData): BuildspaceIndex {
  const cached = indexes.get(b);
  if (cached) return cached;
  const projects = b.projects;
  const projectsById = new Map(projects.map((p) => [p.id, p]));
  const peopleByProject = new Map<string, BuildspacePerson[]>();
  for (const person of b.people) {
    const bucket = peopleByProject.get(person.projectId);
    if (bucket) bucket.push(person);
    else peopleByProject.set(person.projectId, [person]);
  }
  const mentorsByProject = new Map<string, LaunchpadMentor[]>();
  const mentorProjects = new Map<string, string[]>();
  for (const project of projects) {
    mentorsByProject.set(project.id, []);
    for (const id of project.mentorIds) {
      const assigned = mentorProjects.get(id);
      if (assigned) assigned.push(project.id);
      else mentorProjects.set(id, [project.id]);
    }
  }
  // Keep the response's mentor order, not the project's mentorIds order.
  for (const mentor of b.mentors) for (const projectId of mentorProjects.get(mentor.id) ?? []) mentorsByProject.get(projectId)!.push(mentor);
  const activityByProject = new Map<string, BuildspaceActivity[]>();
  const activityByPerson = new Map<string, BuildspaceActivity[]>();
  const checkedInPeople = new Set<string>();
  for (const activity of b.activity) {
    const project = activityByProject.get(activity.projectId);
    if (project) project.push(activity);
    else activityByProject.set(activity.projectId, [activity]);
    if (activity.personId) {
      const person = activityByPerson.get(activity.personId);
      if (person) person.push(activity);
      else activityByPerson.set(activity.personId, [activity]);
      if (activity.kind === "weekly" && activity.date >= b.weekStart && activity.date <= b.today) checkedInPeople.add(activity.personId);
    }
  }
  const index = {
    projectsById,
    peopleByProject,
    mentorsByProject,
    activityByProject,
    activityByPerson,
    checkedInPeople,
    cohortArchived: b.cohorts.find((c) => c.id === b.cohortId)?.status === "archived",
  };
  indexes.set(b, index);
  return index;
}

/** Archived cohorts are read-only history: nobody in them is waiting on a check-in. */
export const missed = (p: BuildspacePerson, b: BuildspaceData) => {
  const index = buildspaceIndex(b);
  return p.archivedAt === null && !index.cohortArchived && p.joinedOn <= b.today && !index.checkedInPeople.has(p.id);
};
export const projectPeople = (p: BuildspaceProject, b: BuildspaceData) => buildspaceIndex(b).peopleByProject.get(p.id) ?? [];
export const projectMentors = (p: BuildspaceProject, b: BuildspaceData) => buildspaceIndex(b).mentorsByProject.get(p.id) ?? [];
/** Search text deliberately excludes activity; attention flags use the indexed activity separately. */
export const projectSearchText = (p: BuildspaceProject, b: BuildspaceData, personId?: string) => [
  p.name,
  p.description,
  ...projectPeople(p, b).filter((x) => !personId || x.id === personId).map((x) => x.name),
  ...projectMentors(p, b).map((x) => x.name),
].join(" ").toLowerCase();
/** Person-scoped activity includes project-wide entries, in the register response's order. */
export const projectActivities = (projectId: string, b: BuildspaceData, personId?: string) =>
  (buildspaceIndex(b).activityByProject.get(projectId) ?? []).filter((a) => !personId || !a.personId || a.personId === personId);
export const projectById = (id: string, b: BuildspaceData) => buildspaceIndex(b).projectsById.get(id);
export const absenceThisWeek = (p: BuildspacePerson, b: BuildspaceData) =>
  (buildspaceIndex(b).activityByPerson.get(p.id) ?? []).some((a) => a.kind === "absence" && a.date >= b.weekStart && a.date <= b.today);
export const trackShort = (s: string) => (s.startsWith("Extended") ? "XR" : s);
export const kindLabel = {
  weekly: "Weekly check-in",
  mentor: "Mentor check-in",
  request: "General request",
  purchase: "Purchase",
  absence: "Absence",
};
/** Who can read an entry outside the officer dashboard (officers always see everything). */
export const VISIBILITY_LABEL = {
  officers: "Officers only",
  mentors: "Mentors + officers",
  team: "Team, mentors + officers",
} as const;
