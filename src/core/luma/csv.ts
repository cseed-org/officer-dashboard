import { csvTimestamp } from './time';
import { z } from 'zod';
import { normalizeEmail } from '../metrics';
export interface GuestImport {
  clearFields?: string[];
  email: string;
  name: string | null;
  lumaGuestId: string | null;
  approvalStatus: string | null;
  registeredAt: string | null;
  checkedInAt: string | null;
}
export interface ReflectionImport {
  email: string;
  submittedAt: string;
}
export interface ImportResult<T> {
  rows: T[];
  errors: { row: number; message: string }[];
  totalRows: number;
}
/** RFC 4180 quotes, escaped quotes, CRLF and embedded newlines; no external runtime. */
export function csvRows(text: string, sourceRows?: number[]): string[][] {
  let physicalLine = 1,
    recordLine = 1;
  const rows: string[][] = [];
  let row: string[] = [],
    field = '',
    quoted = false,
    ended = false;
  text = text.replace(/^\uFEFF/, '');
  for (let i = 0; i < text.length; i++) {
    const c = text[i]!;
    if (c === '\r' || (c === '\n' && text[i - 1] !== '\r')) physicalLine++;
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else {
          quoted = false;
          ended = true;
        }
      } else field += c;
    } else if (c === '"') {
      if (field || ended) throw new Error('Malformed CSV quote');
      quoted = true;
    } else if (c === ',') {
      row.push(field);
      field = '';
      ended = false;
    } else if (c === '\n' || c === '\r') {
      if (c === '\r' && text[i + 1] === '\n') i++;
      row.push(field);
      if (row.some((v) => v.trim())) {
        rows.push(row);
        sourceRows?.push(recordLine);
      }
      recordLine = physicalLine;
      row = [];
      field = '';
      ended = false;
    } else {
      if (ended && c.trim()) throw new Error('Unexpected text after CSV quote');
      field += c;
    }
  }
  if (quoted) throw new Error('Unterminated CSV quote');
  row.push(field);
  if (row.some((v) => v.trim())) {
    rows.push(row);
    sourceRows?.push(recordLine);
  }
  return rows;
}
const key = (s: string) =>
  s
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '');
const aliases = {
  email: ['email', 'emailaddress', 'guestemail', 'youremail'],
  name: ['name', 'fullname', 'guestname'],
  lumaGuestId: ['ap i id'.replaceAll(' ', ''), 'guestid', 'lumaguestid'],
  approvalStatus: ['approvalstatus', 'status', 'registrationstatus'],
  registeredAt: [
    'registeredat',
    'registrationtime',
    'registeredtime',
    'createdat',
  ],
  checkedInAt: ['checkedinat', 'checkedintime', 'checkintime', 'checkinat'],
  submittedAt: ['timestamp', 'submittedat', 'submissiontime', 'submittedtime'],
};
function parse<T>(
  text: string,
  kind: 'guests' | 'reflections',
  convert: (get: (k: keyof typeof aliases) => string | null) => T,
): ImportResult<T> {
  const positions: number[] = [];
  const [header, ...data] = csvRows(text, positions);
  if (!header) throw new Error('CSV is empty');
  const keys = header.map(key);
  const index = (k: keyof typeof aliases) =>
    keys.findIndex((h) => aliases[k].includes(h));
  if (index('email') < 0) throw new Error('Missing email header');
  if (kind === 'reflections' && index('submittedAt') < 0)
    throw new Error('Missing timestamp header');
  const rows: T[] = [],
    errors: ImportResult<T>['errors'] = [];
  data.forEach((row, i) => {
    try {
      rows.push(convert((k) => row[index(k)]?.trim() || null));
    } catch (e) {
      errors.push({
        row: positions[i + 1] ?? i + 2,
        message: String(e instanceof Error ? e.message : e),
      });
    }
  });
  return { rows, errors, totalRows: data.length };
}
function email(value: string | null) {
  return z.email().parse(normalizeEmail(value ?? ''));
}
const date = csvTimestamp;
export function parseGuests(text: string) {
  return parse(text, 'guests', (g) => ({
    email: email(g('email')),
    name: g('name'),
    lumaGuestId: g('lumaGuestId'),
    approvalStatus: g('approvalStatus'),
    registeredAt: date(g('registeredAt')),
    checkedInAt: date(g('checkedInAt')),
  }));
}
export function parseReflections(text: string) {
  return parse(text, 'reflections', (g) => {
    const submittedAt = date(g('submittedAt'));
    if (!submittedAt) throw new Error('Missing timestamp');
    return { email: email(g('email')), submittedAt };
  });
}
