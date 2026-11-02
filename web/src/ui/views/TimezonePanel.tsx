import { useEffect, useState } from 'preact/hooks';
import type { CurrentUser, WidgetPrefs, ClubSettings } from '../../lib/contracts';
import { api } from '../../lib/api/client';
import { applyTimezone, browserTimezone, dateTimeFormatter, effectiveTimezone, refreshTimezone, timezoneOptions, timezoneSnapshot } from '../../lib/timezone';
import { Section, Field } from '../components/kit';

export function TimezonePanel({ me }: { me: CurrentUser }) {
  const snapshot = timezoneSnapshot();
  const [prefs, setPrefs] = useState<WidgetPrefs | null>(snapshot.prefs);
  const [club, setClub] = useState<ClubSettings | null>(snapshot.club);
  const [mode, setMode] = useState(snapshot.prefs?.timezoneMode ?? 'club');
  const [manual, setManual] = useState(snapshot.prefs?.timezone || effectiveTimezone());
  const [clubZone, setClubZone] = useState(snapshot.club?.clubTimezone ?? effectiveTimezone());
  const [location, setLocation] = useState(snapshot.club?.location ?? '');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');
  const [error, setError] = useState('');
  const [now, setNow] = useState(new Date());
  const zones = timezoneOptions();
  useEffect(() => {
    const changed = () => {
      const next = timezoneSnapshot();
      setPrefs(next.prefs); setClub(next.club);
    };
    window.addEventListener('cseed-timezone', changed);
    return () => window.removeEventListener('cseed-timezone', changed);
  }, []);
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 30_000); return () => clearInterval(timer); }, []);
  async function savePersonal() {
    setBusy(true); setError(''); setNote('');
    try {
      const p = await api('prefsPut', { body: { timezoneMode: mode, timezone: mode === 'auto' ? browserTimezone() : mode === 'manual' ? manual : '' } });
      setPrefs(p); setNote('Time zone saved.'); applyTimezone(p);
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  async function saveClub() {
    setBusy(true); setError(''); setNote('');
    try {
      const c = await api('clubSettingsPut', { body: { clubTimezone: clubZone, location } });
      setClub(c); setNote('Club location and time zone saved.'); await refreshTimezone();
    } catch (e) { setError((e as Error).message); } finally { setBusy(false); }
  }
  return <>
    <datalist id="iana-timezones">{zones.map((z) => <option key={z} value={z} />)}</datalist>
    <Section title="Time zone" id="time-zone">
      <form class="form" onSubmit={(e) => { e.preventDefault(); void savePersonal(); }}>
        <fieldset class="fieldset" disabled={busy}>
          <legend>Show times in</legend>
          <div class="choice-row">
            {([
              ['auto', 'Automatic', 'Follow this device; checked on load and focus.'],
              ['club', 'Club default', club?.clubTimezone ?? 'Club location'],
              ['manual', 'Choose manually', 'Keep a selected IANA time zone.'],
            ] as const).map(([value, label, sub]) => <label class="choice" key={value}>
              <input type="radio" name="timezone-mode" value={value} checked={mode === value} onChange={() => setMode(value)} />
              <span class="choice-title">{label}</span><span class="choice-sub">{sub}</span>
            </label>)}
          </div>
        </fieldset>
        {mode === 'manual' && <Field id="manual-timezone" label="Manual time zone" hint="Search by region or city, for example Asia/Tokyo.">
          <input id="manual-timezone" class="input" list="iana-timezones" value={manual} onInput={(e) => setManual(e.currentTarget.value)} required disabled={busy} />
        </Field>}
        <p class="muted small">Current time: <time data-timezone-preview>{dateTimeFormatter({ dateStyle: 'medium', timeStyle: 'short' }).format(now)} · {prefs?.effectiveTimezone ?? effectiveTimezone()}</time></p>
        <p class="muted small">Saved to your account. Automatic detection needs access to your device’s time zone; until detected, the club default applies.</p>
        <div><button class="btn btn--primary" type="submit" disabled={busy}>Save time zone</button></div>
      </form>
    </Section>
    <Section title="Club location" id="club-location">
      {me.role === 'leadership' ? <form class="form" onSubmit={(e) => { e.preventDefault(); void saveClub(); }}>
        <Field id="club-location-label" label="Club location"><input id="club-location-label" class="input" value={location} onInput={(e) => setLocation(e.currentTarget.value)} maxLength={200} required disabled={busy} /></Field>
        <Field id="club-timezone" label="Club default time zone" hint="Set the IANA zone for the club’s location. Used by everyone choosing Club default."><input id="club-timezone" class="input" list="iana-timezones" value={clubZone} onInput={(e) => setClubZone(e.currentTarget.value)} required disabled={busy} /></Field>
        <div><button class="btn" type="submit" disabled={busy}>Save club location</button></div>
      </form> : <><dl class="facts"><div><dt>Location</dt><dd>{club?.location}</dd></div><div><dt>Default time zone</dt><dd>{club?.clubTimezone}</dd></div></dl><p class="muted small">Leadership and owners manage the club location and default time zone.</p></>}
    </Section>
    {error && <p class="notice notice--negative" role="alert">{error}</p>}
    {note && <p class="notice notice--positive" role="status">{note}</p>}
  </>;
}
