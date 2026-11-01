import { flattenError } from 'zod';
import { useState } from 'preact/hooks';
import { AdminUserCreateRequest, type AdminUser } from '../../lib/contracts';
import { ROLES, type Role } from '../../lib/domain';
import { api, fieldErrors } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { Badge, DataTable, describe, Field, Section } from '../components/kit';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';
import { relative } from '../../lib/format';

export default function AdminUsersView({ meId }: { meId: string }) {
  const state = useApi('adminUsers');
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);

  const patch = async (u: AdminUser, body: { role?: Role; active?: boolean }, msg: string) => {
    if (state.status !== 'ready') return;
    setBusyId(u.id);
    setNotice(null);
    try {
      const { user } = await api('adminUserUpdate', { params: { id: u.id }, body });
      state.set({ users: state.data.users.map((x) => (x.id === user.id ? user : x)) });
      setNotice({ tone: 'positive', text: msg });
    } catch (e) {
      setNotice({ tone: 'negative', text: errorMessage(e) });
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div class="stack">
      {notice && <Notice tone={notice.tone}>{notice.text}</Notice>}
      <Section title="officers" id="officers">
        <Async state={state} loading={<Loading rows={5} label="Loading officers" />}>
          {(d) => {
            const users = [...d.users].sort((a, b) => Number(b.active) - Number(a.active) || a.name.localeCompare(b.name));
            if (users.length <= 1)
              return (
                <>
                  <Empty title="Only you so far">Add officers below by their Discord user id. They can sign in with Discord right away.</Empty>
                  <UserTable users={users} meId={meId} busyId={busyId} patch={patch} />
                </>
              );
            return <UserTable users={users} meId={meId} busyId={busyId} patch={patch} />;
          }}
        </Async>
      </Section>
      <Section title="add an officer" id="add-officer">
        <AddUser
          onAdded={(u) => {
            if (state.status === 'ready') state.set({ users: [...state.data.users, u] });
            setNotice({ tone: 'positive', text: `${u.name} can now sign in with Discord.` });
          }}
        />
      </Section>
    </div>
  );
}

function UserTable({
  users,
  meId,
  busyId,
  patch,
}: {
  users: AdminUser[];
  meId: string;
  busyId: string | null;
  patch: (u: AdminUser, body: { role?: Role; active?: boolean }, msg: string) => void;
}) {
  return (
    <DataTable
      caption="Officers"
      rows={users}
      rowKey={(u) => u.id}
      columns={[
        {
          key: 'n',
          label: 'name',
          primary: true,
          render: (u) => (
            <>
              <span class="cell-title">
                {u.name}
                {u.id === meId && <span class="muted small"> (you)</span>}
              </span>
              <span class="cell-sub">
                {u.title ?? 'no title'} · <span class="mono">{u.discordId}</span>
              </span>
            </>
          ),
        },
        {
          key: 'r',
          label: 'role',
          render: (u) => (
            <>
              <label class="visually-hidden" for={`role-${u.id}`}>
                Role for {u.name}
              </label>
              <select
                id={`role-${u.id}`}
                class="select select--auto"
               
                value={u.role}
                disabled={busyId === u.id || !u.active}
                onChange={(e) => {
                  const role = e.currentTarget.value as Role;
                  e.currentTarget.value = u.role; // keep until the server confirms
                  patch(u, { role }, `${u.name} is now ${role === 'director' ? 'a director' : 'an officer'}.`);
                }}
              >
                {ROLES.map((r) => (
                  <option value={r} key={r}>
                    {r}
                  </option>
                ))}
              </select>
            </>
          ),
        },
        { key: 's', label: 'status', render: (u) => (u.active ? <Badge tone="positive">active</Badge> : <Badge tone="neutral">deactivated</Badge>) },
        { key: 'l', label: 'last sign-in', hideOnPhone: true, render: (u) => <span class="small">{relative(u.lastLoginAt)}</span> },
        {
          key: 'a',
          label: '',
          cardLabel: '',
          render: (u) =>
            u.id === meId ? null : (
              <button
                type="button"
                class={`linkish${u.active ? ' linkish--danger' : ''}`}
                disabled={busyId === u.id}
                onClick={() => {
                  if (u.active && !confirm(`Deactivate ${u.name}? They are signed out everywhere and can't sign in until you restore them.`)) return;
                  patch(u, { active: !u.active }, u.active ? `${u.name} is deactivated and signed out.` : `${u.name} is active again.`);
                }}
              >
                {u.active ? 'deactivate' : 'restore'}
              </button>
            ),
        },
      ]}
    />
  );
}

function AddUser({ onAdded }: { onAdded: (u: AdminUser) => void }) {
  const empty = { discordId: '', name: '', role: 'officer' as Role, title: '', email: '' };
  const [f, setF] = useState(empty);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f, v: string) => setF((s) => ({ ...s, [k]: v }));

  return (
    <form
      class="form"
      noValidate
      onSubmit={async (e) => {
        e.preventDefault();
        setError(null);
        const parsed = AdminUserCreateRequest.safeParse({
          discordId: f.discordId.trim(),
          name: f.name,
          role: f.role,
          title: f.title.trim() || null,
          email: f.email.trim() || null,
        });
        if (!parsed.success) {
          const fe = flattenError(parsed.error).fieldErrors as Record<string, string[] | undefined>;
          setErrors(Object.fromEntries(Object.entries(fe).map(([k, v]) => [k, k === 'name' ? 'Enter their name.' : k === 'email' ? 'That is not an email address.' : v![0]!])));
          return;
        }
        setErrors({});
        setBusy(true);
        try {
          const { user } = await api('adminUserCreate', { body: parsed.data });
          onAdded(user);
          setF(empty);
        } catch (err) {
          setErrors(fieldErrors(err));
          setError(errorMessage(err));
        } finally {
          setBusy(false);
        }
      }}
    >
      <div class="grid-fields">
        <Field id="au-discord" label="Discord user id" error={errors.discordId} hint="Discord → Settings → Advanced → Developer Mode, then right-click the person → Copy User ID.">
          <input class="input input--money" inputMode="numeric" autoComplete="off" {...describe('au-discord', errors.discordId, true)} value={f.discordId} onInput={(e) => set('discordId', e.currentTarget.value)} />
        </Field>
        <Field id="au-name" label="name" error={errors.name}>
          <input class="input" {...describe('au-name', errors.name)} value={f.name} onInput={(e) => set('name', e.currentTarget.value)} />
        </Field>
        <Field id="au-title" label="title" optional error={errors.title}>
          <input class="input" placeholder="e.g. VP Events" {...describe('au-title', errors.title)} value={f.title} onInput={(e) => set('title', e.currentTarget.value)} />
        </Field>
        <Field id="au-email" label="email" optional error={errors.email}>
          <input class="input" type="email" {...describe('au-email', errors.email)} value={f.email} onInput={(e) => set('email', e.currentTarget.value)} />
        </Field>
        <Field id="au-role" label="role" hint="Directors approve requests, see every report and manage officers.">
          <select class="select" {...describe('au-role', undefined, true)} value={f.role} onChange={(e) => set('role', e.currentTarget.value)}>
            <option value="officer">officer</option>
            <option value="director">director</option>
          </select>
        </Field>
      </div>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={busy}>
          {busy ? 'adding…' : 'add officer'}
        </button>
      </div>
    </form>
  );
}
