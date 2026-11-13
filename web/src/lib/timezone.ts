import { hasUnsavedEdits, watchUnsavedEdits } from './unsaved';
/** One effective wall clock for every island. Date-only values remain calendar dates. */
import { api } from './api/client';
import type { ClubSettings, WidgetPrefs } from './contracts';
import { syncSidebarFromServer } from './sidebar';

export const DEFAULT_TIMEZONE = 'America/Los_Angeles';
let zone = DEFAULT_TIMEZONE;
let prefs: WidgetPrefs | null = null;
let club: ClubSettings | null = null;
let refreshing: Promise<void> | null = null;
export const effectiveTimezone = () => zone;
let pending: WidgetPrefs | null = null;
export const pendingTimezone = () => pending?.effectiveTimezone;
export function applyTimezone(p: WidgetPrefs) {
  prefs = p;
  pending = null;
  syncSidebarFromServer(p.sidebarWidth);
  if (zone === p.effectiveTimezone) { window.dispatchEvent(new Event('cseed-timezone-pending')); return; }
  if (hasUnsavedEdits()) {
    pending = p;
    window.dispatchEvent(new Event('cseed-timezone-pending'));
    return;
  }
  zone = p.effectiveTimezone;
  window.dispatchEvent(new Event('cseed-timezone'));
  window.dispatchEvent(new Event('cseed-timezone-pending'));
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
/** Never fatal: if preferences can't load, pages keep the club default and retry on focus. */
export async function initTimezone() {
  if (!watching) {
    watching = true;
    watchUnsavedEdits();
    window.addEventListener('cseed-unsaved', () => {
      if (pending && !hasUnsavedEdits()) applyTimezone(pending);
    });
    const refresh = () => { if (document.visibilityState !== 'hidden') void refreshTimezone().catch(() => {}); };
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
  }
  await refreshTimezone().catch((err) => console.warn('[timezone] using the club default', err));
}
const formatters = new Map<string, Intl.DateTimeFormat>();
export function dateTimeFormatter(options: Intl.DateTimeFormatOptions, dateOnly = false) {
  const opts = { ...options, timeZone: dateOnly ? 'UTC' : zone };
  const key = JSON.stringify(opts);
  let formatter = formatters.get(key);
  if (!formatter) { formatter = new Intl.DateTimeFormat('en-US', opts); formatters.set(key, formatter); }
  return formatter;
}
