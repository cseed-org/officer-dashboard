// D3 Officers and Members prototype behaviour. Local only: no network calls, credentials
// or persisted records. Rows render from the JSON sample data that build-people.py embeds,
// so the directory, drawers, filters and confirm steps always agree with each other.
const byId = id => document.getElementById(id);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const esc = s => String(s ?? '').replace(/[&<>"]/g, c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;'}[c]));
const cap = s => s.charAt(0).toUpperCase() + s.slice(1);
const NOW = 'Nov 1, 2026, 4:20 PM';

// ---------------------------------------------------------------- Shared: viewer and permissions
const viewer = byId('viewer');
const stateChoice = byId('state-choice');
const role = () => viewer.value;
const isDirector = () => role() !== 'Officer';
const isLead = () => ['Leadership', 'Owner'].includes(role());
const isOwner = () => role() === 'Owner';

function applyPermissions() {
  $$('[data-director],[data-private]').forEach(e => e.hidden = !isDirector());
  $$('[data-leadership]').forEach(e => e.hidden = !isLead());
  $$('[data-owner]').forEach(e => e.hidden = !isOwner());
  $$('[data-officer-only]').forEach(e => e.hidden = isDirector());
}

function setUserChip(name, roleLabel) {
  document.querySelector('.user-chip-name').textContent = name;
  document.querySelector('.user-chip-role').textContent = roleLabel;
  document.querySelector('.user-chip .avatar').textContent = name.split(' ').map(w => w[0]).join('');
  document.querySelector('.user-chip').setAttribute('aria-label', name + ' profile');
}

// ---------------------------------------------------------------- Shared: dialogs and announcements
// Native modal dialogs give Escape, an inert background and focus containment. This adds the
// first-control focus and returns focus to whatever opened the dialog.
function openDialog(dialog, trigger = document.activeElement) {
  dialog.returnTo = trigger;
  dialog.showModal();
  const first = $$('input:not([type=hidden]),select,textarea,button:not([data-close])', dialog).find(e => !e.disabled && e.offsetParent);
  (first || dialog.querySelector('button'))?.focus();
}
$$('dialog').forEach(d => d.addEventListener('close', () => { if (d.returnTo?.isConnected) d.returnTo.focus(); }));
$$('[data-close]').forEach(b => b.onclick = () => b.closest('dialog').close());

// One live region at a time: the result note of the topmost open dialog, else the page notice.
function announce(message, tone = 'positive') {
  const open = $$('dialog[open]').at(-1);
  const target = open?.querySelector('[data-result]') || byId('announce');
  target.className = 'notice notice--' + tone;
  target.textContent = message;
  target.hidden = false;
}
function clearResult(dialog) { const r = dialog.querySelector('[data-result]'); if (r) { r.hidden = true; r.textContent = ''; } }

// The fallback state panel shared by both pages.
function showFallback({title, body, retry, error = false, loading = false}) {
  const panel = byId('fallback');
  panel.hidden = false;
  panel.classList.toggle('state--error', error);
  panel.setAttribute('aria-busy', String(loading));
  panel.setAttribute('role', error ? 'alert' : 'status');
  byId('state-spinner').hidden = !loading;
  byId('state-title').textContent = title;
  byId('state-body').textContent = body;
  byId('retry').hidden = !retry;
  if (retry) { byId('retry').textContent = retry.label; byId('retry').onclick = retry.run; }
}
const hideFallback = () => { byId('fallback').hidden = true; };

function rowHTML(headers, cells, attrs = '') {
  return `<tr ${attrs}>` + cells.map((v, i) => `<td data-label="${headers[i]}" class="${i === 0 ? 'dt-primary' : ''}"><span class="people-cell">${v}</span></td>`).join('') + '</tr>';
}
function tableHTML(headers, rows, caption) {
  return `<div class="table-wrap"><table class="dt"><caption class="visually-hidden">${caption}</caption><thead><tr>`
    + headers.map(h => `<th scope="col">${h}</th>`).join('') + `</tr></thead><tbody>${rows.join('')}</tbody></table></div>`;
}
function resetFilters() {
  $$('.toolbar input').forEach(e => e.value = '');
  $$('.toolbar select').forEach(e => e.selectedIndex = 0);
}

// ================================================================ Officers page
if (byId('officer-data')) {
  const data = JSON.parse(byId('officer-data').textContent);
  const people = data.officers;
  const owners = new Set(data.owners);
  const find = id => people.find(p => p.id === id);
  const me = () => find(data.viewers[role()]);
  const badge = r => `<span class="role-badge" data-role="${r}">${cap(r)}</span>`;
  const ownerBadge = '<span class="badge badge--neutral">Owner</span>';
  const statusBadge = s => s === 'Active' ? '<span class="badge badge--positive">Active</span>' : '<span class="badge badge--neutral">Deactivated</span>';
  let current = null;

  function matches(p) {
    const q = byId('query').value.trim().toLowerCase();
    const team = byId('team-filter').value, r = byId('role-filter').value, st = byId('status-filter').value;
    if (q && ![p.name, p.pref, p.handle].some(v => v.toLowerCase().includes(q))) return false;
    if (team !== 'All subteams' && p.team !== team) return false;
    if (r === 'Owner' ? !owners.has(p.id) : r !== 'All roles' && p.role !== r.toLowerCase()) return false;
    if (st !== 'All statuses' && p.status !== st) return false;
    return true;
  }

  function renderDirectory() {
    const headers = ['Officer', 'Role', 'Status'];
    let html = '', shown = 0;
    for (const team of data.teams) {
      const rows = people.filter(p => p.team === team && matches(p));
      if (!rows.length) continue;
      shown += rows.length;
      html += `<section class="stack people-group" data-team="${team}"><div class="section-head"><h2>${team}</h2><span class="muted small">${rows.length} ${rows.length === 1 ? 'officer' : 'officers'}</span></div>`
        + tableHTML(headers, rows.map(p => rowHTML(headers, [
          `<button class="linkish cell-title" data-person="${p.id}">${esc(p.name)}</button><span class="cell-sub">@${esc(p.handle)} · ${esc(p.title)}</span>`,
          badge(p.role) + (owners.has(p.id) ? ' ' + ownerBadge : ''),
          statusBadge(p.status),
        ], `data-id="${p.id}"`)), team + ' officers') + '</section>';
    }
    const dir = byId('directory');
    dir.innerHTML = html;
    dir.hidden = !shown;
    $$('[data-person]', dir).forEach(b => b.onclick = () => openProfile(b.dataset.person, b));
    const active = people.filter(p => p.status === 'Active').length;
    byId('officer-count').textContent = `${active} active · ${people.length - active} deactivated · ${data.teams.length} subteams · Fall 2026`;
    if (shown) hideFallback();
    else showFallback({title: 'No matching officers', body: 'Try another name or handle, or clear the filters.', retry: {label: 'Clear filters', run: () => { resetFilters(); renderDirectory(); }}});
  }

  function applyState() {
    const value = stateChoice.value;
    byId('invites').hidden = !isDirector() || value !== 'Ready';
    if (value === 'Ready') return renderDirectory();
    byId('directory').hidden = true;
    if (value === 'No officers yet') showFallback({
      title: 'It’s just you so far',
      body: 'Alex Chen set up this workspace as its owner. Invite your officers and they appear here once they sign in with Discord.',
      retry: isDirector() ? {label: 'Invite officer', run: e => openDialog(byId('invite'), e.currentTarget)} : null,
    });
    if (value === 'Loading') showFallback({title: 'Loading officers…', body: 'Fetching the directory.', loading: true});
    if (value === 'Error') showFallback({title: 'Couldn’t load the directory', body: 'The dashboard server didn’t respond. Your filters are kept.', error: true, retry: {label: 'Try again', run: () => { stateChoice.value = 'Ready'; applyState(); }}});
  }

  function refreshViewer() {
    applyPermissions();
    const p = me();
    setUserChip(p.name, cap(p.role) + (owners.has(p.id) && isOwner() ? ' · Owner' : ''));
    applyState();
  }

  // ---- Profile drawer
  function openProfile(id, trigger) {
    current = find(id);
    fillProfile();
    clearResult(byId('profile'));
    if (trigger) openDialog(byId('profile'), trigger);
  }

  function fillProfile() {
    const p = current, self = p.id === me().id, owner = owners.has(p.id), active = p.status === 'Active';
    applyPermissions();
    byId('pf-role').innerHTML = badge(p.role) + (owner ? ' ' + ownerBadge : '');
    byId('pf-status').innerHTML = statusBadge(p.status);
    byId('pf-name').textContent = p.name + (self ? ' (you)' : '');
    byId('pf-sub').textContent = `${p.team} · ${p.pronouns} · Officer since ${p.since}`;
    const set = (key, value) => byId(key).textContent = value || '—';
    set('pf-discord', `@${p.handle} · ${p.discord}`); set('pf-phone', p.phone); set('pf-birthday', p.birthday);
    set('pf-pref', p.pref); set('pf-title', p.title); set('pf-personal', p.personal); set('pf-school', p.school);
    set('pf-grad', p.grad); set('pf-linkedin', p.linkedin); set('pf-since', p.since);

    // Workspace access: leadership only, never on your own record.
    const access = byId('pf-access'), note = byId('pf-access-note'), select = byId('new-role'), deactivate = byId('deactivate');
    access.hidden = !isLead() || self;
    note.hidden = true;
    if (role() === 'Director') { note.hidden = false; note.textContent = 'Leadership manages roles and deactivation.'; }
    if (isLead() && self) { note.hidden = false; note.textContent = 'You can’t change your own role or deactivate yourself. Ask another leadership member.'; }
    select.value = cap(p.role);
    const ownerLocked = owner && !isOwner();
    select.disabled = ownerLocked || !active;
    deactivate.textContent = active ? 'Deactivate officer' : 'Reactivate officer';
    deactivate.classList.toggle('btn--danger', active);
    deactivate.disabled = ownerLocked || (owner && active);
    byId('role-hint').textContent = ownerLocked ? 'Only an owner can change an owner’s role or deactivate them.'
      : owner && active ? 'Remove owner status before deactivating an owner.'
      : !active ? 'Reactivate this officer to change their role.'
      : 'Choose a different role to review the change.';
    byId('review-role').disabled = true;

    // Owner controls: owner only.
    byId('pf-owner').hidden = !isOwner();
    const sole = owner && owners.size === 1;
    byId('make-owner').hidden = owner; byId('transfer').hidden = owner; byId('remove-owner').hidden = !owner;
    byId('make-owner').disabled = byId('transfer').disabled = !active;
    byId('remove-owner').disabled = sole;
    byId('owner-hint').textContent = sole ? `${p.name} is the only owner. Add a co-owner before removing owner status.`
      : owner ? `${[...owners].filter(o => o !== p.id).map(o => find(o).name).join(', ')} would remain owner.`
      : !active ? 'Reactivate this officer before granting ownership.'
      : 'A co-owner gets leadership access and every owner control.';
  }

  byId('new-role').onchange = () => {
    const changed = byId('new-role').value.toLowerCase() !== current.role;
    byId('review-role').disabled = !changed;
    byId('role-hint').textContent = changed ? `${cap(current.role)} → ${byId('new-role').value}` : 'Choose a different role to review the change.';
  };

  // ---- Confirm steps (one centred dialog, filled per action)
  function confirmStep({title, body, action, danger = false, run, done}) {
    byId('confirm-title').textContent = title;
    byId('confirm-body').textContent = body;
    const go = byId('confirm-go');
    go.textContent = action;
    go.classList.toggle('btn--danger', danger);
    go.onclick = () => {
      run();
      byId('confirm').close();
      if (stateChoice.value === 'Ready') renderDirectory();
      if (byId('profile').open) fillProfile();
      announce(done + ` Audit entry recorded: ${me().name} · ${NOW}.`);
    };
    openDialog(byId('confirm'));
  }

  byId('review-role').onclick = () => {
    const p = current, next = byId('new-role').value;
    confirmStep({
      title: `Change ${p.pref}’s role?`, action: 'Change role',
      body: `${p.name} goes from ${cap(p.role)} to ${next}. Their access changes the next time they load a page.` + (next === 'Leadership' ? ' Leadership can manage roles and deactivate officers.' : ''),
      run: () => { p.role = next.toLowerCase(); }, done: `${p.name} is now ${next}.`,
    });
  };
  byId('deactivate').onclick = () => {
    const p = current;
    if (p.status === 'Active') confirmStep({
      title: `Deactivate ${p.pref}?`, action: 'Deactivate officer', danger: true,
      body: `${p.name} loses workspace access immediately and their open tasks become unassigned. Past activity stays attributed to them. You can reactivate them later.`,
      run: () => { p.status = 'Deactivated'; }, done: `${p.name} is deactivated.`,
    });
    else confirmStep({
      title: `Reactivate ${p.pref}?`, action: 'Reactivate officer',
      body: `${p.name} gets workspace access back as ${cap(p.role)} the next time they sign in.`,
      run: () => { p.status = 'Active'; }, done: `${p.name} is active again.`,
    });
  };
  byId('make-owner').onclick = () => {
    const p = current;
    confirmStep({
      title: `Make ${p.pref} a co-owner?`, action: 'Make co-owner',
      body: `${p.name} gets every owner control, including transferring ownership.` + (p.role !== 'leadership' ? ` Their role also becomes Leadership, because owners have leadership access.` : ''),
      run: () => { owners.add(p.id); p.role = 'leadership'; }, done: `${p.name} is now a co-owner.`,
    });
  };
  byId('transfer').onclick = () => {
    const p = current;
    confirmStep({
      title: `Transfer ownership to ${p.pref}?`, action: 'Transfer ownership', danger: true,
      body: `${p.name} becomes the owner with leadership access. You lose owner status and stay Leadership. Only ${p.pref} can make you an owner again.`,
      run: () => {}, done: `Preview only: ownership would move to ${p.name}. The prototype keeps you as owner so you can keep exploring.`,
    });
  };
  byId('remove-owner').onclick = () => {
    const p = current, rest = [...owners].filter(o => o !== p.id).map(o => find(o).name).join(', ');
    confirmStep({
      title: `Remove ${p.pref}’s owner status?`, action: 'Remove owner status', danger: true,
      body: `${p.name} stays ${cap(p.role)} but loses owner controls. ${rest} remains owner.`,
      run: () => { owners.delete(p.id); }, done: `${p.name} is no longer an owner.`,
    });
  };

  // ---- Invite dialog
  const inv = {name: byId('invite-name'), team: byId('invite-team'), discord: byId('invite-discord'), dm: byId('dm'), link: byId('link'),
    send: byId('send-invite'), status: byId('invite-status'), error: byId('invite-error'), summary: byId('invite-summary')};
  const statusText = t => inv.status.querySelector('.invite-status-text').innerHTML = t;
  const first = () => inv.name.value.trim().split(' ')[0] || 'them';
  let inviteStage = 'ready';

  function resetInvite(clear) {
    inviteStage = 'ready';
    if (clear) { inv.name.value = ''; inv.discord.value = ''; }
    [inv.name, inv.team, inv.discord, inv.dm, inv.link].forEach(e => e.disabled = false);
    inv.summary.hidden = true; inv.error.hidden = true; byId('invite-link').hidden = true; byId('invite-copied').hidden = true;
    inv.status.className = 'invite-status'; inv.status.querySelector('.spinner').hidden = true;
    inv.send.textContent = 'Send invitation'; byId('invite-done').hidden = true; byId('invite-cancel').hidden = false;
    readyText();
  }
  function readyText() {
    if (inviteStage !== 'ready') return;
    const none = !inv.dm.checked && !inv.link.checked;
    inv.send.disabled = none;
    byId('delivery-hint').textContent = none ? 'Choose at least one delivery method.' : 'Choose one or both.';
    statusText(none ? 'Choose how to send the invitation.' : `Ready to invite ${esc(first())} to ${esc(inv.team.value)}.`);
  }
  inv.name.addEventListener('input', readyText);
  [inv.team, inv.dm, inv.link].forEach(e => e.addEventListener('change', readyText));

  inv.send.onclick = () => {
    if (inviteStage === 'sent') { resetInvite(true); inv.name.focus(); return; }
    if (!inv.name.value.trim() || !/^\d{17,20}$/.test(inv.discord.value.trim())) {
      (inv.name.value.trim() ? inv.discord : inv.name).reportValidity?.();
      statusText('Add a name and a 17–20 digit Discord ID.');
      return;
    }
    inviteStage = 'sending';
    [inv.name, inv.team, inv.discord, inv.dm, inv.link].forEach(e => e.disabled = true);
    const ways = [inv.dm.checked && 'Discord DM', inv.link.checked && 'Copy link'].filter(Boolean).join(' and ');
    inv.summary.innerHTML = [['Name', inv.name.value], ['Discord ID', inv.discord.value], ['Subteam', inv.team.value], ['Delivery', ways]]
      .map(([k, v]) => `<div><dt>${k}</dt><dd>${esc(v)}</dd></div>`).join('');
    inv.summary.hidden = false; inv.error.hidden = true;
    inv.status.className = 'invite-status'; inv.status.querySelector('.spinner').hidden = false;
    statusText(inv.dm.checked ? `Sending DM to ${esc(first())}…` : 'Creating invitation link…');
    inv.send.disabled = true; inv.send.textContent = 'Sending…';
    setTimeout(() => {
      inv.status.querySelector('.spinner').hidden = true; inv.send.disabled = false;
      if (inv.dm.checked && byId('sim-dm-fail').checked) {
        inviteStage = 'failed';
        inv.status.className = 'invite-status';
        statusText('Invitation created. The link below works now.');
        inv.error.querySelector('.invite-status-text').textContent = `Discord couldn’t deliver the DM: ${first()} doesn’t accept DMs from server members. Ask them to allow DMs from the CSEED server and retry, or share the link below.`;
        inv.error.hidden = false; byId('invite-link').hidden = false; inv.send.textContent = 'Retry DM';
        return;
      }
      inviteStage = 'sent';
      inv.status.className = 'invite-status invite-status--sent';
      statusText(inv.dm.checked ? 'DM sent ✓ <time datetime="2026-11-01T16:12">Nov 1, 4:12 PM</time>' : 'Invitation link ready ✓');
      byId('invite-link').hidden = !inv.link.checked && inviteStage !== 'failed';
      inv.send.textContent = 'Invite another'; byId('invite-done').hidden = false; byId('invite-cancel').hidden = true;
    }, 700);
  };
  byId('copy-invite').onclick = () => { byId('invite-copied').hidden = false; };
  byId('invite').addEventListener('close', () => {
    if (inviteStage === 'sent') announce(`Invitation sent to ${inv.name.value}. They appear under Invites until they sign in.`);
    if (inviteStage !== 'ready') resetInvite(false);
  });

  // Invite list actions
  $$('[data-invite-action]').forEach(b => b.onclick = () => {
    const who = b.dataset.invitee, row = b.closest('tr'), a = b.dataset.inviteAction;
    if (a === 'Resend') announce(`DM resent to ${who} ✓ Nov 1, 4:20 PM. The invite now expires Nov 8.`);
    if (a === 'Copy link') announce(`Invite link for ${who} copied.`);
    if (a === 'Retry DM') announce(`The DM to ${who} still couldn’t be delivered: they don’t accept server DMs. Share the invite link instead.`, 'attention');
    if (a === 'Remove') { row.remove(); announce(`Removed the expired invite for ${who}.`); }
    if (a === 'Revoke') confirmStep({
      title: `Revoke ${who.split(' ')[0]}’s invite?`, action: 'Revoke invite', danger: true,
      body: `The invite link stops working and the Discord DM’s button shows as expired. You can invite ${who} again later.`,
      run: () => { row.querySelector('[data-label="Status"] .people-cell').innerHTML = '<span class="badge badge--neutral">Revoked</span>'; row.querySelector('[data-label="Actions"] .people-cell').textContent = '—'; },
      done: `Invite for ${who} revoked.`,
    });
  });

  // ---- My profile: calendar email picker
  const savedEmails = {};
  function emailsFor(p) {
    const f = p.pref.toLowerCase();
    return savedEmails[p.id] ??= {list: [p.school, p.personal, `${f}@cseed.co`, `${f}.work@example.com`, `${f}.calendar@example.com`], chosen: p.school};
  }
  function renderCalendar() {
    const p = me(), store = emailsFor(p), full = store.list.length >= 5;
    byId('cal-owner').textContent = `${p.name} · Your calendar settings`;
    byId('email-select').innerHTML = store.list.map(e => `<option ${e === store.chosen ? 'selected' : ''}>${esc(e)}</option>`).join('')
      + `<option value="__add" ${full ? 'disabled' : ''}>Add another…</option>`;
    byId('email-note').textContent = full ? '5 of 5 saved. Remove one below to add another.' : `${store.list.length} of 5 saved.`;
    byId('saved-emails').innerHTML = store.list.map(e => `<li class="list-row"><span class="list-main">${esc(e)}${e === store.chosen ? '<span class="cell-sub">Calendar email</span>' : ''}</span>`
      + `<button class="linkish linkish--danger" data-remove-email="${esc(e)}" aria-label="Remove ${esc(e)}" ${store.list.length === 1 ? 'disabled' : ''}>Remove</button></li>`).join('');
    $$('[data-remove-email]').forEach(b => b.onclick = () => {
      store.list = store.list.filter(e => e !== b.dataset.removeEmail);
      if (store.chosen === b.dataset.removeEmail) store.chosen = store.list[0];
      renderCalendar(); byId('email-select').focus();
      announce(`Removed ${b.dataset.removeEmail}. You can add another address.`);
    });
  }
  byId('email-select').onchange = e => {
    const store = emailsFor(me());
    if (e.target.value !== '__add') { store.chosen = e.target.value; return renderCalendar(); }
    const address = 'alex.new@example.com'.replace('alex', me().pref.toLowerCase());
    store.list.push(address); store.chosen = address; renderCalendar();
    announce(`Prototype: added ${address}. Production shows an inline add form here (see the shared picker spec).`, 'info');
  };
  byId('delivery').onchange = e => byId('delivery-note').textContent = e.target.selectedIndex
    ? 'Google Calendar isn’t connected yet. Saving takes you to Google to allow calendar access; events then sync to this address.'
    : 'Event invitations arrive as .ics email attachments at the address above.';
  byId('save-calendar').onclick = () => announce(`Calendar preferences saved. Invitations go to ${emailsFor(me()).chosen} ${byId('delivery').selectedIndex ? 'through Google Calendar sync' : 'as .ics attachments'}.`);
  $$('[data-open-own]').forEach(b => b.onclick = () => { renderCalendar(); clearResult(byId('own-profile')); openDialog(byId('own-profile'), b); });

  $$('[data-open="invite"]').forEach(b => b.onclick = () => { resetInvite(false); openDialog(byId('invite'), b); });
  byId('query').oninput = renderDirectory;
  ['team-filter', 'role-filter', 'status-filter'].forEach(id => byId(id).onchange = renderDirectory);
  viewer.onchange = refreshViewer;
  stateChoice.onchange = applyState;
  refreshViewer();
}

// ================================================================ Members page
if (byId('member-data')) {
  const data = JSON.parse(byId('member-data').textContent);
  const members = data.members;
  const headers = ['Member', 'UW / CS email', 'Studies', 'Joined', 'Past officer'];
  const fieldLabels = {name: 'Name', pref: 'Preferred name', uw: 'UW email', cs: 'CS email', major: 'Major', grad: 'Graduation year', campus: 'Campus', joined: 'Join date', officer: 'Has been officer'};
  const fmtDate = iso => new Date(iso + 'T12:00:00Z').toLocaleDateString('en-US', {month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC'});
  const show = (key, v) => !v ? 'Not provided' : key === 'joined' ? fmtDate(v) : v;
  const viewerName = () => data.viewers[role()];
  let page = 1, current = null;
  const syncFailed = () => stateChoice.value === 'Sync failed';
  const filtered = () => byId('query').value.trim() || ['campus-filter', 'grad-filter', 'history-filter'].some(id => byId(id).selectedIndex > 0);

  function matches(m) {
    const q = byId('query').value.trim().toLowerCase();
    const campus = byId('campus-filter').value, grad = byId('grad-filter').value, history = byId('history-filter').selectedIndex;
    if (q && ![m.name, m.pref, m.uw, m.cs].some(v => v.toLowerCase().includes(q))) return false;
    if (campus !== 'All campuses' && m.campus !== campus) return false;
    if (grad !== 'All years' && m.grad !== grad) return false;
    if (history && m.officer !== (history === 1 ? 'Yes' : 'No')) return false;
    return true;
  }

  function renderTable() {
    const rows = members.filter(matches);
    byId('member-table').innerHTML = tableHTML(headers, rows.map(m => rowHTML(headers, [
      `<button class="linkish cell-title" data-member="${m.id}">${esc(m.name)}</button><span class="cell-sub">Preferred: ${esc(m.pref)}</span>`,
      `${esc(m.uw)}<span class="cell-sub">CS: ${esc(m.cs || 'Not provided')}</span>`,
      `${esc(m.major)}<span class="cell-sub">Class of ${m.grad} · ${m.campus}</span>`,
      fmtDate(m.joined), m.officer,
    ])), 'Members from website signups');
    $$('[data-member]').forEach(b => b.onclick = () => openMember(b.dataset.member, b));
    const total = data.total, pages = Math.ceil(total / data.pageSize);
    byId('page-label').textContent = filtered()
      ? `${rows.length} matching in this sample · the server returns the full count and pages`
      : `${(page - 1) * 25 + 1}–${Math.min(page * 25, total)} of 1,086 members · Page ${page} of ${pages}`;
    byId('previous').disabled = filtered() || page === 1;
    byId('next').disabled = filtered() || page === pages;
    byId('directory').hidden = !rows.length;
    if (rows.length) hideFallback();
    else showFallback({title: 'No members match', body: 'Try a different name or email, or clear the filters.', retry: {label: 'Clear filters', run: () => { resetFilters(); renderTable(); }}});
  }

  const syncOK = '<strong>Daily sync · Up to date</strong><br>Last pulled Nov 1, 2026, 3:00 AM PT · 1,086 people.<br><span class="small">Next pull Nov 2, 3:00 AM PT. Reads use the local mirror; confirmed edits write to Cloudflare D1 and refresh the mirror.</span>';
  function setSync(tone, html) { const s = byId('sync-status'); s.className = 'notice notice--' + tone; s.innerHTML = html; }

  function applyState() {
    const value = stateChoice.value;
    byId('export').disabled = value === 'Not connected';
    setSync('info', syncOK);
    if (value === 'Ready' || value === 'Empty') {
      if (value === 'Empty') { resetFilters(); byId('query').value = 'quantum'; }
      return renderTable();
    }
    if (value === 'Sync failed') {
      setSync('attention', '<strong>Daily sync failed · Showing data from Oct 31</strong><br>Today’s 3:00 AM pull was rejected because the Cloudflare API token has expired. The records below are from the last successful pull, Oct 31, 2026, 3:00 AM PT, and may be out of date.<br><span class="small">Edits are paused until leadership updates the token in Admin.</span>'
        + (isDirector() ? ' <button class="btn btn--small" id="retry-sync">Retry sync</button>' : ''));
      byId('retry-sync')?.addEventListener('click', () => { stateChoice.value = 'Ready'; applyState(); announce('Sync complete · 1,086 people · Nov 1, 2026, 4:22 PM.'); });
      return renderTable();
    }
    byId('directory').hidden = true;
    if (value === 'Loading') showFallback({title: 'Loading members…', body: `Fetching page ${page} from the mirror.`, loading: true});
    if (value === 'Page error') showFallback({title: 'This page didn’t load', body: 'The dashboard server didn’t respond. Your search and filters are kept.', error: true, retry: {label: 'Try again', run: () => { stateChoice.value = 'Ready'; applyState(); }}});
    if (value === 'Not connected') {
      setSync('attention', '<strong>Cloudflare D1 · Not connected</strong><br>No credentials are configured, so the daily pull is skipped.');
      showFallback({title: 'No member data yet', body: 'Members mirror the website’s People table in Cloudflare D1. Leadership can connect Cloudflare in Admin; the first pull runs right after.'});
    }
  }

  // ---- Member drawer and D1 edit
  const inputs = $$('[data-field]');
  function fillMember() {
    const m = current, set = (id, v) => byId(id).textContent = v;
    set('md-name', m.name); set('md-sub', `${m.major} · Class of ${m.grad} · ${m.campus}`);
    for (const k of ['pref', 'uw', 'cs', 'major', 'grad', 'campus', 'joined', 'officer']) set('md-' + k, show(k, m[k]));
    set('md-source', `cseed-membership / People · ${m.id}`);
    set('md-mirror', `Signed up through the membership form on ${fmtDate(m.joined)}. Mirrored ${syncFailed() ? 'Oct 31' : 'Nov 1'}, 3:00 AM PT.`);
    inputs.forEach(i => { i.value = m[i.dataset.field]; i.disabled = syncFailed(); });
    byId('audit-note').value = ''; byId('audit-note').disabled = syncFailed();
    byId('edit-warning').textContent = syncFailed() ? 'Editing is paused: the last sync failed, so this record may be out of date.' : 'Saving writes to Cloudflare D1, the source for the public membership site.';
    updateReview();
  }
  function openMember(id, trigger) {
    current = members.find(m => m.id === id);
    applyPermissions(); fillMember(); clearResult(byId('member'));
    openDialog(byId('member'), trigger);
  }
  const diffs = () => inputs.filter(i => i.value.trim() !== current[i.dataset.field]).map(i => ({key: i.dataset.field, from: current[i.dataset.field], to: i.value.trim()}));
  function updateReview() {
    const n = diffs().length, note = byId('audit-note').value.trim();
    byId('review-changes').disabled = syncFailed() || !n || !note;
    byId('review-hint').textContent = syncFailed() ? 'Editing is paused until sync recovers.'
      : !n ? 'Change a field to review.' : !note ? `${n} ${n === 1 ? 'field' : 'fields'} changed. Add an audit note to review.`
      : `${n} ${n === 1 ? 'field' : 'fields'} changed. Review before writing to D1.`;
  }
  [...inputs, byId('audit-note')].forEach(i => i.addEventListener('input', updateReview));
  inputs.forEach(i => i.addEventListener('change', updateReview));

  byId('review-changes').onclick = e => {
    const list = diffs();
    if (!list.length) return;
    byId('d1-target').innerHTML = `Update <strong>${esc(current.name)}</strong> (${current.id}) in <span class="mono">cseed-membership → People</span>? Only these fields change:`;
    byId('d1-diff').innerHTML = list.map(d => `<tr><td data-label="Field" class="dt-primary">${fieldLabels[d.key]}</td><td data-label="Current">${esc(show(d.key, d.from))}</td><td data-label="New"><strong>${esc(show(d.key, d.to))}</strong></td></tr>`).join('');
    byId('d1-audit').textContent = `Audit note: “${byId('audit-note').value.trim()}”`;
    byId('d1-actor').textContent = `Recorded as ${viewerName()} · ${NOW}. If the write fails, nothing is saved and your edits stay in the form.`;
    byId('d1-error').hidden = true;
    byId('write-d1').disabled = false; byId('write-d1').textContent = 'Confirm and write to D1';
    openDialog(byId('d1-confirm'), e.currentTarget);
  };
  byId('write-d1').onclick = () => {
    const list = diffs();
    if (!list.length) return;
    if (byId('sim-d1-fail').checked) { byId('d1-error').hidden = false; byId('write-d1').textContent = 'Try again'; return; }
    list.forEach(d => current[d.key] = d.to);
    byId('d1-confirm').close();
    renderTable(); fillMember();
    announce(`Saved to D1 and refreshed the mirror. Changed ${list.map(d => fieldLabels[d.key]).join(', ')}. Audit entry: ${viewerName()} · ${NOW}.`);
  };

  byId('export').onclick = () => announce(`members-2026-11-01.csv is ready: all ${filtered() ? 'matching' : '1,086'} members with the current filters, not just this page. The export is recorded in the audit log.`, 'info');
  byId('next').onclick = () => { page++; renderTable(); };
  byId('previous').onclick = () => { page--; renderTable(); };
  byId('query').oninput = () => { page = 1; renderTable(); };
  ['campus-filter', 'grad-filter', 'history-filter'].forEach(id => byId(id).onchange = () => { page = 1; renderTable(); });
  viewer.onchange = () => { applyPermissions(); setUserChip(viewerName(), role() === 'Owner' ? 'Leadership · Owner' : role()); applyState(); };
  stateChoice.onchange = applyState;
  viewer.onchange();
}
