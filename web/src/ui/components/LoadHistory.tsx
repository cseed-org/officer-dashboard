import { useEffect, useRef, useState } from 'preact/hooks';
import { errorMessage } from './states';

/** An explicit history page, cancelled when its cohort/account view changes. */
export function LoadHistory({ load }: { load: (signal: AbortSignal) => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);
  return <div class="stack stack--tight">
    <button type="button" class="linkish" disabled={busy} onClick={async () => {
      const ctrl = new AbortController(); active.current = ctrl; setBusy(true); setError('');
      try { await load(ctrl.signal); }
      catch (error) { if (!ctrl.signal.aborted) setError(errorMessage(error)); }
      finally { if (!ctrl.signal.aborted) setBusy(false); }
    }}>{busy ? 'Loading activity…' : 'Load older activity'}</button>
    {error && <p role="alert">{error}</p>}
  </div>;
}

export function mergeActivity<T extends { id: string; date: string; createdAt: number }>(old: T[], more: T[]): T[] {
  const byId = new Map(old.map((a) => [a.id, a]));
  for (const a of more) byId.set(a.id, a);
  return [...byId.values()].sort((a, b) => {
    if (a.date !== b.date) return a.date < b.date ? 1 : -1;
    if (a.createdAt !== b.createdAt) return b.createdAt - a.createdAt;
    return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
  });
}
