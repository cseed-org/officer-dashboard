import type {
  BuildspaceActivity,
  BuildspaceData,
  BuildspacePerson,
  BuildspaceProject,
} from "../../../lib/contracts";
export const openActivity = (a: BuildspaceActivity) =>
  ["request", "purchase"].includes(a.kind) &&
  (a.purchaseStatus
    ? !["denied", "fulfilled", "reimbursed", "closed"].includes(
        a.purchaseStatus,
      )
    : a.status === "open");
export const missed = (p: BuildspacePerson, b: BuildspaceData) =>
  p.joinedOn <= b.today &&
  !b.activity.some(
    (a) =>
      a.kind === "weekly" &&
      a.personId === p.id &&
      a.date >= b.weekStart &&
      a.date <= b.today,
  );
export const projectPeople = (p: BuildspaceProject, b: BuildspaceData) =>
  b.people.filter((x) => x.projectId === p.id);
export const projectMentors = (p: BuildspaceProject, b: BuildspaceData) =>
  b.mentors.filter((x) => p.mentorIds.includes(x.id));
export const trackShort = (s: string) => (s.startsWith("Extended") ? "XR" : s);
export const kindLabel = {
  weekly: "Weekly check-in",
  mentor: "Mentor check-in",
  request: "General request",
  purchase: "Purchase",
  absence: "Absence",
};
