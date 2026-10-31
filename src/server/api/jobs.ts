import { eq, sql } from 'drizzle-orm';
import * as s from '../db/schema';
import * as C from '../../core/contracts';
import {
  type ApiContext,
  type BatchItem,
  found,
  fail,
  now,
  mutate,
} from './context';
import { requestDetail } from './read-model';
import { settlementWrites, unchanged } from './purchases';
import { getNotifier } from '../services';
import { getEnv } from '../env';
import {
  maxAgentSettlementCents,
  settlementToleranceFromEnv,
} from '../../core/settlement';
import { formatCents } from '../../core/money';
async function jobView(
  c: ApiContext,
  j: typeof s.fulfillmentJobs.$inferSelect,
) {
  return {
    ...j,
    request: await requestDetail(c.db, j.requestId, null),
    createdAt: j.createdAt.toISOString(),
    claimedAt: j.claimedAt?.toISOString() ?? null,
  };
}
export async function jobs(
  c: ApiContext,
  name: string,
  id: string | undefined,
  body: unknown,
  query: unknown,
) {
  if (name === 'agentJobs') {
    const q = C.AgentJobsQuery.parse(query);
    return {
      jobs: await Promise.all(
        (
          await c.db
            .select()
            .from(s.fulfillmentJobs)
            .where(eq(s.fulfillmentJobs.status, q.status))
        ).map((j) => jobView(c, j)),
      ),
    };
  }
  const j = found(
    await c.db
      .select()
      .from(s.fulfillmentJobs)
      .where(eq(s.fulfillmentJobs.id, found(id)))
      .get(),
  );
  const r = found(
    await c.db
      .select()
      .from(s.purchaseRequests)
      .where(eq(s.purchaseRequests.id, j.requestId))
      .get(),
  );
  const writes: BatchItem[] = [];
  let change: Partial<typeof s.fulfillmentJobs.$inferInsert>;
  if (name === 'agentJobClaim') {
    if (
      j.status !== 'queued' ||
      r.status !== 'approved'
    )
      fail(409, 'conflict', 'Job is not available to claim');
    if (j.attempts >= 3)
      fail(
        409,
        'conflict',
        'Job reached three attempts; a director must resolve the request manually',
      );
    change = {
      status: 'claimed',
      claimedByTokenId: c.tokenId,
      claimedAt: now(),
      attempts: j.attempts + 1,
      updatedAt: now(),
    };
    writes.push(
      c.db
        .update(s.purchaseRequests)
        .set({ status: 'fulfilling', updatedAt: now() })
        .where(eq(s.purchaseRequests.id, r.id)),
    );
  } else {
    if (
      j.status !== 'claimed' ||
      j.claimedByTokenId !== c.tokenId ||
      r.status !== 'fulfilling'
    )
      fail(
        409,
        'conflict',
        'Only the token holding this active claim may complete it',
      );
    const b = C.AgentJobCompleteRequest.parse(body);
    if (b.outcome === 'done') {
      // N2: automation may not book more than the approved estimate plus tolerance.
      // Record the attempt on the still-claimed job and leave the request
      // `fulfilling` so a director settles it via /transition.
      const ceiling = maxAgentSettlementCents(
        r.totalCents,
        settlementToleranceFromEnv(getEnv()),
      );
      if (b.actualTotalCents > ceiling) {
        const message = `Actual ${formatCents(b.actualTotalCents)} exceeds the approved ${formatCents(r.totalCents)} plus tolerance (max ${formatCents(ceiling)}); held for director review`;
        // Terminal for the agent (M1): the claim is cleared and the job is out of
        // `queued`, so neither a later complete/failed nor a claim can re-queue it
        // and trigger a second purchase.
        const held = {
          status: 'held' as const,
          claimedByTokenId: null,
          claimedAt: null,
          lastError: message,
          resultJson: JSON.stringify(b),
          updatedAt: now(),
        };
        await mutate(
          c,
          sql`${unchanged(r)} AND EXISTS (SELECT 1 FROM fulfillment_jobs WHERE id = ${j.id} AND status = ${j.status} AND updated_at = ${j.updatedAt.getTime()})`,
          [
            c.db
              .update(s.fulfillmentJobs)
              .set(held)
              .where(eq(s.fulfillmentJobs.id, j.id)),
          ],
          'job.held_for_review',
          'fulfillment_job',
          j.id,
          j,
          held,
        );
        await getNotifier().notify(
          {
            type: 'request.held',
            requestId: r.id,
            number: r.number,
            title: r.title,
            attemptedCents: b.actualTotalCents,
            approvedCents: r.totalCents,
            reason: message,
          },
          `${c.url.origin}/requests/${r.id}`,
        );
        fail(409, 'conflict', message);
      }
    }
    change = {
      status: b.outcome === 'failed' && j.attempts < 3 ? 'queued' : b.outcome,
      ...(b.outcome === 'failed' ? { claimedByTokenId: null, claimedAt: null } : {}),
      lastError: b.outcome === 'failed' ? b.error : null,
      resultJson: JSON.stringify(b),
      updatedAt: now(),
    };
    if (b.outcome === 'done')
      writes.push(
        ...(await settlementWrites(c, r, { ...b, comment: b.notes })),
      );
    writes.push(
      c.db
        .update(s.purchaseRequests)
        .set({
          status: b.outcome === 'done' ? 'fulfilled' : 'approved',
          ...(b.outcome === 'done'
            ? { actualTotalCents: b.actualTotalCents }
            : {}),
          updatedAt: now(),
        })
        .where(eq(s.purchaseRequests.id, r.id)),
    );
  }
  writes.push(
    c.db
      .update(s.fulfillmentJobs)
      .set(change)
      .where(eq(s.fulfillmentJobs.id, j.id)),
    c.db
      .update(s.serviceTokens)
      .set({ lastUsedAt: now() })
      .where(eq(s.serviceTokens.id, c.tokenId!)),
  );
  await mutate(
    c,
    sql`${unchanged(r)} AND EXISTS (SELECT 1 FROM fulfillment_jobs WHERE id = ${j.id} AND status = ${j.status} AND updated_at = ${j.updatedAt.getTime()})`,
    writes,
    `job.${change.status}`,
    'fulfillment_job',
    j.id,
    j,
    change,
  );
  if (change.status === 'done')
    await getNotifier().notify(
      {
        type: 'request.status',
        requestId: r.id,
        number: r.number,
        title: r.title,
        status: 'fulfilled',
        totalCents:
          C.AgentJobCompleteRequest.parse(body).outcome === 'done'
            ? (body as { actualTotalCents: number }).actualTotalCents
            : r.totalCents,
        requester: found(
          await c.db
            .select()
            .from(s.users)
            .where(eq(s.users.id, r.requesterId))
            .get(),
        ).name,
      },
      `${c.url.origin}/requests/${r.id}`,
    );
  return {
    job: await jobView(
      c,
      found(
        await c.db
          .select()
          .from(s.fulfillmentJobs)
          .where(eq(s.fulfillmentJobs.id, j.id))
          .get(),
      ),
    ),
  };
}
