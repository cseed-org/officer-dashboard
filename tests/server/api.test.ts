import { beforeAll, afterAll, describe, expect, it, vi } from 'vitest';
import { Miniflare, convertV4MiniflareOptions } from 'miniflare';
import { build } from 'esbuild';
import { migrationStatements } from '../db/engines';
import { buildSeedSql } from '../../scripts/seed/sql';
import { ENDPOINTS, type CurrentUser } from '../../src/core/contracts';
vi.setConfig({ testTimeout: 60000 });
const state = { db: null as D1Database | null };
let worker: Miniflare;
const director: CurrentUser = {
  id: 'user-director',
  name: 'Jordan Lee',
  discordId: '100000000000000001',
  role: 'director',
  title: null,
  avatarUrl: null,
};
const officer: CurrentUser = {
  ...director,
  id: 'user-priya',
  name: 'Priya',
  discordId: '100000000000000002',
  role: 'officer',
};
let dispose: () => Promise<void>;
async function call(
  path: string,
  method = 'GET',
  body?: unknown,
  user: CurrentUser | null = officer,
  token?: string,
) {
  const request = new Request(`http://localhost${path}`, {
    method,
    headers: {
      ...(body && !(body instanceof FormData)
        ? { 'Content-Type': 'application/json' }
        : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      'X-Test-User': JSON.stringify(user),
    },
    ...(body
      ? { body: body instanceof FormData ? body : JSON.stringify(body) }
      : {}),
  });
  const response = await worker.dispatchFetch(request.url, {
    method: request.method,
    headers: request.headers,
    body: request.body ? await request.arrayBuffer() : undefined,
  });
  // Parsed response shapes are validated separately against the endpoint contracts.
  const result: any = await response.json();
  if (response.status >= 500) console.error(path, result);
  return { status: response.status, body: result };
}
const purchase = {
  type: 'club_purchase',
  title: 'API test',
  vendor: 'Test vendor',
  quantity: 2,
  unitCostCents: 5000,
  taxShipCents: 0,
  expenseAccountId: 'acct-5100',
  fundId: 'fund-general',
  functionalClass: 'program',
  justification: 'Testing lifecycle',
};
beforeAll(async () => {
  // Run the API inside workerd, as production does. Pulling thousands of D1 rows
  // through getPlatformProxy's lazy host RPC creates a TCP call per property access.
  const bundled = await build({
    stdin: {
      contents: `
    import { handleApi } from './src/server/api/handler';
    export default { async fetch(request) {
      return handleApi({ request,url: new URL(request.url),locals: { user: JSON.parse(request.headers.get('X-Test-User') ?? 'null'),sessionId: null } });
    } };
  `,
      resolveDir: process.cwd(),
      sourcefile: 'api-test-worker.ts',
    },
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'browser',
    target: 'es2022',
    external: ['cloudflare:workers'],
    define: { 'import.meta.env.DEV': 'true' },
  });
  worker = new Miniflare(
    convertV4MiniflareOptions({
      modules: true,
      script: bundled.outputFiles[0]!.text,
      compatibilityDate: '2026-10-01',
      compatibilityFlags: ['nodejs_compat'],
      d1Databases: { DB: 'api-test' },
    }),
  );
  state.db = (await worker.getD1Database('DB')) as unknown as D1Database;
  dispose = () => worker.dispose();
  for (const statement of migrationStatements())
    await state.db.prepare(statement).run();
  // Seed SQL is generated without triggers and has no semicolons inside values.
  const seed = buildSeedSql()
    .split(/;\n/)
    .map((s) => s.trim())
    .filter((s) => s.replace(/--.*$/gm, '').trim());
  for (let i = 0; i < seed.length; i += 50)
    await state.db.batch(
      seed.slice(i, i + 50).map((statement) => state.db!.prepare(statement)),
    );
}, 60000);
afterAll(async () => {
  await dispose?.();
});
describe('real D1 API', () => {
  it('enforces roles, validates inputs and protects the last director', async () => {
    expect((await call('/api/admin/users')).status).toBe(403);
    expect((await call('/api/requests/nope/approve', 'POST', {})).status).toBe(
      403,
    );
    expect((await call('/api/events', 'POST', {})).status).toBe(400);
    expect(
      (
        await call(
          '/api/admin/users/user-director',
          'PATCH',
          { active: false },
          director,
        )
      ).status,
    ).toBe(409);
    expect((await call('/api/lookups', 'GET', undefined, null)).status).toBe(
      401,
    );
  });
  it('settles actual cost, links journal, releases encumbrance and forbids repeat settlement', async () => {
    const made = await call('/api/requests', 'POST', purchase);
    expect(made.status).toBe(200);
    const id = made.body.request.id;
    expect(
      (await call(`/api/requests/${id}/submit`, 'POST')).body.request.status,
    ).toBe('submitted');
    const approved = await call(
      `/api/requests/${id}/approve`,
      'POST',
      {},
      director,
    );
    expect(approved.status).toBe(200);
    expect(approved.body.request.status).toBe('approved');
    const committed = await call('/api/finance/summary');
    expect(committed.body.totals.committedCents).toBeGreaterThanOrEqual(10000);
    expect(
      (
        await call(
          `/api/requests/${id}/transition`,
          'POST',
          { to: 'fulfilling' },
          director,
        )
      ).status,
    ).toBe(200);
    const settled = await call(
      `/api/requests/${id}/transition`,
      'POST',
      { to: 'fulfilled', actualTotalCents: 9500 },
      director,
    );
    expect(settled.status).toBe(200);
    expect(settled.body.request.actualTotalCents).toBe(9500);
    expect(settled.body.request.journalEntryIds).toHaveLength(1);
    const after = await call('/api/finance/summary');
    expect(after.body.totals.committedCents).toBe(
      committed.body.totals.committedCents - 10000,
    );
    expect(after.body.totals.spentCents).toBe(
      committed.body.totals.spentCents + 9500,
    );
    expect(
      (
        await call(
          `/api/requests/${id}/transition`,
          'POST',
          { to: 'fulfilled', actualTotalCents: 9500 },
          director,
        )
      ).status,
    ).toBe(409);
    const entry = await state
      .db!.prepare(
        'SELECT sum(debit_cents) d, sum(credit_cents) c FROM journal_lines WHERE entry_id = ?',
      )
      .bind(settled.body.request.journalEntryIds[0])
      .first();
    expect(entry?.d).toBe(9500);
    expect(entry?.c).toBe(9500);
  });
  it('forbids self approval and draft edits after submission', async () => {
    const made = await call(
        '/api/requests',
        'POST',
        { ...purchase, submit: true },
        director,
      ),
      id = made.body.request.id;
    expect(
      (await call(`/api/requests/${id}/approve`, 'POST', {}, director)).status,
    ).toBe(403);
    expect(
      (await call(`/api/requests/${id}`, 'PATCH', { title: 'Edit' }, director))
        .status,
    ).toBe(409);
  });
  it('hashes and revokes scoped tokens; ingests idempotently and supports exclusive job claims', async () => {
    const issued = await call(
      '/api/admin/tokens',
      'POST',
      { name: 'test', scopes: ['ingest:luma', 'agent:jobs'] },
      director,
    );
    expect(issued.status).toBe(200);
    const token = issued.body.plaintext;
    expect(issued.body.token.tokenHash).toBeUndefined();
    const stored = await state
      .db!.prepare('SELECT token_hash FROM service_tokens WHERE id = ?')
      .bind(issued.body.token.id)
      .first();
    expect(stored?.token_hash).not.toBe(token);
    const input = {
      event: {
        lumaId: 'test-ingest',
        name: 'Test import',
        startsAt: '2026-11-01T10:00:00Z',
      },
      guests: [{ email: 'A@EXAMPLE.ORG', checkedInAt: '2026-11-01T10:00:00Z' }],
    };
    const first = await call('/api/ingest/luma', 'POST', input, null, token);
    expect(first.status).toBe(200);
    expect(first.body.created).toBe(1);
    const again = await call('/api/ingest/luma', 'POST', input, null, token);
    expect(again.body.created).toBe(0);
    expect(again.body.updated).toBe(1);
    const made = await call('/api/requests', 'POST', {
      ...purchase,
      submit: true,
    });
    const id = made.body.request.id;
    await call(`/api/requests/${id}/approve`, 'POST', {}, director);
    const jobs = await call('/api/agent/jobs', 'GET', undefined, null, token);
    const job = jobs.body.jobs.find(
      (j: { request: { id: string } }) => j.request.id === id,
    );
    expect(
      (
        await call(
          `/api/agent/jobs/${job.id}/claim`,
          'POST',
          undefined,
          null,
          token,
        )
      ).status,
    ).toBe(200);
    expect(
      (
        await call(
          `/api/agent/jobs/${job.id}/claim`,
          'POST',
          undefined,
          null,
          token,
        )
      ).status,
    ).toBe(409);
    const completed = await call(
      `/api/agent/jobs/${job.id}/complete`,
      'POST',
      { outcome: 'done', actualTotalCents: 9800 },
      null,
      token,
    );
    expect(completed.status).toBe(200);
    expect(completed.body.job.request.actualTotalCents).toBe(9800);
    await call(
      `/api/admin/tokens/${issued.body.token.id}`,
      'DELETE',
      undefined,
      director,
    );
    expect(
      (await call('/api/agent/jobs', 'GET', undefined, null, token)).status,
    ).toBe(401);
  });
  it('returns contract-valid read models and ties reports together', async () => {
    for (const endpoint of [
      'lookups',
      'overview',
      'eventsList',
      'requestsList',
      'financeSummary',
      'budgetsGet',
      'periods',
      'accounts',
      'adminUsers',
      'adminTokens',
      'adminAudit',
    ] as const) {
      const spec = ENDPOINTS[endpoint];
      const response = await call(spec.path, 'GET', undefined, director);
      expect(response.status, endpoint).toBe(200);
      expect(spec.response.safeParse(response.body).success, endpoint).toBe(
        true,
      );
    }
    expect((await call('/api/finance/reports/trial-balance')).status).toBe(403);
    const tb = await call(
      '/api/finance/reports/trial-balance',
      'GET',
      undefined,
      director,
    );
    expect(tb.body.totals.debitCents).toBe(tb.body.totals.creditCents);
    const position = await call(
      '/api/finance/reports/statement-of-financial-position',
      'GET',
      undefined,
      director,
    );
    expect(position.body.totals.balanced).toBe(true);
    const activities = await call(
      '/api/finance/reports/statement-of-activities',
      'GET',
      undefined,
      director,
    );
    expect(activities.body.netAssetsEnding.totalCents).toBe(
      position.body.totals.netAssetsCents,
    );
    for (const name of [
      'statement-of-functional-expenses',
      'fund-balances',
      'budget-vs-actual',
      'form-990',
    ])
      expect(
        (await call(`/api/finance/reports/${name}`, 'GET', undefined, director))
          .status,
        name,
      ).toBe(200);
  });
  it('releases restricted purchases, pays reimbursements on close, and serializes approvals', async () => {
    const made = await call('/api/requests', 'POST', {
      ...purchase,
      type: 'reimbursement',
      fundId: 'fund-buildspace',
      submit: true,
    });
    const id = made.body.request.id;
    const decisions = await Promise.all([
      call(`/api/requests/${id}/approve`, 'POST', {}, director),
      call(`/api/requests/${id}/approve`, 'POST', {}, director),
    ]);
    expect(decisions.map((r) => r.status).sort()).toEqual([200, 409]);
    expect(
      (
        await call(
          `/api/requests/${id}/transition`,
          'POST',
          { to: 'reimbursed', actualTotalCents: 1000000 },
          director,
        )
      ).status,
    ).toBe(409);
    const settled = await call(
      `/api/requests/${id}/transition`,
      'POST',
      { to: 'reimbursed', actualTotalCents: 8000 },
      director,
    );
    expect(settled.status).toBe(200);
    expect(settled.body.request.journalEntryIds).toHaveLength(2);
    const release = await state
      .db!.prepare(
        "SELECT count(*) n FROM journal_entries WHERE purchase_request_id=? AND source='release'",
      )
      .bind(id)
      .first();
    expect(release?.n).toBe(1);
    const activity = await call(
      '/api/finance/reports/statement-of-activities',
      'GET',
      undefined,
      director,
    );
    expect(
      activity.body.expenses.every(
        (r: { withDonorCents: number }) => r.withDonorCents === 0,
      ),
    ).toBe(true);
    const closed = await call(
      `/api/requests/${id}/transition`,
      'POST',
      { to: 'closed' },
      director,
    );
    expect(closed.status).toBe(200);
    expect(closed.body.request.journalEntryIds).toHaveLength(3);
    const payable = await state
      .db!.prepare(
        "SELECT sum(l.debit_cents-l.credit_cents) n FROM journal_lines l JOIN journal_entries e ON e.id=l.entry_id JOIN accounts a ON a.id=l.account_id WHERE e.purchase_request_id=? AND a.code='2100'",
      )
      .bind(id)
      .first();
    expect(payable?.n).toBe(0);
  });
  it('keeps check-ins on sparse re-import, validates CSV dry run and cleans draft attachments', async () => {
    const issued = await call(
      '/api/admin/tokens',
      'POST',
      { name: 'import only', scopes: ['ingest:luma'] },
      director,
    );
    const token = issued.body.plaintext;
    expect(
      (await call('/api/agent/jobs', 'GET', undefined, null, token)).status,
    ).toBe(403);
    const input = {
      event: {
        lumaId: 'sparse',
        name: 'Sparse data',
        startsAt: '2026-11-01T10:00:00Z',
      },
      guests: [
        { email: 'sparse@example.org', checkedInAt: '2026-11-01T10:00:00Z' },
      ],
    };
    const first = await call('/api/ingest/luma', 'POST', input, null, token);
    const second = await call(
      '/api/ingest/luma',
      'POST',
      { ...input, guests: [{ email: 'sparse@example.org' }] },
      null,
      token,
    );
    expect(second.status).toBe(200);
    expect(second.body.metrics.checkedIn).toBe(1);
    const form = new FormData();
    form.set('eventId', first.body.eventId);
    form.set('kind', 'reflections');
    form.set('dryRun', 'true');
    form.set(
      'file',
      new File(
        ['Email,Timestamp\nsparse@example.org,2026-11-01T11:00:00Z'],
        'reflections.csv',
        { type: 'text/csv' },
      ),
    );
    const dry = await call('/api/events/import', 'POST', form);
    expect(dry.status).toBe(200);
    expect(dry.body.metrics.matchedReflections).toBe(1);
    const count = await state
      .db!.prepare('SELECT count(*) n FROM reflections WHERE event_id=?')
      .bind(first.body.eventId)
      .first();
    expect(count?.n).toBe(0);
    const cleared = await call(
      '/api/ingest/luma',
      'POST',
      {
        ...input,
        guests: [{ email: 'sparse@example.org', checkedInAt: null }],
      },
      null,
      token,
    );
    expect(cleared.status).toBe(200);
    expect(cleared.body.metrics.checkedIn).toBe(0);
    const draft = await call('/api/requests', 'POST', purchase);
    const id = draft.body.request.id;
    const upload = new FormData();
    upload.set('kind', 'receipt');
    upload.set(
      'file',
      new File(['%PDF-1.4\nTest'], 'test.pdf', { type: 'application/pdf' }),
    );
    const attached = await call(
      `/api/requests/${id}/attachments`,
      'POST',
      upload,
    );
    expect(attached.status).toBe(200);
    expect((await call(`/api/requests/${id}`, 'DELETE')).status).toBe(200);
    expect((await call(`/api/requests/${id}`)).status).toBe(404);
    const audit = await state
      .db!.prepare(
        "SELECT before_json FROM audit_log WHERE entity_id=? AND action='request.deleted'",
      )
      .bind(id)
      .first();
    expect(JSON.parse(String(audit?.before_json)).attachments).toHaveLength(1);
  });
  it('denies and cancels requests, retries failed jobs, and invalidates deactivated sessions', async () => {
    const submitted = await call('/api/requests', 'POST', {
      ...purchase,
      submit: true,
    });
    const id = submitted.body.request.id;
    expect(
      (
        await call(
          `/api/requests/${id}/deny`,
          'POST',
          { comment: 'Outside budget' },
          director,
        )
      ).body.request.status,
    ).toBe('denied');
    expect(
      (await call(`/api/requests/${id}/approve`, 'POST', {}, director)).status,
    ).toBe(409);
    const cancelled = await call('/api/requests', 'POST', {
      ...purchase,
      submit: true,
    });
    const cid = cancelled.body.request.id;
    await call(`/api/requests/${cid}/approve`, 'POST', {}, director);
    expect(
      (
        await call(
          `/api/requests/${cid}/transition`,
          'POST',
          { to: 'cancelled' },
          director,
        )
      ).status,
    ).toBe(200);
    expect(
      (
        await state
          .db!.prepare('SELECT status FROM fulfillment_jobs WHERE request_id=?')
          .bind(cid)
          .first()
      )?.status,
    ).toBe('cancelled');
    const createdUser = await call(
      '/api/admin/users',
      'POST',
      { discordId: '100000000000000099', name: 'Session test' },
      director,
    );
    await state
      .db!.prepare(
        "INSERT INTO sessions (id,user_id,expires_at) VALUES ('session-test',?,?)",
      )
      .bind(createdUser.body.user.id, Date.now() + 86400000)
      .run();
    expect(
      (
        await call(
          `/api/admin/users/${createdUser.body.user.id}`,
          'PATCH',
          { active: false },
          director,
        )
      ).status,
    ).toBe(200);
    expect(
      (
        await state
          .db!.prepare(
            "SELECT count(*) n FROM sessions WHERE id='session-test'",
          )
          .first()
      )?.n,
    ).toBe(0);
  });
  it('upserts a full 5,000-guest ingest in bounded bulk statements', async () => {
    const issued = await call(
      '/api/admin/tokens',
      'POST',
      { name: 'bulk ingest', scopes: ['ingest:luma'] },
      director,
    );
    const input = {
      event: {
        lumaId: 'bulk-5000',
        name: 'Bulk event',
        startsAt: '2026-11-01T10:00:00Z',
      },
      guests: Array.from({ length: 5000 }, (_, i) => ({
        email: `bulk${i}@example.org`,
      })),
    };
    const first = await call(
      '/api/ingest/luma',
      'POST',
      input,
      null,
      issued.body.plaintext,
    );
    expect(first.status).toBe(200);
    expect(first.body.created).toBe(5000);
    const again = await call(
      '/api/ingest/luma',
      'POST',
      input,
      null,
      issued.body.plaintext,
    );
    expect(again.status).toBe(200);
    expect(again.body.created).toBe(0);
    expect(again.body.updated).toBe(5000);
    expect(again.body.metrics.registered).toBe(5000);
  });
});
