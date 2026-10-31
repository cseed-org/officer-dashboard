import { validateJournalLines, type JournalLineInput } from '../journal';
import { contributionObligations } from '../contributions';
import type {
  AccountType,
  FunctionalClass,
  JournalSource,
  Restriction,
  FiscalPeriodStatus,
} from '../domain';
export interface Line extends JournalLineInput {
  functionalClass?: FunctionalClass | null;
  memo?: string | null;
}
export interface EntryInput {
  id: string;
  entryDate: string;
  memo: string;
  source: JournalSource;
  lines: readonly Line[];
  reversesEntryId?: string | null;
}
export interface Period {
  id: string;
  startDate: string;
  endDate: string;
  status: FiscalPeriodStatus;
}
export interface AccountingContext {
  accounts: readonly {
    id: string;
    type: AccountType;
    active: boolean;
    restriction?: Restriction | null;
  }[];
  funds: readonly { id: string; restriction: Restriction; active: boolean }[];
  periods: readonly Period[];
  reversedEntryIds?: readonly string[];
}
export type PostedEntry = Readonly<
  EntryInput & { status: 'posted'; lines: readonly Readonly<Line>[] }
>;
export function postEntry(
  input: EntryInput,
  context: AccountingContext,
): PostedEntry {
  const validation = validateJournalLines(input.lines);
  if (!validation.ok) throw new Error(validation.errors.join('; '));
  if (!Number.isSafeInteger(validation.totalCents))
    throw new Error('unsafe total cents');
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(input.entryDate) ||
    Number.isNaN(Date.parse(input.entryDate)) ||
    new Date(input.entryDate).toISOString().slice(0, 10) !== input.entryDate
  )
    throw new Error('invalid entry date');
  const periods = context.periods.filter(
    (p) => p.startDate <= input.entryDate && p.endDate >= input.entryDate,
  );
  if (!periods.length || periods.some((p) => p.status !== 'open'))
    throw new Error('entry requires an open period');
  for (const line of input.lines) {
    const account = context.accounts.find(
      (a) => a.id === line.accountId && a.active,
    );
    if (
      !account ||
      !context.funds.some((f) => f.id === line.fundId && f.active)
    )
      throw new Error('inactive or unknown account/fund');
    if (account.type === 'expense' && !line.functionalClass)
      throw new Error('expense requires functional class');
  }
  return Object.freeze({
    ...input,
    status: 'posted',
    lines: Object.freeze(input.lines.map((l) => Object.freeze({ ...l }))),
  });
}
export function reverseEntry(
  entry: PostedEntry,
  input: { id: string; entryDate: string },
  context: AccountingContext,
) {
  if (context.reversedEntryIds?.includes(entry.id))
    throw new Error('entry already reversed');
  return postEntry(
    {
      ...input,
      memo: `Reversal: ${entry.memo}`,
      source: 'reversal',
      reversesEntryId: entry.id,
      lines: entry.lines.map((l) => ({
        ...l,
        debitCents: l.creditCents,
        creditCents: l.debitCents,
      })),
    },
    context,
  );
}
export function closePeriod(period: Period, to: 'closed' | 'locked'): Period {
  if (
    period.status === 'locked' ||
    (period.status === 'closed' && to !== 'locked')
  )
    throw new Error('period cannot reopen');
  return Object.freeze({ ...period, status: to });
}
/** Reclassifies net assets while retaining the grant fund for purpose tracking. */
export function releaseRestriction(
  input: {
    id: string;
    entryDate: string;
    memo: string;
    amountCents: number;
    fundId: string;
    restrictedNetAssetAccountId: string;
    unrestrictedNetAssetAccountId: string;
    availableRestrictedCents: number;
  },
  context: AccountingContext,
) {
  const restricted = context.accounts.find(
    (a) => a.id === input.restrictedNetAssetAccountId,
  );
  const unrestricted = context.accounts.find(
    (a) => a.id === input.unrestrictedNetAssetAccountId,
  );
  if (
    context.funds.find((f) => f.id === input.fundId)?.restriction !==
      'with_donor' ||
    restricted?.type !== 'net_asset' ||
    restricted.restriction !== 'with_donor' ||
    unrestricted?.type !== 'net_asset' ||
    unrestricted.restriction !== 'without_donor'
  )
    throw new Error(
      'release requires donor → without donor net asset accounts',
    );
  if (
    !Number.isSafeInteger(input.availableRestrictedCents) ||
    input.amountCents > input.availableRestrictedCents
  )
    throw new Error('release exceeds available restricted net assets');
  return postEntry(
    {
      ...input,
      source: 'release',
      lines: [
        {
          accountId: restricted.id,
          fundId: input.fundId,
          debitCents: input.amountCents,
          creditCents: 0,
        },
        {
          accountId: unrestricted.id,
          fundId: input.fundId,
          debitCents: 0,
          creditCents: input.amountCents,
        },
      ],
    },
    context,
  );
}
export function recordContribution(
  input: EntryInput & {
    amountCents: number;
    quidProQuoCents: number;
    kind: string;
    inKindDescription?: string;
  },
  context: AccountingContext,
) {
  if (
    !Number.isSafeInteger(input.amountCents) ||
    input.amountCents <= 0 ||
    !Number.isSafeInteger(input.quidProQuoCents) ||
    input.quidProQuoCents < 0 ||
    input.quidProQuoCents > input.amountCents
  )
    throw new Error('invalid contribution cents');
  if (input.kind === 'in_kind' && !input.inKindDescription?.trim())
    throw new Error('in-kind description required');
  const validation = validateJournalLines(input.lines);
  if (!validation.ok || validation.totalCents !== input.amountCents)
    throw new Error('contribution entry must match contribution amount');
  const revenue = input.lines.filter(
    (l) =>
      context.accounts.find((a) => a.id === l.accountId)?.type === 'revenue',
  );
  if (
    revenue.reduce((n, l) => n + l.creditCents - l.debitCents, 0) !==
    input.amountCents
  )
    throw new Error('contribution revenue must match amount');
  if (new Set(input.lines.map((l) => l.fundId)).size !== 1)
    throw new Error('contribution must use one fund');
  const entry = postEntry({ ...input, source: 'contribution' }, context);
  return {
    entry,
    ...contributionObligations(input),
    inKind: input.kind === 'in_kind',
  };
}
