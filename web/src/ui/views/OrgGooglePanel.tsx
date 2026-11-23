import { useEffect, useState } from 'preact/hooks';
import { api, endpointUrl } from '../../lib/api/client';
import type { OrgGoogleStatus } from '../../lib/contracts';
import { Drawer } from '../components/Drawer';
import { errorMessage } from '../components/states';

/** `?orgGoogle=` after Google consent returns (server/internal/api/orgcal_oauth.go). */
const RESULT: Record<string, { tone: 'positive' | 'negative' | 'info'; text: string }> = {
  connected: { tone: 'positive', text: 'Org Google account connected. The four calendars are being set up and shared with officers.' },
  failed: { tone: 'negative', text: 'Google sign-in was cancelled or failed. Nothing changed; try Connect again.' },
  scope_required: { tone: 'negative', text: 'Google did not grant calendar access. Connect again and allow “See, edit, share and permanently delete all the calendars”.' },
  not_configured: { tone: 'info', text: 'Google Calendar OAuth is not configured on this dashboard yet.' },
};

/**
 * Leadership's card for the organization's Google account (plan decision 1): connect the
 * dedicated org Gmail once; the dashboard then owns the Internal, External, Weekly Programs and
 * Events calendars in it. Connect and Reconnect are navigations to Google consent, not fetches.
 */
export function OrgGooglePanel() {
  const [st, setSt] = useState<OrgGoogleStatus | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ tone: 'positive' | 'negative' | 'info'; text: string } | null>(() => {
    if (typeof location === 'undefined') return null;
    const url = new URL(location.href);
    const r = url.searchParams.get('orgGoogle');
    if (!r) return null;
    // Report it once: a reload or a shared link must not repeat "connected" or "failed".
    url.searchParams.delete('orgGoogle');
    history.replaceState(history.state, '', url.pathname + url.search + url.hash);
    return RESULT[r] ?? RESULT.failed!;
  });
  const load = () => {
    setError(null);
    api('orgGoogleStatus').then(setSt, setError);
  };
  useEffect(load, []);
  // /calendar links here as /settings#org-google; the page renders after sign-in, so scroll once it has.
  useEffect(() => {
    if (st && location.hash === '#org-google') document.getElementById('org-google')?.scrollIntoView({ block: 'start' });
  }, [!!st]);

  const connectHref = endpointUrl('orgGoogleConnect');
  const callback = typeof location === 'undefined' ? '/auth/org-google/callback' : `${location.origin}/auth/org-google/callback`;

  const disconnect = async () => {
    setBusy(true);
    try {
      setSt(await api('orgGoogleDisconnect'));
      setConfirming(false);
      setNote({ tone: 'info', text: 'Org Google account disconnected. Meetings stay on the dashboard; nothing syncs until it is connected again.' });
    } catch (e) {
      setNote({ tone: 'negative', text: errorMessage(e) });
      setConfirming(false);
    } finally {
      setBusy(false);
    }
  };

  let body;
  if (error) {
    body = (
      <p class="notice notice--negative" role="alert" data-org-google="error">
        Couldn’t load the org Google status: {errorMessage(error)}{' '}
        <button type="button" class="btn btn--small" onClick={load}>
          Try again
        </button>
      </p>
    );
  } else if (!st) {
    body = <div class="skel skel--row" aria-busy="true" />;
  } else if (!st.configured) {
    body = (
      <div class="stack stack--tight" data-org-google="not-configured">
        {st.connected ? (
          <>
            <p class="notice notice--attention">
              Connected as <strong data-org-google-email>{st.email}</strong>, but Google Calendar OAuth is no longer configured on this dashboard, so nothing
              syncs until it is.
            </p>
            <div class="action-row">
              <button type="button" class="btn" onClick={() => setConfirming(true)} disabled={busy} data-org-google-disconnect>
                Disconnect
              </button>
            </div>
          </>
        ) : (
          <p class="notice notice--attention">Google Calendar OAuth isn’t configured on this dashboard, so the org account can’t be connected yet.</p>
        )}
        <p class="field-hint" style={{ overflowWrap: 'anywhere' }}>
          An owner sets <code>GOOGLE_OAUTH_CLIENT_ID</code> and <code>GOOGLE_OAUTH_CLIENT_SECRET</code> (the same Google OAuth client as officers’ calendar sync) and adds{' '}
          <code>{callback}</code> as an authorized redirect URI in that client. See <code>docs/configuration.md</code>.
        </p>
      </div>
    );
  } else {
    const trouble = st.needsReconnect || !!st.lastSyncError;
    body = (
      <div class="stack stack--tight" data-org-google={trouble ? 'needs-attention' : st.connected ? 'connected' : 'not-connected'}>
        {st.connected ? (
          <p class={`notice${trouble ? '' : ' notice--positive'}`}>
            Connected as <strong data-org-google-email>{st.email}</strong>. Internal and External meetings, Weekly Programs and Events sync to its four calendars,
            shared read-only with every active officer.
          </p>
        ) : (
          <p class="notice">
            Not connected. Meetings are saved on the dashboard and sync once the organization’s Google account is connected. Use the dedicated org Gmail, not a
            personal account: the dashboard creates and owns four calendars in it.
          </p>
        )}
        {trouble && (
          <p class="notice notice--attention" role="status" data-org-google-warning>
            {st.needsReconnect ? 'Google revoked the dashboard’s access to the org account. Reconnect to resume syncing.' : 'The last sync with Google failed.'}
            {st.lastSyncError && (
              <>
                {' '}
                <span class="muted">Error: {st.lastSyncError}</span>
              </>
            )}
          </p>
        )}
        <div class="action-row">
          {(!st.connected || trouble) && (
            <a class={`btn${!st.connected || st.needsReconnect ? ' btn--primary' : ''}`} href={connectHref} data-reload data-org-google-connect>
              {st.connected ? 'Reconnect Google' : 'Connect org Google account'}
            </a>
          )}
          {st.connected && (
            <button type="button" class="btn" onClick={() => setConfirming(true)} disabled={busy} data-org-google-disconnect>
              Disconnect
            </button>
          )}
        </div>
        {!st.connected && <p class="field-hint">Consent opens on Google and returns here.</p>}
      </div>
    );
  }

  return (
    <div class="stack" data-org-google-panel>
      {body}
      {note && (
        <p class={`notice notice--${note.tone}`} role="status" data-org-google-note>
          {note.text}
        </p>
      )}
      <Drawer id="org-google-disconnect" kind="dialog" open={confirming} onClose={() => setConfirming(false)} title="Disconnect the org Google account?">
        <div class="stack">
          <p>
            Syncing with {st?.email ?? 'the org account'} stops: new and changed meetings stay on the dashboard only, and Google edits aren’t pulled. The four calendars
            and their events stay in the Google account, and connecting the same account again picks them up.
          </p>
          <div class="action-row">
            <button type="button" class="btn btn--danger" onClick={disconnect} disabled={busy} data-confirm-org-disconnect>
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
