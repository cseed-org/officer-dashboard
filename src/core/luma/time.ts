/** CSVs without an offset are Washington local time; API ingest always requires an explicit offset. */
export function csvTimestamp(value: string | null): string | null {
  if (!value) return null;
  if (/(?:z|[+-]\d{2}:?\d{2})$/i.test(value)) {
    const instant = new Date(value);
    if (!Number.isFinite(instant.getTime()))
      throw new Error(`Invalid timestamp: ${value}`);
    return instant.toISOString();
  }
  const iso =
    /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{1,2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?)?$/.exec(
      value,
    );
  const us =
    /^(\d{1,2})\/(\d{1,2})\/(\d{4})\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*(AM|PM)?$/i.exec(
      value,
    );
  if (!iso && !us)
    throw new Error(
      `Ambiguous timestamp; use ISO 8601 or MM/DD/YYYY HH:mm:ss: ${value}`,
    );
  const y = Number(iso?.[1] ?? us![3]),
    m = Number(iso?.[2] ?? us![1]),
    d = Number(iso?.[3] ?? us![2]);
  let h = Number(iso?.[4] ?? us?.[4] ?? 0);
  const min = Number(iso?.[5] ?? us?.[5] ?? 0),
    sec = Number(iso?.[6] ?? us?.[6] ?? 0),
    ms = Number((iso?.[7] ?? '').padEnd(3, '0'));
  if (us?.[7]) {
    if (h < 1 || h > 12) throw new Error('Invalid 12-hour timestamp');
    h = (h % 12) + (us[7].toUpperCase() === 'PM' ? 12 : 0);
  }
  const desired = Date.UTC(y, m - 1, d, h, min, sec, ms),
    parts = new Date(desired);
  if (
    parts.getUTCFullYear() !== y ||
    parts.getUTCMonth() !== m - 1 ||
    parts.getUTCDate() !== d ||
    h > 23 ||
    min > 59 ||
    sec > 59
  )
    throw new Error(`Invalid timestamp: ${value}`);
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/Los_Angeles',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  });
  const local = (n: number) => {
    const p = Object.fromEntries(
      formatter.formatToParts(n).map((p) => [p.type, p.value]),
    );
    return Date.UTC(
      Number(p.year),
      Number(p.month) - 1,
      Number(p.day),
      Number(p.hour),
      Number(p.minute),
      Number(p.second),
      ms,
    );
  };
  // Try both standard and daylight offsets; ambiguous autumn times take the earlier instant.
  const candidates = [desired + 7 * 3600000, desired + 8 * 3600000].filter(
    (n) => local(n) === desired,
  );
  if (!candidates.length)
    throw new Error(
      `Nonexistent Pacific local time; supply an offset: ${value}`,
    );
  return new Date(Math.min(...candidates)).toISOString();
}

/** Calendar date in the organization's Washington time zone. */
export function pacificDate(value: Date | string): string {
 const parts = Object.fromEntries(new Intl.DateTimeFormat('en-US', { timeZone: 'America/Los_Angeles',year: 'numeric',month: '2-digit',day: '2-digit' }).formatToParts(new Date(value)).map(p => [p.type,p.value]));
 return `${parts.year}-${parts.month}-${parts.day}`;
}
