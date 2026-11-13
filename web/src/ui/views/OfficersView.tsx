import { useEffect, useMemo, useState } from 'preact/hooks';
import { api, ApiCallError, fieldErrors } from '../../lib/api/client';
import { useApi } from '../../lib/api/useApi';
import type { CurrentUser, DMResult, Invite, InviteResponse, Officer, ProfileResponse, Subteam } from '../../lib/contracts';
import { fmtDate, fmtDateTime, fmtShort, safeUrl } from '../../lib/format';
import { Drawer } from '../components/Drawer';
import { ErrorState, Loading } from '../components/states';
import { CalendarPanel } from './CalendarPanel';
import { ProfileForm } from './ProfileForm';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const birthday = (md: string | null) => {
  if (!md) return '—';
  const [m, d] = md.split('-');
  return `${MONTHS[+m! - 1]} ${+d!}`;
};
const cap = (s: string) => s[0]!.toUpperCase() + s.slice(1);
const errText = (e: unknown, fallback: string) => (e instanceof ApiCallError || e instanceof Error ? e.message : fallback);

function RoleBadge({ o }: { o: Pick<Officer, 'role' | 'isOwner'> }) {
  return (
    <>
      <span class="role-badge" data-role={o.role}>
        {cap(o.role)}
      </span>
      {o.isOwner && (
        <>
          {' '}
          <span class="badge badge--neutral">Owner</span>
        </>
      )}
    </>
  );
}

const StatusBadge = ({ o }: { o: Officer }) =>
  !o.active ? (
    <span class="badge badge--neutral">Deactivated</span>
  ) : o.onboarding === 'pending' ? (
    <span class="badge badge--attention">Setting up</span>
  ) : !o.profileComplete ? (
    <span class="badge badge--attention">Profile incomplete</span>
  ) : (
    <span class="badge badge--positive">Active</span>
  );

/** One directory line: name (opens the profile), subteam, role and the best email to reach them. */
function OfficerRow({ o, onOpen }: { o: Officer; onOpen: () => void }) {
  const email = o.schoolEmail || o.personalEmail;
  const sub = [o.title, !o.active ? null : o.onboarding === 'pending' ? 'Setting up' : !o.profileComplete ? 'Profile incomplete' : null].filter(Boolean).join(' · ');
  return (
    <tr data-officer={o.id}>
      <td data-label="Officer" class="dt-primary">
        <span class="people-cell">
          <button class="linkish cell-title" onClick={onOpen}>
            {o.preferredName && o.preferredName !== o.name.split(' ')[0] ? `${o.name} (${o.preferredName})` : o.name}
          </button>
          <span class="cell-sub">{sub || '—'}</span>
        </span>
      </td>
      <td data-label="Subteam">{o.subteam?.name ?? '—'}</td>
      <td data-label="Role">
        <span class="people-cell">
          <RoleBadge o={o} />
        </span>
      </td>
      <td data-label="Contact">{email ? <a href={`mailto:${email}`}>{email}</a> : <span class="muted">—</span>}</td>
    </tr>
  );
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

// ─────────────────────────── confirm ───────────────────────────

interface Confirm {
  title: string;
  body: string;
  go: string;
  danger?: boolean;
  run: () => Promise<void>;
}

function ConfirmDialog({ c, onClose }: { c: Confirm | null; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => setError(null), [c]);
  return (
    <Drawer open={!!c} onClose={onClose} id="confirm" kind="dialog" title={c?.title ?? 'Confirm change'}>
      {c && (
        <div class="stack">
          <p>{c.body}</p>
          <p class="field-hint">This change is recorded in the workspace audit log with your name and the time.</p>
          {error && (
            <p class="notice notice--negative" role="alert">
              {error}
            </p>
          )}
          <div class="action-row">
            <button
              class={`btn ${c.danger ? 'btn--danger' : 'btn--primary'}`}
              disabled={busy}
              data-confirm-go
              onClick={async () => {
                setBusy(true);
                try {
                  await c.run();
                  onClose();
                } catch (e) {
                  setError(errText(e, 'That change failed.'));
                } finally {
                  setBusy(false);
                }
              }}
            >
              {c.go}
            </button>
            <button class="btn" onClick={onClose}>
              Cancel
            </button>
          </div>
        </div>
      )}
    </Drawer>
  );
}

// ─────────────────────────── invite dialog ───────────────────────────

function DMStatus({ dm, at }: { dm: DMResult; at?: string | null }) {
  if (dm.status === 'sent')
    return (
      <div class="invite-status invite-status--sent" role="status" data-dm-status="sent">
        <span class="invite-status-text">DM sent ✓</span>
        <time dateTime={dm.sentAt ?? at ?? undefined}>{fmtDateTime(dm.sentAt ?? at ?? null)}</time>
      </div>
    );
  if (dm.status === 'skipped') return null;
  return (
    <div class={`invite-status${dm.status === 'failed' ? ' invite-status--failed' : ''}`} role={dm.status === 'failed' ? 'alert' : 'status'} data-dm-status={dm.status}>
      <span class="invite-status-text">{dm.message ?? 'The DM was not sent.'} Copy the link below instead.</span>
    </div>
  );
}

function InviteLink({ link }: { link: string }) {
  const [copied, setCopied] = useState<boolean | null>(null);
  return (
    <div class="invite-link" data-invite-link>
      <code>{link}</code>
      <button type="button" class="btn" onClick={async () => setCopied(await copy(link))}>
        Copy link
      </button>
      {copied === true && <span class="invite-copied">Link copied ✓</span>}
      {copied === false && <span class="small muted">Select the link to copy it.</span>}
    </div>
  );
}

function InviteDialog({
  open,
  onClose,
  subteams,
  dmConfigured,
  prefill,
  onSent,
}: {
  open: boolean;
  onClose: () => void;
  subteams: Subteam[];
  dmConfigured: boolean;
  prefill: { name: string; discordId: string; subteamId: string } | null;
  onSent: () => void;
}) {
  const [name, setName] = useState('');
  const [discordId, setDiscordId] = useState('');
  const [subteamId, setSubteamId] = useState('');
  const [dm, setDm] = useState(dmConfigured);
  const [link, setLink] = useState(true);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<InviteResponse | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(prefill?.name ?? '');
    setDiscordId(prefill?.discordId ?? '');
    setSubteamId(prefill?.subteamId ?? subteams[0]?.id ?? '');
    setDm(dmConfigured);
    setLink(true);
    setErrors({});
    setError(null);
    setResult(null);
  }, [open]);

  const method = dm && link ? 'both' : dm ? 'dm' : 'link';
  const team = subteams.find((t) => t.id === subteamId)?.name ?? 'their subteam';

  const send = async (e: Event) => {
    e.preventDefault();
    const errs: Record<string, string> = {};
    if (!name.trim()) errs.name = 'Enter their name';
    if (!/^\d{17,20}$/.test(discordId.trim())) errs.discordId = 'Discord IDs are 17–20 digits';
    if (!subteamId) errs.subteamId = 'Choose a subteam';
    if (!dm && !link) errs.method = 'Choose one or both';
    setErrors(errs);
    setError(null);
    if (Object.keys(errs).length) return;
    setBusy(true);
    try {
      const r = await api('inviteCreate', { body: { name: name.trim(), discordId: discordId.trim(), subteamId, method } });
      setResult(r);
      onSent();
    } catch (err) {
      const fe = fieldErrors(err);
      if (Object.keys(fe).length) setErrors(fe);
      else setError(errText(err, 'The invite could not be created.'));
    } finally {
      setBusy(false);
    }
  };

  const showLink = result && (result.invite.method !== 'dm' || result.dm.status !== 'sent');
  const d = (id: string) => ({ 'aria-invalid': errors[id] ? true : undefined, 'aria-describedby': errors[id] ? `inv-${id}-error` : undefined });
  const fe = (id: string) =>
    errors[id] ? (
      <p class="field-error" id={`inv-${id}-error`}>
        {errors[id]}
      </p>
    ) : null;

  return (
    <Drawer open={open} onClose={onClose} id="invite" kind="dialog" title={prefill ? 'Resend invite' : 'Invite an officer'}>
      <form class="stack" onSubmit={send} noValidate data-invite-form>
        <div class="grid-fields">
          <div class="field">
            <label class="field-label" for="inv-name">
              Name
            </label>
            <input class="input" id="inv-name" value={name} autocomplete="off" disabled={!!result} onInput={(e) => setName((e.target as HTMLInputElement).value)} {...d('name')} />
            {fe('name')}
          </div>
          <div class="field">
            <label class="field-label" for="inv-team">
              Subteam
            </label>
            <select class="select" id="inv-team" value={subteamId} disabled={!!result} onChange={(e) => setSubteamId((e.target as HTMLSelectElement).value)} {...d('subteamId')}>
              {subteams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            {fe('subteamId')}
          </div>
        </div>
        <div class="field">
          <label class="field-label" for="inv-discord">
            Discord ID
          </label>
          <input
            class="input mono"
            id="inv-discord"
            inputMode="numeric"
            autocomplete="off"
            value={discordId}
            disabled={!!result}
            onInput={(e) => setDiscordId((e.target as HTMLInputElement).value)}
            aria-invalid={errors.discordId ? true : undefined}
            aria-describedby={`inv-discord-hint${errors.discordId ? ' inv-discordId-error' : ''}`}
          />
          <p class="field-hint" id="inv-discord-hint">
            Discord → Settings → Advanced → Developer mode, then right-click the person and Copy User ID.
          </p>
          {fe('discordId')}
        </div>
        <fieldset class="fieldset" disabled={!!result}>
          <legend>Send invitation by</legend>
          <div class="invite-delivery">
            <label class="choice">
              <input type="checkbox" checked={dm} disabled={!dmConfigured} onChange={(e) => setDm((e.target as HTMLInputElement).checked)} data-invite-dm />
              <span class="choice-title">Discord DM</span>
              <span class="choice-sub">{dmConfigured ? 'The CSEED bot messages them a sign-in link' : 'Not set up: the dashboard has no Discord bot token'}</span>
            </label>
            <label class="choice">
              <input type="checkbox" checked={link} onChange={(e) => setLink((e.target as HTMLInputElement).checked)} data-invite-copy />
              <span class="choice-title">Copy link</span>
              <span class="choice-sub">Share it yourself, in any channel</span>
            </label>
          </div>
          <p class="field-hint" id="inv-method-hint">
            {dmConfigured ? 'Choose one or both.' : 'Discord DMs aren’t configured (DISCORD_BOT_TOKEN), so invites are copy-link only for now.'}
          </p>
          {fe('method')}
        </fieldset>
        {!result && (
          <div class="invite-status" role="status">
            <span class="invite-status-text">{name.trim() ? `Ready to invite ${name.trim().split(/\s+/)[0]} to ${team}.` : 'Fill in who you’re inviting.'}</span>
          </div>
        )}
        {error && (
          <div class="invite-status invite-status--failed" role="alert">
            <span class="invite-status-text">{error}</span>
          </div>
        )}
        {result && (
          <>
            {result.reinvited && <p class="notice">They already had a pending invite. It now has these details and a new link; the old link stopped working.</p>}
            <DMStatus dm={result.dm} />
            {showLink && <InviteLink link={result.link} />}
            <p class="small muted">The link expires {fmtDate(result.invite.expiresAt)}. Only the Discord account {result.invite.discordId} can accept it.</p>
          </>
        )}
        <div class="action-row">
          {!result ? (
            <>
              <button class="btn btn--primary" type="submit" disabled={busy} data-send-invite>
                {busy ? 'Sending…' : 'Send invitation'}
              </button>
              <button class="btn" type="button" onClick={onClose}>
                Cancel
              </button>
            </>
          ) : (
            <button class="btn" type="button" onClick={onClose}>
              Done
            </button>
          )}
        </div>
      </form>
    </Drawer>
  );
}

// ─────────────────────────── edit invite details ───────────────────────────

/** Fix the name or subteam of an invite before setup is finished (an invitee's correction). */
function InviteEditDialog({ invite, subteams, onClose, onSaved }: { invite: Invite | null; subteams: Subteam[]; onClose: () => void; onSaved: (i: Invite) => void }) {
  const [name, setName] = useState('');
  const [subteamId, setSubteamId] = useState('');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!invite) return;
    setName(invite.name);
    setSubteamId(invite.subteam?.id ?? subteams[0]?.id ?? '');
    setErrors({});
    setError(null);
  }, [invite?.id]);
  const save = async (e: Event) => {
    e.preventDefault();
    if (!invite) return;
    if (!name.trim()) {
      setErrors({ name: 'Enter their name' });
      document.getElementById('inv-edit-name')?.focus();
      return;
    }
    setErrors({});
    setError(null);
    setBusy(true);
    try {
      const r = await api('inviteUpdate', { params: { id: invite.id }, body: { name: name.trim(), subteamId } });
      onSaved(r.invite);
    } catch (err) {
      const fe = fieldErrors(err);
      if (Object.keys(fe).length) setErrors(fe);
      else setError(errText(err, 'The invite could not be updated.'));
    } finally {
      setBusy(false);
    }
  };
  const d = (id: string) => ({ 'aria-invalid': errors[id] ? true : undefined, 'aria-describedby': errors[id] ? `inv-edit-${id}-error` : undefined });
  const fe = (id: string) =>
    errors[id] ? (
      <p class="field-error" id={`inv-edit-${id}-error`}>
        {errors[id]}
      </p>
    ) : null;
  return (
    <Drawer open={!!invite} onClose={onClose} id="invite-edit" kind="dialog" title="Edit invite details">
      <form class="stack" onSubmit={save} noValidate data-invite-edit-form>
        <p class="small muted">
          {invite?.status === 'accepted'
            ? 'They’re setting up now. Their Confirm step updates as soon as you save; the link and Discord ID stay the same.'
            : 'The invite link and Discord ID stay the same.'}
        </p>
        <div class="grid-fields">
          <div class="field">
            <label class="field-label" for="inv-edit-name">
              Name
            </label>
            <input class="input" id="inv-edit-name" value={name} maxLength={100} autocomplete="off" onInput={(e) => setName((e.target as HTMLInputElement).value)} {...d('name')} />
            {fe('name')}
          </div>
          <div class="field">
            <label class="field-label" for="inv-edit-team">
              Subteam
            </label>
            <select class="select" id="inv-edit-team" value={subteamId} onChange={(e) => setSubteamId((e.target as HTMLSelectElement).value)} {...d('subteamId')}>
              {subteams.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
            {fe('subteamId')}
          </div>
        </div>
        {error && (
          <p class="notice notice--negative" role="alert">
            {error}
          </p>
        )}
        <div class="action-row">
          <button class="btn btn--primary" type="submit" disabled={busy} data-invite-edit-save>
            {busy ? 'Saving…' : 'Save details'}
          </button>
          <button class="btn btn--text" type="button" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Drawer>
  );
}

// ─────────────────────────── invites list ───────────────────────────

function inviteStatus(i: Invite) {
  if (i.status === 'expired') return <span class="badge badge--neutral">Expired</span>;
  if (i.status === 'accepted') return <span class="badge badge--attention">Accepted · setting up</span>;
  if (i.status === 'onboarded') return <span class="badge badge--positive">Accepted</span>;
  if (i.dmError && i.method === 'dm') return <span class="badge badge--negative">DM failed</span>;
  return <span class="badge badge--attention">Awaiting sign-in</span>;
}

function delivery(i: Invite) {
  if (i.dmSentAt) return <span class="badge badge--positive">DM sent ✓ {fmtDateTime(i.dmSentAt)}</span>;
  if (i.dmError)
    return (
      <>
        <span class="badge badge--negative">DM failed</span>
        <span class="cell-sub">{i.dmError}</span>
      </>
    );
  return <>Link</>;
}

function InvitesSection({
  invites,
  dmConfigured,
  onChanged,
  onResend,
  confirm,
  subteams,
}: {
  invites: Invite[];
  dmConfigured: boolean;
  onChanged: () => void;
  onResend: (i: Invite) => void;
  confirm: (c: Confirm) => void;
  subteams: Subteam[];
}) {
  const [note, setNote] = useState<string | null>(null);
  const [editing, setEditing] = useState<Invite | null>(null);
  const [freshLink, setFreshLink] = useState<{ id: string; link: string } | null>(null);
  const recent = invites.filter((i) => i.status !== 'onboarded' || Date.now() - Date.parse(i.onboardedAt ?? i.createdAt) < 14 * 864e5);
  const counts = { pending: 0, expired: 0, accepted: 0 };
  for (const i of recent) counts[i.status === 'pending' ? 'pending' : i.status === 'expired' ? 'expired' : 'accepted']++;

  const reissue = async (i: Invite, method: 'dm' | 'link') => {
    setNote(null);
    try {
      const r = await api('inviteCreate', { body: { name: i.name, discordId: i.discordId, subteamId: i.subteam?.id ?? '', method } });
      if (method === 'link') {
        setFreshLink({ id: i.id, link: r.link });
        const ok = await copy(r.link);
        setNote(ok ? `New link for ${i.name} copied ✓. Earlier links for this invite stopped working.` : `New link for ${i.name} is below. Earlier links stopped working.`);
      } else setNote(r.dm.status === 'sent' ? `DM sent ✓ to ${i.name}, ${fmtDateTime(r.dm.sentAt)}.` : `${r.dm.message ?? 'The DM failed.'}`);
      onChanged();
    } catch (e) {
      setNote(errText(e, 'That didn’t work.'));
    }
  };

  return (
    <section class="stack" data-invites>
      <div class="section-head">
        <h2>Invites</h2>
        <span class="small muted">
          {counts.pending} pending · {counts.expired} expired · {counts.accepted} accepted
        </span>
      </div>
      {note && (
        <p class="notice" role="status" data-invite-note>
          {note}
        </p>
      )}
      {freshLink && <InviteLink link={freshLink.link} />}
      {recent.length === 0 ? (
        <p class="muted small">No invites yet.</p>
      ) : (
        <div class="table-wrap">
          <table class="dt">
            <caption class="visually-hidden">Officer invites</caption>
            <thead>
              <tr>
                <th scope="col">Invitee</th>
                <th scope="col">Sent</th>
                <th scope="col">Delivery</th>
                <th scope="col">Status</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {recent.map((i) => (
                <tr key={i.id} data-invite={i.discordId}>
                  <td data-label="Invitee" class="dt-primary">
                    <span class="people-cell">
                      <span class="cell-title">{i.name}</span>
                      <span class="cell-sub">
                        {i.subteam?.name ?? '—'} · <span class="mono">{i.discordId}</span>
                      </span>
                    </span>
                  </td>
                  <td data-label="Sent">
                    <span class="people-cell">
                      {fmtDateTime(i.createdAt)}
                      <span class="cell-sub">
                        {i.acceptedAt
                          ? `Accepted ${fmtShort(i.acceptedAt)}`
                          : i.expiresAt
                            ? `${i.status === 'expired' ? 'Expired' : 'Expires'} ${fmtShort(i.expiresAt)}`
                            : ''}
                      </span>
                    </span>
                  </td>
                  <td data-label="Delivery">
                    <span class="people-cell">{delivery(i)}</span>
                  </td>
                  <td data-label="Status">
                    <span class="people-cell">{inviteStatus(i)}</span>
                  </td>
                  <td data-label="Actions">
                    <span class="people-cell">
                      {i.status === 'accepted' ? (
                        <span class="action-row">
                          <button class="btn btn--small" onClick={() => setEditing(i)} data-invite-edit>
                            Edit details
                          </button>
                        </span>
                      ) : i.status === 'pending' || i.status === 'expired' ? (
                        <span class="action-row">
                          <button class="btn btn--small" onClick={() => setEditing(i)} data-invite-edit>
                            Edit details
                          </button>
                          {i.status === 'expired' && (
                            <button class="btn btn--small" onClick={() => onResend(i)}>
                              Resend
                            </button>
                          )}
                          {i.status === 'pending' && dmConfigured && (
                            <button class="btn btn--small" onClick={() => reissue(i, 'dm')}>
                              {i.dmError ? 'Retry DM' : 'Resend DM'}
                            </button>
                          )}
                          {i.status === 'pending' && (
                            <button class="btn btn--small" title="Creates a fresh link; earlier links stop working" onClick={() => reissue(i, 'link')}>
                              Copy link
                            </button>
                          )}
                          <button
                            class="btn btn--small"
                            onClick={() =>
                              confirm({
                                title: i.status === 'expired' ? 'Remove invite' : 'Revoke invite',
                                body: `${i.name}'s invite link will stop working, and their Discord account won't be able to sign up.`,
                                go: i.status === 'expired' ? 'Remove' : 'Revoke',
                                danger: true,
                                run: async () => {
                                  await api('inviteRevoke', { params: { id: i.id } });
                                  onChanged();
                                },
                              })
                            }
                          >
                            {i.status === 'expired' ? 'Remove' : 'Revoke'}
                          </button>
                        </span>
                      ) : (
                        '—'
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <p class="small muted">Invites expire after 7 days. Only the matching Discord account can accept, then completes onboarding after sign-in.</p>
      <InviteEditDialog
        invite={editing}
        subteams={subteams}
        onClose={() => setEditing(null)}
        onSaved={(i) => {
          setEditing(null);
          setNote(`Saved ${i.name}’s details${i.status === 'accepted' ? '. Their setup shows the change now.' : '.'}`);
          onChanged();
        }}
      />
    </section>
  );
}

// ─────────────────────────── profile drawer ───────────────────────────

function ProfileDrawer({
  officer,
  me,
  subteams,
  canManage,
  canChangeRoles,
  onClose,
  onChanged,
  confirm,
}: {
  officer: Officer | null;
  me: CurrentUser;
  subteams: Subteam[];
  canManage: boolean;
  canChangeRoles: boolean;
  onClose: () => void;
  onChanged: () => void;
  confirm: (c: Confirm) => void;
}) {
  const [role, setRole] = useState('');
  const [team, setTeam] = useState('');
  const [title, setTitle] = useState('');
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setRole(officer?.role ?? '');
    setTeam(officer?.subteam?.id ?? '');
    setTitle(officer?.title ?? '');
    setNote(null);
  }, [officer?.id, officer?.role, officer?.subteam?.id, officer?.title]);

  const o = officer;
  const self = o?.id === me.id;
  const ownerLocked = !!o?.isOwner && !me.isOwner;
  const canEditTeam = canManage && !!o && (o.role !== 'leadership' || canChangeRoles);
  // Directors deactivate officers; only leadership deactivates directors or leadership.
  const canDeactivate = !!o && (canChangeRoles || (canManage && o.role === 'officer'));

  const saveTeam = async () => {
    if (!o) return;
    setBusy(true);
    setNote(null);
    try {
      await api('officerUpdate', { params: { id: o.id }, body: { ...(team !== o.subteam?.id ? { subteamId: team } : {}), title: title.trim() || null } });
      setNote({ ok: true, text: 'Saved ✓' });
      onChanged();
    } catch (e) {
      setNote({ ok: false, text: errText(e, 'Could not save.') });
    } finally {
      setBusy(false);
    }
  };

  const fact = (label: string, value: preact.ComponentChildren) => (
    <div>
      <dt>{label}</dt>
      <dd>{value || '—'}</dd>
    </div>
  );

  return (
    <Drawer open={!!o} onClose={onClose} id="profile" title="Officer profile">
      {o && (
        <div class="stack" data-officer-profile={o.id}>
          <div class="action-row">
            <RoleBadge o={o} />
            <StatusBadge o={o} />
          </div>
          <div>
            <h3>{o.name}</h3>
            <p class="muted">
              {[o.subteam?.name, o.title, o.pronouns].filter(Boolean).join(' · ') || 'No profile yet'}
            </p>
          </div>
          <dl class="facts">
            {fact('Discord', <span class="mono">{o.discordId}</span>)}
            {fact(
              'Phone',
              o.phone !== undefined ? <span data-phone>{o.phone}</span> : <span class="muted">Visible to directors and leadership</span>,
            )}
            {fact('Birthday', birthday(o.birthday))}
            {fact('Preferred name', o.preferredName)}
            {fact('Personal email', o.personalEmail && <a href={`mailto:${o.personalEmail}`}>{o.personalEmail}</a>)}
            {fact('School email', o.schoolEmail && <a href={`mailto:${o.schoolEmail}`}>{o.schoolEmail}</a>)}
            {fact('Graduation', o.gradDate)}
            {fact(
              'LinkedIn',
              o.linkedin && (
                <a href={o.linkedin} target="_blank" rel="noopener noreferrer">
                  {o.linkedin.replace(/^https:\/\/(www\.)?/, '')}
                </a>
              ),
            )}
            {fact('Officer since', o.startTerm)}
            {o.lastLoginAt !== undefined && fact('Last sign-in', o.lastLoginAt ? fmtDateTime(o.lastLoginAt) : 'Never')}
          </dl>
          {!o.profileComplete && <p class="notice">{o.onboarding === 'pending' ? `${o.name} is still going through setup.` : `${o.name} hasn’t filled in their profile yet.`}</p>}

          {canEditTeam && o.profileComplete && (
            <section class="stack" data-officer-edit>
              <h3>Subteam and title</h3>
              <div class="grid-fields">
                <div class="field">
                  <label class="field-label" for="pd-team">
                    Subteam
                  </label>
                  <select class="select" id="pd-team" value={team} onChange={(e) => setTeam((e.target as HTMLSelectElement).value)}>
                    {subteams.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                </div>
                <div class="field">
                  <label class="field-label" for="pd-title">
                    Title
                  </label>
                  <input class="input" id="pd-title" value={title} maxLength={100} onInput={(e) => setTitle((e.target as HTMLInputElement).value)} />
                </div>
              </div>
              <div class="action-row">
                <button class="btn" onClick={saveTeam} disabled={busy || (team === (o.subteam?.id ?? '') && title.trim() === (o.title ?? ''))}>
                  Save subteam and title
                </button>
              </div>
            </section>
          )}

          {canDeactivate && !self && (
            <section class="stack" data-access>
              <h3>Workspace access</h3>
              {ownerLocked ? (
                <p class="notice">Only an owner can change an owner’s role or deactivate them.</p>
              ) : (
                <>
                  {canChangeRoles && (
                  <div class="field">
                    <label class="field-label" for="pd-role">
                      Role
                    </label>
                    <select class="select" id="pd-role" value={role} aria-describedby="pd-role-hint" onChange={(e) => setRole((e.target as HTMLSelectElement).value)}>
                      <option value="officer">Officer</option>
                      <option value="director">Director</option>
                      <option value="leadership">Leadership</option>
                    </select>
                    <p class="field-hint" id="pd-role-hint">
                      Choose a different role to review the change.
                    </p>
                  </div>
                  )}
                  <div class="action-row">
                    {canChangeRoles && (
                    <button
                      class="btn"
                      disabled={role === o.role}
                      onClick={() =>
                        confirm({
                          title: 'Change role',
                          body: `${o.name}: ${cap(o.role)} → ${cap(role)}. They are signed out everywhere and sign in again with the new access.`,
                          go: 'Change role',
                          run: async () => {
                            await api('adminUserUpdate', { params: { id: o.id }, body: { role: role as Officer['role'] } });
                            onChanged();
                          },
                        })
                      }
                    >
                      Review role change
                    </button>
                    )}
                    <button
                      class={`btn${o.active ? ' btn--danger' : ''}`}
                      onClick={() =>
                        confirm({
                          title: o.active ? 'Deactivate officer' : 'Reactivate officer',
                          body: o.active
                            ? `${o.name} is signed out and can't sign in until reactivated. Their profile and history stay.`
                            : `${o.name} can sign in again with their Discord account.`,
                          go: o.active ? 'Deactivate' : 'Reactivate',
                          danger: o.active,
                          run: async () => {
                            await api('adminUserUpdate', { params: { id: o.id }, body: { active: !o.active } });
                            onChanged();
                          },
                        })
                      }
                    >
                      {o.active ? 'Deactivate officer' : 'Reactivate officer'}
                    </button>
                  </div>
                </>
              )}
            </section>
          )}

          {me.isOwner && !self && o.active && (
            <section class="stack" data-owner-controls>
              <h3>Owner controls</h3>
              <p class="small muted">Ownership is separate from roles. Owners can add co-owners and transfer ownership. At least one owner must remain.</p>
              <div class="action-row">
                {!o.isOwner ? (
                  <>
                    <button
                      class="btn"
                      onClick={() =>
                        confirm({
                          title: 'Make co-owner',
                          body: `${o.name} becomes an owner${o.role === 'leadership' ? '' : ' and is promoted to leadership'}.`,
                          go: 'Make co-owner',
                          run: async () => {
                            await api('ownerAdd', { params: { userId: o.id } });
                            onChanged();
                          },
                        })
                      }
                    >
                      Make co-owner
                    </button>
                    <button
                      class="btn"
                      onClick={() =>
                        confirm({
                          title: 'Transfer ownership',
                          body: `${o.name} becomes an owner and you stop being one. You keep leadership.`,
                          go: 'Transfer ownership',
                          danger: true,
                          run: async () => {
                            await api('ownerTransfer', { body: { toUserId: o.id } });
                            location.reload();
                          },
                        })
                      }
                    >
                      Transfer ownership
                    </button>
                  </>
                ) : (
                  <button
                    class="btn btn--danger"
                    onClick={() =>
                      confirm({
                        title: 'Remove owner status',
                        body: `${o.name} stays leadership but is no longer an owner.`,
                        go: 'Remove owner status',
                        danger: true,
                        run: async () => {
                          await api('ownerRemove', { params: { userId: o.id } });
                          onChanged();
                        },
                      })
                    }
                  >
                    Remove owner status
                  </button>
                )}
              </div>
            </section>
          )}
          {note && (
            <p class={`notice${note.ok ? ' notice--positive' : ' notice--negative'}`} role="status">
              {note.text}
            </p>
          )}
        </div>
      )}
    </Drawer>
  );
}

function MyProfileDrawer({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const [data, setData] = useState<ProfileResponse | null>(null);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    if (!open) return;
    setData(null);
    api('profileGet').then(setData, setError);
  }, [open]);
  return (
    <Drawer open={open} onClose={onClose} id="own-profile" title="My profile" size="lg">
      <div class="stack">
        {error ? <ErrorState error={error} /> : !data ? <Loading rows={4} label="Loading your profile" /> : <ProfileForm data={data} onSaved={(r) => (setData(r), onSaved())} />}
        <section class="stack" id="calendar">
          <h3>Calendar</h3>
          <CalendarPanel next="/officers?profile=me" removable />
        </section>
      </div>
    </Drawer>
  );
}

// ─────────────────────────── page ───────────────────────────

export default function OfficersView({ me, section = 'directory' }: { me: CurrentUser; section?: 'directory' | 'invites' }) {
  const list = useApi('officersList');
  const isDirector = me.role !== 'officer';
  const [invites, setInvites] = useState<{ invites: Invite[]; dmConfigured: boolean } | null>(null);
  const loadInvites = () => {
    if (isDirector) api('invitesList').then(setInvites, () => setInvites({ invites: [], dmConfigured: false }));
  };
  useEffect(loadInvites, []);
  const [q, setQ] = useState('');
  const [team, setTeam] = useState('');
  const [openId, setOpenId] = useState<string | null>(null);
  const [inviteOpen, setInviteOpen] = useState(false);
  const [prefill, setPrefill] = useState<{ name: string; discordId: string; subteamId: string } | null>(null);
  const [mine, setMine] = useState(() => typeof location !== 'undefined' && new URLSearchParams(location.search).get('profile') === 'me');
  const [confirm, setConfirm] = useState<Confirm | null>(null);

  const supportList = useApi('contactsList', { query: { clubSupport: 'true' } });
  const officers = list.status === 'ready' ? list.data.officers : [];
  const needle = q.trim().toLowerCase();
  const filtered = useMemo(
    () =>
      officers.filter((o) => {
        if (needle && ![o.name, o.preferredName ?? '', o.title ?? '', o.schoolEmail ?? '', o.personalEmail ?? ''].some((v) => v.toLowerCase().includes(needle))) return false;
        if (team && (o.subteam?.id ?? '') !== team) return false;
        return true;
      }),
    [officers, needle, team],
  );
  const supportAll = supportList.status === 'ready' ? supportList.data.items : [];
  const support = supportAll.filter((c) => !needle || [c.name, c.org, c.role, c.email].some((v) => v.toLowerCase().includes(needle)));

  if (list.status === 'loading') return <Loading rows={8} label="Loading officers" />;
  if (list.status === 'error') return <ErrorState error={list.error} onRetry={list.reload} />;
  const { subteams, canManage, canChangeRoles } = list.data;
  const activeRows = filtered.filter((o) => o.active);
  const ex = filtered.filter((o) => !o.active);
  const active = officers.filter((o) => o.active).length;
  const supportLoaded = supportList.status === 'ready';
  const current = officers.find((o) => o.id === openId) ?? null;
  const filtering = !!(q || team);
  const refresh = () => {
    list.reload();
    loadInvites();
  };

  return (
    <div class="stack" data-officers>
      <div class="toolbar">
        <span class="small muted" data-officer-count>
          {active} active{isDirector && officers.length > active ? ` · ${officers.length - active} ex-officers` : ''} · {subteams.length} subteams
        </span>
        <div class="toolbar-actions">
          <button class="btn" onClick={() => setMine(true)} data-open-own>
            My profile
          </button>
          {canManage && (
            <button
              class="btn btn--primary"
              data-open-invite
              onClick={() => {
                setPrefill(null);
                setInviteOpen(true);
              }}
            >
              Invite officer
            </button>
          )}
        </div>
      </div>
      {section === 'directory' && (
        <>
          <div class="toolbar">
            <div class="toolbar-main">
              <label class="field">
                <span class="field-label">Find a person</span>
                <input class="input" type="search" placeholder="Name, title or email" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} data-officer-search />
              </label>
              <label class="field">
                <span class="field-label">Subteam</span>
                <select class="select" value={team} onChange={(e) => setTeam((e.target as HTMLSelectElement).value)}>
                  <option value="">All subteams</option>
                  {subteams.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          </div>

          <div class="stack" data-directory>
              <section class="stack people-group" data-section="officers" aria-labelledby="dir-officers">
                <div class="section-head">
                  <h2 id="dir-officers">Officers</h2>
                  <span class="muted small">{activeRows.length}</span>
                </div>
                {activeRows.length === 0 ? (
                  <p class="muted small" role="status">
                    {filtering ? 'No officers match.' : canManage ? 'No officers yet. Invite the first officers: they get a Discord DM or a link, then set up their profile.' : 'Officers appear here once they’re invited.'}
                  </p>
                ) : (
                  <div class="table-wrap">
                    <table class="dt">
                      <caption class="visually-hidden">Officers</caption>
                      <thead>
                        <tr>
                          <th scope="col">Officer</th>
                          <th scope="col">Subteam</th>
                          <th scope="col">Role</th>
                          <th scope="col">Contact</th>
                        </tr>
                      </thead>
                      <tbody>
                        {activeRows.map((o) => (
                          <OfficerRow key={o.id} o={o} onOpen={() => setOpenId(o.id)} />
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </section>

              <section class="stack people-group" data-section="club-support" aria-labelledby="dir-support">
                <div class="section-head">
                  <h2 id="dir-support">Club Support Contacts</h2>
                  <span class="muted small">
                    {support.length} · <a href="/contacts">Manage in Contacts</a>
                  </span>
                </div>
                {support.length === 0 ? (
                  <p class="muted small" role="status">
                    {supportLoaded && !needle ? 'None yet. Mark an external contact as club support on the Contacts page.' : 'No club support contacts match.'}
                  </p>
                ) : (
                  <div class="table-wrap">
                    <table class="dt">
                      <caption class="visually-hidden">Club support contacts</caption>
                      <thead>
                        <tr>
                          <th scope="col">Contact</th>
                          <th scope="col">Organization</th>
                          <th scope="col">Contact info</th>
                        </tr>
                      </thead>
                      <tbody>
                        {support.map((c) => (
                          <tr key={c.id} data-support={c.id}>
                            <td data-label="Contact" class="dt-primary">
                              <span class="people-cell">
                                <a class="cell-title" href={`/contacts?id=${encodeURIComponent(c.id)}`}>{c.name}</a>
                                <span class="cell-sub">{c.role || '—'}</span>
                              </span>
                            </td>
                            <td data-label="Organization">{c.org || '—'}</td>
                            <td data-label="Contact info">
                              <span class="people-cell">
                                {c.email ? <a href={`mailto:${c.email}`}>{c.email}</a> : <span class="muted">—</span>}
                                {safeUrl(c.linkedin) && <a class="cell-sub" href={safeUrl(c.linkedin)!} target="_blank" rel="noopener noreferrer">LinkedIn</a>}
                              </span>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </section>

              {ex.length > 0 && (
                <section class="stack people-group" data-section="ex-officers" aria-labelledby="dir-ex">
                  <div class="section-head">
                    <h2 id="dir-ex">Ex-Officers</h2>
                    <span class="muted small">{ex.length}</span>
                  </div>
                  <div class="table-wrap">
                    <table class="dt">
                      <caption class="visually-hidden">Ex-officers (access ended)</caption>
                      <thead>
                        <tr>
                          <th scope="col">Former officer</th>
                          <th scope="col">Subteam</th>
                          <th scope="col">Role</th>
                          <th scope="col">Contact</th>
                        </tr>
                      </thead>
                      <tbody>
                        {ex.map((o) => (
                          <OfficerRow key={o.id} o={o} onOpen={() => setOpenId(o.id)} />
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              )}
          </div>
        </>
      )}

      {section === 'invites' && isDirector && invites && (
        <InvitesSection
          invites={invites.invites}
          dmConfigured={invites.dmConfigured}
          onChanged={refresh}
          confirm={setConfirm}
          subteams={subteams}
          onResend={(i) => {
            setPrefill({ name: i.name, discordId: i.discordId, subteamId: i.subteam?.id ?? '' });
            setInviteOpen(true);
          }}
        />
      )}

      <ProfileDrawer
        officer={current}
        me={me}
        subteams={subteams}
        canManage={canManage}
        canChangeRoles={canChangeRoles}
        onClose={() => setOpenId(null)}
        onChanged={list.reload}
        confirm={setConfirm}
      />
      {canManage && (
        <InviteDialog
          open={inviteOpen}
          onClose={() => setInviteOpen(false)}
          subteams={subteams}
          dmConfigured={invites?.dmConfigured ?? false}
          prefill={prefill}
          onSent={refresh}
        />
      )}
      <MyProfileDrawer
        open={mine}
        onClose={() => {
          setMine(false);
          if (location.search.includes('profile=me')) history.replaceState(null, '', '/officers');
        }}
        onSaved={list.reload}
      />
      <ConfirmDialog c={confirm} onClose={() => setConfirm(null)} />
    </div>
  );
}
