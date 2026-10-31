import { eq } from 'drizzle-orm';
import type { PostedEntry } from '../../core/accounting/engine';
import * as s from '../db/schema';
import { type ApiContext, type BatchItem, uuid, now } from './context';
/** Caller submits these statements with the request/job mutation and audit in one D1 batch. */
export function journalWrites(
  c: ApiContext,
  entry: PostedEntry,
  links: { purchaseRequestId?: string; settlementKey?: string } = {},
): BatchItem[] {
  return [
    c.db.insert(s.journalEntries).values({
      id: entry.id,
      entryDate: entry.entryDate,
      memo: entry.memo,
      source: entry.source,
      reversesEntryId: entry.reversesEntryId,
      ...links,
      createdBy: c.user?.id ?? null,
    }),
    c.db.insert(s.journalLines).values(
      entry.lines.map((l, i) => ({
        ...l,
        id: uuid(),
        entryId: entry.id,
        lineNo: i + 1,
      })),
    ),
    c.db
      .update(s.journalEntries)
      .set({ status: 'posted', postedAt: now(), postedBy: c.user?.id ?? null })
      .where(eq(s.journalEntries.id, entry.id)),
  ];
}
