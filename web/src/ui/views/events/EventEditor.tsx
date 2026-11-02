import type { JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api, fieldErrors } from '../../../lib/api/client';
import { useApi } from '../../../lib/api/useApi';
import { fromLocalInput, toLocalInput, TZ_LABEL } from '../../../lib/calendar';
import type { EventDetailResponse, EventStatus, EventUpsertRequest } from '../../../lib/contracts';
import { safeUrl } from '../../../lib/format';
import { describe, Field } from '../../components/kit';
import { errorMessage, ErrorState, Loading, Notice } from '../../components/states';

type Detail = EventDetailResponse['event'];
type Flash = { tone: 'positive' | 'negative'; text: string };

const COVER_TYPES = ['image/jpeg', 'image/png', 'image/webp'];
const COVER_MAX = 5 * 1024 * 1024;

interface Draft {
  name: string;
  starts: string;
  ends: string;
  location: string;
  hostSubteamId: string;
  hostUserId: string;
  description: string;
  capacity: string;
  status: EventStatus;
}

const blank: Draft = { name: '', starts: '', ends: '', location: '', hostSubteamId: '', hostUserId: '', description: '', capacity: '', status: 'draft' };

function fromEvent(e: Detail): Draft {
  return {
    name: e.name,
    starts: toLocalInput(e.startsAt),
    ends: toLocalInput(e.endsAt),
    location: e.location ?? '',
    hostSubteamId: e.hostSubteam?.id ?? '',
    hostUserId: e.host?.id ?? '',
    description: e.description ?? '',
    capacity: e.capacity === null ? '' : String(e.capacity),
    status: e.status,
  };
}

type EditorProps = {
  eventId: string | null;
  onSaved: (id: string, created: boolean) => void;
  onRequestDesign: (id: string) => void;
  onDelete: (id: string) => void;
  onOpenKit: (id: string) => void;
};

/** Load the host pickers (and the event, when editing), then show the form. */
export function EventEditor(props: EditorProps) {
  const look = useApi('lookups');
  // Survives the remount when a new draft becomes an edit of its saved id.
  const [flash, setFlash] = useState<Flash | null>(null);
  if (look.status === 'loading') return <Loading rows={8} label="Loading the editor" />;
  if (look.status === 'error') return <ErrorState error={look.error} onRetry={look.reload} />;
  const form = (event: Detail | null, reload?: () => void) => (
    <EditorForm
      {...props}
      event={event}
      lookups={look.data}
      flash={flash}
      onSaved={(id, created, message) => {
        setFlash(message);
        if (!created) reload?.();
        props.onSaved(id, created);
      }}
      key={event?.id ?? 'new'}
    />
  );
  return props.eventId ? <WithEvent id={props.eventId} render={form} /> : form(null);
}

function WithEvent({ id, render }: { id: string; render: (event: Detail, reload: () => void) => JSX.Element }) {
  const detail = useApi('eventDetail', { params: { id } });
  if (detail.status === 'loading') return <Loading rows={8} label="Loading the editor" />;
  if (detail.status === 'error') return <ErrorState error={detail.error} onRetry={detail.reload} />;
  return render(detail.data.event, detail.reload);
}

function EditorForm({
  event,
  lookups,
  onSaved,
  onRequestDesign,
  onDelete,
  onOpenKit,
  flash,
}: {
  event: Detail | null;
  lookups: { subteams: { id: string; name: string }[]; hosts: { id: string; name: string }[] };
  onSaved: (id: string, created: boolean, message: Flash) => void;
  onRequestDesign: (id: string) => void;
  onDelete: (id: string) => void;
  onOpenKit: (id: string) => void;
  flash: Flash | null;
}) {
  const [d, setD] = useState<Draft>(event ? fromEvent(event) : blank);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(
    flash ?? (event ? null : { tone: 'positive', text: 'New event · Add the details, then save your draft.' }),
  );
  const [cover, setCover] = useState<File | null>(null);
  const [removeCover, setRemoveCover] = useState(false);
  const [preview, setPreview] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const set = (k: keyof Draft) => (e: Event) => setD({ ...d, [k]: (e.currentTarget as HTMLInputElement).value });

  useEffect(() => {
    if (!cover) return setPreview(null);
    const url = URL.createObjectURL(cover);
    setPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [cover]);

  const existingCover = !removeCover && event?.coverUrl ? safeUrl(event.coverUrl) : null;
  const shownCover = preview ?? existingCover;
  const id = event?.id;

  function check(): Record<string, string> {
    const e: Record<string, string> = {};
    if (!d.name.trim()) e.name = 'Give the event a name.';
    const starts = fromLocalInput(d.starts);
    if (!starts) e.startsAt = 'Pick when it starts.';
    const ends = d.ends ? fromLocalInput(d.ends) : null;
    if (d.ends && !ends) e.endsAt = 'Pick a valid end time.';
    if (starts && ends && new Date(ends) < new Date(starts)) e.endsAt = 'The event ends before it starts.';
    if (d.capacity.trim() && !/^\d+$/.test(d.capacity.trim())) e.capacity = 'Capacity is a whole number.';
    else if (d.capacity.trim() && (Number(d.capacity) < 1 || Number(d.capacity) > 100000)) e.capacity = 'Capacity is between 1 and 100,000.';
    return e;
  }

  async function save(ev: Event) {
    ev.preventDefault();
    const found = check();
    setErrors(found);
    if (Object.keys(found).length) {
      setMessage({ tone: 'negative', text: 'Fix the highlighted fields, then save again.' });
      return;
    }
    const body: EventUpsertRequest = {
      name: d.name.trim(),
      startsAt: fromLocalInput(d.starts)!,
      endsAt: d.ends ? fromLocalInput(d.ends) : null,
      location: d.location.trim() || null,
      hostSubteamId: d.hostSubteamId || null,
      hostUserId: d.hostUserId || null,
      description: d.description.trim() || null,
      capacity: d.capacity.trim() ? Number(d.capacity) : null,
      ...(event ? { status: d.status } : {}),
    };
    setBusy(true);
    setMessage(null);
    let sid: string;
    try {
      const saved = event ? await api('eventUpdate', { params: { id: event.id }, body }) : await api('eventCreate', { body });
      sid = saved.event.id;
    } catch (err) {
      setErrors(fieldErrors(err));
      setMessage({ tone: 'negative', text: errorMessage(err) });
      setBusy(false);
      return;
    }
    // The event is saved from here on: a cover failure is reported on its own, and the next
    // Save edits this event instead of creating another draft.
    const done = event ? 'Changes saved.' : 'Draft saved. Open its Luma handoff kit when you are ready to publish.';
    let result: Flash = { tone: 'positive', text: done };
    try {
      if (cover) {
        const f = new FormData();
        f.set('file', cover, cover.name);
        await api('eventCoverPut', { params: { id: sid }, body: f });
        setCover(null);
        if (fileRef.current) fileRef.current.value = '';
      } else if (removeCover && event?.coverUrl) {
        await api('eventCoverDelete', { params: { id: sid } });
      }
      setRemoveCover(false);
    } catch (err) {
      const retry = event ? 'Choose Save again to retry, or pick another image.' : 'Choose the image again to retry.';
      result = { tone: 'negative', text: `${done.replace(/\.$/, '')}, but the cover couldn't be uploaded: ${errorMessage(err)} ${retry}` };
      setErrors({ cover: errorMessage(err) });
    }
    setMessage(result);
    setBusy(false);
    onSaved(sid, !event, result);
  }

  function pickCover(e: Event) {
    const f = (e.currentTarget as HTMLInputElement).files?.[0] ?? null;
    if (f && !COVER_TYPES.includes(f.type)) {
      setErrors({ ...errors, cover: 'Choose a JPG, PNG or WebP image.' });
      (e.currentTarget as HTMLInputElement).value = '';
      return;
    }
    if (f && f.size > COVER_MAX) {
      setErrors({ ...errors, cover: 'The cover must be 5 MB or smaller.' });
      (e.currentTarget as HTMLInputElement).value = '';
      return;
    }
    const { cover: _drop, ...rest } = errors;
    setErrors(rest);
    setCover(f);
    if (f) setRemoveCover(false);
  }

  const isDraft = (event?.status ?? 'draft') === 'draft';
  return (
    <div class="stack" data-event-editor={id ?? 'new'}>
      <Notice tone="info">
        {isDraft
          ? 'Draft · Only saved in this workspace until you publish in Luma.'
          : event?.status === 'cancelled'
            ? 'Cancelled · If it was published in Luma, cancel it there too.'
            : 'Published in Luma · Changes here do not update the Luma page.'}
      </Notice>
      <form class="stack" onSubmit={save} noValidate>
        <Field id="ev-name" label="Event name" error={errors.name}>
          <input class="input" type="text" maxLength={200} value={d.name} onInput={set('name')} {...describe('ev-name', errors.name)} />
        </Field>
        <div class="grid-fields">
          <Field id="ev-starts" label={`Starts · ${TZ_LABEL}`} error={errors.startsAt}>
            <input class="input" type="datetime-local" value={d.starts} onInput={set('starts')} {...describe('ev-starts', errors.startsAt)} />
          </Field>
          <Field id="ev-ends" label={`Ends · ${TZ_LABEL}`} error={errors.endsAt} optional>
            <input class="input" type="datetime-local" value={d.ends} onInput={set('ends')} {...describe('ev-ends', errors.endsAt)} />
          </Field>
        </div>
        <Field id="ev-location" label="Location" error={errors.location} optional>
          <input class="input" type="text" maxLength={200} value={d.location} onInput={set('location')} {...describe('ev-location', errors.location)} />
        </Field>
        <div class="grid-fields">
          <Field id="ev-team" label="Host team" error={errors.hostSubteamId} optional>
            <select class="select" value={d.hostSubteamId} onChange={set('hostSubteamId')} {...describe('ev-team', errors.hostSubteamId)}>
              <option value="">No team</option>
              {lookups.subteams.map((t) => (
                <option value={t.id} key={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          </Field>
          <Field id="ev-host" label="Host officer" error={errors.hostUserId} optional>
            <select class="select" value={d.hostUserId} onChange={set('hostUserId')} {...describe('ev-host', errors.hostUserId)}>
              <option value="">No one yet</option>
              {lookups.hosts.map((u) => (
                <option value={u.id} key={u.id}>
                  {u.name}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <Field id="ev-description" label="Description" error={errors.description} optional hint="Copied into Luma from the handoff kit.">
          <textarea class="textarea" rows={5} maxLength={5000} value={d.description} onInput={set('description')} {...describe('ev-description', errors.description, true)} />
        </Field>
        <div class="grid-fields">
          <Field id="ev-capacity" label="Capacity" error={errors.capacity} optional>
            <input class="input" type="number" inputMode="numeric" min={1} max={100000} step={1} value={d.capacity} onInput={set('capacity')} {...describe('ev-capacity', errors.capacity)} />
          </Field>
          {event && (
            <Field id="ev-status" label="Status">
              <select class="select" id="ev-status" value={d.status} onChange={set('status')}>
                <option value="draft">Draft</option>
                <option value="published">Published</option>
                <option value="completed">Completed</option>
                <option value="cancelled">Cancelled</option>
              </select>
            </Field>
          )}
        </div>
        <div class={`field${errors.cover ? ' field--error' : ''}`}>
          <label class="dropzone" for="ev-cover">
            <span>Cover image · JPG, PNG or WebP, up to 5 MB</span>
            <input
              ref={fileRef}
              id="ev-cover"
              type="file"
              accept="image/jpeg,image/png,image/webp"
              onChange={pickCover}
              aria-invalid={errors.cover ? true : undefined}
              aria-describedby={errors.cover ? 'ev-cover-error' : 'ev-cover-hint'}
            />
          </label>
          {errors.cover && (
            <p class="field-error" id="ev-cover-error">
              {errors.cover}
            </p>
          )}
          {shownCover ? (
            <>
              <img class="event-cover" src={shownCover} alt={`Cover for ${d.name || 'this event'}`} />
              <p class="field-hint" id="ev-cover-hint">
                {cover ? `${cover.name} · saved with the event.` : 'Current cover.'}{' '}
                <button
                  type="button"
                  class="linkish"
                  onClick={() => {
                    setCover(null);
                    if (fileRef.current) fileRef.current.value = '';
                    if (event?.coverUrl) setRemoveCover(true);
                  }}
                >
                  Remove cover
                </button>
              </p>
            </>
          ) : (
            <p class="field-hint" id="ev-cover-hint">
              {removeCover ? 'The cover is removed when you save.' : 'Use approved event artwork. Luma asks for it as an upload.'}
            </p>
          )}
        </div>
        <div class="action-row">
          <button class="btn btn--primary" type="submit" disabled={busy}>
            {busy ? 'Saving…' : event ? 'Save changes' : 'Save draft'}
          </button>
          {id && (
            <>
              <button class="btn" type="button" onClick={() => onOpenKit(id)}>
                Open handoff kit
              </button>
              <button class="btn" type="button" onClick={() => onRequestDesign(id)} data-request-design>
                Request design
              </button>
              <button class="btn" type="button" onClick={() => onDelete(id)} data-delete-event>
                Delete event
              </button>
            </>
          )}
        </div>
        <p role="status" id="ev-saved" class={message ? `notice notice--${message.tone}` : 'visually-hidden'}>
          {message?.text ?? ''}
        </p>
      </form>
    </div>
  );
}
