# Org calendar setup (one time)

Do this once, after a [fresh install](fresh-install.md) or an [upgrade](upgrade-existing.md).
It connects the dedicated org Gmail, which then owns four Google calendars: **Internal
Meetings**, **External Meetings**, **Weekly Programs** and **Events**. How sync works is in
[Organization calendar](../org-calendar.md); this page is the checklist.

Until this is done the dashboard works normally: meetings save locally, nothing goes to Google,
and no invites are sent.

**You need:** leadership access to the dashboard, the org Gmail's password, and edit access to
the Google Cloud project that holds the OAuth client. The buttons below (the **Calendar** page,
the **Settings → org Google Calendar** card) ship with the org-calendar front-end PRs. Without
them, each step gives the API fallback.

**Not verified here:** every Google Cloud Console and Gmail step needs the real accounts. Menu
names change; the settings they name do not.

## 1. Google Cloud: the OAuth client

The org calendar **reuses** `GOOGLE_OAUTH_CLIENT_ID` and `GOOGLE_OAUTH_CLIENT_SECRET`, the
client that per-officer Google sync uses. There are no other variables.

**If you already have the client,** open its project in
[Google Cloud Console](https://console.cloud.google.com) and start at item 3. **If not,**
create it in a project owned by the org Gmail (so it outlives any one officer):

1. **Create a project** (any name, for example `officer-dashboard`).
2. **Google Auth Platform → Get started** (older: **APIs & Services → OAuth consent screen**):
   app name, support email = the org Gmail, **Audience: External** (a consumer Gmail cannot
   choose Internal; Internal is only for Google Workspace organizations, and then only its
   users can sign in), contact email. Then **Clients → Create client → Web application**.
   Copy the client ID and secret (item 7).

Then, for both cases:

3. **APIs & Services → Library → Google Calendar API → Enable.**
4. **Clients** (or **Credentials**) → the OAuth client → **Authorized redirect URIs**. Keep the
   per-officer one and add the org one, exactly, no trailing slash:
   ```
   https://officers.example.org/auth/google/callback
   https://officers.example.org/auth/org-google/callback
   ```
   **Save.** A missing or misspelled URI shows Google's `redirect_uri_mismatch`.
5. **Data access → Add or remove scopes:** `openid`, `.../auth/userinfo.email`,
   `.../auth/calendar.events` (per-officer sync) and **`.../auth/calendar`** (the org account:
   full Calendar, needed to create and share calendars).
6. **Audience → Publishing status.** Choose one:
   - **Testing:** add the org Gmail (and each officer who uses per-officer sync) under **Test
     users** (Google allows up to 100). Google expires refresh tokens after **7 days** in
     Testing, so the org connection shows **needs reconnect** every week. Fine for a trial,
     not for production.
   - **In production (recommended):** **Publish app**. The Calendar scopes are "sensitive", so
     until Google verifies the app, consent shows an "unverified app" warning (continue via
     **Advanced**) and the app is capped at 100 users. For one org Gmail and the officers that
     is enough; verification is only needed beyond that. Google's rules change: check the
     Console's notes when you publish.
7. If the client is new, put its ID and secret in the env file (`server/deploy/dashboard.env`
   for Docker, `/etc/officer-dashboard/env` on a Pi) and restart:
   ```sh
   GOOGLE_OAUTH_CLIENT_ID=<client id>.apps.googleusercontent.com
   GOOGLE_OAUTH_CLIENT_SECRET=<secret>
   ```
   Docker (in `server/deploy`): `docker compose -f compose.cloudflare.yml up -d` recreates the
   container with the new env. Pi: `sudo systemctl restart dashboard`. Without both variables the
   log says `Google Calendar sync is not configured`, and the status in step 2 says
   `"configured":false`.

**Keep the secret stable.** It also seals the stored refresh tokens (org and per-officer).
Rotating it means connecting the org account again, and every officer reconnecting.

## 2. Connect the org Gmail

Use the **dedicated org Gmail**, not a personal account: it becomes the owner of the four
calendars.

1. Make sure every officer has an **account email** that is a Google account (**Officers →
   Access**; Discord sign-in gives none, so leadership fills it in). Sharing uses it; an
   officer without one sees no calendars. See
   [which email is which](fresh-install.md#5-create-the-owner-account).
2. Sign in to the dashboard as **leadership**, in a browser where the org Gmail is the only
   Google account signed in (or a private window), so you pick the right one at consent.
3. Open **Settings → org Google Calendar** (`/settings#org-google`) → **Connect org Google
   account**. Without the card, open
   `https://officers.example.org/api/settings/org-google/connect` in the same browser: it is
   the same link.
4. Choose the org Gmail and **allow** "See, edit, share and permanently delete all the calendars
   you can access using Google Calendar". You return to `/settings?orgGoogle=connected`.

   | Returned | Meaning | Fix |
   | --- | --- | --- |
   | `connected` | Done | — |
   | `failed` | Consent cancelled, more than 10 minutes passed, or another dashboard user finished it | Connect again, in one go, as the same leadership account |
   | `scope_required` | The full Calendar permission was unticked | Connect again and tick it |
   | `not_configured` | No Google client in the env | Step 1, item 7 |
5. Check, signed in as leadership, by opening
   `https://officers.example.org/api/settings/org-google/status`:
   ```json
   {"configured":true,"connected":true,"email":"<org gmail>","needsReconnect":false,"lastSyncError":null}
   ```

## 3. Check the calendars and sharing

The first pass starts at once: it creates the calendars, shares them, then pushes the Events
queued at the upgrade (every non-cancelled Event with a start, past ones included). Pushes of
Events and programs never email anyone (`sendUpdates=none`). There is no "done" signal: wait
until the Events calendar in Google stops filling, and check that `lastSyncError` (step 2.5)
is still `null`.

1. **In Google Calendar as the org Gmail:** under **My calendars**, the four calendars.
   **Events** holds your Events, drafts titled `[Draft] …` (by design: drafts are visible to
   officers).
2. **One calendar → Settings and sharing → Shared with:** every active officer's account
   email, **See all event details** (reader). Owner entries are left alone; readers who are not
   active officers are removed.
3. **As an officer:** the four calendars appear in their Google Calendar. A shared calendar can
   take a moment, and the officer may need to add it from Google's share email.
4. **In the dashboard, as any officer:** `https://officers.example.org/api/orgcal/calendars`
   shows `"googleConnected":true,"syncProblem":false`.

Roster changes (a new officer, a changed account email, a deactivation) reach sharing within
about a minute; a full reconcile also runs daily.

## 4. Agents: use MCP

- **MCP (use this).** An officer connects their own agent to `https://officers.example.org/mcp`
  ([MCP agents](../mcp.md#connecting)); it acts as that officer. Tools: `orgcal_list`,
  `orgcal_create`, `orgcal_update`, `orgcal_delete`, `programs_list`. Writes are limited to
  Internal/External meetings; Events and programs are read-only there. `/mcp` already passes
  Access ([deployment step 4](../deployment.md#4-let-the-agent-paths-bypass-access)).
- **Service tokens with `calendar:read` / `calendar:write`: not for outside callers yet.**
  Directors and leadership can issue them at **Officers → Service tokens**
  (`/officers?tab=tokens`), and the API accepts them (`Authorization: Bearer <token>`). But
  `/api/orgcal/*` sits behind the Access PIN application, and no Access rule lets a script
  through. Do not add `api/orgcal` to the deployment step 5 service-token application: officers'
  browsers use the same paths and would be refused. Opening a path for scripts is a Cloudflare
  Access decision for the dashboard's owners, not covered here. **Not verified here.**

Agent writes default to `sendInvites:false`: they email guests only when they send
`"sendInvites": true` explicitly.

## 5. Live checks

Do these once, with the org Gmail's Google Calendar in one window and the dashboard in
another. Pulls from Google run every **5 minutes**. Without the Calendar page, use an MCP agent
(`orgcal_create`, `orgcal_update`) for the dashboard side.

- [ ] **Internal meeting with invitees.** In the dashboard **Calendar**, create an Internal
  meeting for tomorrow, invite one officer and one subteam, turn **send invites** on (Internal
  defaults to off, External to on). It appears in Google's Internal Meetings within seconds,
  and the invitees get Google's invitation email.
- [ ] **Edit in Google, see it come back.** In Google, change that meeting's title and time.
  Within about 5 minutes the dashboard shows the change.
- [ ] **Locked calendars follow their source.** Change an Event's time on the **Events** page,
  and a session in **Buildspace + Launchpad → Programs**. Google's **Events** and **Weekly
  Programs** calendars update. An edit made in Google to a locked calendar is undone at the
  next pull.
- [ ] **RSVPs survive a dashboard edit.** Invite an address you control, accept from it, then
  change the meeting's description in the dashboard. In Google that guest still shows **Yes**.
- [ ] **Disconnect and reconnect.** **Settings → org Google Calendar → Disconnect.** Without the
  card: on any dashboard page, signed in as leadership, open the browser's developer console
  and run `fetch('/api/settings/org-google', {method: 'DELETE'}).then(r => r.json()).then(console.log)`.
  Meetings stay in the dashboard; `googleConnected` turns false; nothing syncs. **Connect**
  again with the **same** Gmail (step 2): the same four calendars are reused (no duplicates),
  and edits made meanwhile are pushed. A **different** Gmail would get four new calendars,
  leaving the old ones in the old account.

## Troubleshooting

| Symptom | Cause | What to do |
| --- | --- | --- |
| `needsReconnect: true` in the status (step 2.5); **Reconnect Google** on the Settings card | Google revoked the refresh token: the app is in Testing (7-day tokens, step 1.6), the org Gmail's password changed, or its access was removed (Google Account → Security → Third-party connections) | Connect again as the same Gmail (step 2). If it was Testing, publish the app |
| `lastSyncError: "…"` | The last sync pass failed; the log has `org calendar sync failed; retained for retry` | Jobs stay queued and retry (1 min to 1 h). Read the error: a `403` usually means the Calendar API is off (step 1.3) or the scope is missing |
| Log: `org calendar ACL entry failed calendar=… email=… action=… error=…` | Google rejected one sharing entry: the address is not a Google account, or is mistyped | Fix that officer's account email in **Officers → Access**. Others are unaffected |
| Log: `org calendar ACL sync incomplete; retrying with backoff` | The sharing pass hit the errors above | Retries on its own; fix the addresses |
| `syncProblem: true` for officers | One of the first two rows | Leadership checks the status (step 2.5) |
| No calendars after Connect | `"configured":false`, or the Calendar API is off | Step 1, items 3 and 7 |
| `redirect_uri_mismatch` at Google | The org callback URI is missing, or differs from `APP_URL` | Step 1.4: exactly `${APP_URL}/auth/org-google/callback` |

Logs: `docker compose -f compose.cloudflare.yml logs dashboard | grep "org calendar"` (in
`server/deploy`), or `sudo journalctl -u dashboard | grep "org calendar"` on a Pi. The status
flags are kept in memory: after a restart they are clear until the next pass sets them again.
