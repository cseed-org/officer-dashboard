import { useEffect, useRef, useState } from 'preact/hooks';
import { api } from '../../../lib/api/client';
import { useApi } from '../../../lib/api/useApi';
import { whenLong } from '../../../lib/calendar';
import { type EventDetailResponse, lumaEventUrlProblem } from '../../../lib/contracts';
import { safeUrl } from '../../../lib/format';
import { errorMessage, ErrorState, Loading } from '../../components/states';
import { hostLabel } from './shared';

/** There is no Luma API: Luma's own page for starting an event by hand. */
export const LUMA_CREATE_URL = 'https://lu.ma/create';

type Detail = EventDetailResponse['event'];

function CopyRow({ label, value, onCopied }: { label: string; value: string; onCopied: (msg: string) => void }) {
  const [done, setDone] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setDone(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setDone(false), 2500);
      onCopied(`${label} copied.`);
    } catch {
      onCopied('Copy unavailable. Select the field text and copy it manually.');
    }
  };
  return (
    <div class="event-copy" data-copy-field={label.toLowerCase()}>
      <div>
        <span class="field-label">{label}</span>
        <p>{value}</p>
      </div>
      <button type="button" class="btn" onClick={copy}>
        {done ? `Copied ${label.toLowerCase()} ✓` : `Copy ${label.toLowerCase()}`}
      </button>
    </div>
  );
}

export function HandoffKit({ eventId, onChanged }: { eventId: string; onChanged: () => void }) {
  const detail = useApi('eventDetail', { params: { id: eventId } });
  if (detail.status === 'loading') return <Loading rows={8} label="Loading the handoff kit" />;
  if (detail.status === 'error') return <ErrorState error={detail.error} onRetry={detail.reload} />;
  return (
    <Kit
      event={detail.data.event}
      onSaved={() => {
        detail.reload();
        onChanged();
      }}
    />
  );
}

function Kit({ event: e, onSaved }: { event: Detail; onSaved: () => void }) {
  const [copyStatus, setCopyStatus] = useState('');
  const [url, setUrl] = useState(e.lumaUrl ?? '');
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const cover = safeUrl(e.coverUrl);
  const host = hostLabel(e);
  const linked = safeUrl(e.lumaUrl);

  async function patchLink(next: string | null) {
    setBusy(true);
    setNote(null);
    try {
      // Saving the published page moves a draft to published; unlinking leaves the status alone.
      await api('eventUpdate', { params: { id: e.id }, body: next ? { lumaUrl: next, ...(e.status === 'draft' ? { status: 'published' as const } : {}) } : { lumaUrl: null } });
      setProblem(null);
      setNote(next ? null : 'Unlinked.');
      if (!next) setUrl('');
      onSaved();
    } catch (err) {
      setProblem(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  function submit(ev: Event) {
    ev.preventDefault();
    const p = url.trim() ? lumaEventUrlProblem(url) : 'Paste the Luma event URL first.';
    setProblem(p);
    if (!p) void patchLink(url.trim());
  }

  return (
    <div class="stack" data-handoff-kit={e.id}>
      <p class="notice notice--info">Publish manually in Luma. Copy each field, upload the cover, then paste the published URL below. No automatic sync.</p>
      <div>
        <CopyRow label="Name" value={e.name} onCopied={setCopyStatus} />
        <CopyRow label="Time" value={whenLong(e.startsAt, e.endsAt)} onCopied={setCopyStatus} />
        {e.location && <CopyRow label="Location" value={e.location} onCopied={setCopyStatus} />}
        {host && <CopyRow label="Host" value={host} onCopied={setCopyStatus} />}
        {e.description && <CopyRow label="Description" value={e.description} onCopied={setCopyStatus} />}
        {e.capacity !== null && <CopyRow label="Capacity" value={String(e.capacity)} onCopied={setCopyStatus} />}
      </div>
      {(!e.location || !e.description || e.capacity === null) && (
        <p class="field-hint">Fields left blank in the editor are not listed. Add them there if Luma needs them.</p>
      )}
      {cover ? (
        <>
          <img class="event-cover" src={cover} alt={`Cover for ${e.name}`} />
          <a class="btn" href={cover} download>
            Download cover image
          </a>
        </>
      ) : (
        <p class="field-hint">No cover yet. Add one in the editor, or request design for this event.</p>
      )}
      <a class="btn btn--primary" href={LUMA_CREATE_URL} target="_blank" rel="noopener noreferrer">
        Open Luma create ↗ <span class="visually-hidden">(opens in a new tab)</span>
        <span aria-hidden="true"> (new tab)</span>
      </a>
      <form class="stack" onSubmit={submit} noValidate>
        <div class={`field${problem ? ' field--error' : ''}`}>
          <label class="field-label" for="luma-url">
            Paste Luma URL
          </label>
          <input
            class="input"
            id="luma-url"
            type="url"
            inputMode="url"
            placeholder="https://lu.ma/your-event"
            value={url}
            onInput={(ev) => setUrl(ev.currentTarget.value)}
            aria-invalid={problem ? true : undefined}
            aria-describedby="link-state"
          />
        </div>
        {problem ? (
          <p id="link-state" class="notice notice--negative" role="alert">
            {problem}
          </p>
        ) : linked ? (
          <p id="link-state" class="notice notice--positive" role="status">
            Linked ✓ ·{' '}
            <a href={linked} target="_blank" rel="noopener noreferrer">
              {linked}
            </a>{' '}
            · CSV imports still required.
          </p>
        ) : (
          <p id="link-state" class="notice" role="status">
            {note ? `${note} ` : ''}Unlinked · Attendee data is imported by CSV.
          </p>
        )}
        <div class="action-row">
          <button class="btn" type="submit" disabled={busy}>
            {busy ? 'Saving…' : 'Save Luma link'}
          </button>
          {e.lumaUrl && (
            <button class="btn" type="button" disabled={busy} onClick={() => void patchLink(null)}>
              Unlink
            </button>
          )}
        </div>
      </form>
      <section class="stack" aria-labelledby="notion-h">
        <h3 id="notion-h">
          Notion event page <span class="badge">Coming soon</span>
        </h3>
        <div class="field">
          <label class="field-label" for="notion-url">
            Notion page link
          </label>
          {/* TODO(notion): show e.notion.url once a real NotionSync replaces the stub. */}
          <input class="input" id="notion-url" disabled placeholder="Notion page link will appear here" value={e.notion.placeholder ? '' : (e.notion.url ?? '')} />
        </div>
        <p class="field-hint">Placeholder only. No page is created or synced yet.</p>
      </section>
      <p role="status" class="small muted">
        {copyStatus}
      </p>
    </div>
  );
}
