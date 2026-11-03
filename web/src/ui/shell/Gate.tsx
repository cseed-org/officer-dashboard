import { TimezonePendingNotice } from '../components/TimezonePendingNotice';
import type { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { interceptSamePageLinks } from '../../lib/nav';
import { CONTRACT_ERROR_EVENT } from '../../lib/api/client';
import { useMe } from '../../lib/session';
import { ErrorState, Loading } from '../components/states';

/** Waits for /api/me (401 → /login), then renders the page body for that user; a late contract mismatch replaces it with an error. */
export function Gate({ children, label }: { children: (me: CurrentUser) => ComponentChildren; label: string }) {
  const me = useMe();
  const [zoneVersion, setZoneVersion] = useState(0);
  useEffect(() => {
    const changed = () => setZoneVersion((n) => n + 1);
    window.addEventListener('cseed-timezone', changed);
    return () => window.removeEventListener('cseed-timezone', changed);
  }, []);
  useEffect(() => interceptSamePageLinks(), []);
  // A read rendered before the zod contract loaded, then failed validation (lib/api/client.ts).
  const [contractError, setContractError] = useState<unknown>(null);
  useEffect(() => {
    const failed = (e: Event) => setContractError((e as CustomEvent).detail);
    window.addEventListener(CONTRACT_ERROR_EVENT, failed);
    return () => window.removeEventListener(CONTRACT_ERROR_EVENT, failed);
  }, []);
  if (contractError) return <ErrorState error={contractError} onRetry={() => location.reload()} />;
  if (me.status === 'loading') return <Loading rows={6} label={`Loading ${label}`} />;
  if (me.status === 'error') return <ErrorState error={me.error} onRetry={me.retry} />;
  return <><TimezonePendingNotice /><div class="page-enter" key={label === 'settings' ? undefined : zoneVersion}>{children(me.user)}</div></>;
}
