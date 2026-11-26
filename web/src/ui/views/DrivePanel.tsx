import { useEffect, useState } from 'preact/hooks';
import { api, endpointUrl } from '../../lib/api/client';
import type { DriveStatus } from '../../lib/contracts';
import { Drawer } from '../components/Drawer';
import { errorMessage } from '../components/states';

/** `?drive=` after Google consent returns (server/internal/api/drive.go). */
const RESULT: Record<string, { tone: 'positive' | 'negative' | 'info'; text: string }> = {
  connected: { tone: 'positive', text: 'Google account connected. Choose the shared drive folder below if it isn’t set yet.' },
  failed: { tone: 'negative', text: 'Google sign-in was cancelled or failed. Nothing changed; try Connect again.' },
  scope_required: { tone: 'negative', text: 'Google did not grant Drive access. Connect again and allow “See, edit, create and delete all of your Google Drive files”.' },
  not_configured: { tone: 'info', text: 'Google OAuth is not configured on this dashboard yet.' },
};

const gb = (n: number) => `${(n / 1e9).toFixed(1)} GB`;

/**
 * Owners' card for file storage: connect the Google account the dashboard stores receipts and
 * design files as (a member of the club's shared drive), and choose the shared drive folder.
 * Officers never need access to the drive: files open through the dashboard.
 */
export function DrivePanel() {
  const [st, setSt] = useState<DriveStatus | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [folder, setFolder] = useState('');
  const [note, setNote] = useState<{ tone: 'positive' | 'negative' | 'info'; text: string } | null>(() => {
    if (typeof location === 'undefined') return null;
    const url = new URL(location.href);
    const r = url.searchParams.get('drive');
    if (!r) return null;
    // Report it once: a reload or a shared link must not repeat "connected" or "failed".
    url.searchParams.delete('drive');
    history.replaceState(history.state, '', url.pathname + url.search + url.hash);
    return RESULT[r] ?? RESULT.failed!;
  });
  const load = () => {
    setError(null);
    api('driveStatus').then(setSt, setError);
  };
  useEffect(load, []);

  const connectHref = endpointUrl('driveConnect');
  const callback = typeof location === 'undefined' ? '/auth/drive-google/callback' : `${location.origin}/auth/drive-google/callback`;

  const run = async (fn: () => Promise<DriveStatus>, done: string) => {
    setBusy(true);
    try {
      setSt(await fn());
      setNote({ tone: 'info', text: done });
    } catch (e) {
      setNote({ tone: 'negative', text: errorMessage(e) });
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  };
  const disconnect = () => run(() => api('driveDisconnect'), 'Google account disconnected. Uploads answer “not connected” until an account is connected again; stored files stay in Drive.');
  const saveRoot = (e: Event) => {
    e.preventDefault();
    run(() => api('driveRootPut', { body: { folderId: folder.trim() } }), 'Folder saved. New receipts and design files go there.');
  };

  let body;
  if (error) {
    body = (
      <p class="notice notice--negative" role="alert" data-drive="error">
        Couldn’t load the Drive status: {errorMessage(error)}{' '}
        <button type="button" class="btn btn--small" onClick={load}>
          Try again
        </button>
      </p>
    );
  } else if (!st) {
    body = <div class="skel skel--row" aria-busy="true" />;
  } else if (!st.configured) {
    body = (
      <div class="stack stack--tight" data-drive="not-configured">
        <p class="notice notice--attention">Google OAuth isn’t configured on this dashboard, so a Google account can’t be connected for file storage yet.</p>
        <p class="field-hint" style={{ overflowWrap: 'anywhere' }}>
          An owner sets <code>GOOGLE_OAUTH_CLIENT_ID</code> and <code>GOOGLE_OAUTH_CLIENT_SECRET</code> and adds <code>{callback}</code> as an authorized redirect URI in
          that client. See <code>docs/deployment.md</code>.
        </p>
      </div>
    );
  } else {
    const trouble = st.needsReconnect || (st.connected && !!st.rootFolderId && !st.rootReachable) || (st.rootReachable && !st.canWrite);
    body = (
      <div class="stack stack--tight" data-drive={trouble ? 'needs-attention' : st.connected ? 'connected' : 'not-connected'}>
        {st.connected ? (
          <p class={`notice${trouble ? '' : ' notice--positive'}`}>
            Receipts and design files are stored as <strong data-drive-email>{st.email}</strong>
            {st.rootReachable && st.rootName ? (
              <>
                {' '}
                in <strong>{st.rootName}</strong>
                {st.sharedDrive ? ' on the shared drive' : ''}
              </>
            ) : null}
            . Officers open them through the dashboard; they don’t need access to the drive.
          </p>
        ) : (
          <p class="notice">
            {st.needsReconnect
              ? `Google revoked the dashboard’s access to ${st.email ?? 'the Drive account'}. Uploads are paused until it is reconnected.`
              : 'Not connected. Connect the Google account that has Content manager access to the club’s shared drive.'}
          </p>
        )}
        {st.error && (
          <p class="notice notice--attention" role="status" data-drive-warning>
            {st.error}
          </p>
        )}
        {st.rootReachable && !st.canWrite && (
          <p class="notice notice--attention" role="status">
            The connected account can see the folder but cannot add files. Give it Content manager access on the shared drive.
          </p>
        )}
        {st.usage && (
          <p class="small muted" data-drive-usage>
            {gb(st.usage.usedBytes)} of {gb(st.usage.quotaBytes)} used ({st.usage.percent}%). Owners are alerted at 70%.
          </p>
        )}
        {st.connected &&
          (st.rootFromEnv ? (
            <p class="field-hint">
              Folder <code>{st.rootFolderId}</code> is set on the server (<code>DRIVE_ROOT_FOLDER_ID</code>).
            </p>
          ) : (
            <form class="stack stack--tight" onSubmit={saveRoot} data-drive-root>
              <label class="field">
                <span class="field-label">Shared drive folder</span>
                <input
                  type="text"
                  class="input"
                  value={folder}
                  placeholder={st.rootFolderId || 'https://drive.google.com/drive/folders/…'}
                  onInput={(e) => setFolder((e.target as HTMLInputElement).value)}
                  required
                />
                <span class="field-hint">Paste the folder’s link from Drive. The dashboard creates Finance, Events, Design requests and Backups folders inside it.</span>
              </label>
              <div class="action-row">
                <button type="submit" class="btn" disabled={busy || !folder.trim()}>
                  {busy ? 'Checking…' : 'Check and use folder'}
                </button>
              </div>
            </form>
          ))}
        <div class="action-row">
          {(!st.connected || st.needsReconnect) && (
            <a class="btn btn--primary" href={connectHref} data-reload data-drive-connect>
              {st.needsReconnect ? 'Reconnect Google Drive' : 'Connect Google Drive'}
            </a>
          )}
          {st.connected && (
            <button type="button" class="btn" onClick={() => setConfirming(true)} disabled={busy} data-drive-disconnect>
              Disconnect
            </button>
          )}
        </div>
        {!st.connected && <p class="field-hint">Consent opens on Google and returns here.</p>}
      </div>
    );
  }

  return (
    <div class="stack" data-drive-panel>
      {body}
      {note && (
        <p class={`notice notice--${note.tone}`} role="status" data-drive-note>
          {note.text}
        </p>
      )}
      <Drawer id="drive-disconnect" kind="dialog" open={confirming} onClose={() => setConfirming(false)} title="Disconnect Google Drive?">
        <div class="stack">
          <p>
            Uploads stop until a Google account is connected again, and stored receipts and design files can’t be opened in the meantime. Nothing is deleted from
            Drive.
          </p>
          <div class="action-row">
            <button type="button" class="btn btn--danger" onClick={disconnect} disabled={busy} data-confirm-drive-disconnect>
              {busy ? 'Disconnecting…' : 'Disconnect'}
            </button>
            <button type="button" class="btn" onClick={() => setConfirming(false)} disabled={busy}>
              Keep connected
            </button>
          </div>
        </div>
      </Drawer>
    </div>
  );
}
