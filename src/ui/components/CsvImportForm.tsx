import { useState } from 'preact/hooks';
import type { CsvImportResponse } from '../../core/contracts';
import { CSV_MAX_BYTES } from '../../core/contracts';
import { api, fieldErrors } from '../api/client';
import { useApi } from '../api/useApi';
import { bytes, fmtDate, pct } from '../format';
import { describe, Field } from './kit';
import { errorMessage, Notice } from './states';

type Kind = 'luma_guests' | 'reflections';

/**
 * Upload a Luma guest export or the reflection survey export (POST /api/events/import).
 * "Check file" runs a dry run first so officers see row errors before anything is written.
 */
export function CsvImportForm({ eventId, compact, onImported }: { eventId?: string; compact?: boolean; onImported?: (r: CsvImportResponse) => void }) {
  const lookups = useApi('lookups');
  const [kind, setKind] = useState<Kind>('luma_guests');
  const [target, setTarget] = useState<string>(eventId ?? '');
  const [newName, setNewName] = useState('');
  const [newLuma, setNewLuma] = useState('');
  const [newDate, setNewDate] = useState('');
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState<'check' | 'import' | null>(null);
  const [result, setResult] = useState<CsvImportResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const idp = eventId ? `imp-${eventId}` : 'imp';
  const creating = !eventId && target === '__new';

  const validate = () => {
    const e: Record<string, string> = {};
    if (!file) e.file = 'Choose a CSV file.';
    else if (file.size > CSV_MAX_BYTES) e.file = `That file is ${bytes(file.size)}; the limit is ${bytes(CSV_MAX_BYTES)}.`;
    else if (!/\.csv$/i.test(file.name) && file.type !== 'text/csv') e.file = 'That is not a .csv file.';
    if (!eventId && !target) e.eventId = 'Pick the event this export belongs to.';
    if (creating && !newName.trim()) e.eventName = 'Name the new event.';
    return e;
  };

  const run = async (dryRun: boolean) => {
    const e = validate();
    setErrors(e);
    setError(null);
    if (Object.keys(e).length) return;
    const form = new FormData();
    form.set('kind', kind);
    form.set('dryRun', String(dryRun));
    if (creating) {
      form.set('eventName', newName.trim());
      if (newLuma.trim()) form.set('lumaEventId', newLuma.trim());
      if (newDate) form.set('eventStartsAt', new Date(`${newDate}T18:00:00`).toISOString().replace('Z', '+00:00'));
    } else form.set('eventId', eventId ?? target);
    form.set('file', file!);
    setBusy(dryRun ? 'check' : 'import');
    try {
      const r = await api('eventsImport', { body: form });
      setResult(r);
      if (!dryRun) onImported?.(r);
    } catch (err) {
      setErrors(fieldErrors(err));
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  };

  return (
    <form
      class="form"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        void run(false);
      }}
      aria-describedby={`${idp}-help`}
    >
      <p class="muted small" id={`${idp}-help`}>
        {kind === 'luma_guests'
          ? 'In Luma: Manage event → Guests → Download as CSV. Re-importing updates guests by email.'
          : 'Export the reflection survey responses as CSV. Responses are matched to checked-in guests by email.'}
      </p>
      <fieldset class="fieldset">
        <legend class={compact ? 'visually-hidden' : undefined}>what are you importing?</legend>
        <div class="choice-row">
          <label class="choice">
            <input type="radio" name={`${idp}-kind`} value="luma_guests" checked={kind === 'luma_guests'} onChange={() => setKind('luma_guests')} />
            <span class="choice-title">Luma guest list</span>
            <span class="choice-sub">registrations and check-ins</span>
          </label>
          <label class="choice">
            <input type="radio" name={`${idp}-kind`} value="reflections" checked={kind === 'reflections'} onChange={() => setKind('reflections')} />
            <span class="choice-title">reflection survey</span>
            <span class="choice-sub">post-event responses</span>
          </label>
        </div>
      </fieldset>

      {!eventId && (
        <div class="grid-fields">
          <Field id={`${idp}-event`} label="event" error={errors.eventId}>
            <select class="select" {...describe(`${idp}-event`, errors.eventId)} value={target} onChange={(e) => setTarget(e.currentTarget.value)}>
              <option value="">choose an event…</option>
              {lookups.status === 'ready' &&
                lookups.data.events.map((ev) => (
                  <option value={ev.id} key={ev.id}>
                    {ev.name} — {fmtDate(ev.startsAt)}
                  </option>
                ))}
              <option value="__new">+ a new event from this export</option>
            </select>
          </Field>
          {creating && (
            <>
              <Field id={`${idp}-name`} label="event name" error={errors.eventName}>
                <input class="input" {...describe(`${idp}-name`, errors.eventName)} value={newName} onInput={(e) => setNewName(e.currentTarget.value)} />
              </Field>
              <Field id={`${idp}-luma`} label="Luma event id" optional hint="from the event URL, e.g. evt-abc123">
                <input class="input" {...describe(`${idp}-luma`, undefined, true)} value={newLuma} onInput={(e) => setNewLuma(e.currentTarget.value)} />
              </Field>
              <Field id={`${idp}-date`} label="date" optional>
                <input class="input" type="date" id={`${idp}-date`} value={newDate} onInput={(e) => setNewDate(e.currentTarget.value)} />
              </Field>
            </>
          )}
        </div>
      )}

      <Field id={`${idp}-file`} label="CSV file" error={errors.file} hint={`.csv up to ${bytes(CSV_MAX_BYTES)}`}>
        <div class="dropzone">
          <input
            type="file"
            accept=".csv,text/csv"
            {...describe(`${idp}-file`, errors.file, true)}
            onChange={(e) => {
              setFile(e.currentTarget.files?.[0] ?? null);
              setResult(null);
            }}
          />
          {file && (
            <span class="small muted">
              {file.name} · {bytes(file.size)}
            </span>
          )}
        </div>
      </Field>

      {error && <Notice tone="negative">{error}</Notice>}
      {result && <ImportResult r={result} />}

      <div class="form-actions">
        <button type="button" class="btn" disabled={!!busy} onClick={() => void run(true)}>
          {busy === 'check' ? 'checking…' : 'check file'}
        </button>
        <button type="submit" class="btn btn--primary" disabled={!!busy}>
          {busy === 'import' ? 'importing…' : 'import'}
        </button>
      </div>
    </form>
  );
}

function ImportResult({ r }: { r: CsvImportResponse }) {
  const m = r.metrics;
  return (
    <div class={`notice ${r.errors.length ? 'notice--attention' : 'notice--positive'}`} role="status" data-import-result>
      <strong>{r.dryRun ? 'Checked — nothing saved yet. ' : 'Imported. '}</strong>
      {r.rows} rows: {r.created} new, {r.updated} updated, {r.skipped} skipped.{' '}
      {r.kind === 'luma_guests'
        ? `${m.registered} registered, ${m.checkedIn} checked in (${pct(m.checkInRate)}).`
        : `${m.matchedReflections} of ${m.checkedIn} checked-in guests reflected (${pct(m.reflectionRate)}).`}
      {m.unmatchedReflections > 0 && ` ${m.unmatchedReflections} responses don't match a checked-in email.`}
      {!r.dryRun && (
        <>
          {' '}
          <a href={`/events/${encodeURIComponent(r.eventId)}`}>open the event →</a>
        </>
      )}
      {r.errors.length > 0 && (
        <ul class="small">
          {r.errors.slice(0, 8).map((e) => (
            <li key={e.row}>
              row {e.row}: {e.message}
            </li>
          ))}
          {r.errors.length > 8 && <li>…and {r.errors.length - 8} more</li>}
        </ul>
      )}
    </div>
  );
}
