#!/usr/bin/env node
/**
 * Generates server/internal/api/mcp_tools.json: the MCP tools /mcp exposes (docs/mcp.md).
 *
 * Each tool is one dashboard API endpoint from ENDPOINTS in src/core/contracts.ts. The method,
 * path, minimum role and input JSON Schema come from the contract; this file only chooses
 * which endpoints become tools and how they are named and described for an AI agent.
 *
 *   node scripts/gen-mcp-tools.mjs          # write the file
 *   node scripts/gen-mcp-tools.mjs --check  # fail if the file is out of date (CI)
 *
 * Needs `npm ci` (zod, esbuild).
 */
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const out = join(root, 'server/internal/api/mcp_tools.json');
/**
 * The tools. Key = ENDPOINTS name. `hint`: 'read' (read-only), 'destructive' (deletes or
 * cannot be undone; clients confirm first) or 'idempotent' (safe to repeat). Writes default
 * to plain (not read-only, not destructive).
 *
 * Not exposed: sign-in, onboarding and Google Calendar connection (browser flows), file
 * downloads (binary), /api/admin/* (owners, roles, accounts, service tokens: an agent must
 * never manage access or credentials), officer invites (an invite link is a sign-up
 * credential), the service-token APIs (/api/ingest, /api/agent), the builder/mentor portal
 * (/api/portal/*: portal accounts, never an officer; refused below),
 * club settings, preferences/widget layout and Cloudflare sync.
 */
const TOOLS = {
  // ── context ──
  me: { name: 'whoami', title: 'Who am I', hint: 'read', description: 'The signed-in officer this agent acts as: id, name, role (officer < director < leadership), owner flag, subteam and time zone. Call first: role decides which other tools will succeed.' },
  meStats: { name: 'my_stats', title: 'My hosting stats', hint: 'read', description: "The officer's own hosting stats: events hosted, attendees, last event." },
  search: { name: 'search', title: 'Search the dashboard', hint: 'read', description: 'Full-text search across events, purchase requests, officers/users, accounts and external contacts. Use it to turn a name into an id before calling other tools.' },
  overview: { name: 'dashboard_overview', title: 'Dashboard overview', hint: 'read', description: 'Home-page overview: upcoming events, counts and items needing attention.' },
  lookups: { name: 'lookups', title: 'Lookup lists', hint: 'read', description: 'Reference lists the other tools take ids from: subteams, chart-of-accounts options, funds and similar. Call before creating purchase requests or events if you need an account, fund or subteam id.' },
  calendarFeed: { name: 'calendar_feed', title: 'Calendar feed', hint: 'read', description: 'Everything on the club calendar between two dates (events, purchase deadlines, Buildspace dates). start and end are YYYY-MM-DD.' },
  clubSettingsGet: { name: 'club_settings', title: 'Club settings', hint: 'read', description: 'Club-wide settings: default time zone, location and club links.' },

  // ── events ──
  eventsList: { name: 'list_events', title: 'List events', hint: 'read', description: 'List club events, newest first, with filters (status, search text, date range). Paginate with cursor/nextCursor.' },
  eventDetail: { name: 'get_event', title: 'Get event', hint: 'read', description: 'Full details of one event: schedule, venue, hosts, Luma link, design need, counts.' },
  eventCreate: { name: 'create_event', title: 'Create event', description: 'Plan a new club event. Times are ISO 8601 with offset; use whoami/club_settings for the time zone.' },
  eventUpdate: { name: 'update_event', title: 'Update event', description: 'Change fields of an event (partial update: send only what changes).' },
  eventDelete: { name: 'delete_event', title: 'Delete event', hint: 'destructive', description: 'Permanently delete an event. Ask the officer to confirm first.' },
  eventMetrics: { name: 'get_event_metrics', title: 'Event metrics', hint: 'read', description: 'Registration, check-in and reflection metrics for one event.' },
  eventsImport: { name: 'import_event_csv', title: 'Import Luma CSV', multipart: 'csv', description: 'Import a Luma guest export or survey CSV into an event (registrations/check-ins or reflections). Pass the CSV text in `content`. Use dryRun "true" first to preview.' },
  eventDesignNeed: { name: 'set_event_design_need', title: 'Set event design need', hint: 'idempotent', description: 'Say whether an event needs design work (posters, slides…). Only director+ or the host may mark "not needed".' },
  designRequestDraftFromEvent: { name: 'draft_design_request_for_event', title: 'Draft design request from event', hint: 'read', description: 'A design request prefilled from an event (title, due date, brief). Review it, then create it with create_design_request_for_event.' },
  designRequestCreateFromEvent: { name: 'create_design_request_for_event', title: 'Create design request for event', description: 'Create a design request linked to an event.' },

  // ── purchase requests ──
  requestsList: { name: 'list_purchase_requests', title: 'List purchase requests', hint: 'read', description: 'List purchase/reimbursement requests. scope "mine" (default) or "all"; filter by status. Paginate with cursor.' },
  requestDetail: { name: 'get_purchase_request', title: 'Get purchase request', hint: 'read', description: 'One purchase request with line details, approvals, attachments and history.' },
  requestCreate: { name: 'create_purchase_request', title: 'Create purchase request', description: 'Draft a purchase or reimbursement request (amounts in integer cents). It stays a draft until submit_purchase_request.' },
  requestUpdate: { name: 'update_purchase_request', title: 'Update purchase request', description: 'Edit a draft (or returned) purchase request.' },
  requestSubmit: { name: 'submit_purchase_request', title: 'Submit purchase request', description: 'Submit a draft purchase request for director approval.' },
  requestAttach: { name: 'attach_receipt', title: 'Attach receipt', multipart: 'file', description: 'Attach a receipt or quote (PDF or image, max 10 MB) to a purchase request. Pass the file as base64 in `contentBase64` with its `mimeType` and `filename`.' },
  requestApprove: { name: 'approve_purchase_request', title: 'Approve purchase request', description: 'Approve a submitted purchase request (director+, never your own). Confirm with the officer before approving money.' },
  requestDeny: { name: 'deny_purchase_request', title: 'Deny purchase request', description: 'Deny a submitted purchase request with a reason (director+).' },
  requestTransition: { name: 'transition_purchase_request', title: 'Advance purchase request', description: 'Move an approved purchase request through fulfilment: ordered, settled (with actual amount), closed, cancelled (director+). Confirm with the officer before settling money.' },
  requestDelete: { name: 'delete_purchase_request', title: 'Delete purchase request', hint: 'destructive', description: 'Delete a draft purchase request. Ask the officer to confirm first.' },

  // ── finance ──
  financeSummary: { name: 'finance_summary', title: 'Finance summary', hint: 'read', description: 'Club finance summary for a fiscal year: balances, spending by budget, pending requests.' },
  financeReport: { name: 'finance_report', title: 'Finance report', hint: 'read', description: 'A financial report by name. Officers may read budget-vs-actual and fund-balances; the others need the director role.' },
  budgetsGet: { name: 'get_budgets', title: 'Get budgets', hint: 'read', description: 'Budget lines for a fiscal year.' },
  budgetsPut: { name: 'set_budgets', title: 'Set budgets', hint: 'idempotent', description: 'Replace budget lines for a fiscal year (director+). Confirm with the officer first.' },
  periods: { name: 'list_fiscal_periods', title: 'List fiscal periods', hint: 'read', description: 'Fiscal periods and whether each is closed (director+).' },
  periodsCreate: { name: 'create_fiscal_periods', title: 'Create fiscal periods', description: "Create a fiscal year's periods (director+)." },
  periodClose: { name: 'close_fiscal_period', title: 'Close fiscal period', hint: 'destructive', description: 'Close a fiscal period so no more entries post to it (director+). Cannot be undone here; confirm first.' },
  accounts: { name: 'list_accounts', title: 'Chart of accounts', hint: 'read', description: 'The chart of accounts (director+).' },

  // ── design board ──
  designRequestsList: { name: 'list_design_requests', title: 'List design requests', hint: 'read', description: 'All design requests on the Kanban board with stage, assignee and due date, plus upcoming events that still need design.' },
  designRequestDetail: { name: 'get_design_request', title: 'Get design request', hint: 'read', description: 'One design request with brief, comments, files and what you may do with it.' },
  designRequestCreate: { name: 'create_design_request', title: 'Create design request', description: 'Ask the Design team for a design (poster, social post, slides…).' },
  designRequestUpdate: { name: 'update_design_request', title: 'Update design request', description: 'Edit a design request (partial update).' },
  designRequestAssign: { name: 'assign_design_request', title: 'Assign design request', description: 'Assign or unassign the designer (Design subteam or director+).' },
  designRequestMove: { name: 'move_design_request', title: 'Move design request', description: 'Move a design request to another board stage (Design subteam or director+).' },
  designRequestApprove: { name: 'approve_design_request', title: 'Approve design', description: 'Approve the version in review (requester only).' },
  designRequestComment: { name: 'comment_on_design_request', title: 'Comment on design request', description: 'Add a comment to a design request.' },
  designRequestUpload: { name: 'upload_design_file', title: 'Upload design file', multipart: 'file', description: 'Attach a reference or deliverable file to a design request. Pass base64 in `contentBase64` with `mimeType` and `filename`.' },
  designRequestDelete: { name: 'delete_design_request', title: 'Delete design request', hint: 'destructive', description: 'Delete a design request. Ask the officer to confirm first.' },

  // ── external contacts ──
  contactsList: { name: 'list_contacts', title: 'List external contacts', hint: 'read', description: 'External contacts (speakers, sponsors, partners) with filters.' },
  contactDetail: { name: 'get_contact', title: 'Get contact', hint: 'read', description: 'One external contact with speaking history and outreach log.' },
  contactCreate: { name: 'create_contact', title: 'Create contact', description: 'Add an external contact.' },
  contactUpdate: { name: 'update_contact', title: 'Update contact', description: 'Edit an external contact (partial update).' },
  contactOutreachCreate: { name: 'log_outreach', title: 'Log outreach', description: 'Record outreach to a contact (channel, outcome, notes).' },
  contactDelete: { name: 'delete_contact', title: 'Delete contact', hint: 'destructive', description: 'Delete an external contact. Ask the officer to confirm first.' },

  // ── members ──
  membersList: { name: 'list_members', title: 'List members', hint: 'read', description: 'Search club members (the D1 mirror).' },
  memberDetail: { name: 'get_member', title: 'Get member', hint: 'read', description: 'One member record.' },
  memberUpdate: { name: 'update_member', title: 'Update member', description: 'Edit a member record (director+).' },
  membersExport: { name: 'export_members_csv', title: 'Export members CSV', hint: 'read', description: 'Members as CSV text, with the same filters as list_members (director+).' },

  // ── officers ──
  officersList: { name: 'list_officers', title: 'List officers', hint: 'read', description: 'The officer directory: names, roles, titles, subteams, contact details.' },
  officerDetail: { name: 'get_officer', title: 'Get officer', hint: 'read', description: 'One officer profile.' },
  officerActivity: { name: 'get_officer_activity', title: 'Officer activity', hint: 'read', description: "An officer's recent activity across the dashboard." },
  officerUpdate: { name: 'update_officer', title: 'Update officer', description: "Change an officer's title, subteam or start term (director+)." },

  // ── Buildspace Launchpad ──
  buildspaceList: { name: 'buildspace_overview', title: 'Buildspace overview', hint: 'read', description: 'Buildspace Launchpad: cohorts, projects, people, mentors (with availability and load), builder/mentor accounts, activity and weekly schedule. Filter by `cohort` id; `archived` "1" includes archived projects and people.' },
  buildspaceProjectSave: { name: 'save_buildspace_project', title: 'Save Buildspace project', hint: 'idempotent', description: 'Create or update a Buildspace project by id.' },
  buildspacePersonSave: { name: 'save_buildspace_person', title: 'Save Buildspace person', hint: 'idempotent', description: 'Create or update a Buildspace participant by id.' },
  buildspaceActivityCreate: { name: 'log_buildspace_activity', title: 'Log Buildspace activity', description: "Log a Buildspace weekly check-in, mentor note, request, purchase or absence. `visibility` (\"team\", \"mentors\" or \"officers\", the default) decides who sees it in the builder and mentor portals. Returns the week's summary, not the new entry's id; read buildspace_overview for ids." },
  buildspaceActivityResolve: { name: 'resolve_buildspace_activity', title: 'Resolve Buildspace request', hint: 'idempotent', description: 'Mark an open Buildspace request (activity kind "request") resolved. Find ids in buildspace_overview.' },
  buildspaceSchedulePut: { name: 'set_buildspace_schedule', title: 'Set Buildspace schedule', hint: 'idempotent', description: 'Replace the Buildspace weekly schedule (leadership).' },

  // ── Launchpad cohorts, builder/mentor accounts, mentor roster ──
  launchpadCohortCreate: { name: 'create_launchpad_cohort', title: 'Create Launchpad cohort', description: 'Start a new Launchpad cohort (name, start date, optional end date).' },
  launchpadCohortUpdate: { name: 'update_launchpad_cohort', title: 'Update Launchpad cohort', description: 'Rename a cohort, change its dates, or archive/reactivate it (status "archived" or "active"). Partial update.' },
  launchpadCohortDelete: { name: 'delete_launchpad_cohort', title: 'Delete Launchpad cohort', hint: 'destructive', description: 'Permanently delete a cohort. Ask the officer to confirm first; archiving (update_launchpad_cohort) is usually what they want.' },
  launchpadInvite: { name: 'invite_launchpad_builders', title: 'Invite builders to a cohort', description: 'Email builder invites for a cohort (up to 200 addresses). Through an agent invites go by email only (method "email"): an invite link is a sign-up credential, so links are never returned here. Already-active or duplicate addresses come back in `skipped`.' },
  launchpadAccountResend: { name: 'resend_launchpad_invite', title: 'Resend Launchpad invite', description: 'Email a fresh invite to a pending builder or mentor account (the old link stops working). The link itself is not returned to an agent.' },
  launchpadAccountDisable: { name: 'disable_launchpad_account', title: 'Disable Launchpad account', description: 'Disable a builder or mentor portal account and sign it out everywhere.' },
  launchpadAccountEnable: { name: 'enable_launchpad_account', title: 'Enable Launchpad account', description: 'Re-enable a disabled builder or mentor portal account.' },
  launchpadAccountRevoke: { name: 'revoke_launchpad_invite', title: 'Revoke Launchpad invite', hint: 'destructive', description: 'Revoke a pending builder or mentor invite (the account is removed). Ask the officer to confirm first.' },
  launchpadMentorInvite: { name: 'invite_launchpad_mentor', title: 'Invite mentor to the portal', description: "Email a mentor contact an invite to the mentor portal (uses the contact's email unless `email` is given). Through an agent, email only; the link is never returned." },
  launchpadMentorSettings: { name: 'set_launchpad_mentor_settings', title: 'Set mentor availability', hint: 'idempotent', description: 'Set whether a mentor contact is available for auto-assign and the most projects they take (null for no cap).' },
  launchpadAutoAssignPreview: { name: 'preview_launchpad_mentor_assignment', title: 'Preview mentor auto-assign', hint: 'read', description: "Propose mentor assignments for a cohort's projects without saving anything. Review them with the officer, then apply with apply_launchpad_mentor_assignment." },
  launchpadAutoAssign: { name: 'apply_launchpad_mentor_assignment', title: 'Apply mentor assignments', description: 'Save mentor assignments for projects in a cohort (usually the preview, possibly edited).' },
  launchpadProjectArchive: { name: 'archive_launchpad_project', title: 'Archive Launchpad project', hint: 'idempotent', description: 'Archive a Launchpad project (hidden from the board; restore_launchpad_project brings it back).' },
  launchpadProjectRestore: { name: 'restore_launchpad_project', title: 'Restore Launchpad project', hint: 'idempotent', description: 'Restore an archived Launchpad project.' },
  launchpadProjectDelete: { name: 'delete_launchpad_project', title: 'Delete Launchpad project', hint: 'destructive', description: 'Permanently delete a Launchpad project. Ask the officer to confirm first; archiving is usually what they want.' },
  launchpadPersonArchive: { name: 'archive_launchpad_person', title: 'Archive Launchpad person', hint: 'idempotent', description: 'Archive a Launchpad participant (restore_launchpad_person brings them back).' },
  launchpadPersonRestore: { name: 'restore_launchpad_person', title: 'Restore Launchpad person', hint: 'idempotent', description: 'Restore an archived Launchpad participant.' },
  launchpadPersonDelete: { name: 'delete_launchpad_person', title: 'Delete Launchpad person', hint: 'destructive', description: 'Permanently delete a Launchpad participant. Ask the officer to confirm first; archiving is usually what they want.' },

  // ── me ──
  profileGet: { name: 'get_my_profile', title: 'Get my profile', hint: 'read', description: "The officer's own profile." },
  profilePut: { name: 'update_my_profile', title: 'Update my profile', hint: 'idempotent', description: "Save the officer's own profile (send the full profile)." },
};

// Zod spells date formats out as long regexes; `format` already says it, and every byte of
// the tool list is read by the agent on each conversation.
function slim(node) {
  if (Array.isArray(node)) return node.map(slim);
  if (!node || typeof node !== 'object') return node;
  const out = {};
  for (const [k, v] of Object.entries(node)) {
    if (k === 'pattern' && node.format) continue;
    out[k] = slim(v);
  }
  return out;
}

const RANK = { officer: 1, director: 2, leadership: 3 };

function generate(contracts, z) {
  const { ENDPOINTS } = contracts;
  const toSchema = (s) => {
    const js = z.toJSONSchema(s, { io: 'input', unrepresentable: 'any', target: 'draft-2020-12' });
    delete js.$schema;
    return slim(js);
  };
  const tools = [];
  for (const [key, t] of Object.entries(TOOLS)) {
    const ep = ENDPOINTS[key];
    if (!ep) throw new Error(`TOOLS.${key}: no such endpoint in ENDPOINTS`);
    if (!(ep.auth in RANK)) throw new Error(`TOOLS.${key}: auth ${ep.auth} cannot be an MCP tool`);
    if (ep.path === '/api/portal' || ep.path.startsWith('/api/portal/')) throw new Error(`TOOLS.${key}: portal endpoints are never MCP tools`);
    const properties = {};
    const required = [];
    const pathParams = [...ep.path.matchAll(/:(\w+)/g)].map((m) => m[1]);
    for (const p of pathParams) {
      properties[p] = p === 'report'
        ? { type: 'string', enum: [...contracts.FinanceReportName.options], description: 'The report name.' }
        : { type: 'string', description: `The ${p === 'id' ? 'record' : p} id (from a list_* or search result).` };
      required.push(p);
    }
    let queryKeys = [];
    if (ep.query) {
      const qs = toSchema(ep.query);
      queryKeys = Object.keys(qs.properties ?? {});
      for (const k of queryKeys) properties[k] = qs.properties[k];
      for (const k of qs.required ?? []) if (!required.includes(k)) required.push(k);
    }
    let fieldKeys = [];
    if (t.multipart) {
      const fields = { eventsImport: contracts.CsvImportFields, requestAttach: contracts.AttachmentUploadFields, designRequestUpload: contracts.DesignFileUploadFields }[key];
      if (!fields) throw new Error(`TOOLS.${key}: unknown multipart fields`);
      const fs = toSchema(fields);
      fieldKeys = Object.keys(fs.properties ?? {});
      for (const k of fieldKeys) properties[k] = fs.properties[k];
      for (const k of fs.required ?? []) if (!required.includes(k)) required.push(k);
      properties.filename = { type: 'string', description: 'File name, e.g. receipt.pdf.' };
      if (t.multipart === 'csv') {
        properties.content = { type: 'string', description: 'The CSV text.' };
        required.push('content');
      } else {
        properties.contentBase64 = { type: 'string', description: 'The file bytes, base64-encoded.' };
        properties.mimeType = { type: 'string', description: 'The file MIME type, e.g. application/pdf or image/png.' };
        required.push('contentBase64', 'mimeType', 'filename');
      }
    } else if (ep.body && typeof ep.body === 'object') {
      const bs = toSchema(ep.body);
      properties.body = { ...bs, description: ep.method === 'PATCH' ? 'The fields to change (only these are updated).' : 'The JSON request body.' };
      required.push('body');
    }
    const annotations = { title: t.title, readOnlyHint: t.hint === 'read', destructiveHint: t.hint === 'destructive', idempotentHint: t.hint === 'read' || t.hint === 'idempotent', openWorldHint: false };
    tools.push({
      name: t.name,
      title: t.title,
      description: t.description + (ep.auth !== 'officer' ? ` Requires the ${ep.auth} role.` : ''),
      endpoint: key,
      method: ep.method,
      path: ep.path,
      minRole: ep.auth,
      queryKeys,
      fieldKeys,
      multipart: t.multipart ?? '',
      response: ep.response === 'csv' ? 'text' : 'json',
      annotations,
      inputSchema: { type: 'object', properties, required, additionalProperties: false },
    });
  }
  const names = new Set();
  for (const t of tools) {
    if (names.has(t.name)) throw new Error(`duplicate tool name ${t.name}`);
    names.add(t.name);
  }
  return { generatedFrom: 'src/core/contracts.ts ENDPOINTS via scripts/gen-mcp-tools.mjs; do not edit', tools };
}

async function main() {
  const work = mkdtempSync(join(tmpdir(), 'mcp-tools-'));
  const bundle = join(work, 'contracts.mjs');
  try {
    // One bundle for the contracts and zod, so toJSONSchema sees the same zod instance.
    await build({
      stdin: { contents: "export * from './src/core/contracts.ts'; export { z } from 'zod';", resolveDir: root, loader: 'ts' },
      bundle: true, format: 'esm', platform: 'node', outfile: bundle, logLevel: 'error',
    });
    const contracts = await import(pathToFileURL(bundle).href);
    const { z } = contracts;
    const json = JSON.stringify(generate(contracts, z), null, 2) + '\n';
    if (process.argv.includes('--check')) {
      let current = '';
      try { current = readFileSync(out, 'utf8'); } catch {}
      if (current !== json) {
        console.error('server/internal/api/mcp_tools.json is out of date: run `node scripts/gen-mcp-tools.mjs`');
        process.exit(1);
      }
      console.log('mcp_tools.json is up to date');
    } else {
      writeFileSync(out, json);
      console.log(`wrote ${out}`);
    }
  } finally {
    rmSync(work, { recursive: true, force: true });
  }
}

await main();
