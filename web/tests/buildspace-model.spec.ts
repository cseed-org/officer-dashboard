import { expect, test } from '@playwright/test';
import type { BuildspaceActivity, BuildspaceData } from '../src/lib/contracts';
import {
  missed,
  openActivity,
  projectActivities,
  projectMentors,
  projectPeople,
  projectSearchText,
} from '../src/ui/views/buildspace/model';

test.beforeEach(({}, info) => test.skip(info.project.name !== 'desktop', 'pure model tests run once'));

/** Explicit large cohort: 200 builders, 100 two-person projects, 10 mentors, 10,000 entries. */
function largeCohort(): BuildspaceData {
  const mentors = Array.from({ length: 10 }, (_, i) => ({ id: `m${i}`, name: `Mentor ${i}` })) as BuildspaceData['mentors'];
  const projects = Array.from({ length: 100 }, (_, i) => ({
    id: `pr${i}`, name: `Project ${i}`, description: `Cohort project ${i}`, mentorIds: [`m${i % 10}`], archivedAt: null,
  })) as BuildspaceData['projects'];
  const people = Array.from({ length: 200 }, (_, i) => ({
    id: `p${i}`, projectId: `pr${Math.floor(i / 2)}`, name: `Builder ${i}`, joinedOn: i % 25 === 24 ? '2027-01-01' : '2026-01-01', archivedAt: i % 20 === 19 ? 1 : null,
  })) as BuildspaceData['people'];
  const activity = people.flatMap((person, i) => Array.from({ length: 50 }, (_, j) => ({
    id: `a${i}-${j}`, projectId: person.projectId, personId: j === 0 ? null : person.id,
    kind: j === 0 || j % 4 === 1 ? 'request' : j % 4 === 0 ? 'absence' : j % 4 === 2 ? 'purchase' : i % 7 === 0 ? 'request' : 'weekly',
    date: '2026-11-18', status: 'open', purchaseStatus: null,
  }))) as BuildspaceActivity[];
  return {
    projects, people, mentors, activity,
    cohortId: 'c1', cohorts: [{ id: 'c1', status: 'active' }],
    weekStart: '2026-11-16', weekDue: '2026-11-22', today: '2026-11-18',
  } as unknown as BuildspaceData;
}

test('M-031: indexed aggregates match the original person and project filters on a large cohort', () => {
  const b = largeCohort();
  const legacyMissed = (person: BuildspaceData['people'][number]) => person.archivedAt === null &&
    b.cohorts.find((c) => c.id === b.cohortId)?.status !== 'archived' && person.joinedOn <= b.today &&
    !b.activity.some((a) => a.kind === 'weekly' && a.personId === person.id && a.date >= b.weekStart && a.date <= b.today);
  for (const person of b.people) {
    expect(missed(person, b), person.id).toBe(legacyMissed(person));
    const expected = b.activity.filter((a) => a.projectId === person.projectId && (!a.personId || a.personId === person.id));
    expect(projectActivities(person.projectId, b, person.id), person.id).toEqual(expected);
    // Project-wide requests count for this person; another person's entries do not.
    expect(projectActivities(person.projectId, b, person.id).some((a) => a.personId === null && openActivity(a))).toBe(true);
  }
  for (const project of b.projects) {
    expect(projectPeople(project, b)).toEqual(b.people.filter((p) => p.projectId === project.id));
    expect(projectMentors(project, b)).toEqual(b.mentors.filter((m) => project.mentorIds.includes(m.id)));
    expect(projectActivities(project.id, b)).toEqual(b.activity.filter((a) => a.projectId === project.id));
  }
  const first = b.people[1]!;
  expect(missed(first, b)).toBe(false);
  const reloaded = { ...b, activity: b.activity.filter((a) => a.personId !== first.id || a.kind !== 'weekly') };
  expect(missed(first, reloaded), 'a replacement register response gets a fresh index').toBe(true);
});

test('M-031 benchmark: no-match searches and drawer opens read each large-cohort array once', async () => {
  const reads = { activity: 0, people: 0, projects: 0, mentors: 0 };
  const raw = largeCohort();
  const { projects, people } = raw;
  const b = new Proxy(raw, {
    get(target, key, receiver) {
      if (key in reads) reads[key as keyof typeof reads]++;
      return Reflect.get(target, key, receiver);
    },
  });
  const started = performance.now();
  // Four search revisions and a drawer open for every builder. No attention flag is active.
  for (let round = 0; round < 4; round++) {
    for (const project of projects) expect(projectSearchText(project, b).includes('no-such-builder')).toBe(false);
    for (const person of people) projectActivities(person.projectId, b, person.id);
  }
  const elapsedMs = performance.now() - started;
  // Deterministic guard, independent of machine speed. CPU profile: unthrottled Node in CI.
  expect(reads).toEqual({ activity: 1, people: 1, projects: 1, mentors: 1 });
  await test.info().attach('buildspace-large-cohort.json', {
    contentType: 'application/json',
    body: JSON.stringify({ builders: 200, projects: 100, mentors: 10, activity: 10_000, searchRounds: 4, drawerOpens: 800, cpuProfile: 'unthrottled Node', elapsedMs, reads }),
  });
});
