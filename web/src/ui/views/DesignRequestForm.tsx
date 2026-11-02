import { flattenError } from 'zod';
import { useEffect, useRef, useState } from 'preact/hooks';
import { api, fieldErrors as apiFieldErrors, isStorageUnavailable } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import {
  ATTACHMENT_MAX_BYTES,
  ATTACHMENT_MIME_TYPES,
  DESIGN_TYPES,
  DesignRequestCreateRequest,
  type DesignRequestDetail,
  type DesignRequestDraft,
  type DesignType,
  type LookupsResponse,
} from '../../lib/contracts';
import { TYPE_LABEL, URGENCY_LABEL } from '../../lib/design';
import { bytes, fmtDate } from '../../lib/format';
import { describe, Field } from '../components/kit';
import { Async, errorMessage, Loading, Notice } from '../components/states';

interface FormState {
  title: string;
  type: DesignType;
  link: string; // event id, PROGRAM, or ''
  program: string;
  dueDate: string;
  urgency: 'normal' | 'high';
  formats: string;
  brief: string;
  references: string;
}

const PROGRAM = '__program';
const DRAFT_KEY = 'cseed-design-draft';
const FIELD_ID: Record<string, string> = {
  title: 'dr-title', type: 'dr-type', eventId: 'dr-link', program: 'dr-program', dueDate: 'dr-due', urgency: 'dr-urgency',
  formats: 'dr-formats', brief: 'dr-brief', references: 'dr-refs', files: 'dr-files',
};
const FRIENDLY: Record<string, string> = {
  title: 'Give the request a short title.',
  brief: 'Describe what you need: the message, the audience and anything that must appear.',
  dueDate: 'Use a valid date.',
  eventId: 'Pick an event from the list.',
};

const empty: FormState = { title: '', type: 'poster', link: '', program: '', dueDate: '', urgency: 'normal', formats: '', brief: '', references: '' };

function fromRequest(r: DesignRequestDetail): FormState {
  return {
    title: r.title, type: r.type, link: r.event ? r.event.id : r.program ? PROGRAM : '', program: r.program ?? '', dueDate: r.dueDate ?? '',
    urgency: r.urgency, formats: r.formats, brief: r.brief, references: r.references,
  };
}

function fromDraft(d: DesignRequestDraft): FormState {
  return { ...empty, title: d.title, type: d.type, link: d.eventId, dueDate: d.dueDate ?? '', urgency: d.urgency, formats: d.formats, brief: d.brief };
}

function toBody(f: FormState) {
  return {
    title: f.title,
    type: f.type,
    formats: f.formats,
    dueDate: f.dueDate || null,
    urgency: f.urgency,
    brief: f.brief,
    references: f.references,
    eventId: f.link && f.link !== PROGRAM ? f.link : null,
    program: f.link === PROGRAM && f.program.trim() ? f.program : null,
  };
}

const readDraft = (): FormState | null => {
  try {
    const raw = sessionStorage.getItem(DRAFT_KEY);
    return raw ? { ...empty, ...(JSON.parse(raw) as Partial<FormState>) } : null;
  } catch {
    return null;
  }
};
const writeDraft = (f: FormState | null) => {
  try {
    if (f) sessionStorage.setItem(DRAFT_KEY, JSON.stringify(f));
    else sessionStorage.removeItem(DRAFT_KEY);
  } catch {
    /* storage blocked: the form state still lives in memory */
  }
};

export interface Created {
  request: DesignRequestDetail;
  uploadFailed: number;
  storageOff: boolean;
}

/** New request (optionally prefilled from an event) or edit of an existing one. */
export default function DesignRequestForm({
  eventId,
  editing,
  onDone,
  onCancel,
}: {
  eventId?: string | undefined;
  editing?: DesignRequestDetail;
  onDone: (r: Created) => void;
  onCancel: () => void;
}) {
  if (eventId && !editing) return <FromEvent key={eventId} eventId={eventId} onDone={onDone} onCancel={onCancel} />;
  return <Plain editing={editing} onDone={onDone} onCancel={onCancel} />;
}

function Plain({ editing, onDone, onCancel }: { editing?: DesignRequestDetail | undefined; onDone: (r: Created) => void; onCancel: () => void }) {
  const lookups = useApi('lookups');
  return (
    <Async state={lookups} loading={<Loading rows={8} label="Loading the form" />}>
      {(l) => <Form lookups={l} editing={editing} persist={!editing} initial={editing ? fromRequest(editing) : readDraft() ?? empty} onDone={onDone} onCancel={onCancel} />}
    </Async>
  );
}

function FromEvent({ eventId, onDone, onCancel }: { eventId: string; onDone: (r: Created) => void; onCancel: () => void }) {
  const lookups = useApi('lookups');
  const draft = useApi('designRequestDraftFromEvent', { params: { id: eventId } });
  if (draft.status === 'loading' || lookups.status === 'loading') return <Loading rows={8} label="Preparing the request from the event" />;
  if (lookups.status === 'error') return <Async state={lookups}>{() => null}</Async>;
  if (draft.status === 'error') {
    return (
      <>
        <Notice tone="attention">Couldn't prefill from that event: {errorMessage(draft.error)} Fill in the brief below.</Notice>
        <Form lookups={lookups.data} initial={{ ...empty, link: eventId }} onDone={onDone} onCancel={onCancel} />
      </>
    );
  }
  return <Form lookups={lookups.data} initial={fromDraft(draft.data.draft)} onDone={onDone} onCancel={onCancel} />;
}

function Form({ lookups, editing, initial, persist = false, onDone, onCancel }: { lookups: LookupsResponse; editing?: DesignRequestDetail | undefined; initial: FormState; persist?: boolean; onDone: (r: Created) => void; onCancel: () => void }) {
  const [f, setF] = useState<FormState>(initial);
  const [files, setFiles] = useState<File[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const summaryRef = useRef<HTMLDivElement>(null);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((s) => ({ ...s, [k]: v }));

  // Keep an unsent new request across reloads and network failures (cleared on success).
  useEffect(() => {
    if (persist) writeDraft(f);
  }, [f, persist]);

  const events = lookups.events.slice().sort((a, b) => b.startsAt.localeCompare(a.startsAt));
  const linkedMissing = f.link && f.link !== PROGRAM && !events.some((e) => e.id === f.link);

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    const next = [...files];
    const problems: string[] = [];
    for (const file of Array.from(list)) {
      if (!(ATTACHMENT_MIME_TYPES as readonly string[]).includes(file.type)) problems.push(`${file.name}: use PDF, PNG, JPEG, WebP or HEIC`);
      else if (file.size > ATTACHMENT_MAX_BYTES) problems.push(`${file.name} is ${bytes(file.size)}; the limit is ${bytes(ATTACHMENT_MAX_BYTES)}`);
      else if (next.length >= 10) problems.push('Up to 10 files at a time');
      else if (!next.some((x) => x.name === file.name && x.size === file.size)) next.push(file);
    }
    setFiles(next);
    setErrors((e) => {
      const { files: _drop, ...rest } = e;
      return problems.length ? { ...rest, files: problems.join('. ') + '.' } : rest;
    });
  };

  const showErrors = (e: Record<string, string>) => {
    setErrors(e);
    requestAnimationFrame(() => {
      const first = Object.keys(FIELD_ID).find((k) => e[k]);
      if (first) document.getElementById(FIELD_ID[first]!)?.focus();
      else summaryRef.current?.focus();
    });
  };

  const save = async () => {
    setFailure(null);
    const parsed = DesignRequestCreateRequest.safeParse(toBody(f));
    if (!parsed.success) {
      const flat = flattenError(parsed.error).fieldErrors as Record<string, string[] | undefined>;
      const e: Record<string, string> = {};
      for (const k of Object.keys(flat)) if (flat[k]?.length) e[k] = FRIENDLY[k] ?? flat[k]![0]!;
      if (f.link === PROGRAM && !f.program.trim()) e.program = 'Name the program, or choose "Not linked".';
      return showErrors(e);
    }
    if (f.link === PROGRAM && !f.program.trim()) return showErrors({ program: 'Name the program, or choose "Not linked".' });
    setErrors({});
    setBusy(editing ? 'Saving…' : 'Submitting…');
    try {
      let { request } = editing ? await api('designRequestUpdate', { params: { id: editing.id }, body: parsed.data }) : await api('designRequestCreate', { body: parsed.data });
      if (persist) writeDraft(null);
      let failed = 0;
      let storageOff = false;
      for (const [i, file] of files.entries()) {
        setBusy(`Uploading ${i + 1} of ${files.length}…`);
        const form = new FormData();
        form.set('kind', 'reference');
        form.set('file', file);
        try {
          request = (await api('designRequestUpload', { params: { id: request.id }, body: form })).request;
        } catch (err) {
          failed++;
          if (isStorageUnavailable(err)) {
            storageOff = true;
            failed = files.length - i;
            break;
          }
        }
      }
      setBusy(null);
      onDone({ request, uploadFailed: failed, storageOff });
    } catch (err) {
      setBusy(null);
      const fe = apiFieldErrors(err);
      setFailure(`${errorMessage(err)} Your brief is still here; try again when you're ready.`);
      showErrors(Object.fromEntries(Object.keys(fe).map((k) => [k, FRIENDLY[k] ?? fe[k]!])));
    }
  };

  const errCount = Object.keys(errors).length;

  return (
    <form
      class="form"
      noValidate
      aria-busy={busy ? 'true' : undefined}
      data-design-form
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <div ref={summaryRef} tabIndex={-1}>
        {errCount > 0 && <Notice tone="negative">{errCount === 1 ? 'One field needs attention.' : `${errCount} fields need attention.`}</Notice>}
        {failure && <Notice tone="negative">{failure}</Notice>}
      </div>

      <div class="grid-fields">
        <Field id="dr-title" label="Title" error={errors.title} wide>
          <input class="input" {...describe('dr-title', errors.title)} value={f.title} maxLength={200} placeholder="e.g. Venture night poster" onInput={(e) => set('title', e.currentTarget.value)} />
        </Field>
        <Field id="dr-type" label="Type" error={errors.type}>
          <select class="select" {...describe('dr-type', errors.type)} value={f.type} onChange={(e) => set('type', e.currentTarget.value as DesignType)}>
            {DESIGN_TYPES.map((t) => (
              <option value={t} key={t}>
                {TYPE_LABEL[t]}
              </option>
            ))}
          </select>
        </Field>
        <Field id="dr-link" label="Linked event or program" optional error={errors.eventId}>
          <select class="select" {...describe('dr-link', errors.eventId)} value={f.link} onChange={(e) => set('link', e.currentTarget.value)}>
            <option value="">Not linked</option>
            {linkedMissing && editing?.event && <option value={f.link}>{editing.event.name}</option>}
            {events.map((ev) => (
              <option value={ev.id} key={ev.id}>
                {ev.name} · {fmtDate(ev.startsAt)}
              </option>
            ))}
            <option value={PROGRAM}>A program (not an event)…</option>
          </select>
        </Field>
        {f.link === PROGRAM && (
          <Field id="dr-program" label="Program name" error={errors.program}>
            <input class="input" {...describe('dr-program', errors.program)} value={f.program} maxLength={200} placeholder="e.g. Mentorship program" onInput={(e) => set('program', e.currentTarget.value)} />
          </Field>
        )}
        <Field id="dr-due" label="Due date" optional error={errors.dueDate} hint="Vancouver date. Leave a week for design and review.">
          <input class="input" type="date" {...describe('dr-due', errors.dueDate, true)} value={f.dueDate} onInput={(e) => set('dueDate', e.currentTarget.value)} />
        </Field>
        <Field id="dr-urgency" label="Urgency" error={errors.urgency}>
          <select class="select" {...describe('dr-urgency', errors.urgency)} value={f.urgency} onChange={(e) => set('urgency', e.currentTarget.value as 'normal' | 'high')}>
            <option value="normal">{URGENCY_LABEL.normal}</option>
            <option value="high">{URGENCY_LABEL.high}</option>
          </select>
        </Field>
        <Field id="dr-formats" label="Sizes / formats" optional error={errors.formats} wide>
          <input class="input" {...describe('dr-formats', errors.formats)} value={f.formats} maxLength={500} placeholder="e.g. A3 print PDF; 1080 × 1350 PNG" onInput={(e) => set('formats', e.currentTarget.value)} />
        </Field>
      </div>
      <Field id="dr-brief" label="Brief" error={errors.brief} hint="The message, the audience, and anything that must appear (date, place, QR code, sponsors).">
        <textarea class="textarea" rows={5} {...describe('dr-brief', errors.brief, true)} value={f.brief} maxLength={4000} onInput={(e) => set('brief', e.currentTarget.value)} />
      </Field>
      <Field id="dr-refs" label="References" optional error={errors.references} hint="Templates, links or past work to follow.">
        <textarea class="textarea" rows={3} {...describe('dr-refs', errors.references, true)} value={f.references} maxLength={4000} onInput={(e) => set('references', e.currentTarget.value)} />
      </Field>
      <Field id="dr-files" label={editing ? 'Add reference files' : 'Attachments'} optional error={errors.files} hint={`Copy, logos or examples · PDF, PNG, JPEG, WebP or HEIC, up to ${bytes(ATTACHMENT_MAX_BYTES)} each`}>
        <input
          class="input"
          type="file"
          multiple
          accept={ATTACHMENT_MIME_TYPES.join(',')}
          {...describe('dr-files', errors.files, true)}
          onChange={(e) => {
            addFiles(e.currentTarget.files);
            e.currentTarget.value = '';
          }}
        />
      </Field>
      {files.length > 0 && (
        <ul class="files" aria-label="Files to upload">
          {files.map((file, i) => (
            <li key={`${file.name}-${file.size}`}>
              <span class="file-name">{file.name}</span>
              <span class="file-meta">{bytes(file.size)}</span>
              <button type="button" class="linkish linkish--danger" onClick={() => setFiles(files.filter((_, j) => j !== i))} aria-label={`Remove ${file.name}`}>
                remove
              </button>
            </li>
          ))}
        </ul>
      )}

      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={!!busy}>
          {busy ?? (editing ? 'Save changes' : 'Submit design request')}
        </button>
        <button
          type="button"
          class="linkish"
          onClick={() => {
            if (persist) writeDraft(null);
            onCancel();
          }}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

