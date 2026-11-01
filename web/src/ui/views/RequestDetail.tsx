import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { ATTACHMENT_MAX_BYTES, ATTACHMENT_MIME_TYPES, type PurchaseRequestDetail } from '../../lib/contracts';
import type { PurchaseStatus } from '../../lib/domain';
import { api, endpointUrl, isStorageUnavailable, STORAGE_UNAVAILABLE_MESSAGE } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { describe, Field, Section, StatusBadge } from '../components/kit';
import { Empty, ErrorState, errorMessage, Loading, Notice } from '../components/states';
import { bytes, safeUrl, dollarsToCents, fmtDate, fmtDateTime, formatCents, FUNCTIONAL_LABEL, TYPE_LABEL } from '../../lib/format';
import { Dropzone } from './RequestForm';
import { href } from '../../lib/routes';
import { SettlementHoldNotice } from '../components/SettlementHoldNotice';

export interface Flash {
  created?: 'submitted' | 'draft' | null;
  uploadFailed?: number;
  storageOff?: boolean;
}

/** One-line summary under the drawer title. */
export function RequestSummary({ r }: { r: PurchaseRequestDetail }) {
  return (
    <p class="page-sub detail-sub">
      <span class="mono">#{r.number}</span> · {TYPE_LABEL[r.type]} · {r.requester.name} · {r.vendor} · <span class="mono">{formatCents(r.totalCents)}</span> <StatusBadge status={r.status} />
    </p>
  );
}

/**
 * Request detail (in the /finance slide-over): decision, settlement and other actions,
 * facts, receipts and the timeline. `onChanged` lets the list behind it refresh.
 */
export default function RequestDetail({
  id,
  meId,
  flash = {},
  onTitle,
  onChanged,
  onDeleted,
}: {
  id: string;
  meId: string;
  flash?: Flash;
  onTitle?: (title: string) => void;
  onChanged?: () => void;
  onDeleted?: () => void;
}) {
  const state = useApi('requestDetail', { params: { id } });
  const created = flash.created ?? null;
  const uploadFailed = flash.uploadFailed ?? 0;
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string; n: number } | null>(null);
  const statusRef = useRef<HTMLDivElement>(null);
  // Keyboard and screen-reader users land on the outcome: after a decision or other action,
  // and on arrival from the new-request form (flash.created).
  const ready = state.status === 'ready';
  const loadedTitle = ready ? state.data.request.title : null;
  useEffect(() => {
    if (loadedTitle) onTitle?.(loadedTitle);
  }, [loadedTitle]);
  useEffect(() => {
    if (ready && (notice || created || uploadFailed)) statusRef.current?.focus();
  }, [notice?.n, ready]);

  if (state.status === 'loading') return <Loading rows={8} label="Loading the request" />;
  if (state.status === 'error') return <ErrorState error={state.error} onRetry={state.reload} />;

  const r = state.data.request;
  const update = (next: PurchaseRequestDetail, text: string) => {
    state.set({ request: next });
    setNotice({ tone: 'positive', text, n: Date.now() });
    onChanged?.();
  };
  const onError = (err: unknown) => setNotice({ tone: 'negative', text: errorMessage(err), n: Date.now() });

  return (
    <div data-state="ready" data-request-detail={r.number}>
      <RequestSummary r={r} />

      <div class="stack">
        <SettlementHoldNotice hold={r.settlementHold} />
        {(created || uploadFailed > 0 || notice) && (
          <div class="notice-stack" ref={statusRef} tabIndex={-1} data-status-region>
            {created === 'submitted' && <Notice tone="positive">Submitted. A director has been notified and will review it.</Notice>}
            {created === 'draft' && <Notice tone="positive">Saved as a draft. Submit it when you are ready.</Notice>}
            {uploadFailed > 0 && (
              <Notice tone="negative">
                {uploadFailed} file{uploadFailed === 1 ? '' : 's'} didn't upload.{' '}
                {flash.storageOff ? STORAGE_UNAVAILABLE_MESSAGE : `Add ${uploadFailed === 1 ? 'it' : 'them'} again below.`}
              </Notice>
            )}
            {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
          </div>
        )}

        <Actions r={r} meId={meId} onDone={update} onError={onError} onDeleted={onDeleted} />

        <Section title="details" id="details">
          <dl class="facts">
            <Fact label="vendor">{r.vendor}</Fact>
            <Fact label="item link">
              {safeUrl(r.itemUrl) ? (
                <a class="data-link" href={safeUrl(r.itemUrl)!} target="_blank" rel="noopener noreferrer">
                  {hostOf(r.itemUrl!)} <span aria-hidden="true">↗</span>
                </a>
              ) : (
                '—'
              )}
            </Fact>
            <Fact label="quantity × unit cost">
              <span class="mono">
                {r.quantity} × {formatCents(r.unitCostCents)}
              </span>
            </Fact>
            <Fact label="tax + shipping">
              <span class="mono">{formatCents(r.taxShipCents)}</span>
            </Fact>
            <Fact label={r.actualTotalCents === null ? 'estimated total' : 'approved estimate'}>
              <span class="mono">{formatCents(r.totalCents)}</span>
            </Fact>
            {r.actualTotalCents !== null && (
              <Fact label="actually paid">
                <span class="mono">{formatCents(r.actualTotalCents)}</span>
                {r.actualTotalCents !== r.totalCents && (
                  <span class="cell-sub">
                    {r.actualTotalCents < r.totalCents ? `${formatCents(r.totalCents - r.actualTotalCents)} under` : `${formatCents(r.actualTotalCents - r.totalCents)} over`}
                  </span>
                )}
              </Fact>
            )}
            <Fact label="budget category">
              {r.expenseAccount.code} · {r.expenseAccount.name}
            </Fact>
            <Fact label="fund">{r.fund.name}</Fact>
            <Fact label="event">{r.event ? <a href={href.event(r.event.id)}>{r.event.name}</a> : '—'}</Fact>
            <Fact label="functional class">{FUNCTIONAL_LABEL[r.functionalClass]}</Fact>
            <Fact label="needed by">{fmtDate(r.neededBy)}</Fact>
            <Fact label="requested by">{r.requester.name}</Fact>
          </dl>
          <div>
            <p class="small muted flush">
              justification
            </p>
            <p class="prose-pre flush">{r.justification}</p>
          </div>
        </Section>

        <Attachments r={r} onUploaded={(next) => state.set({ request: next })} />

        <Section title="timeline" id="timeline">
          <Timeline r={r} />
          {r.journalEntryIds.length > 0 && (
            <p class="small muted">
              Posted to the ledger: <span class="mono">{r.journalEntryIds.join(', ')}</span>
            </p>
          )}
        </Section>
      </div>
    </div>
  );
}

/** GET /api/attachments/:id serves local files and redirects Drive files to their webViewLink. */
const attachmentHref = (a: { id: string; url: string | null }) => safeUrl(a.url ?? endpointUrl('attachmentDownload', { params: { id: a.id } }));

const hostOf = (u: string) => {
  try {
    return new URL(u).hostname.replace(/^www\./, '');
  } catch {
    return u;
  }
};

function Fact({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function Timeline({ r }: { r: PurchaseRequestDetail }) {
  type Item = { key: string; title: string; when: string | null; tone?: string; comment?: string | null; by?: string };
  const items: Item[] = [{ key: 'created', title: 'created as a draft', when: r.createdAt, by: r.requester.name }];
  if (r.submittedAt) items.push({ key: 'submitted', title: 'submitted for approval', when: r.submittedAt, by: r.requester.name, tone: 'attention' });
  for (const a of r.approvals)
    items.push({ key: a.id, title: a.decision === 'approved' ? 'approved' : 'denied', when: a.createdAt, by: a.director.name, comment: a.comment, tone: a.decision === 'approved' ? 'positive' : 'negative' });
  // Past-tense history labels, separate from STATUS_LABEL: the badge says where the request
  // is now ("awaiting payment"), the timeline says what already happened.
  const later: Partial<Record<PurchaseStatus, string>> = {
    fulfilling: 'purchase started',
    fulfilled: 'bought',
    reimbursed: 'payout approved',
    closed: r.type === 'reimbursement' ? 'reimbursement paid' : 'closed',
    cancelled: 'cancelled',
  };
  const order: PurchaseStatus[] = ['fulfilling', 'fulfilled', 'reimbursed', 'closed'];
  const reached = order.indexOf(r.status);
  if (reached >= 0) {
    for (const s of order.slice(0, reached + 1)) {
      if (s === 'fulfilling' && r.type !== 'club_purchase') continue;
      if ((s === 'fulfilled' && r.type !== 'club_purchase') || (s === 'reimbursed' && r.type !== 'reimbursement')) continue;
      const settled = s === 'fulfilled' || s === 'reimbursed';
      items.push({
        key: s,
        title: later[s]! + (settled && r.actualTotalCents !== null ? ` · ${formatCents(r.actualTotalCents)}` : ''),
        when: s === r.status ? r.updatedAt : null,
        tone: settled ? 'positive' : undefined,
      });
    }
  }
  if (r.status === 'cancelled') items.push({ key: 'cancelled', title: 'cancelled', when: r.updatedAt });
  const next: Partial<Record<PurchaseStatus, string>> = {
    draft: 'submit when ready',
    submitted: 'waiting for a director',
    approved: r.type === 'club_purchase' ? 'next: the club buys it' : 'next: reimbursement is paid',
    fulfilling: 'next: purchase confirmed with the receipt',
    fulfilled: 'next: close it out',
    reimbursed: 'next: a director confirms the reimbursement was paid',
  };
  return (
    <ol class="timeline">
      {items.map((i) => (
        <li key={i.key} class={i.tone ? `is-${i.tone}` : undefined}>
          <span class="timeline-title">{i.title}</span>
          <span class="timeline-meta">
            {[i.by, i.when ? fmtDateTime(i.when) : null].filter(Boolean).join(' · ')}
          </span>
          {i.comment && <p class="timeline-comment">“{i.comment}”</p>}
        </li>
      ))}
      {next[r.status] && (
        <li class="is-future">
          <span class="timeline-title muted">{next[r.status]}</span>
        </li>
      )}
    </ol>
  );
}

function Actions({
  r,
  meId,
  onDone,
  onError,
  onDeleted,
}: {
  r: PurchaseRequestDetail;
  meId: string;
  onDone: (r: PurchaseRequestDetail, msg: string) => void;
  onError: (e: unknown) => void;
  onDeleted?: (() => void) | undefined;
}) {
  const [comment, setComment] = useState('');
  const [commentError, setCommentError] = useState<string | undefined>();
  const [busy, setBusy] = useState<string | null>(null);
  const [actual, setActual] = useState(() => (r.totalCents / 100).toFixed(2));
  const [orderRef, setOrderRef] = useState('');
  const [actualError, setActualError] = useState<string | undefined>();
  const t = new Set(r.allowedTransitions);
  // Decisions only happen on a submitted request. (A request being bought also lists
  // `approved` — the agent's revert when buying fails — which is not a director decision.)
  const deciding = r.status === 'submitted';
  const canApprove = deciding && t.has('approved');
  const canDeny = deciding && t.has('denied');
  const canDecide = canApprove || canDeny;
  const settleTo = t.has('fulfilled') ? 'fulfilled' : t.has('reimbursed') ? 'reimbursed' : null;
  const mine = r.requester.id === meId;

  const run = async (label: string, fn: () => Promise<{ request: PurchaseRequestDetail }>, msg: string) => {
    setBusy(label);
    try {
      const res = await fn();
      setComment('');
      onDone(res.request, msg);
    } catch (e) {
      onError(e);
    } finally {
      setBusy(null);
    }
  };

  const visible = [...t].filter((s) => s !== 'approved' && s !== 'denied');
  if (!canDecide && !visible.length && !r.canEdit) {
    if (r.status === 'submitted' && mine) return <Notice tone="attention">Waiting for a director. You'll get a Discord ping when it's decided.</Notice>;
    return null;
  }

  return (
    <div class="stack stack--tight">
      {canDecide && (
        <section class="action-panel" aria-labelledby="decide-h">
          <h2 id="decide-h" tabIndex={-1}>your decision</h2>
          {mine && !canApprove && (
            <p class="small flush" data-self-approval>
              Another director must approve your own requests. You can still deny or cancel it.
            </p>
          )}
          <Field id="decision-comment" label="comment" optional={!commentError} error={commentError} hint="Required to deny. The requester sees it.">
            <textarea class="textarea" {...describe('decision-comment', commentError, true)} value={comment} maxLength={2000} onInput={(e) => setComment(e.currentTarget.value)} />
          </Field>
          <div class="action-row">
            {canApprove && (
              <button
                type="button"
                class="btn btn--primary"
                disabled={!!busy}
                onClick={() => {
                  if (!confirm(`Approve ${formatCents(r.totalCents)} for “${r.title}” from ${r.requester.name}? This commits the money in the budget.`)) return;
                  void run('approve', () => api('requestApprove', { params: { id: r.id }, body: { comment: comment.trim() || undefined } }), 'Approved. The requester has been notified.');
                }}
              >
                {busy === 'approve' ? 'approving…' : `approve ${formatCents(r.totalCents)}`}
              </button>
            )}
            {canDeny && (
              <button
                type="button"
                class="btn btn--danger"
                disabled={!!busy}
                onClick={() => {
                  if (!comment.trim()) {
                    setCommentError('Say why it is denied, so the requester can fix it.');
                    document.getElementById('decision-comment')?.focus();
                    return;
                  }
                  setCommentError(undefined);
                  void run('deny', () => api('requestDeny', { params: { id: r.id }, body: { comment: comment.trim() } }), 'Denied. The requester has been notified.');
                }}
              >
                {busy === 'deny' ? 'denying…' : 'deny'}
              </button>
            )}
          </div>
        </section>
      )}

      {r.status === 'fulfilling' && (
        <p class="small muted flush">
          Being bought. If the purchase fails, the purchasing agent puts the request back to approved; it doesn’t need approving again.
        </p>
      )}

      {settleTo && (
        <section class="action-panel" aria-labelledby="settle-h">
          <h2 id="settle-h">{settleTo === 'reimbursed' ? 'record the reimbursement' : 'record the purchase'}</h2>
          <p class="small muted flush">
            The ledger posts what was actually paid, which can differ from the {formatCents(r.totalCents)} estimate. Attach the receipt first.
          </p>
          <div class="grid-fields">
            <Field id="settle-actual" label="actual total" error={actualError}>
              <span class="money">
                <input class="input input--money" inputMode="decimal" {...describe('settle-actual', actualError)} value={actual} onInput={(e) => setActual(e.currentTarget.value)} />
              </span>
            </Field>
            <Field id="settle-ref" label="order / payment reference" optional>
              <input class="input" id="settle-ref" value={orderRef} maxLength={200} onInput={(e) => setOrderRef(e.currentTarget.value)} />
            </Field>
          </div>
          <div class="action-row">
            <button
              type="button"
              class="btn btn--primary"
              disabled={!!busy}
              onClick={() => {
                const cents = dollarsToCents(actual);
                if (cents === null || Number.isNaN(cents)) {
                  setActualError('Enter the amount paid, like 95.00.');
                  return;
                }
                setActualError(undefined);
                void run(
                  'settle',
                  () =>
                    api('requestTransition', {
                      params: { id: r.id },
                      body: { to: settleTo, actualTotalCents: cents, attachmentIds: r.attachments.map((a) => a.id), orderReference: orderRef.trim() || undefined },
                    }),
                  settleTo === 'reimbursed'
                    ? `Payout of ${formatCents(cents)} to ${r.requester.name} approved and posted to the ledger; it is awaiting payment.`
                    : `Marked bought and posted ${formatCents(cents)} to the ledger.`,
                );
              }}
            >
              {busy === 'settle' ? 'saving…' : settleTo === 'reimbursed' ? 'approve payout' : 'mark bought'}
            </button>
          </div>
        </section>
      )}

      {(t.has('submitted') || t.has('fulfilling') || t.has('closed') || t.has('cancelled') || r.canEdit) && (
        <div class="action-row" role="group" aria-label="Other actions">
          {t.has('submitted') && (
            <button type="button" class="btn btn--primary" disabled={!!busy} onClick={() => run('submit', () => api('requestSubmit', { params: { id: r.id } }), 'Submitted. A director has been notified.')}>
              {busy === 'submit' ? 'submitting…' : 'submit for approval'}
            </button>
          )}
          {t.has('fulfilling') && (
            <button type="button" class="btn" disabled={!!busy} onClick={() => run('fulfilling', () => api('requestTransition', { params: { id: r.id }, body: { to: 'fulfilling' } }), 'Marked as being purchased.')}>
              mark as buying
            </button>
          )}
          {t.has('closed') && (
            r.type === 'reimbursement' && r.status === 'reimbursed' ? (
              // reimbursed → closed records the cash payout to the officer.
              <button
                type="button"
                class="btn btn--primary"
                disabled={!!busy}
                onClick={() => {
                  const amount = formatCents(r.actualTotalCents ?? r.totalCents);
                  if (!confirm(`Confirm ${r.requester.name} has been paid ${amount}? This records the payout in the books and closes the request.`)) return;
                  void run('close', () => api('requestTransition', { params: { id: r.id }, body: { to: 'closed' } }), `Payout of ${amount} to ${r.requester.name} recorded. Request closed.`);
                }}
              >
                {busy === 'close' ? 'saving…' : 'confirm reimbursement paid'}
              </button>
            ) : (
              <button type="button" class="btn" disabled={!!busy} onClick={() => run('close', () => api('requestTransition', { params: { id: r.id }, body: { to: 'closed' } }), 'Closed.')}>
                close
              </button>
            )
          )}
          {t.has('cancelled') && (
            <button
              type="button"
              class="btn btn--danger"
              disabled={!!busy}
              onClick={() => {
                if (confirm('Cancel this request? This can’t be undone.')) void run('cancel', () => api('requestTransition', { params: { id: r.id }, body: { to: 'cancelled' } }), 'Cancelled.');
              }}
            >
              cancel request
            </button>
          )}
          {r.canEdit && (
            <button
              type="button"
              class="linkish linkish--danger"
              disabled={!!busy}
              onClick={async () => {
                if (!confirm('Delete this draft?')) return;
                try {
                  await api('requestDelete', { params: { id: r.id } });
                  if (onDeleted) onDeleted();
                  else location.assign(href.requests());
                } catch (e) {
                  onError(e);
                }
              }}
            >
              delete draft
            </button>
          )}
        </div>
      )}
    </div>
  );
}

function Attachments({ r, onUploaded }: { r: PurchaseRequestDetail; onUploaded: (r: PurchaseRequestDetail) => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const canUpload = !['denied', 'closed', 'cancelled'].includes(r.status);

  const upload = async (list: FileList | null) => {
    if (!list?.length) return;
    setError(null);
    const added = [];
    const problems: string[] = [];
    for (const [i, file] of Array.from(list).entries()) {
      if (!(ATTACHMENT_MIME_TYPES as readonly string[]).includes(file.type)) {
        problems.push(`${file.name}: use PDF, PNG, JPEG, HEIC or WebP`);
        continue;
      }
      if (file.size > ATTACHMENT_MAX_BYTES) {
        problems.push(`${file.name} is over ${bytes(ATTACHMENT_MAX_BYTES)}`);
        continue;
      }
      setBusy(`uploading ${i + 1} of ${list.length}…`);
      const form = new FormData();
      form.set('kind', 'receipt');
      form.set('file', file);
      try {
        added.push((await api('requestAttach', { params: { id: r.id }, body: form })).attachment);
      } catch (e) {
        if (isStorageUnavailable(e)) {
          problems.length = 0;
          problems.push(STORAGE_UNAVAILABLE_MESSAGE);
          break;
        }
        problems.push(`${file.name}: ${errorMessage(e)}`);
      }
    }
    setBusy(null);
    if (problems.length) setError(problems.join('. '));
    if (added.length) onUploaded({ ...r, attachments: [...r.attachments, ...added] });
  };

  return (
    <Section title="receipts & files" id="files">
      <p class="small muted flush">
        Receipts are filed in CSEED’s shared Google Drive, and the links open there. You need access to that drive to view them; ask a
        director if Drive says you don’t have permission.
      </p>
      {r.attachments.length === 0 ? (
        <Empty title="No files yet">{r.type === 'reimbursement' ? 'Attach the receipt so the reimbursement can be paid.' : 'Attach the quote or the receipt once it is bought.'}</Empty>
      ) : (
        <ul class="files">
          {r.attachments.map((a) => (
            <li key={a.id}>
              <span class="file-name">
                {attachmentHref(a) ? (
                  <a href={attachmentHref(a)!} target="_blank" rel="noopener noreferrer">
                    {a.filename}
                  </a>
                ) : (
                  a.filename
                )}
              </span>
              <span class="file-meta">
                {a.kind} · {bytes(a.sizeBytes)} · {fmtDate(a.createdAt)}
              </span>
            </li>
          ))}
        </ul>
      )}
      {error && <Notice tone="negative">{error}</Notice>}
      {canUpload && (
        <Field id="att-files" label="add files" hint={busy ?? `PDF or photo, up to ${bytes(ATTACHMENT_MAX_BYTES)} each`}>
          <Dropzone onFiles={(f) => void upload(f)}>
            <input
              type="file"
              multiple
              accept={ATTACHMENT_MIME_TYPES.join(',')}
              {...describe('att-files', undefined, true)}
              disabled={!!busy}
              onChange={(e) => {
                void upload(e.currentTarget.files);
                e.currentTarget.value = '';
              }}
            />
          </Dropzone>
        </Field>
      )}
    </Section>
  );
}
