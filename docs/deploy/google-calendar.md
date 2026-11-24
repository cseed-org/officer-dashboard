# Google calendar setup

You come here from [step 7 of the deploy guide](README.md#7-connect-the-org-google-calendar), or
once after an [upgrade](upgrade.md) to V0.15. It connects the org Gmail (the club's own Google
account), which then owns four Google calendars: **Internal Meetings**, **External Meetings**,
**Weekly Programs** and **Events**. The dashboard shares them with every officer and keeps them in
sync. How the sync works is in [Organization calendar](../org-calendar.md).

Until you do this, the dashboard works normally: meetings save in the dashboard, nothing goes to
Google, and no invites are sent.

**You need:** owner or leadership access to the dashboard, the org Gmail and its password, and
about 30 minutes.

**Not verified here:** the Google Cloud Console steps need a real account. Google renames menus;
the settings named here stay the same.

## 1. Create the Google OAuth client

The OAuth client is how the dashboard asks Google for permission. The same client also serves
per-officer Google sync. If you already have one for this dashboard, open its project and start
at item 4.

1. Open [Google Cloud Console](https://console.cloud.google.com) **signed in as the org Gmail**,
   so the client belongs to the club and outlives any one officer.
2. **Create a project:** project picker at the top → **New project**. Name it `officer-dashboard`
   → **Create**. Select it.
3. **Google Auth Platform** → **Get started** (older consoles: **APIs & Services** → **OAuth
   consent screen**):
   - App name: `CSEED Officer Dashboard`. Support email: the org Gmail.
   - **Audience: External.** (A normal Gmail can't choose Internal; that is for Google Workspace
     organisations.)
   - Contact email: the org Gmail. Accept and **Create**.
4. **Turn on the Calendar API:** **APIs & Services** → **Library** → search **Google Calendar
   API** → **Enable**.
5. **Create the client:** **Google Auth Platform** → **Clients** → **Create client** → type **Web
   application**. Under **Authorized redirect URIs**, add both, exactly (no slash at the end):
   ```
   https://<your-hostname>/auth/google/callback
   https://<your-hostname>/auth/org-google/callback
   ```
   **Create**. Copy the **Client ID** and **Client Secret** and save them.
   (Already had a client? Open it and add the second URI.)
6. **Scopes** (the permissions the app may ask for): **Google Auth Platform** → **Data access** →
   **Add or remove scopes**. Tick these four, or paste them into **Manually add scopes**:
   ```
   openid
   https://www.googleapis.com/auth/userinfo.email
   https://www.googleapis.com/auth/calendar.events
   https://www.googleapis.com/auth/calendar
   ```
   **Update**, then **Save**. (The org account needs the full `calendar` scope to create and share
   calendars.)
7. **Publish the app:** **Google Auth Platform** → **Audience** → **Publish app** → confirm. You
   should see the publishing status **In production**. Do this: an app left in **Testing** loses
   its Google connection every 7 days. You don't need to send the app for Google's review. Until
   it is reviewed, the consent screen warns "Google hasn't verified this app" (you continue through
   **Advanced**), and at most 100 people can connect, which is enough for the officers.

You should have a Client ID ending in `.apps.googleusercontent.com` and a Client Secret.

## 2. Add the client to the dashboard

1. On the server, in `~/officer-dashboard/server/deploy`, edit `dashboard.env`. Remove the `#`
   from these two lines and fill them in:
   ```sh
   GOOGLE_OAUTH_CLIENT_ID=<Client ID>
   GOOGLE_OAUTH_CLIENT_SECRET=<Client Secret>
   ```
2. Restart the dashboard with the new settings:
   ```sh
   docker compose -f compose.cloudflare.yml up -d
   ```
3. Check, after about 30 seconds:
   ```sh
   docker compose -f compose.cloudflare.yml logs --since 2m dashboard | grep "Google Calendar sync"
   ```
   You should see nothing. `Google Calendar sync is not configured` means one of the two lines
   is still empty or commented out.

Keep this secret the same from now on: it also locks the stored Google connections. Changing
it means connecting the org account, and every officer's Google, again.

## 3. Connect the org Gmail

1. Make sure every officer added so far has an **account email** that is a Google account
   (**Officers → Access**). Calendars are shared with that address; an officer without one sees
   none. Officers you add later are shared with automatically.
2. Open a private browser window, so the org Gmail is the only Google account in it. Go to
   `https://<your-hostname>` and sign in to the dashboard as owner or leadership.
3. **Settings** → **org Google Calendar** → **Connect org Google account**.
4. Choose the org Gmail. Google may warn the app is unverified: **Advanced** → continue. Allow
   "See, edit, share and permanently delete all the calendars you can access using Google
   Calendar".

You should land back on Settings with the card saying **Connected as** the org Gmail.

If you land with an error instead:

| The page says | Why | Fix |
| --- | --- | --- |
| sign-in cancelled or failed | you cancelled, took more than 10 minutes, or someone else finished it | connect again in one go |
| Google did not grant calendar access | the calendar permission was unticked | connect again and tick it |
| `redirect_uri_mismatch` (on Google's page) | the second redirect URI is missing or misspelled | item 5 of step 1, exactly `https://<your-hostname>/auth/org-google/callback` |

## 4. Check the calendars

The first sync starts at once. It creates the four calendars, shares them, then copies in every
existing Event. Pushes of Events and programs never email anyone. Give it a few minutes.

1. **In Google Calendar, as the org Gmail:** under **My calendars** you should see the four
   calendars. **Events** holds your Events; drafts are titled `[Draft] …` on purpose.
2. **One calendar → Settings and sharing → Shared with:** you should see every active officer's
   account email, with **See all event details**.
3. **As an officer:** the four calendars should appear in their Google Calendar. It can take a
   moment, and they may need to accept Google's share email.
4. **In the dashboard:** the **Settings → org Google Calendar** card should say **Connected as**
   the org Gmail, with no warning under it.

Roster changes (a new officer, a changed email, someone deactivated) reach the sharing within
about a minute.

## 5. Try it once

Pulls from Google run every 5 minutes. With the org Gmail's Google Calendar in one window and the
dashboard in another:

- [ ] **A meeting goes to Google.** In the dashboard's **Calendar**, create an Internal meeting
  for tomorrow, invite one officer, turn **send invites** on. You should see it in Google's
  **Internal Meetings** within seconds, and the officer gets Google's invitation.
- [ ] **An edit comes back.** In Google, change that meeting's title. You should see the change in
  the dashboard within about 5 minutes.
- [ ] **Locked calendars follow the dashboard.** Change an Event's time on the **Events** page.
  You should see Google's **Events** calendar update. An edit made in Google to **Events** or
  **Weekly Programs** is undone at the next pull.

**Disconnecting:** **Settings → org Google Calendar → Disconnect** stops the sync; meetings stay in
the dashboard. Reconnect with the **same** Gmail and the same four calendars are reused. A
different Gmail gets four new calendars.

## Agents and the calendar

Officers' own AI agents use the calendar through MCP (`https://<your-hostname>/mcp`,
[MCP agents](../mcp.md#connecting)), acting as that officer. Tools: `orgcal_list`,
`orgcal_create`, `orgcal_update`, `orgcal_delete`, `programs_list`. Agents can write Internal and
External meetings only, and email guests only when they ask for it (`"sendInvites": true`).

Service tokens with `calendar:read` or `calendar:write` (**Officers → Service tokens**) work in
the API, but scripts outside can't reach `/api/orgcal/…`: it sits behind the PIN sign-in. Don't
add `api/orgcal` to the service-token application in [Cloudflare step 5](cloudflare.md#5-create-a-service-token-for-bots):
officers' browsers use the same paths and would be refused. Opening a path for scripts is a
decision for the dashboard's owners.

Problems after setup: [Troubleshooting → Google calendar](troubleshooting.md#google-calendar).

**← [Back to step 7 of the deploy guide](README.md#7-connect-the-org-google-calendar)**
