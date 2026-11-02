import { pendingTimezone } from '../../lib/timezone';
import type { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { interceptSamePageLinks } from '../../lib/nav';
import { useMe } from '../../lib/session';
import { ErrorState, Loading } from '../components/states';

/** Waits for /api/me (401 → /login), then renders the page body for that user. */
export function Gate({ children, label }: { children: (me: CurrentUser) => ComponentChildren; label: string }) {
  const me = useMe();
  const [pending, setPending] = useState<string | undefined>();
  const [zoneVersion, setZoneVersion] = useState(0);
  useEffect(() => {
    const changed = () => setZoneVersion((n) => n + 1);
    const deferred = () => setPending(pendingTimezone());
    window.addEventListener('cseed-timezone-pending', deferred);
    window.addEventListener('cseed-timezone', changed);
    return () => { window.removeEventListener('cseed-timezone', changed); window.removeEventListener('cseed-timezone-pending', deferred); };
  }, []);
  useEffect(() => interceptSamePageLinks(), []);
  if (me.status === 'loading') return <Loading rows={6} label={`Loading ${label}`} />;
  if (me.status === 'error') return <ErrorState error={me.error} onRetry={me.retry} />;
  return <>{pending && <p class="notice" role="status">Your time zone changed to {pending}; times will update after you save or close your form.</p>}<div class="page-enter" key={label === 'settings' ? undefined : zoneVersion}>{children(me.user)}</div></>;
}
