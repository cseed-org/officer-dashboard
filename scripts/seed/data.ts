/**
 * Seed data for local development and tests: a nonprofit chart of accounts mapped to
 * Form 990, funds, fake officers, events with guests, purchase requests, a budget and a
 * few balanced journal entries.
 *
 * THE CHART OF ACCOUNTS AND 990 MAPPINGS ARE A STARTING POINT, NOT TAX ADVICE.
 * Have CSEED's CPA review them before real books are kept.
 */
import type {
  AccountType,
  ContributionKind,
  DonorType,
  Form990Part,
  FunctionalClass,
  FundKind,
  JournalSource,
  NormalBalance,
  PurchaseStatus,
  PurchaseType,
  Restriction,
  Role,
} from '../../src/core/domain';

export const FISCAL_YEAR = 2027; // Jul 2026 – Jun 2027

// ───────────── People ─────────────
export interface SeedUser { id: string; discordId: string; name: string; role: Role; title: string; email: string }
export const users: SeedUser[] = [
  { id: 'user-director', discordId: '100000000000000001', name: 'Jordan Lee', role: 'director', title: 'Executive Director', email: 'jordan@example.org' },
  { id: 'user-priya', discordId: '100000000000000002', name: 'Priya Shah', role: 'officer', title: 'VP Events', email: 'priya@example.org' },
  { id: 'user-marcus', discordId: '100000000000000003', name: 'Marcus Chen', role: 'officer', title: 'Treasurer', email: 'marcus@example.org' },
  { id: 'user-ava', discordId: '100000000000000004', name: 'Ava Romero', role: 'officer', title: 'BuildHer Lead', email: 'ava@example.org' },
];

// ───────────── Chart of accounts ─────────────
export interface SeedAccount {
  code: string;
  name: string;
  type: AccountType;
  normal: NormalBalance;
  part: Form990Part | null;
  line: string | null;
  label: string | null;
  restriction?: Restriction;
  budgetable?: boolean;
  description?: string;
}
const A = (a: SeedAccount) => a;
export const accounts: SeedAccount[] = [
  // Assets — Form 990 Part X (balance sheet)
  A({ code: '1000', name: 'Checking', type: 'asset', normal: 'debit', part: 'X', line: '1', label: 'Cash—non-interest-bearing' }),
  A({ code: '1100', name: 'Accounts receivable', type: 'asset', normal: 'debit', part: 'X', line: '4', label: 'Accounts receivable, net' }),
  A({ code: '1150', name: 'Grants & pledges receivable', type: 'asset', normal: 'debit', part: 'X', line: '3', label: 'Pledges and grants receivable, net' }),
  A({ code: '1300', name: 'Prepaid expenses', type: 'asset', normal: 'debit', part: 'X', line: '9', label: 'Prepaid expenses and deferred charges' }),
  // Liabilities — Part X
  A({ code: '2000', name: 'Accounts payable', type: 'liability', normal: 'credit', part: 'X', line: '17', label: 'Accounts payable and accrued expenses' }),
  A({ code: '2100', name: 'Reimbursements payable', type: 'liability', normal: 'credit', part: 'X', line: '17', label: 'Accounts payable and accrued expenses', description: 'Owed to officers for approved out-of-pocket purchases' }),
  // Net assets — Part X, ASC 958 classes
  A({ code: '3000', name: 'Net assets without donor restrictions', type: 'net_asset', normal: 'credit', part: 'X', line: '27', label: 'Net assets without donor restrictions', restriction: 'without_donor' }),
  A({ code: '3100', name: 'Net assets with donor restrictions', type: 'net_asset', normal: 'credit', part: 'X', line: '28', label: 'Net assets with donor restrictions', restriction: 'with_donor' }),
  // Revenue — Part VIII
  A({ code: '4000', name: 'Contributions — individuals', type: 'revenue', normal: 'credit', part: 'VIII', line: '1f', label: 'All other contributions, gifts, grants' }),
  A({ code: '4100', name: 'Government grants', type: 'revenue', normal: 'credit', part: 'VIII', line: '1e', label: 'Government grants (contributions)' }),
  A({ code: '4110', name: 'Foundation & private grants', type: 'revenue', normal: 'credit', part: 'VIII', line: '1f', label: 'All other contributions, gifts, grants' }),
  A({ code: '4200', name: 'Corporate sponsorships', type: 'revenue', normal: 'credit', part: 'VIII', line: '1f', label: 'All other contributions (qualified sponsorship payments)', description: 'Qualified sponsorship payments; advertising-style deals may be UBI — ask the CPA' }),
  A({ code: '4300', name: 'Program service revenue', type: 'revenue', normal: 'credit', part: 'VIII', line: '2a', label: 'Program service revenue', description: 'Event tickets, workshop fees' }),
  A({ code: '4400', name: 'In-kind contributions', type: 'revenue', normal: 'credit', part: 'VIII', line: '1f', label: 'All other contributions (noncash; also reported on line 1g)' }),
  // Expenses — Part IX (functional split by journal line functional_class)
  A({ code: '5100', name: 'Program supplies', type: 'expense', normal: 'debit', part: 'IX', line: '24a', label: 'Other expenses: program supplies', budgetable: true }),
  A({ code: '5200', name: 'Food & events', type: 'expense', normal: 'debit', part: 'IX', line: '19', label: 'Conferences, conventions, and meetings', budgetable: true }),
  A({ code: '5300', name: 'Travel', type: 'expense', normal: 'debit', part: 'IX', line: '17', label: 'Travel', budgetable: true }),
  A({ code: '5400', name: 'Software & IT', type: 'expense', normal: 'debit', part: 'IX', line: '14', label: 'Information technology', budgetable: true }),
  A({ code: '5500', name: 'Printing & publications', type: 'expense', normal: 'debit', part: 'IX', line: '13', label: 'Office expenses (printing)', budgetable: true }),
  A({ code: '5900', name: 'Other expenses', type: 'expense', normal: 'debit', part: 'IX', line: '24e', label: 'All other expenses', budgetable: true }),
];
export const acct = (code: string) => `acct-${code}`;

// ───────────── Donors & funds ─────────────
export interface SeedDonor { id: string; name: string; type: DonorType; email: string | null }
export const donors: SeedDonor[] = [
  { id: 'donor-foundation', name: 'Sample Community Foundation', type: 'foundation', email: 'grants@example.org' },
  { id: 'donor-city', name: 'Sample City Office of Economic Development', type: 'government', email: null },
  { id: 'donor-acme', name: 'Acme Software (sample sponsor)', type: 'corporate', email: 'sponsorships@example.com' },
  { id: 'donor-alum', name: 'Sam Rivera (sample alum)', type: 'individual', email: 'sam@example.com' },
];

export interface SeedFund {
  id: string; code: string; name: string; kind: FundKind; restriction: Restriction;
  purpose: string | null; donorId: string | null; start: string | null; end: string | null; awardedCents: number | null;
}
export const funds: SeedFund[] = [
  { id: 'fund-general', code: 'GEN', name: 'General operating', kind: 'general', restriction: 'without_donor', purpose: 'Unrestricted operations', donorId: null, start: null, end: null, awardedCents: null },
  { id: 'fund-buildspace', code: 'GR-BUILDSPACE', name: 'Buildspace program grant (sample)', kind: 'grant', restriction: 'with_donor', purpose: 'Buildspace cohort supplies, food and demo night', donorId: 'donor-foundation', start: '2026-08-01', end: '2027-06-30', awardedCents: 500_000 },
  { id: 'fund-buildher', code: 'GR-BUILDHER', name: 'BuildHer workshop grant (sample)', kind: 'grant', restriction: 'with_donor', purpose: 'BuildHer workshop series for women in tech', donorId: 'donor-city', start: '2026-10-01', end: '2027-05-31', awardedCents: 250_000 },
];

// ───────────── Events ─────────────
export interface SeedEvent {
  id: string; lumaId: string; name: string; startsAt: string; endsAt: string; location: string;
  host: string; registered: number; checkInPct: number; reflectionPct: number;
}
export const events: SeedEvent[] = [
  { id: 'event-kickoff', lumaId: 'evt-kickoff26', name: 'Fall Kickoff', startsAt: '2026-09-18T18:00:00-07:00', endsAt: '2026-09-18T20:00:00-07:00', location: 'Student Union 210', host: 'user-priya', registered: 64, checkInPct: 0.78, reflectionPct: 0.42 },
  { id: 'event-resume', lumaId: 'evt-resume26', name: 'Resume Workshop', startsAt: '2026-10-02T17:30:00-07:00', endsAt: '2026-10-02T19:00:00-07:00', location: 'Library Lab B', host: 'user-marcus', registered: 38, checkInPct: 0.66, reflectionPct: 0.6 },
  { id: 'event-buildspace-demo', lumaId: 'evt-bsdemo26', name: 'Buildspace Demo Night', startsAt: '2026-10-09T18:30:00-07:00', endsAt: '2026-10-09T21:00:00-07:00', location: 'Engineering Atrium', host: 'user-priya', registered: 92, checkInPct: 0.71, reflectionPct: 0.35 },
  { id: 'event-buildher-1', lumaId: 'evt-bh1-26', name: 'BuildHer: Intro to Web Apps', startsAt: '2026-10-16T17:00:00-07:00', endsAt: '2026-10-16T19:30:00-07:00', location: 'Engineering 120', host: 'user-ava', registered: 30, checkInPct: 0.83, reflectionPct: 0.72 },
  { id: 'event-founder-talk', lumaId: 'evt-founder26', name: 'Founder Fireside', startsAt: '2026-10-23T18:00:00-07:00', endsAt: '2026-10-23T19:30:00-07:00', location: 'Business Hall 300', host: 'user-marcus', registered: 51, checkInPct: 0.55, reflectionPct: 0.25 },
  { id: 'event-hack-night', lumaId: 'evt-hack26', name: 'Hack Night', startsAt: '2026-11-13T18:00:00-08:00', endsAt: '2026-11-13T23:00:00-08:00', location: 'Engineering Atrium', host: 'user-ava', registered: 44, checkInPct: 0, reflectionPct: 0 },
];

// ───────────── Purchase requests ─────────────
export interface SeedRequest {
  id: string; number: string; requester: string; type: PurchaseType; title: string; vendor: string; itemUrl: string | null;
  quantity: number; unitCostCents: number; taxShipCents: number; account: string; fund: string; event: string | null;
  functionalClass: FunctionalClass; justification: string; status: PurchaseStatus;
  submittedAt: string | null; decidedAt: string | null; decision?: { decision: 'approved' | 'denied'; comment: string };
}
export const requests: SeedRequest[] = [
  { id: 'req-0001', number: '2027-0001', requester: 'user-priya', type: 'reimbursement', title: 'Kickoff pizza', vendor: "Pagliacci", itemUrl: null, quantity: 15, unitCostCents: 2450, taxShipCents: 3750, account: '5200', fund: 'fund-general', event: 'event-kickoff', functionalClass: 'program', justification: 'Food for 60 attendees at Fall Kickoff', status: 'reimbursed', submittedAt: '2026-09-12', decidedAt: '2026-09-13', decision: { decision: 'approved', comment: 'Within the kickoff budget.' } },
  { id: 'req-0002', number: '2027-0002', requester: 'user-ava', type: 'club_purchase', title: 'Arduino starter kits', vendor: 'Adafruit', itemUrl: 'https://www.adafruit.com/', quantity: 8, unitCostCents: 2195, taxShipCents: 1080, account: '5100', fund: 'fund-buildspace', event: 'event-buildspace-demo', functionalClass: 'program', justification: 'Hardware for Buildspace teams; grant-funded', status: 'fulfilled', submittedAt: '2026-09-20', decidedAt: '2026-09-21', decision: { decision: 'approved', comment: 'Charge to the Buildspace grant.' } },
  { id: 'req-0003', number: '2027-0003', requester: 'user-marcus', type: 'club_purchase', title: 'Figma education upgrade', vendor: 'Figma', itemUrl: 'https://www.figma.com/education/', quantity: 1, unitCostCents: 14400, taxShipCents: 0, account: '5400', fund: 'fund-general', event: null, functionalClass: 'mgmt_general', justification: 'Shared design workspace for officers', status: 'denied', submittedAt: '2026-10-05', decidedAt: '2026-10-06', decision: { decision: 'denied', comment: 'Education plan is free; apply for that first.' } },
  { id: 'req-0004', number: '2027-0004', requester: 'user-ava', type: 'club_purchase', title: 'BuildHer workshop snacks', vendor: 'Costco', itemUrl: null, quantity: 1, unitCostCents: 18500, taxShipCents: 1850, account: '5200', fund: 'fund-buildher', event: 'event-hack-night', functionalClass: 'program', justification: 'Snacks and drinks for 40 people at Hack Night', status: 'approved', submittedAt: '2026-10-27', decidedAt: '2026-10-28', decision: { decision: 'approved', comment: 'Approved; keep the receipt.' } },
  { id: 'req-0005', number: '2027-0005', requester: 'user-priya', type: 'reimbursement', title: 'Demo night banner printing', vendor: 'FedEx Office', itemUrl: null, quantity: 2, unitCostCents: 4200, taxShipCents: 860, account: '5500', fund: 'fund-general', event: 'event-buildspace-demo', functionalClass: 'program', justification: 'Two vinyl banners for Demo Night', status: 'submitted', submittedAt: '2026-10-29', decidedAt: null },
  { id: 'req-0006', number: '2027-0006', requester: 'user-marcus', type: 'club_purchase', title: 'Hack Night prizes', vendor: 'Amazon', itemUrl: null, quantity: 3, unitCostCents: 5000, taxShipCents: 0, account: '5900', fund: 'fund-general', event: 'event-hack-night', functionalClass: 'program', justification: 'Gift cards for top three teams', status: 'draft', submittedAt: null, decidedAt: null },
];

// ───────────── Budget (FY2027) ─────────────
export const budgets: { account: string; fund: string | null; amountCents: number; notes: string }[] = [
  { account: '5200', fund: 'fund-general', amountCents: 300_000, notes: 'Meeting food, kickoff, socials' },
  { account: '5100', fund: 'fund-general', amountCents: 80_000, notes: 'General supplies' },
  { account: '5300', fund: 'fund-general', amountCents: 150_000, notes: 'Conference travel' },
  { account: '5400', fund: 'fund-general', amountCents: 60_000, notes: 'Domains, hosting, tools' },
  { account: '5500', fund: 'fund-general', amountCents: 40_000, notes: 'Banners, flyers' },
  { account: '5900', fund: 'fund-general', amountCents: 50_000, notes: 'Prizes, misc' },
  { account: '5100', fund: 'fund-buildspace', amountCents: 300_000, notes: 'Grant: hardware and supplies' },
  { account: '5200', fund: 'fund-buildspace', amountCents: 200_000, notes: 'Grant: demo night' },
  { account: '5200', fund: 'fund-buildher', amountCents: 150_000, notes: 'Grant: workshop food' },
  { account: '5100', fund: 'fund-buildher', amountCents: 100_000, notes: 'Grant: workshop materials' },
];

// ───────────── Journal ─────────────
export interface SeedLine { account: string; fund: string; debit?: number; credit?: number; fc?: FunctionalClass }
export interface SeedEntry { id: string; date: string; memo: string; source: JournalSource; request?: string; lines: SeedLine[] }
export const journal: SeedEntry[] = [
  { id: 'je-0001', date: '2026-07-01', memo: 'Opening balance (sample)', source: 'opening_balance', lines: [
    { account: '1000', fund: 'fund-general', debit: 320_000 },
    { account: '3000', fund: 'fund-general', credit: 320_000 },
  ] },
  { id: 'je-0002', date: '2026-08-15', memo: 'Buildspace grant received', source: 'contribution', lines: [
    { account: '1000', fund: 'fund-buildspace', debit: 500_000 },
    { account: '4110', fund: 'fund-buildspace', credit: 500_000 },
  ] },
  { id: 'je-0003', date: '2026-09-05', memo: 'Acme sponsorship', source: 'contribution', lines: [
    { account: '1000', fund: 'fund-general', debit: 150_000 },
    { account: '4200', fund: 'fund-general', credit: 150_000 },
  ] },
  { id: 'je-0004', date: '2026-09-15', memo: 'Kickoff pizza reimbursement owed (2027-0001)', source: 'purchase_request', request: 'req-0001', lines: [
    { account: '5200', fund: 'fund-general', debit: 40_500, fc: 'program' },
    { account: '2100', fund: 'fund-general', credit: 40_500 },
  ] },
  { id: 'je-0005', date: '2026-09-16', memo: 'Pay reimbursement to Priya Shah (2027-0001)', source: 'purchase_request', request: 'req-0001', lines: [
    { account: '2100', fund: 'fund-general', debit: 40_500 },
    { account: '1000', fund: 'fund-general', credit: 40_500 },
  ] },
  { id: 'je-0006', date: '2026-09-20', memo: 'Gift from Sam Rivera', source: 'contribution', lines: [
    { account: '1000', fund: 'fund-general', debit: 30_000 },
    { account: '4000', fund: 'fund-general', credit: 30_000 },
  ] },
  { id: 'je-0007', date: '2026-09-24', memo: 'Arduino kits, Adafruit (2027-0002)', source: 'purchase_request', request: 'req-0002', lines: [
    { account: '5100', fund: 'fund-buildspace', debit: 18_640, fc: 'program' },
    { account: '1000', fund: 'fund-buildspace', credit: 18_640 },
  ] },
  { id: 'je-0008', date: '2026-10-01', memo: 'BuildHer grant awarded, payment pending', source: 'contribution', lines: [
    { account: '1150', fund: 'fund-buildher', debit: 250_000 },
    { account: '4100', fund: 'fund-buildher', credit: 250_000 },
  ] },
];

export const contributions: { id: string; donor: string; fund: string; date: string; kind: ContributionKind; amountCents: number; qpq: number; entry: string }[] = [
  { id: 'contrib-0001', donor: 'donor-foundation', fund: 'fund-buildspace', date: '2026-08-15', kind: 'check', amountCents: 500_000, qpq: 0, entry: 'je-0002' },
  { id: 'contrib-0002', donor: 'donor-acme', fund: 'fund-general', date: '2026-09-05', kind: 'online', amountCents: 150_000, qpq: 0, entry: 'je-0003' },
  { id: 'contrib-0003', donor: 'donor-alum', fund: 'fund-general', date: '2026-09-20', kind: 'online', amountCents: 30_000, qpq: 0, entry: 'je-0006' },
  { id: 'contrib-0004', donor: 'donor-city', fund: 'fund-buildher', date: '2026-10-01', kind: 'other', amountCents: 250_000, qpq: 0, entry: 'je-0008' },
];
