import type { ComponentChildren } from 'preact';
import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type { CurrentUser, WidgetPrefs, ClubSettings } from '../../lib/contracts';
import { api, fieldErrors } from '../../lib/api/client';
import { applyTimezone, browserTimezone, dateTimeFormatter, effectiveTimezone, refreshTimezone, timezoneOptions, timezoneSnapshot } from '../../lib/timezone';
import { Section, Field, describe } from '../components/kit';

function TimezoneClock() {
  const [now, setNow] = useState(new Date());
  useEffect(() => { const timer = window.setInterval(() => setNow(new Date()), 30_000); return () => clearInterval(timer); }, []);
  return <time data-timezone-preview>{dateTimeFormatter({ dateStyle: 'medium', timeStyle: 'short' }).format(now)} · {effectiveTimezone()}</time>;
}

export function TimezonePanel({ me, between }: { me: CurrentUser; between?: ComponentChildren }) {
  const snapshot = timezoneSnapshot();
  const dirtyFields = useRef(new Set<string>());
  const [, setPrefs] = useState<WidgetPrefs | null>(snapshot.prefs);
  const [club, setClub] = useState<ClubSettings | null>(snapshot.club);
  const [mode, setMode] = useState(snapshot.prefs?.timezoneMode ?? 'club');
  const [manual, setManual] = useState(snapshot.prefs?.timezone || effectiveTimezone());
  const [clubZone, setClubZone] = useState(snapshot.club?.clubTimezone ?? effectiveTimezone());
  const [location, setLocation] = useState(snapshot.club?.location ?? '');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ at: 'personal' | 'club'; text: string; ok: boolean } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const zones = useMemo(timezoneOptions, []);
  useEffect(() => {
    const changed = () => {
      const next = timezoneSnapshot();
      setPrefs(next.prefs); setClub(next.club);
      const dirty = dirtyFields.current;
      if (next.prefs) {
        if (!dirty.has('mode')) setMode(next.prefs.timezoneMode);
        if (!dirty.has('manual')) setManual(next.prefs.timezone || effectiveTimezone());
      }
      if (next.club) {
        if (!dirty.has('clubZone')) setClubZone(next.club.clubTimezone);
        if (!dirty.has('location')) setLocation(next.club.location);
      }
    };
    window.addEventListener('cseed-timezone-snapshot', changed);
    return () => window.removeEventListener('cseed-timezone-snapshot', changed);
  }, []);
  async function savePersonal() {
    setBusy(true); setErrors({}); setNote(null);
    try {
      const p = await api('prefsPut', { body: { timezoneMode: mode, timezone: mode === 'auto' ? browserTimezone() : mode === 'manual' ? manual.trim() : '' } });
      dirtyFields.current.delete('mode'); dirtyFields.current.delete('manual');
      setPrefs(p); setNote({ at: 'personal', text: 'Time zone saved.', ok: true }); applyTimezone(p);
    } catch (e) { fail('personal', e); } finally { setBusy(false); }
  }
  async function saveClub() {
    setBusy(true); setErrors({}); setNote(null);
    try {
      const c = await api('clubSettingsPut', { body: { clubTimezone: clubZone.trim(), location } });
      dirtyFields.current.delete('clubZone'); dirtyFields.current.delete('location');
      setClub(c); setNote({ at: 'club', text: 'Club location and time zone saved.', ok: true }); await refreshTimezone();
    } catch (e) { fail('club', e); } finally { setBusy(false); }
  }
  function fail(at: 'personal' | 'club', e: unknown) {
    const fe = fieldErrors(e);
    setErrors(fe);
    setNote({ at, text: Object.keys(fe).length ? 'Check the highlighted field.' : (e as Error).message, ok: false });
  }
  const status = (at: 'personal' | 'club') => note?.at === at && <p class={`notice ${note.ok ? 'notice--positive' : 'notice--negative'}`} role={note.ok ? 'status' : 'alert'}>{note.text}</p>;
  return <>
    <datalist id="iana-timezones">{zones.map((z) => <option key={z} value={z} />)}</datalist>
    <Section title="time zone" id="time-zone">
      <form class="form" onSubmit={(e) => { e.preventDefault(); void savePersonal(); }}>
        <fieldset class="fieldset" disabled={busy}>
          <legend>Show times in</legend>
          <div class="choice-row">
            {([
              ['auto', 'Automatic', 'Follow this device; checked on load and focus.'],
              ['club', 'Club default', club?.clubTimezone ?? 'Club location'],
              ['manual', 'Choose manually', 'Keep a selected IANA time zone.'],
            ] as const).map(([value, label, sub]) => <label class="choice" key={value}>
              <input type="radio" name="timezone-mode" value={value} checked={mode === value} onChange={() => { dirtyFields.current.add('mode'); setMode(value); }} />
              <span class="choice-title">{label}</span><span class="choice-sub">{sub}</span>
            </label>)}
          </div>
        </fieldset>
        {mode === 'manual' && <Field id="manual-timezone" label="Manual time zone" error={errors.timezone} hint="Search by region or city, for example Asia/Tokyo.">
          <input {...describe('manual-timezone', errors.timezone, true)} class="input" list="iana-timezones" value={manual} onInput={(e) => { dirtyFields.current.add('manual'); setManual(e.currentTarget.value); }} required disabled={busy} />
        </Field>}
        <p class="muted small">Current time: <TimezoneClock /></p>
        {status('personal')}
        <div><button class="btn btn--primary" type="submit" disabled={busy}>Save time zone</button></div>
      </form>
    </Section>
    {between}
    <Section title="club location" id="club-location">
      {me.role === 'leadership' ? <form class="form" onSubmit={(e) => { e.preventDefault(); void saveClub(); }}>
        <Field id="club-location-label" label="Club location" error={errors.location}><input {...describe('club-location-label', errors.location)} class="input" value={location} onInput={(e) => { dirtyFields.current.add('location'); setLocation(e.currentTarget.value); }} maxLength={200} required disabled={busy} /></Field>
        <Field id="club-timezone" label="Club default time zone" error={errors.clubTimezone} hint="Set the IANA zone for the club’s location. Used by everyone choosing Club default."><input {...describe('club-timezone', errors.clubTimezone, true)} class="input" list="iana-timezones" value={clubZone} onInput={(e) => { dirtyFields.current.add('clubZone'); setClubZone(e.currentTarget.value); }} required disabled={busy} /></Field>
        {status('club')}
        <div><button class="btn" type="submit" disabled={busy}>Save club location</button></div>
      </form> : <><dl class="facts"><div><dt>Location</dt><dd>{club?.location}</dd></div><div><dt>Default time zone</dt><dd>{club?.clubTimezone}</dd></div></dl></>}
    </Section>
  </>;
}
