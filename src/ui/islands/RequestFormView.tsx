import type { ComponentChildren } from 'preact';
import { flattenError } from 'zod';
import { useMemo, useRef, useState } from 'preact/hooks';
import {
  ATTACHMENT_MAX_BYTES,
  ATTACHMENT_MIME_TYPES,
  PurchaseRequestCreateRequest,
  type LookupsResponse,
} from '../../core/contracts';
import { FUNCTIONAL_CLASSES, type FunctionalClass, type PurchaseType } from '../../core/domain';
import { api, fieldErrors as apiFieldErrors } from '../api/client';
import { useApi } from '../api/useApi';
import { describe, Field } from '../components/kit';
import { Async, errorMessage, Loading, Notice } from '../components/states';
import { bytes, dollarsToCents, fmtDate, formatCents, FUNCTIONAL_LABEL } from '../format';

interface FormState {
  type: PurchaseType;
  title: string;
  vendor: string;
  itemUrl: string;
  quantity: string;
  unitCost: string;
  taxShip: string;
  expenseAccountId: string;
  fundId: string;
  eventId: string;
  functionalClass: FunctionalClass;
  neededBy: string;
  justification: string;
}

const FUNCTIONAL_HINT: Record<FunctionalClass, string> = {
  program: 'events, workshops, member programs',
  mgmt_general: 'admin, tools, officer operations',
  fundraising: 'sponsor outreach, donor events',
};

const FIELD_ORDER = ['type', 'title', 'vendor', 'itemUrl', 'quantity', 'unitCostCents', 'taxShipCents', 'expenseAccountId', 'fundId', 'eventId', 'functionalClass', 'neededBy', 'justification', 'files'];
const FIELD_ID: Record<string, string> = {
  type: 'rq-type-reimbursement',
  title: 'rq-title',
  vendor: 'rq-vendor',
  itemUrl: 'rq-url',
  quantity: 'rq-qty',
  unitCostCents: 'rq-unit',
  taxShipCents: 'rq-tax',
  expenseAccountId: 'rq-account',
  fundId: 'rq-fund',
  eventId: 'rq-event',
  functionalClass: 'rq-class',
  neededBy: 'rq-needed',
  justification: 'rq-why',
  files: 'rq-files',
};

/** Turn contract (zod) messages into plain language for the form. */
const FRIENDLY: Record<string, string> = {
  title: 'Say what you are buying.',
  vendor: 'Who is selling it?',
  itemUrl: 'That link doesn’t look like a full URL (https://…).',
  quantity: 'Quantity must be a whole number from 1 to 10,000.',
  unitCostCents: 'Enter the price per item, like 24.50.',
  taxShipCents: 'Enter tax and shipping, like 3.75, or leave it at 0.',
  expenseAccountId: 'Pick a budget category.',
  fundId: 'Pick the fund that pays for it.',
  justification: 'Explain what it is for, so a director can approve it.',
  neededBy: 'Use a date.',
};

function toBody(f: FormState, submit: boolean) {
  const unit = dollarsToCents(f.unitCost);
  const tax = dollarsToCents(f.taxShip);
  return {
    type: f.type,
    title: f.title,
    vendor: f.vendor,
    itemUrl: f.itemUrl.trim() ? f.itemUrl.trim() : null,
    quantity: f.quantity.trim() === '' ? Number.NaN : Number(f.quantity),
    unitCostCents: unit ?? Number.NaN,
    taxShipCents: tax ?? 0,
    expenseAccountId: f.expenseAccountId,
    fundId: f.fundId,
    eventId: f.eventId || null,
    functionalClass: f.functionalClass,
    justification: f.justification,
    neededBy: f.neededBy || null,
    submit,
  };
}

export default function RequestFormView({ initialEventId }: { initialEventId?: string }) {
  const lookups = useApi('lookups');
  return (
    <Async state={lookups} loading={<Loading rows={8} label="Loading the form" />}>
      {(l) => <RequestForm lookups={l} initialEventId={initialEventId} />}
    </Async>
  );
}

function RequestForm({ lookups, initialEventId }: { lookups: LookupsResponse; initialEventId?: string }) {
  const general = lookups.funds.find((f) => f.kind === 'general') ?? lookups.funds[0];
  const [f, setF] = useState<FormState>({
    type: 'reimbursement',
    title: '',
    vendor: '',
    itemUrl: '',
    quantity: '1',
    unitCost: '',
    taxShip: '0',
    expenseAccountId: '',
    fundId: general?.id ?? '',
    eventId: lookups.events.some((e) => e.id === initialEventId) ? initialEventId! : '',
    functionalClass: 'program',
    neededBy: '',
    justification: '',
  });
  const [files, setFiles] = useState<File[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const summaryRef = useRef<HTMLDivElement>(null);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((s) => ({ ...s, [k]: v }));

  const total = useMemo(() => {
    const q = Number(f.quantity);
    const u = dollarsToCents(f.unitCost);
    const t = dollarsToCents(f.taxShip) ?? 0;
    if (!Number.isInteger(q) || q < 1 || u === null || Number.isNaN(u) || Number.isNaN(t)) return null;
    return q * u + t;
  }, [f.quantity, f.unitCost, f.taxShip]);

  const addFiles = (list: FileList | null) => {
    if (!list) return;
    const next = [...files];
    const problems: string[] = [];
    for (const file of Array.from(list)) {
      if (!(ATTACHMENT_MIME_TYPES as readonly string[]).includes(file.type)) problems.push(`${file.name}: use PDF, PNG, JPEG, HEIC or WebP`);
      else if (file.size > ATTACHMENT_MAX_BYTES) problems.push(`${file.name} is ${bytes(file.size)}; the limit is ${bytes(ATTACHMENT_MAX_BYTES)}`);
      else if (next.length >= 10) problems.push('Up to 10 files per request');
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
      const first = FIELD_ORDER.find((k) => e[k]);
      if (first) document.getElementById(FIELD_ID[first]!)?.focus();
      else summaryRef.current?.focus();
    });
  };

  const save = async (submit: boolean) => {
    setFailure(null);
    const parsed = PurchaseRequestCreateRequest.safeParse(toBody(f, submit));
    if (!parsed.success) {
      const flat = flattenError(parsed.error).fieldErrors as Record<string, string[] | undefined>;
      const e: Record<string, string> = {};
      for (const k of Object.keys(flat)) if (flat[k]?.length) e[k] = FRIENDLY[k] ?? flat[k]![0]!;
      if (errors.files) e.files = errors.files;
      return showErrors(e);
    }
    setErrors({});
    setBusy(submit ? 'submitting…' : 'saving…');
    try {
      const { request } = await api('requestCreate', { body: parsed.data });
      let failed = 0;
      for (const [i, file] of files.entries()) {
        setBusy(`uploading ${i + 1} of ${files.length}…`);
        const form = new FormData();
        form.set('kind', 'receipt');
        form.set('file', file);
        try {
          await api('requestAttach', { params: { id: request.id }, body: form });
        } catch {
          failed++;
        }
      }
      const qs = new URLSearchParams({ created: submit ? 'submitted' : 'draft' });
      if (failed) qs.set('uploadFailed', String(failed));
      location.assign(`/requests/${encodeURIComponent(request.id)}?${qs}`);
    } catch (err) {
      setBusy(null);
      const fe = apiFieldErrors(err);
      const friendly = Object.fromEntries(Object.keys(fe).map((k) => [k, FRIENDLY[k] ?? fe[k]!]));
      setFailure(errorMessage(err));
      showErrors(friendly);
    }
  };

  const errCount = Object.keys(errors).length;
  const acctHint = lookups.expenseAccounts.find((a) => a.id === f.expenseAccountId)?.form990Line;

  return (
    <form
      class="form"
      noValidate
      aria-busy={busy ? 'true' : undefined}
      onSubmit={(e) => {
        e.preventDefault();
        void save(true);
      }}
    >
      <div ref={summaryRef} tabIndex={-1}>
        {errCount > 0 && (
          <Notice tone="negative">
            {errCount === 1 ? 'One field needs attention.' : `${errCount} fields need attention.`} {failure && failure !== 'Some fields need attention.' ? failure : ''}
          </Notice>
        )}
        {failure && errCount === 0 && <Notice tone="negative">{failure}</Notice>}
      </div>

      <fieldset class="fieldset">
        <legend>what kind of request?</legend>
        <div class="choice-row" role="radiogroup" aria-label="Request type">
          <label class="choice">
            <input type="radio" id="rq-type-reimbursement" name="type" checked={f.type === 'reimbursement'} onChange={() => set('type', 'reimbursement')} />
            <span class="choice-title">pay me back</span>
            <span class="choice-sub">reimbursement — you already paid, attach the receipt</span>
          </label>
          <label class="choice">
            <input type="radio" id="rq-type-club" name="type" checked={f.type === 'club_purchase'} onChange={() => set('type', 'club_purchase')} />
            <span class="choice-title">club buys it</span>
            <span class="choice-sub">club purchase — CSEED orders it after approval</span>
          </label>
        </div>
      </fieldset>

      <fieldset class="fieldset">
        <legend>the item</legend>
        <div class="grid-fields">
          <Field id="rq-title" label="what is it?" error={errors.title} wide>
            <input class="input" {...describe('rq-title', errors.title)} value={f.title} maxLength={200} placeholder="e.g. Pizza for Hack Night" onInput={(e) => set('title', e.currentTarget.value)} />
          </Field>
          <Field id="rq-vendor" label="vendor" error={errors.vendor}>
            <input class="input" {...describe('rq-vendor', errors.vendor)} value={f.vendor} maxLength={200} placeholder="e.g. Costco" autoComplete="organization" onInput={(e) => set('vendor', e.currentTarget.value)} />
          </Field>
          <Field id="rq-url" label="item link" optional error={errors.itemUrl}>
            <input class="input" type="url" inputMode="url" {...describe('rq-url', errors.itemUrl)} value={f.itemUrl} placeholder="https://" onInput={(e) => set('itemUrl', e.currentTarget.value)} />
          </Field>
        </div>
        <div class="grid-fields grid-fields--3">
          <Field id="rq-qty" label="quantity" error={errors.quantity}>
            <input class="input input--money" type="number" min={1} max={10000} step={1} inputMode="numeric" {...describe('rq-qty', errors.quantity)} value={f.quantity} onInput={(e) => set('quantity', e.currentTarget.value)} />
          </Field>
          <Field id="rq-unit" label="unit cost" error={errors.unitCostCents}>
            <span class="money">
              <input class="input input--money" inputMode="decimal" {...describe('rq-unit', errors.unitCostCents)} value={f.unitCost} placeholder="0.00" onInput={(e) => set('unitCost', e.currentTarget.value)} />
            </span>
          </Field>
          <Field id="rq-tax" label="tax + shipping" error={errors.taxShipCents}>
            <span class="money">
              <input class="input input--money" inputMode="decimal" {...describe('rq-tax', errors.taxShipCents)} value={f.taxShip} placeholder="0.00" onInput={(e) => set('taxShip', e.currentTarget.value)} />
            </span>
          </Field>
        </div>
        <div class="total-line">
          <span class="muted">estimated total {total !== null && f.quantity !== '1' && <span class="small">({f.quantity} × {formatCents(dollarsToCents(f.unitCost) ?? 0)} + {formatCents(dollarsToCents(f.taxShip) ?? 0)})</span>}</span>
          <output for="rq-qty rq-unit rq-tax" aria-live="polite" data-total>
            {total === null ? '—' : formatCents(total)}
          </output>
        </div>
      </fieldset>

      <fieldset class="fieldset">
        <legend>where the money comes from</legend>
        <div class="grid-fields">
          <Field id="rq-account" label="budget category" error={errors.expenseAccountId} hint={acctHint ? `Form 990 ${acctHint}` : undefined}>
            <select class="select" {...describe('rq-account', errors.expenseAccountId, !!acctHint)} value={f.expenseAccountId} onChange={(e) => set('expenseAccountId', e.currentTarget.value)}>
              <option value="">choose…</option>
              {lookups.expenseAccounts.map((a) => (
                <option value={a.id} key={a.id}>
                  {a.code} · {a.name}
                </option>
              ))}
            </select>
          </Field>
          <Field id="rq-fund" label="fund" error={errors.fundId} hint="Grant funds can only pay for what the grant covers.">
            <select class="select" {...describe('rq-fund', errors.fundId, true)} value={f.fundId} onChange={(e) => set('fundId', e.currentTarget.value)}>
              <option value="">choose…</option>
              {lookups.funds.map((x) => (
                <option value={x.id} key={x.id}>
                  {x.name}
                  {x.restriction === 'with_donor' ? ' (restricted)' : ''}
                </option>
              ))}
            </select>
          </Field>
          <Field id="rq-event" label="event" optional error={errors.eventId}>
            <select class="select" {...describe('rq-event', errors.eventId)} value={f.eventId} onChange={(e) => set('eventId', e.currentTarget.value)}>
              <option value="">not for a specific event</option>
              {lookups.events.map((ev) => (
                <option value={ev.id} key={ev.id}>
                  {ev.name} — {fmtDate(ev.startsAt)}
                </option>
              ))}
            </select>
          </Field>
          <Field id="rq-class" label="functional class" error={errors.functionalClass} hint={FUNCTIONAL_HINT[f.functionalClass]}>
            <select class="select" {...describe('rq-class', errors.functionalClass, true)} value={f.functionalClass} onChange={(e) => set('functionalClass', e.currentTarget.value as FunctionalClass)}>
              {FUNCTIONAL_CLASSES.map((c) => (
                <option value={c} key={c}>
                  {FUNCTIONAL_LABEL[c]}
                </option>
              ))}
            </select>
          </Field>
          <Field id="rq-needed" label="needed by" optional error={errors.neededBy}>
            <input class="input" type="date" {...describe('rq-needed', errors.neededBy)} value={f.neededBy} onInput={(e) => set('neededBy', e.currentTarget.value)} />
          </Field>
        </div>
      </fieldset>

      <fieldset class="fieldset">
        <legend>why</legend>
        <Field id="rq-why" label="justification" error={errors.justification} hint="Who it's for, how many people, and why this vendor.">
          <textarea class="textarea" {...describe('rq-why', errors.justification, true)} value={f.justification} maxLength={4000} onInput={(e) => set('justification', e.currentTarget.value)} />
        </Field>
      </fieldset>

      <fieldset class="fieldset">
        <legend>receipts</legend>
        <Field
          id="rq-files"
          label={f.type === 'reimbursement' ? 'receipt files' : 'quote or receipt files'}
          optional={f.type !== 'reimbursement'}
          error={errors.files}
          hint={`PDF or photo, up to ${bytes(ATTACHMENT_MAX_BYTES)} each. ${f.type === 'reimbursement' ? 'Reimbursements are paid once a receipt is attached.' : 'You can add the receipt later from the request page.'}`}
        >
          <Dropzone onFiles={addFiles}>
            <input type="file" multiple accept={ATTACHMENT_MIME_TYPES.join(',')} {...describe('rq-files', errors.files, true)} onChange={(e) => {
              addFiles(e.currentTarget.files);
              e.currentTarget.value = '';
            }} />
          </Dropzone>
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
      </fieldset>

      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={!!busy}>
          {busy && busy !== 'saving…' ? busy : 'submit for approval'}
        </button>
        <button type="button" class="btn" disabled={!!busy} onClick={() => void save(false)}>
          {busy === 'saving…' ? busy : 'save as draft'}
        </button>
        <a href="/requests" class="small">
          cancel
        </a>
      </div>
    </form>
  );
}

export function Dropzone({ onFiles, children }: { onFiles: (f: FileList | null) => void; children: ComponentChildren }) {
  const [over, setOver] = useState(false);
  return (
    <div
      class={`dropzone${over ? ' is-over' : ''}`}
      onDragOver={(e) => {
        e.preventDefault();
        setOver(true);
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setOver(false);
        onFiles(e.dataTransfer?.files ?? null);
      }}
    >
      {children}
      <span class="small muted">or drop files here</span>
    </div>
  );
}
