import { isRegistered } from '../../core/luma/status';
import { eq } from 'drizzle-orm';
import * as s from '../db/schema';
import type { Db } from '../db/client';
import type { CurrentUser } from '../../core/contracts';
import { eventMetrics, matchReflections } from '../../core/metrics';
import { nextStatuses } from '../../core/purchase-lifecycle';
import { found, iso } from './context';
export const userSummary = (u: s.UserRow) => ({
  id: u.id,
  name: u.name,
  avatarUrl: u.avatarUrl,
});
export async function catalog(db: Db) {
  const [users, accounts, funds, events] = await Promise.all([
    db.select().from(s.users),
    db.select().from(s.accounts),
    db.select().from(s.funds),
    db.select().from(s.events),
  ]);
  return { users, accounts, funds, events };
}
export async function metrics(db: Db, eventId: string) {
  const [guests, reflections] = await Promise.all([
    db.select().from(s.eventGuests).where(eq(s.eventGuests.eventId, eventId)),
    db.select().from(s.reflections).where(eq(s.reflections.eventId, eventId)),
  ]);
  const checked = guests.filter((g) => g.checkedInAt);
  const matched = matchReflections({
    checkedInEmails: checked.map((g) => g.email),
    reflectionEmails: reflections.map((r) => r.email),
  });
  return eventMetrics({
    registered: guests.filter((g) =>
      isRegistered(g.approvalStatus, g.checkedInAt),
    ).length,
    checkedIn: checked.length,
    reflections: reflections.length,
    matchedReflections: matched.matched,
  });
}
export async function eventItem(
  db: Db,
  e: typeof s.events.$inferSelect,
  cat?: Awaited<ReturnType<typeof catalog>>,
) {
  cat ??= await catalog(db);
  return {
    id: e.id,
    lumaId: e.lumaId,
    name: e.name,
    startsAt: e.startsAt,
    location: e.location,
    host: cat.users.find((u) => u.id === e.hostUserId) ?? null,
    metrics: await metrics(db, e.id),
  };
}
export function requestItem(
  r: typeof s.purchaseRequests.$inferSelect,
  cat: Awaited<ReturnType<typeof catalog>>,
) {
  return {
    ...r,
    requester: found(cat.users.find((u) => u.id === r.requesterId)),
    expenseAccount: found(
      cat.accounts.find((a) => a.id === r.expenseAccountId),
    ),
    fund: found(cat.funds.find((f) => f.id === r.fundId)),
    submittedAt: iso(r.submittedAt),
    updatedAt: r.updatedAt.toISOString(),
  };
}
export async function requestDetail(
  db: Db,
  id: string,
  user: CurrentUser | null,
) {
  const r = found(
    await db
      .select()
      .from(s.purchaseRequests)
      .where(eq(s.purchaseRequests.id, id))
      .get(),
  );
  const cat = await catalog(db);
  const [approvals, attachments, entries] = await Promise.all([
    db.select().from(s.approvals).where(eq(s.approvals.requestId, id)),
    db.select().from(s.attachments).where(eq(s.attachments.requestId, id)),
    db
      .select()
      .from(s.journalEntries)
      .where(eq(s.journalEntries.purchaseRequestId, id)),
  ]);
  const canEdit = user?.id === r.requesterId && r.status === 'draft';
  const allowedTransitions = nextStatuses(r.type, r.status).filter((to) => {
    if (to === 'submitted') return user?.id === r.requesterId;
    if (to === 'approved')
      return user?.role === 'director' && user.id !== r.requesterId;
    return user?.role === 'director';
  });
  return {
    ...requestItem(r, cat),
    event: cat.events.find((e) => e.id === r.eventId) ?? null,
    decidedAt: iso(r.decidedAt),
    createdAt: r.createdAt.toISOString(),
    approvals: approvals.map((a) => ({
      ...a,
      director: found(cat.users.find((u) => u.id === a.directorId)),
      createdAt: a.createdAt.toISOString(),
    })),
    attachments: attachments.map((a) => ({
      ...a,
      url: a.webViewLink ?? `/api/attachments/${a.id}`,
      uploadedBy: found(cat.users.find((u) => u.id === a.uploadedBy)),
      createdAt: a.createdAt.toISOString(),
    })),
    journalEntryIds: entries
      .filter((e) => e.status === 'posted')
      .map((e) => e.id),
    allowedTransitions,
    canEdit,
  };
}
