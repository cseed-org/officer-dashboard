import { TimezonePendingNotice } from '../components/TimezonePendingNotice';
import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import type { CurrentUser } from '../../lib/contracts';
import { interceptSamePageLinks } from '../../lib/nav';
import { CONTRACT_ERROR_EVENT } from '../../lib/api/client';
import { useMe } from '../../lib/session';
import { hasUnsavedEdits } from '../../lib/unsaved';
import { ErrorState, Loading } from '../components/states';

/**
 * Waits for /api/me (401 → /login), then renders the page body for that user; a late contract
 * mismatch replaces it with an error. When the user's permissions change while the page is open
 * (lib/session.ts), the page remounts so every view reloads its data for the new permissions;
 * with unsaved edits it only re-renders and offers a reload, so no draft is lost.
 */
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
  const epoch = me.status === 'ready' ? me.epoch : 0;
  const [mount, setMount] = useState(0);
  const [changed, setChanged] = useState<'remounted' | 'stale' | null>(null);
  const seen = useRef(epoch);
  useEffect(() => {
    if (epoch === seen.current) return;
    seen.current = epoch;
    if (hasUnsavedEdits()) setChanged('stale');
    else {
      setMount((n) => n + 1);
      setChanged('remounted');
    }
  }, [epoch]);
  if (contractError) return <ErrorState error={contractError} onRetry={() => location.reload()} />;
  if (me.status === 'loading') return <Loading rows={6} label={`Loading ${label}`} />;
  if (me.status === 'error') return <ErrorState error={me.error} onRetry={me.retry} />;
  const key = `${label === 'settings' ? 0 : zoneVersion}-${mount}`;
  return (
    <>
      <TimezonePendingNotice />
      {changed && (
        <div class="notice notice--info permissions-notice" role="status" data-permissions-changed>
          <span>
            {changed === 'stale'
              ? 'Your permissions changed. Save or discard your edits, then reload to see what you can do now.'
              : 'Your permissions changed. This page now shows what you can do.'}
          </span>
          {changed === 'stale' && <button type="button" class="btn btn--small" onClick={() => location.reload()}>Reload</button>}
          <button type="button" class="btn btn--text btn--small" onClick={() => setChanged(null)}>Dismiss</button>
        </div>
      )}
      <div class="page-enter" key={key}>{children(me.user)}</div>
    </>
  );
}
