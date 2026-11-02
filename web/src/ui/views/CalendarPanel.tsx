import { useEffect, useState } from 'preact/hooks';
import { api, ApiCallError, endpointUrl } from '../../lib/api/client';
import type { CalendarSettings } from '../../lib/contracts';
import { EmailPicker } from '../components/EmailPicker';

const GOOGLE_RESULT: Record<string, string> = {
  connected: 'Google Calendar connected. Upcoming events are on their way.',
  cancelled: 'Google consent was cancelled. Try again, or use .ics email invites.',
  failed: 'Google authorization failed or was revoked. Retry, or use .ics email invites.',
  not_configured: 'Google Calendar sync is not configured on this dashboard.',
};

/**
 * Calendar delivery: the saved-email picker, the delivery mode, Google connection and a
 * test invite. `next` is where Google consent returns to.
 */
export function CalendarPanel({ next, removable = false, onState }: { next: string; removable?: boolean; onState?: (s: CalendarSettings) => void }) {
  const [st, setSt] = useState<CalendarSettings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [note, setNote] = useState<{ tone: 'info' | 'positive' | 'negative'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [googleMsg] = useState(() => {
    if (typeof location === 'undefined') return null;
    const g = new URLSearchParams(location.search).get('google');
    return g ? { key: g, text: GOOGLE_RESULT[g] ?? GOOGLE_RESULT.failed! } : null;
  });

  const apply = (s: CalendarSettings) => {
    setSt(s);
    onState?.(s);
  };
  const load = () => api('calendarGet').then(apply, (e) => setError(e instanceof Error ? e.message : 'Could not load calendar settings.'));
  useEffect(() => {
    load();
  }, []);

  if (error)
    return (
      <p class="notice notice--negative" role="alert">
        {error}{' '}
        <button type="button" class="btn btn--small" onClick={() => (setError(null), load())}>
          Try again
        </button>
      </p>
    );
  if (!st) return <div class="skel skel--row" aria-busy="true" />;

  const setMode = async (mode: 'ics' | 'google') => {
    setNote(null);
    if (mode === 'google' && !st.google.connected) {
      apply({ ...st, mode: 'google' }); // shows the Connect panel; not saved until connected
      return;
    }
    const before = st;
    apply({ ...st, mode }); // optimistic; reverted if the server refuses
    setBusy(true);
    try {
      apply(await api('calendarPut', { body: { mode } }));
    } catch (e) {
      apply(before);
      setNote({ tone: 'negative', text: e instanceof ApiCallError ? e.message : 'Could not change delivery.' });
    } finally {
      setBusy(false);
    }
  };

  const disconnect = async () => {
    setBusy(true);
    try {
      apply(await api('googleDisconnect'));
      setNote({ tone: 'info', text: 'Google Calendar disconnected. Invites now arrive as .ics emails.' });
    } catch (e) {
      setNote({ tone: 'negative', text: e instanceof Error ? e.message : 'Could not disconnect.' });
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    setBusy(true);
    setNote(null);
    try {
      const r = await api('calendarTest');
      setNote({ tone: r.status === 'sent' ? 'positive' : r.status === 'failed' ? 'negative' : 'info', text: r.message });
    } catch (e) {
      setNote({ tone: 'negative', text: e instanceof Error ? e.message : 'The test invite failed.' });
    } finally {
      setBusy(false);
    }
  };

  const connectHref = endpointUrl('googleConnect') + `?next=${encodeURIComponent(next)}`;
  const icsNote = !st.ics.configured
    ? '.ics email invites are not configured on this dashboard yet (no SMTP server). Your choice is saved; invites start once an owner sets it up.'
    : st.activeEmail
      ? `Event invitations arrive as .ics email attachments at ${st.activeEmail}.`
      : 'Add a calendar email above, or no invitations are sent.';

  return (
    <div class="stack" data-calendar-panel>
      <EmailPicker
        removable={removable}
        label={st.mode === 'google' ? 'Saved contact email · Google sync uses the account you connect' : 'Send calendar invites to'}
        onChange={(active) => setSt((s) => (s ? { ...s, activeEmail: active } : s))}
      />
      <fieldset class="fieldset">
        <legend>How should events reach you?</legend>
        <div class="choice-row choice-row--compact">
          <label class="choice">
            <input type="radio" name="cal-mode" value="ics" checked={st.mode === 'ics'} onChange={() => setMode('ics')} />
            <span class="choice-title">.ics email invites</span>
            <span class="choice-sub">An invite email to the address above, for any calendar app</span>
          </label>
          <label class="choice">
            <input type="radio" name="cal-mode" value="google" checked={st.mode === 'google'} disabled={!st.hasProfile} onChange={() => setMode('google')} />
            <span class="choice-title">Google Calendar sync</span>
            <span class="choice-sub">{st.google.configured ? 'Connect Google once; events appear and update by themselves' : 'Not configured on this dashboard'}</span>
          </label>
        </div>
        {!st.hasProfile && <p class="field-hint">Save your profile first to choose Google sync.</p>}
      </fieldset>
      {st.mode === 'google' ? (
        <div class="stack stack--tight" data-google>
          {!st.google.configured ? (
            <p class="notice" data-google-status>
              Google Calendar sync is not configured on this dashboard. Use .ics email invites for now.
            </p>
          ) : st.google.connected ? (
            <>
              <p class="notice notice--positive" data-google-status>
                Connected{st.google.accountEmail ? ` as ${st.google.accountEmail}` : ''}. New and changed events sync to your primary Google Calendar.
              </p>
              <div class="action-row">
                <button type="button" class="btn" onClick={disconnect} disabled={busy}>
                  Disconnect Google
                </button>
              </div>
            </>
          ) : (
            <>
              <div class="action-row">
                <a class="btn btn--primary" href={connectHref} data-google-connect>
                  Connect Google Calendar
                </a>
              </div>
              <p class="field-hint" data-google-status>
                Consent opens on Google. Cancel returns here. Until you connect, nothing syncs and invites keep arriving as .ics emails.
              </p>
            </>
          )}
        </div>
      ) : (
        <p class="notice" data-ics-status>
          {icsNote}
        </p>
      )}
      {googleMsg && (
        <p class={`notice${googleMsg.key === 'connected' ? ' notice--positive' : googleMsg.key === 'not_configured' ? '' : ' notice--negative'}`} role="status">
          {googleMsg.text}
        </p>
      )}
      <div class="action-row">
        <button type="button" class="btn" onClick={test} disabled={busy} data-calendar-test>
          Send a test invite
        </button>
        {note && (
          <span class={`small ${note.tone === 'negative' ? 'field-error' : 'muted'}`} role="status" data-calendar-note>
            {note.text}
          </span>
        )}
      </div>
    </div>
  );
}
