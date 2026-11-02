/** One effective wall clock for every island. Date-only values remain calendar dates. */
import { api } from './api/client';
import type { ClubSettings, WidgetPrefs } from './contracts';

export const DEFAULT_TIMEZONE = 'America/Los_Angeles';
let zone = DEFAULT_TIMEZONE;
let prefs: WidgetPrefs | null = null;
let club: ClubSettings | null = null;
let refreshing: Promise<void> | null = null;
export const effectiveTimezone = () => zone;
export function applyTimezone(p: WidgetPrefs) {
  prefs = p;
  if (zone === p.effectiveTimezone) return;
  zone = p.effectiveTimezone;
  window.dispatchEvent(new Event('cseed-timezone'));
}
export const browserTimezone = () => {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || ''; } catch { return ''; }
};
export const timezoneOptions = () => Array.from(new Set([DEFAULT_TIMEZONE, 'UTC', browserTimezone(), ...(Intl.supportedValuesOf?.('timeZone') ?? [])])).filter(Boolean).sort();
export async function refreshTimezone() {
  refreshing ??= (async () => {
    const [p, c] = await Promise.all([api('prefsGet'), api('clubSettingsGet')]);
    club = c;
    const detected = browserTimezone();
    if (p.timezoneMode === 'auto' && detected && detected !== p.timezone) {
      applyTimezone(await api('prefsPut', { body: { timezoneMode: 'auto', timezone: detected } }));
    } else applyTimezone(p);
  })().finally(() => { refreshing = null; });
  return refreshing;
}
export const timezoneSnapshot = () => ({ prefs, club });
let watching = false;
export async function initTimezone() {
  await refreshTimezone();
  if (watching) return;
  watching = true;
  const refresh = () => { if (document.visibilityState !== 'hidden') void refreshTimezone().catch(() => {}); };
  window.addEventListener('focus', refresh);
  document.addEventListener('visibilitychange', refresh);
}
const formatters = new Map<string, Intl.DateTimeFormat>();
export function dateTimeFormatter(options: Intl.DateTimeFormatOptions, dateOnly = false) {
  const opts = { ...options, timeZone: dateOnly ? 'UTC' : zone };
  const key = JSON.stringify(opts);
  let formatter = formatters.get(key);
  if (!formatter) { formatter = new Intl.DateTimeFormat('en-US', opts); formatters.set(key, formatter); }
  return formatter;
}
