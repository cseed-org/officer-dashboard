import { flattenError } from 'zod';
import { useState } from 'preact/hooks';
import { ServiceTokenCreateRequest, type ServiceToken } from '../../lib/contracts';
import { SERVICE_TOKEN_SCOPES, type ServiceTokenScope } from '../../lib/domain';
import { api } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { SCOPE_HUE } from '../../lib/colors';
import { fmtDate, fmtDateTime, relative } from '../../lib/format';
import { Drawer } from '../components/Drawer';
import { Badge, describe, Field } from '../components/kit';
import { DotLabel, RowMenu } from '../components/people';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';

const SCOPE_INFO: Record<ServiceTokenScope, string> = {
  'ingest:luma': 'push Luma events and guests (scraper)',
  'agent:jobs': 'claim and complete approved purchases (buying agent)',
};

const tokenState = (t: ServiceToken) =>
  t.revokedAt
    ? { tone: 'neutral' as const, label: 'revoked' }
    : t.expiresAt && new Date(t.expiresAt) < new Date()
      ? { tone: 'negative' as const, label: 'expired' }
      : { tone: 'positive' as const, label: 'active' };

/** Service tokens: list with scope dot-labels and a ⋯ menu; create in a drawer; the secret once, in a copy panel. */
export default function AdminTokensView({ creating, onCloseCreate }: { creating: boolean; onCloseCreate: () => void }) {
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
    <div class="stack stack--tight">
      {created && <NewSecret name={created.name} plaintext={created.plaintext} onDone={() => setCreated(null)} />}
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      <Async state={state} loading={<Loading rows={3} label="Loading tokens" />}>
        {(d) => {
          if (d.tokens.length === 0) return <Empty title="No service tokens">Create one when you set up the Luma scraper or the purchasing agent.</Empty>;
          const live = d.tokens.filter((t) => tokenState(t).label === 'active').length;
          return (
            <>
              <div class="toolbar">
                <span class="toolbar-count">
                  {live} active token{live === 1 ? '' : 's'}
                  {d.tokens.length > live && <span class="muted"> · {d.tokens.length - live} revoked or expired</span>}
                </span>
              </div>
              <div class="table-wrap">
                <table class="dt dt--people" data-tokens>
                  <caption class="visually-hidden">Service tokens</caption>
                  <thead>
                    <tr>
                      <th scope="col">token</th>
                      <th scope="col">permissions</th>
                      <th scope="col" class="people-seen">last used</th>
                      <th scope="col" class="people-seen">expires</th>
                      <th scope="col" class="people-menu">
                        <span class="visually-hidden">actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.tokens.map((t) => {
                      const st = tokenState(t);
                      return (
                        <tr key={t.id} class={st.label === 'active' ? undefined : 'is-inactive'} data-token={t.name}>
                          <td class="people-who-cell">
                            <span class="people-who">
                            <span class="people-text">
                              <span class="cell-title">
                                {t.name} <Badge tone={st.tone}>{st.label}</Badge>
                              </span>
                              <span class="cell-sub">
                                <span class="mono">{t.prefix}…</span> · by {t.createdBy.name}, {fmtDate(t.createdAt)}
                              </span>
                            </span>
                            </span>
                          </td>
                          <td class="people-role">
                            <span class="dot-labels">
                              {t.scopes.map((s) => (
                                <DotLabel hue={SCOPE_HUE[s] ?? 'blue'} key={s}>
                                  <span class="mono">{s}</span>
                                </DotLabel>
                              ))}
                            </span>
                          </td>
                          <td class="people-seen">
                            <span class="small muted" title={t.lastUsedAt ? fmtDateTime(t.lastUsedAt) : undefined}>
                              {t.lastUsedAt ? relative(t.lastUsedAt) : 'never used'}
                            </span>
                          </td>
                          <td class="people-seen">
                            <span class="small muted">{t.expiresAt ? fmtDate(t.expiresAt) : 'never'}</span>
                          </td>
                          <td class="people-menu">
                            <RowMenu label={`Actions for ${t.name}`} items={t.revokedAt ? [] : [{ label: 'revoke', danger: true, onSelect: () => void revoke(t) }]} />
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </>
          );
        }}
      </Async>

      <Drawer id="create-token" open={creating} onClose={onCloseCreate} title="create a service token">
        <CreateToken
          onCancel={onCloseCreate}
          onCreated={(t, plaintext) => {
            if (state.status === 'ready') state.set({ tokens: [t, ...state.data.tokens] });
            setCreated({ id: t.id, name: t.name, plaintext });
            setNotice(null);
            onCloseCreate();
            // After the drawer returns focus, move it to the one-time secret.
            setTimeout(() => document.getElementById('secret-h')?.focus(), 50);
          }}
        />
      </Drawer>
    </div>
  );
}

function NewSecret({ name, plaintext, onDone }: { name: string; plaintext: string; onDone: () => void }) {
  const [copied, setCopied] = useState(false);
  return (
    <section class="secret-panel" aria-labelledby="secret-h" role="alert">
      <div class="secret-panel-head">
        <h2 id="secret-h" tabIndex={-1}>
          copy “{name}” now
        </h2>
        <p class="small muted flush">This is the only time the token is shown. Store it in the agent’s secrets; CSEED keeps only a hash.</p>
      </div>
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

function CreateToken({ onCreated, onCancel }: { onCreated: (t: ServiceToken, plaintext: string) => void; onCancel: () => void }) {
  const [name, setName] = useState('');
  const [scopes, setScopes] = useState<ServiceTokenScope[]>([]);
  const [expires, setExpires] = useState('90');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  return (
    <form
      class="form form--stack"
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
          if (fe.name) document.getElementById('tk-name')?.focus();
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
      <Field id="tk-name" label="name" error={errors.name} hint="What will use it, e.g. Luma scraper (Pi).">
        <input class="input" {...describe('tk-name', errors.name, true)} value={name} maxLength={100} onInput={(e) => setName(e.currentTarget.value)} />
      </Field>
      <fieldset class={`fieldset${errors.scopes ? ' field--error' : ''}`} aria-describedby={errors.scopes ? 'tk-scopes-error' : undefined}>
        <legend class="field-label">permissions</legend>
        {SERVICE_TOKEN_SCOPES.map((s) => (
          <label class="check" key={s}>
            <input type="checkbox" checked={scopes.includes(s)} onChange={(e) => setScopes(e.currentTarget.checked ? [...scopes, s] : scopes.filter((x) => x !== s))} />
            <span>
              <DotLabel hue={SCOPE_HUE[s] ?? 'blue'}>
                <span class="mono">{s}</span>
              </DotLabel>
              <span class="muted small check-sub">{SCOPE_INFO[s]}</span>
            </span>
          </label>
        ))}
        {errors.scopes && (
          <p class="field-error" id="tk-scopes-error">
            {errors.scopes}
          </p>
        )}
      </fieldset>
      <Field id="tk-exp" label="expires">
        <select class="select" id="tk-exp" value={expires} onChange={(e) => setExpires(e.currentTarget.value)}>
          <option value="30">in 30 days</option>
          <option value="90">in 90 days</option>
          <option value="365">in a year</option>
          <option value="never">never</option>
        </select>
      </Field>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={busy}>
          {busy ? 'creating…' : 'create token'}
        </button>
        <button type="button" class="btn btn--text" onClick={onCancel}>
          cancel
        </button>
      </div>
    </form>
  );
}

