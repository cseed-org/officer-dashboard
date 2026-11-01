import { flattenError } from 'zod';
import { useMemo, useState } from 'preact/hooks';
import { AdminUserCreateRequest, type AdminUser } from '../../lib/contracts';
import type { Role } from '../../lib/domain';
import { api, fieldErrors } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import { ROLE_HUE } from '../../lib/colors';
import { fmtDateTime, relative } from '../../lib/format';
import { Drawer } from '../components/Drawer';
import { describe, Field } from '../components/kit';
import { Avatar, CopyText, RoleToggle, RowMenu } from '../components/people';
import { Async, Empty, errorMessage, Loading, Notice } from '../components/states';

const ROLE_OPTIONS = [
  { key: 'officer' as const, label: 'officer', hue: ROLE_HUE.officer },
  { key: 'director' as const, label: 'director', hue: ROLE_HUE.director },
  { key: 'leadership' as const, label: 'leadership', hue: ROLE_HUE.leadership },
];

/**
 * Officers: a quiet list with a toolbar (count, search, show deactivated), an inline
 * role toggle (confirmed), a ⋯ menu for deactivate/reactivate (confirmed) and the add form
 * in a drawer opened from the tab's "+ add officer" button.
 */
export default function AdminUsersView({ meId, isOwner, canChangeRoles, adding, onCloseAdd }: { isOwner: boolean; meId: string; canChangeRoles: boolean; adding: boolean; onCloseAdd: () => void }) {
  const state = useApi('adminUsers');
  const [notice, setNotice] = useState<{ tone: 'positive' | 'negative'; text: string; n: number } | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [q, setQ] = useState('');
  const [showInactive, setShowInactive] = useState(false);

  const patch = async (u: AdminUser, body: { role?: Role; active?: boolean }, msg: string) => {
    if (state.status !== 'ready') return;
    setBusyId(u.id);
    setNotice(null);
    try {
      const { user } = await api('adminUserUpdate', { params: { id: u.id }, body });
      state.set({ users: state.data.users.map((x) => (x.id === user.id ? user : x)) });
      setNotice({ tone: 'positive', text: msg, n: Date.now() });
    } catch (e) {
      setNotice({ tone: 'negative', text: errorMessage(e), n: Date.now() });
    } finally {
      setBusyId(null);
    }
  };

  const ownership = async (u: AdminUser, action: 'add' | 'remove' | 'transfer') => {
    const message = action === 'transfer'
      ? `Transfer ownership to ${u.name}? They become an owner and you remain leadership without owner access. Other co-owners keep their access.`
      : action === 'add' ? `Make ${u.name} a co-owner? Owners can transfer ownership and manage other owners.${u.role !== 'leadership' ? ' This also promotes them to leadership and signs them out of existing sessions.' : ''}` : `Remove owner access from ${u.name}? They remain leadership. The last owner cannot be removed.`;
    if (!confirm(message)) return;
    setBusyId(u.id); setNotice(null);
    try {
      if (action === 'transfer') await api('ownerTransfer', { body: { toUserId: u.id } });
      else await api(action === 'add' ? 'ownerAdd' : 'ownerRemove', { params: { userId: u.id } });
      // Refresh the signed-in user's owner flag as well as the directory.
      location.reload();
    } catch (e) { setNotice({ tone: 'negative', text: errorMessage(e), n: Date.now() }); }
    finally { setBusyId(null); }
  };

  return (
    <div class="stack stack--tight">
      {notice && (
        <Notice tone={notice.tone} key={notice.n}>
          {notice.text}
        </Notice>
      )}
      <Async state={state} loading={<Loading rows={5} label="Loading officers" />}>
        {(d) => (
          <div class="list-block">
          <UserList
            users={d.users}
            meId={meId}
            canChangeRoles={canChangeRoles}
            isOwner={isOwner}
            ownership={ownership}
            busyId={busyId}
            patch={patch}
            q={q}
            setQ={setQ}
            showInactive={showInactive}
            setShowInactive={setShowInactive}
          />
          </div>
        )}
      </Async>

      <Drawer id="add-officer" open={adding} onClose={onCloseAdd} title="add an officer">
        <AddUser
          canChangeRoles={canChangeRoles}
          onCancel={onCloseAdd}
          onAdded={(u) => {
            if (state.status === 'ready') state.set({ users: [...state.data.users, u] });
            setNotice({ tone: 'positive', text: `${u.name} can now sign in with Discord.`, n: Date.now() });
            onCloseAdd();
          }}
        />
      </Drawer>
    </div>
  );
}

function UserList({
  users,
  meId,
  canChangeRoles,
  isOwner,
  ownership,
  busyId,
  patch,
  q,
  setQ,
  showInactive,
  setShowInactive,
}: {
  users: AdminUser[];
  meId: string;
  canChangeRoles: boolean;
  isOwner: boolean;
  ownership: (u: AdminUser, action: 'add' | 'remove' | 'transfer') => void;
  busyId: string | null;
  patch: (u: AdminUser, body: { role?: Role; active?: boolean }, msg: string) => void;
  q: string;
  setQ: (s: string) => void;
  showInactive: boolean;
  setShowInactive: (b: boolean) => void;
}) {
  const inactive = users.filter((u) => !u.active).length;
  const term = q.trim().toLowerCase();
  const rows = useMemo(
    () =>
      [...users]
        .filter((u) => showInactive || u.active)
        .filter((u) => !term || [u.name, u.title ?? '', u.discordId, u.role].some((s) => s.toLowerCase().includes(term)))
        .sort((a, b) => Number(b.active) - Number(a.active) || a.name.localeCompare(b.name)),
    [users, showInactive, term],
  );
  const active = users.length - inactive;

  return (
    <>
      <div class="toolbar list-toolbar">
        <span class="toolbar-count" data-officer-count>
          {active} active officer{active === 1 ? '' : 's'}
          {inactive > 0 && <span class="muted"> · {inactive} deactivated</span>}
        </span>
        <div class="toolbar-actions">
          {inactive > 0 && (
            <label class="toggle">
              <input type="checkbox" checked={showInactive} onChange={(e) => setShowInactive(e.currentTarget.checked)} />
              show deactivated
            </label>
          )}
          <div class="toolbar-search">
            <label class="visually-hidden" for="officer-search">
              Search officers
            </label>
            <input id="officer-search" class="input" type="search" placeholder="search name, title or id" value={q} onInput={(e) => setQ(e.currentTarget.value)} />
          </div>
        </div>
      </div>

      {users.length <= 1 && <Empty title="Only you so far">Add officers by their Discord user id. They can sign in with Discord right away.</Empty>}
      {rows.length === 0 && users.length > 1 ? (
        <Empty title="No officers match">Nothing matches “{q.trim()}”.</Empty>
      ) : (
        rows.length > 0 && (
          <div class="table-wrap">
            <table class="dt dt--people" data-officers>
              <caption class="visually-hidden">Officers</caption>
              <thead>
                <tr>
                  <th scope="col">officer</th>
                  <th scope="col">role</th>
                  <th scope="col" class="people-seen">last sign-in</th>
                  <th scope="col" class="people-menu">
                    <span class="visually-hidden">actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((u) => (
                  <tr key={u.id} class={u.active ? undefined : 'is-inactive'} data-officer={u.name}>
                    <td class="people-who-cell">
                      <span class="people-who">
                      <Avatar name={u.name} id={u.id} />
                      <span class="people-text">
                        <span class="cell-title">
                          {u.name}
                          {u.isOwner && <span class="tag">Owner</span>}
                          {u.id === meId && <span class="muted small"> (you)</span>}
                          {!u.active && <span class="tag">deactivated</span>}
                        </span>
                        <span class="cell-sub">
                          {u.title && <span class="people-title">{u.title}</span>}
                          <CopyText text={u.discordId} label="Discord id" />
                        </span>
                      </span>
                      </span>
                    </td>
                    <td class="people-role">
                      <RoleToggle
                        label={`Role for ${u.name}`}
                        value={u.role}
                        options={ROLE_OPTIONS}
                        disabled={!canChangeRoles || (u.isOwner && !isOwner) || busyId === u.id || !u.active}
                        onChange={(role) => {
                          if (!confirm(`Make ${u.name} ${role}? Only leadership can change roles.`)) return;
                          patch(u, { role }, `${u.name} is now ${role}.`);
                        }}
                      />
                    </td>
                    <td class="people-seen">
                      <span class="small muted" title={u.lastLoginAt ? fmtDateTime(u.lastLoginAt) : undefined}>
                        {u.lastLoginAt ? relative(u.lastLoginAt) : 'never signed in'}
                      </span>
                    </td>
                    <td class="people-menu">
                      {isOwner && u.active && <div class="stack stack--tight">
                        <button class="btn btn--small" disabled={busyId !== null} onClick={() => ownership(u, u.isOwner ? 'remove' : 'add')}>{u.isOwner ? 'Remove owner' : 'Make co-owner'}</button>
                        {u.id !== meId && <button class="btn btn--small" disabled={busyId !== null} onClick={() => ownership(u, 'transfer')}>Transfer ownership</button>}
                      </div>}
                      <RowMenu
                        label={`Actions for ${u.name}`}
                        items={
                          (u.id === meId || (u.isOwner && !isOwner) || (u.role === 'leadership' && !canChangeRoles))
                            ? []
                            : [
                                u.active
                                  ? {
                                      label: 'deactivate',
                                      danger: true,
                                      onSelect: () => {
                                        if (!confirm(`Deactivate ${u.name}? They are signed out everywhere and can't sign in until you reactivate them.`)) return;
                                        patch(u, { active: false }, `${u.name} is deactivated and signed out.`);
                                      },
                                    }
                                  : {
                                      label: 'reactivate',
                                      onSelect: () => {
                                        if (!confirm(`Reactivate ${u.name}? They can sign in with Discord again.`)) return;
                                        patch(u, { active: true }, `${u.name} is active again.`);
                                      },
                                    },
                              ]
                        }
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      )}
    </>
  );
}

function AddUser({ onAdded, onCancel, canChangeRoles }: { canChangeRoles: boolean; onAdded: (u: AdminUser) => void; onCancel: () => void }) {
  const empty = { discordId: '', name: '', role: 'officer' as Role, title: '', email: '' };
  const [f, setF] = useState(empty);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (k: keyof typeof f, v: string) => setF((s) => ({ ...s, [k]: v }));

  return (
    <form
      class="form form--stack"
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
          const next = Object.fromEntries(Object.entries(fe).map(([k, v]) => [k, k === 'name' ? 'Enter their name.' : k === 'email' ? 'That is not an email address.' : v![0]!]));
          setErrors(next);
          const first = ['discordId', 'name', 'title', 'email'].find((k) => next[k]);
          if (first) document.getElementById(`au-${first === 'discordId' ? 'discord' : first}`)?.focus();
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
      <Field id="au-discord" label="Discord user id" error={errors.discordId} hint="In Discord: Settings → Advanced → Developer Mode, then right-click the person → Copy User ID.">
        <input class="input input--money" inputMode="numeric" autoComplete="off" {...describe('au-discord', errors.discordId, true)} value={f.discordId} onInput={(e) => set('discordId', e.currentTarget.value)} />
      </Field>
      <Field id="au-name" label="name" error={errors.name}>
        <input class="input" autoComplete="off" {...describe('au-name', errors.name)} value={f.name} onInput={(e) => set('name', e.currentTarget.value)} />
      </Field>
      <Field id="au-title" label="title" optional error={errors.title}>
        <input class="input" placeholder="e.g. VP Events" {...describe('au-title', errors.title)} value={f.title} onInput={(e) => set('title', e.currentTarget.value)} />
      </Field>
      <Field id="au-email" label="email" optional error={errors.email}>
        <input class="input" type="email" autoComplete="off" {...describe('au-email', errors.email)} value={f.email} onInput={(e) => set('email', e.currentTarget.value)} />
      </Field>
      <fieldset class="fieldset">
        <legend class="field-label">role</legend>
        <div class="choice-row choice-row--compact">
          {ROLE_OPTIONS.filter((o) => canChangeRoles || o.key === 'officer').map((o) => (
            <label class="choice" key={o.key}>
              <input type="radio" name="au-role" value={o.key} checked={f.role === o.key} onChange={() => set('role', o.key)} />
              <span class="choice-title dot-label" data-hue={o.hue}>
                {o.label}
              </span>
              <span class="choice-sub">{o.key === 'leadership' ? 'owns all access and role changes' : o.key === 'director' ? 'approves requests, sees every report, manages officers' : 'requests purchases, runs events'}</span>
            </label>
          ))}
        </div>
      </fieldset>
      {error && <Notice tone="negative">{error}</Notice>}
      <div class="form-actions">
        <button type="submit" class="btn btn--primary" disabled={busy}>
          {busy ? 'adding…' : 'add officer'}
        </button>
        <button type="button" class="btn btn--text" onClick={onCancel}>
          cancel
        </button>
      </div>
    </form>
  );
}
