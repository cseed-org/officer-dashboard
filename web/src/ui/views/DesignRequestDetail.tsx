import { useEffect, useState } from 'preact/hooks';
import { api, isStorageUnavailable, STORAGE_UNAVAILABLE_MESSAGE } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import {
  ATTACHMENT_MAX_BYTES,
  ATTACHMENT_MIME_TYPES,
  type CurrentUser,
  type DesignFile,
  type DesignFileKind,
  type DesignRequestDetail as Detail,
  type DesignRequestResponse,
  type DesignStage,
  type UserSummary,
} from '../../lib/contracts';
import { dueState, firstName, fmtDueLong, STAGE_HUE, STAGE_LABEL, TYPE_LABEL } from '../../lib/design';
import { bytes, fmtDateTime } from '../../lib/format';
import { href } from '../../lib/routes';
import { safeUrl } from '../../lib/url';
import { Badge, describe, Field } from '../components/kit';
import { errorMessage, ErrorState, Loading, Notice } from '../components/states';
import DesignRequestForm, { type Created } from './DesignRequestForm';

const KIND_LABEL: Record<DesignFileKind, string> = { reference: 'Reference', review: 'Review version', delivered: 'Delivered file' };

export function StageBadge({ stage }: { stage: DesignStage }) {
  return (
    <Badge tone="neutral" hue={STAGE_HUE[stage]}>
      <span data-stage={stage}>{STAGE_LABEL[stage]}</span>
    </Badge>
  );
}

/** The request drawer: brief, files, approval, designer actions and comments. */
export default function DesignRequestDetail({
  id,
  me,
  onChanged,
  onTitle,
  onDeleted,
}: {
  id: string;
  me: CurrentUser;
  onChanged: () => void;
  onTitle: (t: string) => void;
  onDeleted: (title: string) => void;
}) {
  const state = useApi('designRequestDetail', { params: { id } });
  const title = state.status === 'ready' ? state.data.request.title : null;
  useEffect(() => {
    if (title) onTitle(title);
  }, [title]);
  if (state.status === 'loading') return <Loading rows={8} label="Loading the request" />;
  if (state.status === 'error') return <ErrorState error={state.error} onRetry={state.reload} />;
  return (
    <Body
      data={state.data}
      me={me}
      onUpdate={(d) => {
        state.set(d);
        onChanged();
      }}
      onDeleted={onDeleted}
    />
  );
}

function Body({ data, me, onUpdate, onDeleted }: { data: DesignRequestResponse; me: CurrentUser; onUpdate: (d: DesignRequestResponse) => void; onDeleted: (title: string) => void }) {
  const r = data.request;
  const p = r.permissions;
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(null);
  const [editing, setEditing] = useState(false);

  const run = async (label: string, fn: () => Promise<DesignRequestResponse>, done: string) => {
    setBusy(label);
    setMessage(null);
    try {
      onUpdate(await fn());
      setMessage({ tone: 'positive', text: done });
    } catch (err) {
      setMessage({ tone: 'negative', text: errorMessage(err) });
    } finally {
      setBusy(null);
    }
  };

  if (editing) {
    return (
      <DesignRequestForm
        editing={r}
        onCancel={() => setEditing(false)}
        onDone={({ request, uploadFailed, storageOff }: Created) => {
          setEditing(false);
          onUpdate({ request, designers: data.designers });
          setMessage(uploadFailed ? { tone: 'negative', text: storageOff ? STORAGE_UNAVAILABLE_MESSAGE : `Saved, but ${uploadFailed} file(s) didn't upload.` } : { tone: 'positive', text: 'Changes saved.' });
        }}
      />
    );
  }

  const files = (k: DesignFileKind) => r.files.filter((f) => f.kind === k);
  const due = dueState(r);
  const requesterIsMe = r.requester.id === me.id;

  return (
    <div class="stack" data-state="ready" data-design-detail={r.id} data-stage={r.stage}>
      <div class="detail-meta">
        <StageBadge stage={r.stage} />
        <p class="page-sub">
          Requested by {requesterIsMe ? `${r.requester.name} (you)` : r.requester.name} · {r.assignee ? `Assigned to ${r.assignee.id === me.id ? `${r.assignee.name} (you)` : r.assignee.name}` : 'Unassigned'}
        </p>
      </div>

      {message && (
        <Notice tone={message.tone === 'positive' ? 'positive' : 'negative'}>
          {message.text}
        </Notice>
      )}

      <dl class="facts">
        <div>
          <dt>Type</dt>
          <dd>{TYPE_LABEL[r.type]}</dd>
        </div>
        <div>
          <dt>Due / urgency</dt>
          <dd>
            {r.dueDate ? fmtDueLong(r.dueDate) : 'No due date'} · {r.urgency === 'high' ? 'High' : 'Normal'}
            {due === 'overdue' && <span class="kanban-due is-overdue"> · Overdue</span>}
            {due === 'soon' && <span class="kanban-due is-soon"> · Due soon</span>}
          </dd>
        </div>
        <div>
          <dt>{r.event ? 'Event' : 'Program'}</dt>
          <dd>{r.event ? <a href={href.event(r.event.id)}>{r.event.name}</a> : r.program ?? 'Not linked'}</dd>
        </div>
        <div>
          <dt>Formats</dt>
          <dd>{r.formats || 'Not specified'}</dd>
        </div>
      </dl>

      {(p.canEdit || p.canDelete) && (
        <div class="design-actions">
          {p.canEdit && (
            <button type="button" class="btn btn--small" onClick={() => setEditing(true)} disabled={!!busy}>
              Edit request
            </button>
          )}
          {p.canDelete && (
            <button
              type="button"
              class="btn btn--small btn--danger"
              disabled={!!busy}
              onClick={async () => {
                if (!confirm(r.stage === 'new' && requesterIsMe ? `Withdraw “${r.title}”?` : `Delete “${r.title}” and its files and comments?`)) return;
                setBusy('Deleting…');
                try {
                  await api('designRequestDelete', { params: { id: r.id } });
                  onDeleted(r.title);
                } catch (err) {
                  setBusy(null);
                  setMessage({ tone: 'negative', text: errorMessage(err) });
                }
              }}
            >
              {r.stage === 'new' && requesterIsMe ? 'Withdraw request' : 'Delete request'}
            </button>
          )}
        </div>
      )}

      <section class="stack stack--tight" aria-labelledby="dr-brief-h">
        <h3 id="dr-brief-h">Brief</h3>
        <p class="design-prose">{r.brief}</p>
      </section>

      <section class="stack stack--tight" aria-labelledby="dr-refs-h">
        <h3 id="dr-refs-h">References & attachments</h3>
        {r.references ? <p class="design-prose">{r.references}</p> : files('reference').length === 0 && <p class="muted">No references yet.</p>}
        <FileList files={files('reference')} label="Reference files" />
      </section>

      {(files('review').length > 0 || r.stage === 'in-review') && (
        <section class="stack stack--tight" aria-labelledby="dr-review-h">
          <h3 id="dr-review-h">Review files</h3>
          {files('review').length ? <FileList files={files('review')} label="Review files" /> : <p class="muted">The designer hasn't attached a version yet; check the comments.</p>}
        </section>
      )}

      {r.stage === 'in-review' &&
        (p.canApprove ? (
          <div class="stack stack--tight" data-approval>
            <Notice tone="info">This version is waiting for your approval. Approving marks it delivered.</Notice>
            <div class="design-actions">
              <button type="button" class="btn btn--primary" disabled={!!busy} onClick={() => run('Approving…', () => api('designRequestApprove', { params: { id: r.id } }), 'Approved and delivered. Thank you!')}>
                {busy === 'Approving…' ? busy : 'Approve this version'}
              </button>
              {p.canRequestChanges && (
                <button type="button" class="btn" disabled={!!busy} onClick={() => run('Sending back…', () => api('designRequestMove', { params: { id: r.id }, body: { stage: 'in-progress' } }), 'Sent back to In progress. Add a comment with what to change.')}>
                  Request changes
                </button>
              )}
            </div>
          </div>
        ) : (
          <p class="notice notice--info" data-approval>
            Waiting for {firstName(r.requester.name)}, the requester, to approve this version. Approval is available only to the requester while in review.
          </p>
        ))}

      {r.stage === 'delivered' && r.approvedBy && r.approvedAt && <Notice tone="positive">Approved by {r.approvedBy.name} · {fmtDateTime(r.approvedAt)}</Notice>}
      {(r.stage === 'delivered' || files('delivered').length > 0) && (
        <section class="stack stack--tight" aria-labelledby="dr-delivered-h">
          <h3 id="dr-delivered-h">Delivered files</h3>
          {files('delivered').length ? <FileList files={files('delivered')} label="Delivered files" /> : <p class="muted">No delivered files attached.</p>}
        </section>
      )}

      {(p.canAssign || p.canAssignSelf) && <DesignerActions r={r} designers={data.designers} me={me} busy={!!busy} run={run} onStep={onUpdate} />}

      {p.uploadKinds.length > 0 && <Upload r={r} kinds={p.uploadKinds} onUpdate={onUpdate} />}

      <Comments r={r} me={me} onUpdate={onUpdate} />
    </div>
  );
}

function FileList({ files, label }: { files: DesignFile[]; label: string }) {
  if (!files.length) return null;
  return (
    <ul class="files" aria-label={label}>
      {files.map((f) => (
        <li key={f.id}>
          <a class="file-name" href={safeUrl(f.url) ?? '#'} target={f.url.startsWith('http') ? '_blank' : undefined} rel="noopener noreferrer" download={f.url.startsWith('/') ? f.filename : undefined}>
            {f.filename}
          </a>
          <span class="file-meta">
            {KIND_LABEL[f.kind]} · {bytes(f.sizeBytes)} · {f.uploadedBy.name}, {fmtDateTime(f.createdAt)}
          </span>
        </li>
      ))}
    </ul>
  );
}

type Run = (label: string, fn: () => Promise<DesignRequestResponse>, done: string) => Promise<void>;

/** Assignee + stage: the keyboard/touch alternative to dragging (and the only path on phones). */
function DesignerActions({ r, designers, me, busy, run, onStep }: { r: Detail; designers: UserSummary[]; me: CurrentUser; busy: boolean; run: Run; onStep: (d: DesignRequestResponse) => void }) {
  const p = r.permissions;
  const [assignee, setAssignee] = useState(r.assignee?.id ?? '');
  const [stage, setStage] = useState<DesignStage>(r.stage);
  useEffect(() => {
    setAssignee(r.assignee?.id ?? '');
    setStage(r.stage);
  }, [r.assignee?.id, r.stage]);

  const options = designers.filter((d) => p.canAssign || d.id === me.id || d.id === r.assignee?.id);
  const changed = assignee !== (r.assignee?.id ?? '') || stage !== r.stage;
  const meDesigner = designers.some((d) => d.id === me.id);
  const current = r.assignee?.id ?? '';
  // Past New a card keeps its designer, so "Unassigned + New" moves first, then unassigns.
  const unassignAfterMove = !assignee && current !== '' && stage === 'new' && r.stage !== 'new';
  const apply = () => {
    const done = [
      assignee !== current ? (assignee ? `Assigned to ${designers.find((d) => d.id === assignee)?.name ?? 'the designer'}` : 'Unassigned') : '',
      stage !== r.stage ? `Moved to ${STAGE_LABEL[stage]}` : '',
    ].filter(Boolean);
    return run(
      'Updating…',
      async () => {
        let res: DesignRequestResponse | null = null;
        // Show each step that landed even if a later one fails.
        const step = async (p: Promise<DesignRequestResponse>) => {
          res = await p;
          onStep(res);
        };
        if (assignee !== current && !unassignAfterMove) await step(api('designRequestAssign', { params: { id: r.id }, body: { assigneeId: assignee || null } }));
        if (stage !== r.stage) await step(api('designRequestMove', { params: { id: r.id }, body: { stage } }));
        if (unassignAfterMove) await step(api('designRequestAssign', { params: { id: r.id }, body: { assigneeId: null } }));
        return res!;
      },
      `${done.join('. ')}.`,
    );
  };

  return (
    <section class="design-worker" aria-labelledby="dr-worker-h" data-designer-actions>
      <h3 id="dr-worker-h">Designer actions</h3>
      <div class="grid-fields">
        <Field id="dr-assignee" label="Assignee" hint={stage !== 'new' && !assignee ? (current ? 'Work past New keeps a designer: move it to New to unassign.' : 'Work past New needs a designer; moving it assigns you.') : undefined}>
          <select class="select" {...describe('dr-assignee', undefined, stage !== 'new' && !assignee)} aria-label="Assignee" value={assignee} disabled={busy || (!p.canAssign && !p.canAssignSelf)} onChange={(e) => setAssignee(e.currentTarget.value)}>
            <option value="">Unassigned</option>
            {options.map((d) => (
              <option value={d.id} key={d.id}>
                {d.name}
                {d.id === me.id ? ' (self-assign)' : ''}
              </option>
            ))}
          </select>
        </Field>
        <Field id="dr-stage" label="Move to">
          <select class="select" id="dr-stage" aria-label="Move to" value={stage} disabled={busy || p.moveTo.length === 0} onChange={(e) => setStage(e.currentTarget.value as DesignStage)}>
            <option value={r.stage}>{STAGE_LABEL[r.stage]} (current)</option>
            {p.moveTo.map((s) => (
              <option value={s} key={s}>
                {STAGE_LABEL[s]}
              </option>
            ))}
          </select>
        </Field>
      </div>
      <div class="design-actions">
        <button type="button" class="btn btn--primary" disabled={busy || !changed || (stage !== 'new' && !assignee && (current !== '' || !meDesigner))} onClick={() => void apply()}>
          Update request
        </button>
        {p.canAssignSelf && !changed && (
          <button type="button" class="btn" disabled={busy} onClick={() => run('Assigning…', () => api('designRequestAssign', { params: { id: r.id }, body: { assigneeId: me.id } }), 'Assigned to you.')}>
            Assign to me
          </button>
        )}
      </div>
      <p class="small muted">Designers self-assign and move work. Delivery follows the requester's approval.</p>
    </section>
  );
}

function Upload({ r, kinds, onUpdate }: { r: Detail; kinds: DesignFileKind[]; onUpdate: (d: DesignRequestResponse) => void }) {
  const [kind, setKind] = useState<DesignFileKind>(kinds.includes('review') && r.stage !== 'delivered' ? (r.stage === 'in-review' || r.stage === 'in-progress' ? 'review' : 'reference') : kinds[0]!);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const upload = async (list: FileList | null) => {
    if (!list?.length) return;
    setBusy(true);
    setMsg(null);
    let ok = 0;
    for (const file of Array.from(list)) {
      if (!(ATTACHMENT_MIME_TYPES as readonly string[]).includes(file.type) || file.size > ATTACHMENT_MAX_BYTES) {
        setMsg({ ok: false, text: `${file.name}: use PDF, PNG, JPEG, WebP or HEIC up to ${bytes(ATTACHMENT_MAX_BYTES)}.` });
        continue;
      }
      const form = new FormData();
      form.set('kind', kind);
      form.set('file', file);
      try {
        onUpdate(await api('designRequestUpload', { params: { id: r.id }, body: form }));
        ok++;
      } catch (err) {
        setMsg({ ok: false, text: isStorageUnavailable(err) ? STORAGE_UNAVAILABLE_MESSAGE : `${file.name}: ${errorMessage(err)}` });
        if (isStorageUnavailable(err)) break;
      }
    }
    setBusy(false);
    if (ok) setMsg((m) => m ?? { ok: true, text: `${ok === 1 ? 'File' : `${ok} files`} added.` });
  };
  return (
    <section class="stack stack--tight" aria-labelledby="dr-upload-h">
      <h3 id="dr-upload-h">Add files</h3>
      <div class="design-upload">
        {kinds.length > 1 && (
          <Field id="dr-upload-kind" label="File type">
            <select class="select" id="dr-upload-kind" value={kind} onChange={(e) => setKind(e.currentTarget.value as DesignFileKind)}>
              {kinds.map((k) => (
                <option value={k} key={k}>
                  {KIND_LABEL[k]}
                </option>
              ))}
            </select>
          </Field>
        )}
        <Field id="dr-upload-file" label="Choose files" hint={`PDF, PNG, JPEG, WebP or HEIC · up to ${bytes(ATTACHMENT_MAX_BYTES)} each`}>
          <input
            class="input"
            type="file"
            multiple
            accept={ATTACHMENT_MIME_TYPES.join(',')}
            {...describe('dr-upload-file', undefined, true)}
            disabled={busy}
            onChange={(e) => {
              void upload(e.currentTarget.files);
              e.currentTarget.value = '';
            }}
          />
        </Field>
      </div>
      {busy && <p class="small muted" role="status">Uploading…</p>}
      {msg && <Notice tone={msg.ok ? 'positive' : 'negative'}>{msg.text}</Notice>}
    </section>
  );
}

function Comments({ r, me, onUpdate }: { r: Detail; me: CurrentUser; onUpdate: (d: DesignRequestResponse) => void }) {
  const [body, setBody] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>();
  return (
    <section class="stack stack--tight" aria-labelledby="dr-comments-h">
      <h3 id="dr-comments-h">Comments</h3>
      {r.comments.length ? (
        <ol class="timeline" aria-label="Comments">
          {r.comments.map((c) => (
            <li key={c.id}>
              <span class="timeline-title">
                {c.author.id === me.id ? `${c.author.name} (you)` : c.author.name}
                {c.author.id === r.requester.id ? ' · requester' : c.author.id === r.assignee?.id ? ' · designer' : ''}
              </span>
              <span class="timeline-meta">{fmtDateTime(c.createdAt)}</span>
              <p class="design-prose">{c.body}</p>
            </li>
          ))}
        </ol>
      ) : (
        <p class="muted">No comments yet.</p>
      )}
      <form
        class="form"
        noValidate
        onSubmit={async (e) => {
          e.preventDefault();
          if (!body.trim()) {
            setError('Write a comment first.');
            document.getElementById('dr-comment')?.focus();
            return;
          }
          setBusy(true);
          setError(undefined);
          try {
            onUpdate(await api('designRequestComment', { params: { id: r.id }, body: { body } }));
            setBody('');
          } catch (err) {
            setError(errorMessage(err)); // the text stays in the box
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field id="dr-comment" label="Add a comment" error={error}>
          <textarea class="textarea" rows={3} {...describe('dr-comment', error)} value={body} maxLength={4000} onInput={(e) => setBody(e.currentTarget.value)} />
        </Field>
        <div class="form-actions">
          <button type="submit" class="btn" disabled={busy}>
            {busy ? 'Posting…' : 'Post comment'}
          </button>
        </div>
      </form>
    </section>
  );
}
