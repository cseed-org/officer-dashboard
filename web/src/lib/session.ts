/**
 * The signed-in user, loaded once per page from GET /api/me and shared by every island.
 * 401 sends the browser to /login?next=<here>.
 *
 * It stays current while the tab is open: when any API response shows the user's permissions
 * changed (X-Perm-Version, lib/api/client.ts), on any 403, and when the tab regains focus,
 * /api/me is fetched again; a change updates every island and bumps `epoch`, which the page
 * Gate uses to remount the page for the new permissions.
 */
import { useEffect, useState } from 'preact/hooks';
import { api, ApiCallError, permissions, PERMISSIONS_EVENT, redirectToLogin } from './api/client';
import type { CurrentUser } from './contracts';

import { initTimezone } from './timezone';

const ROLE_KEY = 'cseed-role';
let mePromise: Promise<CurrentUser> | null = null;
let current: CurrentUser | null = null;
/** Bumped each time the signed-in user's permissions change while the page is open. */
let epoch = 0;
const listeners = new Set<() => void>();

function applyRole(role: string | null) {
  try {
    if (role) localStorage.setItem(ROLE_KEY, role);
    else localStorage.removeItem(ROLE_KEY);
  } catch {
    /* storage blocked */
  }
  if (role) document.documentElement.dataset.role = role;
  else delete document.documentElement.dataset.role;
}

export function loadMe(): Promise<CurrentUser> {
  mePromise ??= api('me')
    .then(async ({ user, onboarding }) => {
      await initTimezone();
      current = user;
      permissions.accept();
      applyRole(user.role);
      watchPermissions();
      // Invitees finish setup before anything else.
      if (onboarding && location.pathname !== '/onboarding') {
        location.replace('/onboarding');
        return new Promise<CurrentUser>(() => {});
      }
      return user;
    })
    .catch((err) => {
      if (err instanceof ApiCallError && err.status === 401) {
        applyRole(null);
        redirectToLogin();
        return new Promise<CurrentUser>(() => {}); // the page is going away
      }
      mePromise = null;
      throw err;
    });
  return mePromise;
}

let refreshing: Promise<void> | null = null;
let again = false;
/**
 * Fetch /api/me again and apply it if anything changed. A request already in flight is reused,
 * and one more follows it when another change was noticed meanwhile (its answer may predate it).
 */
export function refreshMe(): Promise<void> {
  if (!current) return Promise.resolve();
  if (refreshing) {
    again = true;
    return refreshing;
  }
  again = false;
  lastCheck = Date.now();
  refreshing = api('me')
    .then(({ user, onboarding }) => {
      const changed = permissions.changed();
      permissions.accept();
      if (onboarding && location.pathname !== '/onboarding') {
        location.replace('/onboarding');
        return;
      }
      if (!changed && JSON.stringify(user) === JSON.stringify(current)) return;
      current = user;
      mePromise = Promise.resolve(user);
      applyRole(user.role);
      if (changed) epoch++;
      for (const l of listeners) l();
    })
    .catch(() => {
      /* 401 already went to /login; anything else is retried on the next signal */
    })
    .finally(() => {
      refreshing = null;
      if (again) void refreshMe();
    });
  return refreshing;
}

let lastCheck = 0;
let watching = false;
/** Listen for permission signals once per page. */
function watchPermissions() {
  if (watching || typeof window === 'undefined') return;
  watching = true;
  window.addEventListener(PERMISSIONS_EVENT, () => void refreshMe());
  // Coming back to the tab: a light check, at most every 10 seconds.
  const onReturn = () => {
    if (document.visibilityState === 'visible' && Date.now() - lastCheck > 10_000) void refreshMe();
  };
  window.addEventListener('focus', onReturn);
  document.addEventListener('visibilitychange', onReturn);
}

export type MeState =
  | { status: 'loading' }
  | { status: 'ready'; user: CurrentUser; epoch: number }
  | { status: 'error'; error: unknown; retry: () => void };

export function useMe(): MeState {
  // Always start as 'loading' so the first client render matches the static HTML (hydration
  // keeps server attributes it doesn't expect to change); a cached user resolves next tick.
  const [state, setState] = useState<MeState>({ status: 'loading' });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    const sync = () => live && current && setState({ status: 'ready', user: current, epoch });
    listeners.add(sync);
    if (current) sync();
    else
      loadMe().then(
        () => sync(),
        (error) => live && setState({ status: 'error', error, retry: () => setTick((t) => t + 1) }),
      );
    return () => {
      live = false;
      listeners.delete(sync);
    };
  }, [tick]);
  return state;
}

/** Forget the role cached for the pre-paint script (sign-out). */
export function clearCachedRole() {
  applyRole(null);
}

export const isDirector = (u: CurrentUser | null | undefined) => !!u && u.role !== 'officer';

export const initials = (name: string) =>
  name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join('') || '?';
