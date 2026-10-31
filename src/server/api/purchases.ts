import { and, eq, sql, inArray } from 'drizzle-orm';
import * as s from '../db/schema';
import * as C from '../../core/contracts';
import type { PurchaseStatus } from '../../core/domain';
import { canTransition } from '../../core/purchase-lifecycle';
import { postEntry, releaseRestriction } from '../../core/accounting/engine';
import { journalWrites } from './accounting';
import { fiscalYearFor } from '../../core/fiscal';
import { getFileStore, getNotifier } from '../services';
import { validateUpload } from '../files/drive';
import {
  type ApiContext,
  type BatchItem,
  found,
  fail,
  director,
  officer,
  uuid,
  now,
  mutate,
  page,
} from './context';
import { catalog, requestDetail, requestItem } from './read-model';
type RequestRow = typeof s.purchaseRequests.$inferSelect;
export async function validateCategory(
  c: ApiContext,
  r: { expenseAccountId: string; fundId: string; eventId?: string | null },
) {
  const account = await c.db
    .select()
    .from(s.accounts)
    .where(eq(s.accounts.id, r.expenseAccountId))
    .get();
  if (!account?.active || !account.budgetable || account.type !== 'expense')
    fail(400, 'bad_request', 'Select an active budgetable expense account');
  const fund = await c.db
    .select()
    .from(s.funds)
    .where(eq(s.funds.id, r.fundId))
    .get();
  if (!fund?.active) fail(400, 'bad_request', 'Select an active fund');
  if (r.eventId)
    found(
      await c.db
        .select()
        .from(s.events)
        .where(eq(s.events.id, r.eventId))
        .get(),
    );
}
export async function settlementWrites(
  c: ApiContext,
  r: RequestRow,
  settlement: C.PurchaseSettlement,
): Promise<BatchItem[]> {
  const receipts = settlement.attachmentIds.length
    ? await c.db
        .select()
        .from(s.attachments)
        .where(inArray(s.attachments.id, settlement.attachmentIds))
    : [];
  if (
    new Set(settlement.attachmentIds).size !== receipts.length ||
    receipts.some((a) => a.requestId !== r.id || a.journalEntryId)
  )
    fail(
      400,
      'bad_request',
      'Receipts must belong to this request and be unposted',
    );
  // A genuinely free order has no accounting movement; zero lines cannot be posted.
  if (settlement.actualTotalCents === 0) return [];
  const [accounts, funds, periods] = await Promise.all([
    c.db.select().from(s.accounts),
    c.db.select().from(s.funds),
    c.db.select().from(s.fiscalPeriods),
  ]);
  const fund = found(funds.find((f) => f.id === r.fundId));
  const postingDate = now().toISOString().slice(0, 10);
  if (
    fund.restriction === 'with_donor' &&
    ((fund.startDate && postingDate < fund.startDate) ||
      (fund.endDate && postingDate > fund.endDate))
  )
    fail(
      409,
      'conflict',
      'Restricted fund is outside its spending dates; review the fund allocation',
    );
  const credit = found(
    accounts.find(
      (a) => a.code === (r.type === 'reimbursement' ? '2100' : '1000'),
    ),
  );
  const entryId = uuid(),
    date = now().toISOString().slice(0, 10);
  const entry = postEntry(
    {
      id: entryId,
      entryDate: date,
      memo: `${r.number}: ${r.title}${settlement.orderReference ? ` (${settlement.orderReference})` : ''}`,
      source: 'purchase_request',
      lines: [
        {
          accountId: r.expenseAccountId,
          fundId: r.fundId,
          functionalClass: r.functionalClass,
          debitCents: settlement.actualTotalCents,
          creditCents: 0,
        },
        {
          accountId: credit.id,
          fundId: r.fundId,
          functionalClass: null,
          debitCents: 0,
          creditCents: settlement.actualTotalCents,
        },
      ],
    },
    { accounts, funds, periods },
  );
  const writes: BatchItem[] = journalWrites(c, entry, {
    purchaseRequestId: r.id,
    settlementKey: r.id,
  });
  if (funds.find((f) => f.id === r.fundId)?.restriction === 'with_donor') {
    const availableQuery = sql`(SELECT coalesce(sum(l.credit_cents-l.debit_cents),0) FROM journal_lines l JOIN journal_entries e ON e.id=l.entry_id JOIN accounts a ON a.id=l.account_id WHERE e.status='posted' AND l.fund_id=${r.fundId} AND ((a.type='revenue' AND NOT (coalesce(a.form990_part,'')='VIII' AND coalesce(a.form990_line,'') LIKE '2%')) OR (a.type='net_asset' AND a.restriction='with_donor')))`;
    const available = await c.db.get<{ cents: number }>(
      sql`SELECT ${availableQuery} AS cents`,
    );
    const release = releaseRestriction(
      {
        id: uuid(),
        entryDate: date,
        memo: `Release for ${r.number}: satisfied purchase purpose`,
        amountCents: settlement.actualTotalCents,
        fundId: r.fundId,
        restrictedNetAssetAccountId: found(
          accounts.find(
            (a) => a.type === 'net_asset' && a.restriction === 'with_donor',
          ),
        ).id,
        unrestrictedNetAssetAccountId: found(
          accounts.find(
            (a) => a.type === 'net_asset' && a.restriction === 'without_donor',
          ),
        ).id,
        availableRestrictedCents: available?.cents ?? 0,
      },
      { accounts, funds, periods },
    );
    const guardId = uuid();
    writes.unshift(
      c.db.insert(s.mutationGuards).values({
        id: guardId,
        ok: sql`CASE WHEN ${availableQuery} >= ${settlement.actualTotalCents} THEN 1 ELSE 0 END`,
      }),
    );
    writes.push(
      ...journalWrites(c, release, { purchaseRequestId: r.id }),
      c.db.delete(s.mutationGuards).where(eq(s.mutationGuards.id, guardId)),
    );
  }
  if (receipts.length)
    writes.push(
      c.db
        .update(s.attachments)
        .set({ journalEntryId: entryId })
        .where(
          inArray(
            s.attachments.id,
            receipts.map((a) => a.id),
          ),
        ),
    );
  return writes;
}
export const unchanged = (r: RequestRow) =>
  sql`EXISTS (SELECT 1 FROM purchase_requests WHERE id = ${r.id} AND status = ${r.status} AND updated_at = ${r.updatedAt.getTime()})`;
async function notify(
  c: ApiContext,
  r: RequestRow,
  action: string,
  comment?: string,
) {
  const common = { requestId: r.id, number: r.number, title: r.title };
  const requester = found(
    await c.db
      .select()
      .from(s.users)
      .where(eq(s.users.id, r.requesterId))
      .get(),
  );
  const event =
    action === 'submit'
      ? {
          ...common,
          type: 'request.submitted' as const,
          totalCents: r.totalCents,
          requester: requester.name,
        }
      : action === 'approve' || action === 'deny'
        ? {
            ...common,
            type:
              action === 'approve'
                ? ('request.approved' as const)
                : ('request.denied' as const),
            director: c.user?.name ?? 'Purchasing agent',
            comment,
          }
        : { ...common, type: 'request.status' as const, status: r.status };
  await getNotifier().notify(
    {
      ...event,
      totalCents: r.actualTotalCents ?? r.totalCents,
      requester: requester.name,
    },
    `${c.url.origin}/requests/${r.id}`,
  );
}
export async function purchases(
  c: ApiContext,
  name: string,
  id: string | undefined,
  body: unknown,
  query: unknown,
): Promise<unknown> {
  const u = officer(c);
  if (name === 'requestsList') {
    const q = C.PurchaseRequestsListQuery.parse(query);
    const cat = await catalog(c.db);
    let rows = await c.db.select().from(s.purchaseRequests);
    rows = rows.filter(
      (r) =>
        (q.scope !== 'mine' || r.requesterId === u.id) &&
        (!q.eventId || r.eventId === q.eventId) &&
        (!q.q ||
          `${r.title} ${r.vendor} ${r.number}`
            .toLowerCase()
            .includes(q.q.toLowerCase())),
    );
    const counts: Record<string, number> = {};
    for (const r of rows) counts[r.status] = (counts[r.status] ?? 0) + 1;
    rows = rows.filter(
      (r) =>
        !q.status ||
        r.status === (q.status === 'pending' ? 'submitted' : q.status),
    );
    return {
      ...page(
        rows.map((r) => requestItem(r, cat)),
        q,
      ),
      counts,
    };
  }
  if (name === 'requestCreate') {
    const { submit, ...b } = C.PurchaseRequestCreateRequest.parse(body);
    await validateCategory(c, b);
    const id = uuid();
    const totalCents = b.quantity * b.unitCostCents + b.taxShipCents;
    if (!Number.isSafeInteger(totalCents))
      fail(400, 'bad_request', 'Total exceeds safe integer cents');
    const value = {
      ...b,
      id,
      number: `${now().getUTCFullYear()}-${id.slice(0, 8).toUpperCase()}`,
      requesterId: u.id,
      totalCents,
      status: submit ? ('submitted' as const) : ('draft' as const),
      submittedAt: submit ? now() : null,
    };
    await mutate(
      c,
      sql`1`,
      [c.db.insert(s.purchaseRequests).values(value)],
      submit ? 'request.created_submitted' : 'request.created',
      'purchase_request',
      id,
      null,
      value,
    );
    const r = found(
      await c.db
        .select()
        .from(s.purchaseRequests)
        .where(eq(s.purchaseRequests.id, id))
        .get(),
    );
    if (submit) await notify(c, r, 'submit');
    return { request: await requestDetail(c.db, id, u) };
  }
  const r = found(
    await c.db
      .select()
      .from(s.purchaseRequests)
      .where(eq(s.purchaseRequests.id, found(id)))
      .get(),
  );
  if (name === 'requestDetail')
    return { request: await requestDetail(c.db, r.id, u) };
  if (name === 'requestUpdate' || name === 'requestDelete') {
    if (u.id !== r.requesterId)
      fail(403, 'forbidden', 'Only the requester may edit or delete');
    if (r.status !== 'draft')
      fail(409, 'conflict', 'Only drafts may be edited or deleted');
    if (name === 'requestDelete') {
      const attached = await c.db
        .select()
        .from(s.attachments)
        .where(eq(s.attachments.requestId, r.id));
      await mutate(
        c,
        unchanged(r),
        [
          c.db.delete(s.attachments).where(eq(s.attachments.requestId, r.id)),
          c.db
            .delete(s.purchaseRequests)
            .where(eq(s.purchaseRequests.id, r.id)),
        ],
        'request.deleted',
        'purchase_request',
        r.id,
        { ...r, attachments: attached },
        null,
      );
      return { ok: true };
    }
    const b = C.PurchaseRequestUpdateRequest.parse(body),
      next = { ...r, ...b };
    await validateCategory(c, next);
    const totalCents = next.quantity * next.unitCostCents + next.taxShipCents;
    if (!Number.isSafeInteger(totalCents))
      fail(400, 'bad_request', 'Total exceeds safe integer cents');
    await mutate(
      c,
      unchanged(r),
      [
        c.db
          .update(s.purchaseRequests)
          .set({ ...b, totalCents, updatedAt: now() })
          .where(eq(s.purchaseRequests.id, r.id)),
      ],
      'request.updated',
      'purchase_request',
      r.id,
      r,
      { ...b, totalCents },
    );
  } else if (name === 'requestAttach') {
    if (u.id !== r.requesterId && u.role !== 'director')
      fail(403, 'forbidden', 'Only requester or director may upload');
    if (['closed', 'cancelled', 'denied'].includes(r.status))
      fail(409, 'conflict', 'Request no longer accepts attachments');
    const form = await c.request.formData(),
      b = C.AttachmentUploadFields.parse(Object.fromEntries(form));
    const file = form.get('file');
    if (!(file instanceof File)) fail(400, 'bad_request', 'File required');
    validateUpload(file.type, file.size);
    const aId = uuid();
    const stored = await getFileStore().put({
      folder: [
        `FY${fiscalYearFor((r.submittedAt ?? r.createdAt).toISOString().slice(0, 10))}`,
        r.id,
      ],
      filename: file.name,
      mimeType: file.type,
      body: await file.arrayBuffer(),
    });
    const value = {
      id: aId,
      requestId: r.id,
      kind: b.kind,
      filename: file.name,
      mimeType: file.type,
      sizeBytes: stored.sizeBytes,
      storage: stored.storage,
      storageKey: stored.key,
      webViewLink: stored.webViewLink,
      uploadedBy: u.id,
    };
    await mutate(
      c,
      unchanged(r),
      [c.db.insert(s.attachments).values(value)],
      'request.attachment',
      'attachment',
      aId,
      null,
      value,
    );
    const detail = await requestDetail(c.db, r.id, u);
    return { attachment: detail.attachments.find((a) => a.id === aId) };
  } else {
    let to: PurchaseStatus;
    let comment: string | undefined;
    const writes: BatchItem[] = [];
    let settlement: C.PurchaseSettlement | undefined;
    if (name === 'requestSubmit') {
      if (r.requesterId !== u.id)
        fail(403, 'forbidden', 'Only requester may submit');
      to = 'submitted';
    } else {
      director(c);
      if (name === 'requestApprove' || name === 'requestDeny') {
        const b =
          name === 'requestDeny'
            ? C.PurchaseDenyRequest.parse(body)
            : C.PurchaseDecisionRequest.parse(body);
        comment = b.comment;
        to = name === 'requestApprove' ? 'approved' : 'denied';
        if (to === 'approved' && r.requesterId === u.id)
          fail(403, 'forbidden', 'You cannot approve your own request');
        writes.push(
          c.db.insert(s.approvals).values({
            id: uuid(),
            requestId: r.id,
            directorId: u.id,
            decision: to,
            comment,
          }),
        );
        if (to === 'approved' && r.type === 'club_purchase')
          writes.push(
            c.db.insert(s.fulfillmentJobs).values({
              id: uuid(),
              requestId: r.id,
              payloadJson: JSON.stringify({
                vendor: r.vendor,
                itemUrl: r.itemUrl,
                quantity: r.quantity,
                totalCents: r.totalCents,
              }),
            }),
          );
      } else {
        const b = C.PurchaseTransitionRequest.parse(body);
        to = b.to;
        comment = b.comment;
        if (b.to === 'fulfilled' || b.to === 'reimbursed') settlement = b;
      }
    }
    if (!canTransition(r.type, r.status, to))
      fail(409, 'invalid_transition', `Cannot move ${r.status} to ${to}`);
    if (settlement) {
      try {
        writes.push(...(await settlementWrites(c, r, settlement)));
      } catch (e) {
        if (
          e instanceof Error &&
          /open period|inactive|functional|unsafe/.test(e.message)
        )
          fail(409, 'conflict', e.message);
        throw e;
      }
    }
    if (
      to === 'closed' &&
      r.type === 'reimbursement' &&
      (r.actualTotalCents ?? 0) > 0
    ) {
      const [accounts, funds, periods] = await Promise.all([
        c.db.select().from(s.accounts),
        c.db.select().from(s.funds),
        c.db.select().from(s.fiscalPeriods),
      ]);
      const payout = postEntry(
        {
          id: uuid(),
          entryDate: now().toISOString().slice(0, 10),
          memo: `Reimbursement paid: ${r.number}`,
          source: 'purchase_request',
          lines: [
            {
              accountId: found(accounts.find((a) => a.code === '2100')).id,
              fundId: r.fundId,
              debitCents: r.actualTotalCents!,
              creditCents: 0,
            },
            {
              accountId: found(accounts.find((a) => a.code === '1000')).id,
              fundId: r.fundId,
              debitCents: 0,
              creditCents: r.actualTotalCents!,
            },
          ],
        },
        { accounts, funds, periods },
      );
      writes.push(...journalWrites(c, payout, { purchaseRequestId: r.id }));
    }
    if (to === 'cancelled')
      writes.push(
        c.db
          .update(s.fulfillmentJobs)
          .set({ status: 'cancelled', updatedAt: now() })
          .where(
            and(
              eq(s.fulfillmentJobs.requestId, r.id),
              inArray(s.fulfillmentJobs.status, ['queued', 'claimed']),
            ),
          ),
      );
    if (to === 'fulfilled')
      writes.push(
        c.db
          .update(s.fulfillmentJobs)
          .set({ status: 'done', updatedAt: now() })
          .where(
            and(
              eq(s.fulfillmentJobs.requestId, r.id),
              inArray(s.fulfillmentJobs.status, ['queued', 'claimed']),
            ),
          ),
      );
    const change = {
      status: to,
      updatedAt: now(),
      ...(to === 'submitted' ? { submittedAt: now() } : {}),
      ...(['approved', 'denied'].includes(to) ? { decidedAt: now() } : {}),
      ...(settlement ? { actualTotalCents: settlement.actualTotalCents } : {}),
    };
    writes.push(
      c.db
        .update(s.purchaseRequests)
        .set(change)
        .where(eq(s.purchaseRequests.id, r.id)),
    );
    await mutate(
      c,
      unchanged(r),
      writes,
      `request.${to}`,
      'purchase_request',
      r.id,
      r,
      {
        ...change,
        comment,
        ...(to === 'approved'
          ? {
              encumbranceMemo: `Encumber ${r.totalCents} cents to ${r.expenseAccountId}/${r.fundId}`,
            }
          : {}),
      },
    );
    if (
      ['submitted', 'approved', 'denied', 'fulfilled', 'reimbursed'].includes(
        to,
      )
    )
      await notify(
        c,
        { ...r, ...change },
        to === 'submitted'
          ? 'submit'
          : to === 'approved'
            ? 'approve'
            : to === 'denied'
              ? 'deny'
              : 'status',
        comment,
      );
  }
  return { request: await requestDetail(c.db, r.id, u) };
}
