#!/usr/bin/env node
/**
 * Contract check: proves the Go server speaks the JSON API that src/core/contracts.ts defines.
 *
 * It builds (or is given) the Go binary, seeds a temporary SQLite database with the FY2027
 * sample books, starts the server on a free port, then
 *   1. calls every GET endpoint,
 *   2. scripts a purchase lifecycle (create → attach → submit → approve by ANOTHER director →
 *      fulfil/settle → close), a reimbursement on a restricted grant, the purchasing-agent
 *      job flow, a Luma ingest, a CSV import, event, budget and fiscal-period writes,
 * and validates every request body, query string and response against the zod schemas in
 * ENDPOINTS (error responses against ApiError). Any schema error, unexpected status or
 * endpoint that was never exercised fails the run.
 *
 *   node scripts/contract-check.mjs [--bin path/to/dashboard] [--keep] [--verbose]
 *
 * Needs `npm ci` (zod, esbuild). Go is only needed when --bin is not given.
 */
import { spawn, spawnSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { DatabaseSync } from 'node:sqlite';
import { build } from 'esbuild';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const flag = (name) => args.includes(name);
const option = (name) => (args.includes(name) ? args[args.indexOf(name) + 1] : undefined);
const verbose = flag('--verbose');

const work = mkdtempSync(join(tmpdir(), 'contract-check-'));
let server;
const cleanup = () => {
  server?.kill('SIGTERM');
  if (!flag('--keep')) rmSync(work, { recursive: true, force: true });
};
process.on('exit', cleanup);
for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, () => process.exit(130));

// ───────────── schemas ─────────────
// contracts.ts has extensionless relative imports, so bundle it (zod included) for node.
const bundle = join(work, 'contracts.mjs');
await build({ entryPoints: [join(root, 'src/core/contracts.ts')], bundle: true, format: 'esm', platform: 'node', outfile: bundle, logLevel: 'error' });
const { ENDPOINTS, ApiError } = await import(pathToFileURL(bundle).href);

// ───────────── binary, database, server ─────────────
let bin = option('--bin');
if (!bin) {
  bin = join(work, 'dashboard');
  process.stdout.write('building the Go server… ');
  const built = spawnSync(process.env.GO ?? 'go', ['build', '-o', bin, './cmd/dashboard'], { cwd: join(root, 'server'), stdio: ['ignore', 'inherit', 'inherit'] });
  if (built.status !== 0) throw new Error('go build failed');
  console.log('done');
}
const dbPath = join(work, 'check.db');
const env = { ...process.env, DB_PATH: dbPath, RECEIPTS_DIR: join(work, 'receipts') };
for (const k of ['DEV_AUTH', 'DISCORD_CLIENT_ID', 'DISCORD_CLIENT_SECRET', 'DISCORD_WEBHOOK_URL', 'GOOGLE_SERVICE_ACCOUNT_JSON', 'BOOTSTRAP_DIRECTOR_DISCORD_ID']) delete env[k];
const seeded = spawnSync(bin, ['seed', '-reset'], { env, encoding: 'utf8' });
if (seeded.status !== 0) throw new Error(`seed failed: ${seeded.stderr}`);
console.log(seeded.stdout.trim());

const port = await new Promise((ok) => {
  const s = createServer().listen(0, '127.0.0.1', () => {
    const p = s.address().port;
    s.close(() => ok(p));
  });
});
const base = `http://127.0.0.1:${port}`;
server = spawn(bin, ['serve'], { env: { ...env, ADDR: `127.0.0.1:${port}`, APP_URL: base }, stdio: ['ignore', 'ignore', verbose ? 'inherit' : 'ignore'] });
server.on('exit', (code) => code && console.error(`server exited with ${code}`));
for (let i = 0; ; i++) {
  try {
    if ((await fetch(`${base}/api/health`)).ok) break;
  } catch {}
  if (i > 100) throw new Error('server did not start');
  await new Promise((r) => setTimeout(r, 50));
}

// ───────────── sessions without Discord: insert rows the way the server would ─────────────
const db = new DatabaseSync(dbPath);
db.exec('PRAGMA busy_timeout = 5000');
function sessionFor(userId) {
  const token = randomBytes(32).toString('base64url');
  const id = createHash('sha256').update(token).digest('hex');
  db.prepare('INSERT INTO sessions (id, user_id, expires_at, user_agent, created_at) VALUES (?, ?, ?, ?, ?)').run(id, userId, Date.now() + 864e5, 'contract-check', Date.now());
  return { cookie: `cseed_session=${token}` };
}
const anon = {};
const directorA = sessionFor('user-director');
const officer = sessionFor('user-priya');

// ───────────── the caller ─────────────
const covered = new Set();
const failures = [];
let responses = 0;
const log = [];

function fail(label, message, detail) {
  failures.push(`${label}: ${message}${detail ? `\n    ${JSON.stringify(detail).slice(0, 600)}` : ''}`);
}

/**
 * Calls an endpoint by its ENDPOINTS name. `expect` is the status the script is asserting
 * (default 200). Bodies and queries are validated before sending, responses after.
 */
async function call(name, { who = anon, params = {}, query, body, form, expect = 200, headers = {}, csv = false } = {}) {
  const spec = ENDPOINTS[name];
  if (!spec) throw new Error(`unknown endpoint ${name}`);
  covered.add(name);
  const label = `${name} ${spec.method} ${spec.path}${expect === 200 ? '' : ` (expect ${expect})`}`;
  let path = spec.path.replace(/:(\w+)/g, (_, k) => encodeURIComponent(params[k] ?? (fail(label, `missing path param ${k}`), k)));
  if (query !== undefined) {
    if (spec.query && expect === 200) {
      const r = spec.query.safeParse(query);
      if (!r.success) fail(label, 'script query violates the query schema', r.error.issues);
    }
    const qs = new URLSearchParams(query).toString();
    if (qs) path += `?${qs}`;
  }
  const init = { method: spec.method, redirect: 'manual', headers: { ...headers } };
  if (who.cookie) init.headers.cookie = who.cookie;
  if (who.bearer) init.headers.authorization = `Bearer ${who.bearer}`;
  if (form) {
    init.body = form;
  } else if (body !== undefined) {
    if (spec.body && spec.body !== 'multipart' && expect === 200) {
      const r = spec.body.safeParse(body);
      if (!r.success) fail(label, 'script body violates the request schema', r.error.issues);
    }
    init.body = JSON.stringify(body);
    init.headers['content-type'] = 'application/json';
  }
  const started = performance.now();
  const res = await fetch(base + path, init);
  const ms = performance.now() - started;
  const type = res.headers.get('content-type') ?? '';
  let json;
  let raw;
  if (type.includes('application/json')) json = await res.json();
  else raw = Buffer.from(await res.arrayBuffer());
  responses++;
  let verdict = 'ok';
  if (res.status !== expect) {
    verdict = `status ${res.status}`;
    fail(label, `expected ${expect}, got ${res.status}`, json ?? raw?.toString().slice(0, 200));
  } else if (expect >= 400) {
    const r = ApiError.safeParse(json);
    if (!r.success) {
      verdict = 'bad ApiError';
      fail(label, 'error body is not an ApiError', { json, issues: r.error.issues });
    }
  } else if (csv || spec.response === 'csv') {
    const text = raw?.toString() ?? '';
    if (!type.startsWith('text/csv') || !res.headers.get('content-disposition')?.includes('attachment')) fail(label, 'CSV headers wrong', { type });
    if (!text.startsWith('"fiscalYear"') || !text.endsWith('\r\n')) fail(label, 'CSV body is not the report table', text.slice(0, 120));
  } else if (typeof spec.response !== 'string') {
    const r = spec.response.safeParse(json);
    if (!r.success) {
      verdict = 'SCHEMA ERROR';
      fail(label, 'response violates the contract', r.error.issues.slice(0, 5));
    }
  }
  log.push(`${verdict === 'ok' ? '✓' : '✗'} ${String(res.status).padEnd(3)} ${spec.method.padEnd(6)} ${path.padEnd(58)} ${ms.toFixed(1).padStart(6)} ms`);
  return { status: res.status, json, raw, headers: res.headers };
}

const today = new Date().toISOString().slice(0, 10);
const must = (cond, message, detail) => cond || fail('assertion', message, detail);
const money = (c) => `${c < 0 ? '-' : ''}$${(Math.abs(c) / 100).toLocaleString('en-US', { minimumFractionDigits: 2 })}`;

// ───────────── 1. every GET endpoint, on the freshly seeded books ─────────────
await call('health');
const me = await call('me', { who: officer });
must(me.json?.user?.id === 'user-priya', 'me returns the signed-in user');
const lookups = await call('lookups', { who: officer });
await call('overview', { who: officer });
await call('search', { who: officer, query: { q: 'demo', limit: '10' } });
must((await call('search', { who: officer, query: { q: 'priya' } })).json.results.length === 0, 'officers get no user results');
must((await call('search', { who: directorA, query: { q: 'priya' } })).json.results.some((r) => r.kind === 'user'), 'directors find users');
const events = await call('eventsList', { who: officer, query: { limit: '3' } });
must(events.json.nextCursor, 'event list pages');
await call('eventsList', { who: officer, query: { cursor: events.json.nextCursor, when: 'all' } });
const eventId = events.json.items[0].id;
await call('eventDetail', { who: officer, params: { id: eventId } });
await call('eventMetrics', { who: officer, params: { id: 'event-kickoff' } });
const requests = await call('requestsList', { who: officer, query: { scope: 'all', limit: '50' } });
for (const r of requests.json.items.slice(0, 6)) await call('requestDetail', { who: officer, params: { id: r.id } });
await call('financeSummary', { who: officer, query: { fiscalYear: '2027', asOf: '2026-10-31' } });
await call('budgetsGet', { who: officer, query: { fiscalYear: '2027' } });
const REPORTS = ['trial-balance', 'statement-of-activities', 'statement-of-financial-position', 'statement-of-functional-expenses', 'budget-vs-actual', 'fund-balances', 'form-990'];
const reports = {};
for (const report of REPORTS) reports[report] = (await call('financeReport', { who: directorA, params: { report }, query: { fiscalYear: '2027', asOf: '2026-10-31' } })).json;
await call('financeReport', { who: directorA, params: { report: 'trial-balance' }, query: { fiscalYear: '2027', format: 'csv' }, csv: true });
await call('financeReport', { who: officer, params: { report: 'trial-balance' }, expect: 403 });
await call('financeReport', { who: officer, params: { report: 'budget-vs-actual' } });
await call('periods', { who: directorA });
await call('accounts', { who: directorA });
await call('adminUsers', { who: directorA });
await call('adminTokens', { who: directorA });
await call('adminAudit', { who: directorA, query: { limit: '5' } });
await call('adminUsers', { who: officer, expect: 403 });
await call('lookups', { expect: 401 });

// The seeded FY2027 statements foot — show the numbers.
{
  const sum = (rows, k) => rows.reduce((a, r) => a + r[k], 0);
  const a = reports['statement-of-activities'];
  const p = reports['statement-of-financial-position'];
  const rev = sum(a.revenue, 'withoutDonorCents');
  const exp = sum(a.expenses, 'withoutDonorCents');
  const change = a.changeInNetAssets.withoutDonorCents;
  console.log('\nSeeded FY2027 statements (asOf 2026-10-31)');
  console.log(`  unrestricted: ${money(rev)} − ${money(exp)} + ${money(a.releasesCents)} released = ${money(rev - exp + a.releasesCents)}  (statement says ${money(change)})`);
  console.log(`  net assets ending ${money(a.netAssetsEnding.totalCents)} = total assets ${money(p.totals.assetsCents)}  (balanced: ${p.totals.balanced})`);
  must(rev - exp + a.releasesCents === 139500 && change === 139500, 'unrestricted change foots to $1,395.00', { rev, exp, rel: a.releasesCents, change });
  must(a.netAssetsEnding.totalCents === 1190860 && p.totals.assetsCents === 1190860, 'net assets ending = assets = $11,908.60');
  must(p.totals.balanced, 'statement of financial position balances');
  const tb = reports['trial-balance'].totals;
  must(tb.debitCents === tb.creditCents, 'trial balance balances', tb);
}

// ───────────── 2. make sure today has an open fiscal period to post into ─────────────
{
  const periods = (await call('periods', { who: directorA })).json.periods;
  if (!periods.some((p) => p.startDate <= today && today <= p.endDate && p.status === 'open')) {
    const [y, m] = today.split('-').map(Number);
    const fy = m >= 7 ? y + 1 : y;
    await call('periodsCreate', { who: directorA, body: { fiscalYear: fy } });
  }
}

// ───────────── 3. purchase lifecycle: club purchase settled below the estimate ─────────────
const createdB = await call('adminUserCreate', { who: directorA, body: { discordId: '100000000000000077', name: 'Second Director', role: 'director', title: 'Treasurer', email: 'second@example.org' } });
const directorB = sessionFor(createdB.json.user.id);
await call('adminUserUpdate', { who: directorA, params: { id: createdB.json.user.id }, body: { title: 'Co-director' } });

const account = lookups.json.expenseAccounts.find((a) => a.code === '5100').id;
const generalFund = lookups.json.funds.find((f) => f.code === 'GEN').id;
const grantFund = lookups.json.funds.find((f) => f.code === 'GR-BUILDSPACE').id;
const fields = { title: 'Contract check kits', vendor: 'Adafruit', quantity: 2, unitCostCents: 5000, taxShipCents: 0, expenseAccountId: account, fundId: generalFund, functionalClass: 'program', justification: 'Exercise every endpoint', itemUrl: 'https://www.adafruit.com/', neededBy: '2027-01-15', eventId };

const draft = await call('requestCreate', { who: directorA, body: { ...fields, type: 'club_purchase', submit: false } });
const id = draft.json.request.id;
must(draft.json.request.status === 'draft' && draft.json.request.totalCents === 10000, 'draft totals 100.00');
must(/^\d{4}-\d{4}$/.test(draft.json.request.number), 'request number is FY-NNNN', draft.json.request.number);
await call('requestUpdate', { who: directorA, params: { id }, body: { title: 'Contract check kits (edited)', taxShipCents: 0 } });
const upload = () => {
  const f = new FormData();
  f.set('kind', 'receipt');
  f.set('file', new Blob(['%PDF-1.4\n% contract check receipt\n'], { type: 'application/pdf' }), 'receipt.pdf');
  return f;
};
const attached = await call('requestAttach', { who: directorA, params: { id }, form: upload() });
const attachmentId = attached.json.attachment.id;
const download = await call('attachmentDownload', { who: officer, params: { id: attachmentId } });
must(download.raw?.toString().startsWith('%PDF-1.4'), 'receipt downloads byte for byte');
await call('requestSubmit', { who: directorA, params: { id } });
await call('requestApprove', { who: directorA, params: { id }, body: {}, expect: 403 }); // no self-approval
const approved = await call('requestApprove', { who: directorB, params: { id }, body: { comment: 'Looks right.' } });
must(approved.json.request.status === 'approved', 'a second director approves');
const committed = (await call('financeSummary', { who: officer })).json.totals.committedCents;
await call('requestTransition', { who: directorB, params: { id }, body: { to: 'fulfilling' } });
const settled = await call('requestTransition', { who: directorB, params: { id }, body: { to: 'fulfilled', actualTotalCents: 9500, attachmentIds: [attachmentId], orderReference: 'ORD-1', comment: '5% discount' } });
must(settled.json.request.actualTotalCents === 9500 && settled.json.request.journalEntryIds.length === 1, 'settled at the actual 95.00 with one journal entry');
const afterSettle = (await call('financeSummary', { who: officer })).json.totals.committedCents;
must(afterSettle === committed - 10000, 'settling releases the 100.00 commitment', { before: committed, after: afterSettle });
await call('requestTransition', { who: directorB, params: { id }, body: { to: 'fulfilled', actualTotalCents: 9500 }, expect: 409 }); // already settled
const closed = await call('requestTransition', { who: directorB, params: { id }, body: { to: 'closed' } });
must(closed.json.request.status === 'closed', 'closed');
await call('requestUpdate', { who: directorA, params: { id }, body: { title: 'too late' }, expect: 409 });

// Reimbursement against a restricted grant: expense + restriction release, then payout on close.
const grantOpen = today >= '2026-08-01' && today <= '2027-06-30'; // the sample grant's spending window
const reimb = await call('requestCreate', { who: officer, body: { ...fields, title: 'Contract check pizza', vendor: 'Pagliacci', type: 'reimbursement', fundId: grantOpen ? grantFund : generalFund, expenseAccountId: lookups.json.expenseAccounts.find((a) => a.code === '5200').id, submit: true, eventId: undefined } });
const rid = reimb.json.request.id;
await call('requestApprove', { who: directorA, params: { id: rid }, body: {} });
const paid = await call('requestTransition', { who: directorB, params: { id: rid }, body: { to: 'reimbursed', actualTotalCents: 8000 } });
must(paid.json.request.journalEntryIds.length === (grantOpen ? 2 : 1), 'restricted settlement posts the expense and the release');
const paidOut = await call('requestTransition', { who: directorA, params: { id: rid }, body: { to: 'closed' } });
must(paidOut.json.request.journalEntryIds.length === (grantOpen ? 3 : 2), 'closing a reimbursement posts the payout');
await call('requestDetail', { who: officer, params: { id: rid } });

// Deny + delete a draft.
const toDeny = await call('requestCreate', { who: officer, body: { ...fields, type: 'club_purchase', submit: true, eventId: undefined } });
await call('requestDeny', { who: directorA, params: { id: toDeny.json.request.id }, body: { comment: 'Out of budget' } });
const scratch = await call('requestCreate', { who: officer, body: { ...fields, type: 'reimbursement', eventId: undefined } });
await call('requestDelete', { who: officer, params: { id: scratch.json.request.id } });

// ───────────── 4. the purchasing agent, with a service token ─────────────
const issued = await call('adminTokenCreate', { who: directorA, body: { name: 'contract check agent', scopes: ['agent:jobs', 'ingest:luma'], expiresInDays: 30 } });
const agent = { bearer: issued.json.plaintext };
const jobRequest = await call('requestCreate', { who: officer, body: { ...fields, type: 'club_purchase', submit: true, eventId: undefined } });
await call('requestApprove', { who: directorA, params: { id: jobRequest.json.request.id }, body: {} });
const jobs = await call('agentJobs', { who: agent, query: { status: 'queued' } });
const job = jobs.json.jobs.find((j) => j.request.id === jobRequest.json.request.id);
must(job, 'the approved club purchase queued a job');
await call('agentJobClaim', { who: agent, params: { id: job.id } });
await call('agentJobClaim', { who: agent, params: { id: job.id }, expect: 409 });
const done = await call('agentJobComplete', { who: agent, params: { id: job.id }, body: { outcome: 'done', actualTotalCents: 9800, orderReference: 'AGENT-1', notes: 'bought' } });
must(done.json.job.request.status === 'fulfilled', 'the agent settles within tolerance');
// A settlement far above the estimate is held for a director.
const heldRequest = await call('requestCreate', { who: officer, body: { ...fields, type: 'club_purchase', submit: true, eventId: undefined } });
await call('requestApprove', { who: directorA, params: { id: heldRequest.json.request.id }, body: {} });
const heldJob = (await call('agentJobs', { who: agent })).json.jobs.find((j) => j.request.id === heldRequest.json.request.id);
await call('agentJobClaim', { who: agent, params: { id: heldJob.id } });
await call('agentJobComplete', { who: agent, params: { id: heldJob.id }, body: { outcome: 'done', actualTotalCents: 50000 }, expect: 409 });
const held = await call('requestDetail', { who: directorA, params: { id: heldRequest.json.request.id } });
must(held.json.request.settlementHold?.attemptedCents === 50000, 'the hold is visible on the request', held.json.request.settlementHold);
await call('agentJobs', { who: agent, query: { status: 'held' } });
await call('agentJobs', { who: officer, expect: 401 });

// Luma ingest + CSV import
const ingest = await call('ingestLuma', { who: agent, body: { event: { lumaId: 'contract-check', name: 'Contract check mixer', startsAt: '2026-12-05T18:00:00-08:00', endsAt: '2026-12-05T20:00:00-08:00', location: 'Room 5', url: 'https://lu.ma/contract-check' }, guests: [{ email: 'Ann@Example.org', name: 'Ann', approvalStatus: 'approved', registeredAt: '2026-11-20T10:00:00Z', checkedInAt: '2026-12-05T18:05:00-08:00' }, { email: 'bo@example.org' }] } });
must(ingest.json.created === 2 && ingest.json.metrics.checkedIn === 1, 'ingest creates two guests', ingest.json);
const again = await call('ingestLuma', { who: agent, body: { event: { lumaId: 'contract-check', name: 'Contract check mixer', startsAt: '2026-12-05T18:00:00-08:00' }, guests: [{ email: 'ann@example.org' }] } });
must(again.json.created === 0 && again.json.updated === 1, 'ingest is idempotent');
const csv = (kind, text, extra = {}) => {
  const f = new FormData();
  f.set('kind', kind);
  for (const [k, v] of Object.entries(extra)) f.set(k, v);
  f.set('file', new Blob([text], { type: 'text/csv' }), `${kind}.csv`);
  return f;
};
const dry = await call('eventsImport', { who: officer, form: csv('reflections', 'Email,Timestamp\nann@example.org,2026-12-06T09:00:00Z\n', { eventId: ingest.json.eventId, dryRun: 'true' }) });
must(dry.json.dryRun === true && dry.json.metrics.matchedReflections === 1, 'dry run reports the match');
const real = await call('eventsImport', { who: officer, form: csv('reflections', 'Email,Timestamp\nann@example.org,2026-12-06T09:00:00Z\n', { eventId: ingest.json.eventId }) });
must(real.json.created === 1, 'import writes the reflection');
await call('eventsImport', { who: officer, form: csv('luma_guests', 'Name,Email\nZed,zed@example.org\nBad,not-an-email\n', { lumaEventId: 'contract-csv', eventName: 'CSV event', eventStartsAt: '2026-12-10T18:00:00-08:00' }) });
await call('eventsImport', { who: officer, form: csv('reflections', 'Name\nAnn\n', { eventId }), expect: 422 });

// Events
const newEvent = await call('eventCreate', { who: officer, body: { name: 'Contract check social', startsAt: '2026-12-12T19:00:00-08:00', endsAt: '2026-12-12T21:00:00-08:00', location: 'Atrium', url: 'https://example.org/social', hostUserId: 'user-ava' } });
await call('eventUpdate', { who: officer, params: { id: newEvent.json.event.id }, body: { location: 'Lobby', endsAt: null } });
await call('eventDetail', { who: officer, params: { id: newEvent.json.event.id } });

// ───────────── 5. director-only finance and admin writes ─────────────
const budgets = (await call('budgetsGet', { who: officer, query: { fiscalYear: '2027' } })).json;
await call('budgetsPut', { who: directorA, body: { fiscalYear: 2027, lines: budgets.lines.map((l) => ({ accountId: l.account.id, fundId: l.fund?.id ?? null, amountCents: l.amountCents, notes: l.notes })) } });
const julyOfFY2027 = (await call('periods', { who: directorA })).json.periods.find((p) => p.startDate === '2026-07-01');
await call('periodClose', { who: directorA, params: { id: julyOfFY2027.id }, body: { to: 'locked' } });
await call('periodClose', { who: directorA, params: { id: julyOfFY2027.id }, body: { to: 'closed' }, expect: 409 });
await call('periodsCreate', { who: directorA, body: { fiscalYear: 2099 } });
await call('periodsCreate', { who: directorA, body: { fiscalYear: 2099 }, expect: 409 });
await call('adminTokenRevoke', { who: directorA, params: { id: issued.json.token.id } });
await call('agentJobs', { who: agent, expect: 401 });
const audit = await call('adminAudit', { who: directorA, query: { entityType: 'purchase_request', entityId: id } });
must(audit.json.items.some((a) => a.action === 'request.fulfilled'), 'the audit log recorded the settlement');

// Error shapes
await call('requestDetail', { who: officer, params: { id: 'nope' }, expect: 404 });
await call('eventCreate', { who: officer, body: {}, expect: 400 });
await call('search', { who: officer, query: {}, expect: 400 });

// Everything with a zod response schema must have been exercised.
const SKIP = new Set(['authLogin', 'authCallback', 'authLogout', 'authDev']); // redirects: covered by the Go tests
const missed = Object.entries(ENDPOINTS).filter(([n, e]) => !SKIP.has(n) && !covered.has(n)).map(([n, e]) => `${n} (${e.method} ${e.path})`);
if (missed.length) fail('coverage', `endpoints never called: ${missed.join(', ')}`);

if (verbose) console.log(log.join('\n'));
const exercised = covered.size;
console.log(`\n${responses} responses from ${exercised} of ${Object.keys(ENDPOINTS).length} endpoints validated against src/core/contracts.ts`);
if (failures.length) {
  console.error(`\n${failures.length} problem(s):\n- ${failures.join('\n- ')}`);
  if (!verbose) console.error('\n(re-run with --verbose for the full call log)');
  process.exit(1);
}
console.log('contract check passed: 0 schema errors');
process.exit(0); // the spawned server would otherwise keep the event loop alive
