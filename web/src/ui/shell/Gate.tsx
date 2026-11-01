import type { ComponentChildren } from 'preact';
import { useEffect } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { interceptSamePageLinks } from '../../lib/nav';
import { useMe } from '../../lib/session';
import { ErrorState, Loading } from '../components/states';

/** Waits for /api/me (401 → /login), then renders the page body for that user. */
export function Gate({ children, label }: { children: (me: CurrentUser) => ComponentChildren; label: string }) {
  const me = useMe();
  useEffect(() => interceptSamePageLinks(), []);
  if (me.status === 'loading') return <Loading rows={6} label={`Loading ${label}`} />;
  if (me.status === 'error') return <ErrorState error={me.error} onRetry={me.retry} />;
  return <div class="page-enter">{children(me.user)}</div>;
}
