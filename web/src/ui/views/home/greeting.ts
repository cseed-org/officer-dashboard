export type GreetingState = { day: string; signedOut: boolean };
type StorageAccess = () => Pick<Storage, 'getItem' | 'setItem'>;
export type GreetingMemory = {
  state: GreetingState | null;
  fallback: boolean;
  available: boolean;
};
export const localDay = (now: Date): string =>
  `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;

export function pickGreeting(now: Date, firstName: string, state: GreetingState | null, random: () => number): string | null {
  const today = localDay(now);
  if (state?.day === today && !state.signedOut) return null;
  const hour = now.getHours();
  if (hour < 5) return 'Go sleep, bro';
  if (random() < 1 / 365) return 'Lenny was here';
  const name = firstName.trim().split(/\s+/)[0];
  const suffix = name ? `, ${name}` : '';
  if (state?.signedOut && state.day === today) return `Welcome back${suffix}`;
  return `Good ${hour < 12 ? 'morning' : hour < 17 ? 'afternoon' : 'evening'}${suffix}`;
}

function decode(raw: string | null): GreetingState | null {
  try {
    const value = JSON.parse(raw ?? 'null');
    return value && typeof value.day === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(value.day) && typeof value.signedOut === 'boolean' ? value : null;
  } catch { return null; }
}

/** Accessors also catch browsers that throw when the storage property itself is read. */
export function readState(userId: string, local: StorageAccess, session: StorageAccess): GreetingMemory {
  const key = `home-greeting:${userId}`;
  try { return { state: decode(local().getItem(key)), fallback: false, available: true }; }
  catch {
    try { return { state: decode(session().getItem(key)), fallback: true, available: true }; }
    catch { return { state: null, fallback: true, available: false }; }
  }
}

/** Persist before displaying. A failed local write falls back to a once-per-tab claim. */
export function writeState(userId: string, state: GreetingState, local: StorageAccess, session: StorageAccess, fallback = false): boolean {
  const key = `home-greeting:${userId}`;
  if (!fallback) {
    try { local().setItem(key, JSON.stringify(state)); return true; }
    catch { /* Use the tab claim below, including quota failures. */ }
  }
  try {
    const storage = session();
    if (storage.getItem(key) || state.signedOut) return false;
    storage.setItem(key, JSON.stringify(state));
    return true;
  } catch { return false; }
}
