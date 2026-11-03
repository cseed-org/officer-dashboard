import { useEffect, useState } from 'preact/hooks';
import { api } from '../../lib/api/client';
import type { ClubSettings, CurrentUser } from '../../lib/contracts';
import { Section } from '../components/kit';

export function ClubLinksPanel({ me }: { me: CurrentUser }) {
  const [settings, setSettings] = useState<ClubSettings | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');
  const load = () => { setError(''); api('clubSettingsGet').then(setSettings, e => setError(e.message)); };
  useEffect(load, []);
  async function save(e: Event) {
    e.preventDefault(); if (!settings) return;
    setBusy(true); setError(''); setNote('');
    try {
      // Re-read unrelated club settings so editing links does not revert a timezone change.
      const latest = await api('clubSettingsGet');
      setSettings(await api('clubSettingsPut', { body: { ...latest, websiteUrl: settings.websiteUrl, notionUrl: settings.notionUrl, driveUrl: settings.driveUrl } }));
      setNote('Club links saved.');
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <Section title="club links" id="club-links">
    <p class="small muted">Shared on everyone’s Links widget. Leadership manages these URLs.</p>
    {error && <p role="alert">{error} <button class="btn" onClick={load}>Retry</button></p>}
    {settings && (me.role === 'leadership' || me.isOwner ? <form class="form" onSubmit={save}>
      {([['websiteUrl', 'Club website'], ['notionUrl', 'Notion'], ['driveUrl', 'Google Drive']] as const).map(([key, label]) => <label class="field" key={key}>{label}<input class="input" type="url" placeholder="https://… (optional)" value={settings[key]} maxLength={2048} disabled={busy} onInput={e => setSettings({ ...settings, [key]: e.currentTarget.value })} /></label>)}
      <button class="btn" disabled={busy}>{busy ? 'Saving…' : 'Save club links'}</button><p role="status">{note}</p>
    </form> : <p>Ask leadership to add or update a club link.</p>)}
  </Section>;
}
