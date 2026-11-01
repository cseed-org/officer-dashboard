/**
 * The signed-in user, loaded once per page from GET /api/me and shared by every island.
 * 401 sends the browser to /login?next=<here>.
 */
import { useEffect, useState } from 'preact/hooks';
import { api, ApiCallError, redirectToLogin } from './api/client';
import type { CurrentUser } from './contracts';

const ROLE_KEY = 'cseed-role';
let mePromise: Promise<CurrentUser> | null = null;
let current: CurrentUser | null = null;

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
    .then(({ user }) => {
      current = user;
      applyRole(user.role);
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

export type MeState = { status: 'loading' } | { status: 'ready'; user: CurrentUser } | { status: 'error'; error: unknown; retry: () => void };

export function useMe(): MeState {
  // Always start as 'loading' so the first client render matches the static HTML (hydration
  // keeps server attributes it doesn't expect to change); a cached user resolves next tick.
  const [state, setState] = useState<MeState>({ status: 'loading' });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    if (current) {
      setState({ status: 'ready', user: current });
      return;
    }
    loadMe().then(
      (user) => live && setState({ status: 'ready', user }),
      (error) => live && setState({ status: 'error', error, retry: () => setTick((t) => t + 1) }),
    );
    return () => {
      live = false;
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
