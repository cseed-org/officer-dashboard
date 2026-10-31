import { flattenError } from 'zod';
import { useState } from 'preact/hooks';
import { ServiceTokenCreateRequest, type ServiceToken } from '../../core/contracts';
import { SERVICE_TOKEN_SCOPES, type ServiceTokenScope } from '../../core/domain';
import { api } from '../api/client';
import { useApi } from '../api/useApi';
import { Badge, DataTable, describe, Field, Section } from '../components/kit';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';
import { fmtDate, relative } from '../format';

const SCOPE_INFO: Record<ServiceTokenScope, string> = {
  'ingest:luma': 'push Luma events and guests (scraper)',
  'agent:jobs': 'claim and complete approved purchases (buying agent)',
};

const tokenState = (t: ServiceToken) =>
  t.revokedAt ? { tone: 'neutral' as const, label: 'revoked' } : t.expiresAt && new Date(t.expiresAt) < new Date() ? { tone: 'negative' as const, label: 'expired' } : { tone: 'positive' as const, label: 'active' };

export default function AdminTokensView() {
  const state = useApi('adminTokens');
  const [created, setCreated] = useState<{ id: string; name: string; plaintext: string } | null>(null);
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(null);

  const revoke = async (t: ServiceToken) => {
    if (!confirm(`Revoke “${t.name}”? Anything using it stops working immediately.`)) return;
    try {
      await api('adminTokenRevoke', { params: { id: t.id } });
      if (state.status === 'ready') state.set({ tokens: state.data.tokens.map((x) => (x.id === t.id ? { ...x, revokedAt: new Date().toISOString() } : x)) });
      // A revoked token's secret must not stay on screen.
      if (created?.id === t.id) setCreated(null);
      setNotice({ tone: 'positive', text: `“${t.name}” is revoked.` });
    } catch (e) {
      setNotice({ tone: 'negative', text: errorMessage(e) });
    }
  };

  return (
    <div class="stack">
      {created && <NewSecret name={created.name} plaintext={created.plaintext} onDone={() => setCreated(null)} />}
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      <Section title="tokens" id="tokens">
        <Async state={state} loading={<Loading rows={3} label="Loading tokens" />}>
          {(d) =>
            d.tokens.length === 0 ? (
              <Empty title="No service tokens">Create one when you set up the Luma scraper or the purchasing agent.</Empty>
            ) : (
              <DataTable
                caption="Service tokens"
                rows={d.tokens}
                rowKey={(t) => t.id}
                columns={[
                  {
                    key: 'n',
                    label: 'name',
                    primary: true,
                    render: (t) => (
                      <>
                        <span class="cell-title">{t.name}</span>
                        <span class="cell-sub">
                          <span class="mono">{t.prefix}…</span> · by {t.createdBy.name} {fmtDate(t.createdAt)}
                        </span>
                      </>
                    ),
                  },
                  { key: 's', label: 'scopes', render: (t) => <span class="mono small">{t.scopes.join(', ')}</span> },
                  { key: 'st', label: 'status', render: (t) => <Badge tone={tokenState(t).tone}>{tokenState(t).label}</Badge> },
                  { key: 'u', label: 'last used', hideOnPhone: true, render: (t) => <span class="small">{relative(t.lastUsedAt)}</span> },
                  { key: 'e', label: 'expires', render: (t) => <span class="small">{t.expiresAt ? fmtDate(t.expiresAt) : 'never'}</span> },
                  {
                    key: 'a',
                    label: '',
                    cardLabel: '',
                    render: (t) =>
                      t.revokedAt ? null : (
                        <button type="button" class="linkish linkish--danger" onClick={() => void revoke(t)}>
                          revoke
                        </button>
                      ),
                  },
                ]}
              />
            )
          }
        </Async>
      </Section>
      <Section title="create a token" id="create-token">
        <CreateToken
          onCreated={(t, plaintext) => {
            if (state.status === 'ready') state.set({ tokens: [t, ...state.data.tokens] });
            setCreated({ id: t.id, name: t.name, plaintext });
            setNotice(null);
            scrollTo({ top: 0, behavior: 'smooth' });
          }}
        />
      </Section>
    </div>
  );
}

function NewSecret({ name, plaintext, onDone }: { name: string; plaintext: string; onDone: () => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <section class="action-panel" aria-labelledby="secret-h" role="alert">
      <h2 id="secret-h">copy “{name}” now</h2>
      <p class="small" style={{ margin: 0 }}>
        This is the only time the token is shown. Store it in the agent’s secrets; CSEED keeps only a hash.
      </p>
      <div class="secret">
        <code data-secret>{plaintext}</code>
        <button
          type="button"
          class="btn btn--primary"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(plaintext);
              setCopied(true);
            } catch {
              setCopied(false);
            }
          }}
        >
          {copied ? 'copied ✓' : 'copy'}
        </button>
        <button type="button" class="btn" onClick={onDone}>
          done
        </button>
      </div>
    </section>
  );
}

function CreateToken({ onCreated }: { onCreated: (t: ServiceToken, plaintext: string) => void }) {
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<ServiceTokenScope[]>([]);
  const [expires, setExpires] = useState('90');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <form
      class="form"
      noValidate
      onSubmit={async (e) => {
        e.preventDefault();
        setError(null);
        const parsed = ServiceTokenCreateRequest.safeParse({ name, scopes, expiresInDays: expires === 'never' ? null : Number(expires) });
        if (!parsed.success) {
          const fe = flattenError(parsed.error).fieldErrors as Record<string, string[] | undefined>;
          setErrors({
            ...(fe.name ? { name: 'Name it after what will use it.' } : {}),
            ...(fe.scopes ? { scopes: 'Pick at least one permission.' } : {}),
          });
          return;
        }
        setErrors({});
        setBusy(true);
        try {
          const res = await api('adminTokenCreate', { body: parsed.data });
          onCreated(res.token, res.plaintext);
          setName('');
          setScopes([]);
        } catch (err) {
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <div class="grid-fields">
        <Field id="tk-name" label="name" error={errors.name} hint="e.g. Luma scraper (Pi)">
          <input class="input" {...describe('tk-name', errors.name, true)} value={name} maxLength={100} onInput={(e) => setName(e.currentTarget.value)} />
        </Field>
        <Field id="tk-exp" label="expires">
          <select class="select" id="tk-exp" value={expires} onChange={(e) => setExpires(e.currentTarget.value)}>
            <option value="30">in 30 days</option>
            <option value="90">in 90 days</option>
            <option value="365">in a year</option>
            <option value="never">never</option>
          </select>
        </Field>
      </div>
      <fieldset class={`fieldset field${errors.scopes ? ' field--error' : ''}`} aria-describedby={errors.scopes ? 'tk-scopes-error' : undefined}>
        <legend class="field-label" style={{ font: 'inherit', fontSize: 'var(--size-sm)', fontWeight: 'var(--weight-body-strong)', border: 0, padding: 0, margin: 0 }}>
          permissions
        </legend>
        {SERVICE_TOKEN_SCOPES.map((s) => (
          <label class="check" key={s}>
            <input type="checkbox" checked={scopes.includes(s)} onChange={(e) => setScopes(e.currentTarget.checked ? [...scopes, s] : scopes.filter((x) => x !== s))} />
            <span>
              <span class="mono">{s}</span> <span class="muted small">— {SCOPE_INFO[s]}</span>
            </span>
          </label>
        ))}
        {errors.scopes && (
          <p class="field-error" id="tk-scopes-error">
            {errors.scopes}
          </p>
        )}
      </fieldset>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={busy}>
          {busy ? 'creating…' : 'create token'}
        </button>
      </div>
    </form>
  );
}
