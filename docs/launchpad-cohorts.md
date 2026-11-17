# Launchpad cohorts, portals and mentor assignment (V0.12)

This extends [Buildspace + Launchpad](buildspace.md). It adds:

- cohorts;
- email invites for builders and mentors;
- builder and mentor portals with their own sign-in;
- a mentor roster with availability, capacity and even auto-assignment;
- archive, restore and delete;
- bulk invite;
- automatic linking to member records.

## Goals

1. Officers create a **cohort** (for example "Winter 2027") and work inside it. The register defaults to the newest active cohort, and older cohorts stay readable.
2. Officers paste a list of emails (or `name,email` CSV) to **invite builders** to a cohort. Each person gets a single-use, expiring link by email (or a copyable link when SMTP is off).
3. A builder opens the link, signs in with Discord and **onboards**. They enter their name, pronouns, designation (their role on the team, such as "Hardware lead") and a short bio, then either start a project or join one already in the cohort. They then have their **own dashboard** with their project, team, mentors, weekly check-in and timeline.
4. Officers keep a **mentor roster**. Mentors are External Contacts of type mentor/both. Each mentor can be invited to a **mentor portal** (their own account), marked available or unavailable, and given a project cap. Mentors are assigned to projects manually or with **Auto-assign**, which spreads unmentored projects evenly across available mentors.
5. Projects and builders can be **archived and restored**. They can be **deleted** only when nothing references them, so history is never lost by accident.
6. Builders' member records are **linked automatically** by email when they onboard, and officers get a member search picker instead of typing IDs.

Non-goals for this version: more than one project per builder, project transfers that carry history, mentor self-sign-up, and email reminders.

## Identity model

**Portal accounts are not users.** Builders and mentors live in `buildspace_launchpad_accounts` with their own session table and cookie. Officer sessions only ever resolve `users` rows, and every officer API is still guarded by `RequiredRoleForPath` (officer or above). A portal account therefore cannot reach officer data by construction, even if a portal handler is buggy. This is a stronger guarantee than adding `builder`/`mentor` roles to `users`: that table is listed in Officers, counted by the last-director trigger, used by calendar and finance foreign keys, and `c.officer()` only checks that a user is present.

| | Officer (users) | Builder account | Mentor account |
| --- | --- | --- | --- |
| Sign-in | Discord, allowlisted | Discord, bound by email invite | Discord, bound by email invite |
| Cookie | `cseed_session` | `cseed_launchpad` (host-prefixed in production) | same |
| APIs | `/api/*` except `/api/portal` | `/api/portal/*` (builder) | `/api/portal/*` (mentor) |
| Pages | officer dashboard | `/launchpad` | `/launchpad` |
| Scope | everything | own cohort, own project, `team` activity | assigned, non-archived projects; `team` + `mentors` activity |

One person can hold an officer session and a portal session in the same browser. The cookies are independent, and `/launchpad` shows a link back to the officer dashboard when an officer session also exists.

### Invite → bind → session

1. An officer invites `ada@uw.edu` to a cohort. The server stores the account (`status=invited`) with `sha256(token)` and an expiry 14 days out. It emails `${APP_URL}/launchpad/join?token=<token>` in the background through the existing SMTP mailer (`cfg.Mailer`, the same relay as .ics invites). Delivery outcome is stored on the account (`invite_sent_at` / `invite_error`). When SMTP is not configured, or `method` is `link`/`both`, the response returns the link once for copying. Tokens are never stored or shown again; **Resend** issues a new one. Through an officer's MCP agent ([mcp.md](mcp.md)) invites go by email only and the link is never returned. The hash stays on the account after acceptance so the join page can say "already used"; binding requires `status=invited`, so it cannot be replayed.
2. `/launchpad/join` calls `GET /api/portal/invite?token=…` (public, rate-limited with the auth limiter). It shows the cohort name, role (builder/mentor), the masked email, and whether the link is still valid.
3. **Continue with Discord** goes to `/auth/login?launchpad=<token>`. This stores `sha256(token)` in a 10-minute host-prefixed cookie, just like officer invites.
4. The OAuth callback handles a `launchpad` cookie **before** the officer path. It finds the invited account by token hash and checks it is unexpired with `status=invited`. It then binds the Discord ID, sets `status=active` and `accepted_at`, creates a portal session and redirects to `/launchpad`. Failures redirect to `/launchpad/join?error=<reason>`: `invalid` (unknown token), `expired`, `used` (already accepted by another Discord account; the same account just signs in again), `disabled` (account disabled or cohort archived), `discord_taken`, or `failed` (server error).
   - Discord ID already bound to another account:
     - An **active** builder account in an **active** cohort, or any mentor account for a mentor invite: refused (`discord_taken`).
     - A builder account in an **archived** cohort: that old account is detached (`discord_id = NULL`, `status = disabled`) and the new one is bound. Returning builders keep their history under the old cohort.
   - Discord ID belongs to an officer `users` row: allowed. They are separate identities.
   - Because `discord_id` is unique across portal accounts, one Discord account cannot be both a builder and a mentor.
5. Later sign-ins happen at `/launchpad/login`, which uses `/auth/login?portal=1`. The callback looks up an active account by Discord ID; failures redirect to `/launchpad/login?error=not_found|disabled|archived`. The ordinary `/login` also falls through to a portal session when the Discord ID has no officer account but has an active portal account, so people who pick the wrong button still get in.
6. Disabling an account, archiving a builder (or their project), or deleting either deletes that account's portal sessions. The next request answers 401. While a builder is archived, sign-in is refused and any session answers 403 `archived`; the account's status is left unchanged so restoring brings access back.

Portal sessions use the same TTL, sliding renewal and absolute lifetime as officer sessions. Unsafe portal requests use the same Origin check (`originOK`). `/api/portal` is listed alongside the service-token prefixes so `RequiredRoleForPath` returns no officer requirement. It is also added to `onboardingOpenPath`, because portal handlers never consult the officer identity. `RedirectSections` gains `launchpad`.

## Data model (migration `0024_launchpad_cohorts.sql`)

- `buildspace_launchpad_cohorts(id, name UNIQUE, starts_on, ends_on?, status active|archived, created_at, updated_at)`.
- Projects and people gain `cohort_id` (backfilled to a generated **Cohort 1** when data exists) and `archived_at`. Triggers require a cohort on insert, fix a project's cohort for life, and keep each builder's cohort equal to their project's.
- People gain `designation`, `pronouns`, `bio` and `email`.
- Activity gains `visibility` (`team` | `mentors` | `officers`) and `author_account_id`. Existing rows stay private (`officers`) except builders' weekly updates and absences (`team`) and mentor check-ins (`mentors`).
- `buildspace_launchpad_mentor_settings(contact_id, available, max_projects?)`. No row means available with no cap.
- `buildspace_launchpad_accounts`: kind builder|mentor; lowercased email; builder → `cohort_id` (+ `person_id` after onboarding); mentor → `contact_id`; unique `discord_id`; status invited|active|disabled; invite token hash/expiry/delivery fields. Constraints: one builder invite per `(cohort, email)`, one account per contact and per person, and an active account must carry a Discord ID.
- `buildspace_launchpad_sessions(id = sha256(token), account_id, expires_at, created_at, user_agent)`.

### Visibility

| Entry author | Default | Choices |
| --- | --- | --- |
| Builder (portal) | team | team |
| Mentor (portal) | mentors | team, mentors |
| Officer (dashboard) | officers | team, mentors, officers |
| Discord ingest | team | — |

Builders read `team` entries on their own project. Mentors read `team` and `mentors` entries on projects they are assigned to. Officers read everything. Linked Finance purchase titles and statuses are only shown to officers.

## Lifecycle rules

- **Cohort**: create, rename and archive need director access. Archiving makes the cohort read-only everywhere: officer writes return 409, builder portals become read-only, its builders drop out of weekly-due calendar items and the awaiting counts, and Discord ingest for its builders returns 400. Restoring is allowed. A cohort can be deleted only when it has no projects, people or accounts.
- **Project archive/restore** (officer): archiving also archives the project's builders (same timestamp) and ends their portal sessions; restoring brings back exactly those builders. Archived projects drop out of mentor load, auto-assign, mentor portals and calendars. **Delete** (director) needs no builders (archived builders count) and no activity.
- **Builder archive/restore** (officer): disables or re-enables the linked account. **Delete** (director) needs no activity. It deletes their portal account, which ends the sessions.
- Every write that touches an archived project, builder or cohort returns 409, except restore.
- Pending builder invites can be revoked (deleting the account row) by any officer. Disabling or enabling an active account needs director access.

## Onboarding (builder)

`/launchpad` shows the onboarding wizard until `onboarded_at` is set:

1. **You**: name (prefilled from the invite), pronouns, designation, bio (≤ 500 characters).
2. **Project**: *Start a new project* (name, description, track, links; stage starts at *Ideating*) or *Join a project* (a picker over the cohort's non-archived projects, showing each project's builders).
3. **Confirm**.

`POST /api/portal/builder/onboarding` does everything in one transaction. It creates the project if needed, then the person: the account's cohort, `joinedOn` = today in the club time zone, the account's email, and its **Discord ID**, so the Discord bot ingest works with no officer step. It links `member_id` when exactly one `members_mirror.uw_email` matches the account email (case-insensitive). Portal-created projects and builders get short random IDs (`lp-p-…`, `lp-b-…`) so derived calendar IDs stay within 64 characters. It then sets `person_id` and `onboarded_at` and records `buildspace_launchpad.portal.onboarded` in the audit log.

A builder record may already carry the account's Discord ID (the column is unique). If that record is in an archived cohort (a returning builder), it gives up the Discord ID and a new record is created in the new cohort. If it is in the account's own cohort, not archived and has no portal account (an officer added the builder before inviting them), onboarding links the account to that record instead of creating a duplicate, applying the submitted profile and project. Any other match answers 409 and is left for an officer.

## Mentor roster and auto-assign

The **Mentors** tab lists every mentor contact with these columns:

- account status (none / invited / active / disabled) with Invite/Resend;
- an **Available** toggle;
- **Cap** (max projects, blank = none);
- **Load** (non-archived projects in active cohorts);
- projects in the selected cohort.

**Auto-assign mentors** (in the Projects and Mentors tabs) opens a preview:

1. Candidates are projects in the selected cohort that are not archived, not *Shipped*, and have no mentor.
2. Eligible mentors are available mentor contacts whose load is below their cap. A portal account is not required; the preview flags mentors without one.
3. Projects are shuffled (`crypto/rand`). Each one goes to the eligible mentor with the **lowest current load**. Ties go first to a mentor whose topics mention the project's track (case-insensitive), then at random. That mentor's load goes up by one. Projects that run out of capacity are returned as `unassigned`.
4. The preview lists every proposed pair with before/after loads. **Shuffle** asks for another preview. **Apply** posts the exact pairs. The server re-checks each pair in one transaction (project still unmentored and active, mentor still eligible, caps respected), assigns all of them or none (409 on any stale pair), and audits each one as `buildspace_launchpad.project.mentor_auto_assigned`.

Each project gets one mentor this way. A second mentor stays a manual choice in the project form.

## API

All officer endpoints live under `/api/buildspace-launchpad` and need officer access unless stated otherwise. Everything is audited.

| Method / path | Auth | Purpose |
| --- | --- | --- |
| `GET /api/buildspace-launchpad?cohort=<id>&archived=1` | officer | Register for one cohort (default: newest active), plus `cohorts`, `cohortId`, `accounts`, the mentor roster (`available`, `maxProjects`, `load`, `account`). Archived rows are included only with `archived=1`. |
| `POST /cohorts` · `PATCH /cohorts/:id` · `DELETE /cohorts/:id` | director | Create; rename/dates/archive/restore; delete when empty. |
| `POST /cohorts/:id/invites` | officer | `{invites: [{email, name?}] (1–200), method: email\|link\|both}`. Returns `{accounts, links, skipped, emailConfigured}`. Re-inviting a pending email issues a new token. |
| `POST /accounts/:id/resend` | officer (director for mentors) | New token and expiry; returns `{account, link?}`. |
| `POST /accounts/:id/disable` · `/enable` | director | Ends sessions / restores access. |
| `DELETE /accounts/:id` | officer (director for mentors) | Revokes a pending invite (invited only). |
| `POST /mentors/:contactId/invite` | director | `{email?, method}`. Defaults to the contact's email. |
| `PUT /mentors/:contactId/settings` | officer | `{available, maxProjects}`. |
| `POST /cohorts/:id/auto-assign/preview` | officer | Returns `{assignments: [{projectId, mentorId}], unassigned, mentors: [{id, loadBefore, loadAfter}]}`. |
| `POST /cohorts/:id/auto-assign` | officer | `{assignments}`. Applies all or none. |
| `POST /projects/:id/archive` · `/restore` · `DELETE /projects/:id` | officer · officer · director | See Lifecycle rules. |
| `POST /people/:id/archive` · `/restore` · `DELETE /people/:id` | officer · officer · director | See Lifecycle rules. |

Existing `PUT /projects/:id` takes `cohortId` when creating (older clients that omit it get the newest active cohort) and ignores it on update. `PUT /people/:id` additionally takes `designation`, `pronouns`, `bio` and `email`. `POST /activity` takes an optional `visibility`. The legacy `/api/buildspace/*` aliases are kept only for the original seven routes; the new routes exist only under the canonical prefix.

Portal endpoints (`/api/portal`, portal cookie):

| Method / path | Who | Purpose |
| --- | --- | --- |
| `GET /api/portal/invite?token=` | public | Invite preview for the join page. |
| `GET /api/portal/me` | any account | Account, kind, cohort, onboarding state, `officer: boolean` (an officer session is also present). |
| `POST /api/portal/logout` | any account | Ends the portal session. |
| `GET /api/portal/builder` | builder | Dashboard: cohort, person, project, teammates, mentors (name, org, topics), team-visible activity, schedule/week, own this-week status, and `joinable` projects while not onboarded. |
| `POST /api/portal/builder/onboarding` | builder, not onboarded, cohort active | See Onboarding. |
| `PATCH /api/portal/builder/profile` | builder | Name, pronouns, designation, bio. |
| `PATCH /api/portal/builder/project` | builder | Name, description, track, stage, links of their own project (not mentors). |
| `POST /api/portal/builder/activity` | builder | `weekly` / `request` / `absence` for themselves; `visibility=team`. |
| `GET /api/portal/mentor` | mentor | Assigned non-archived projects with builders, visible activity, flags (no update this week, needs help, open requests), schedule. |
| `POST /api/portal/mentor/activity` | mentor | `kind=mentor` on an assigned project, optional builder, status on-track/needs-help, visibility team/mentors. |

## Front end

- **Officer page** (`/buildspace-launchpad`):
  - a cohort switcher and **New cohort** in the intro;
  - an **Invites** tab (accounts table with status, sent/failed, expiry, Resend / Copy link / Revoke / Disable, and **Invite builders**, which takes pasted emails or `name,email` CSV and previews valid, duplicate and invalid rows before sending);
  - Mentors tab roster columns plus Invite and **Auto-assign mentors**;
  - archive/restore/delete actions in the drawers and a **Show archived** filter;
  - designation and a member search picker in the builder form.
- **Portal** (`/launchpad`, `/launchpad/join`, `/launchpad/login`): its own light shell with the same tokens and components and no officer sidebar.
  - The builder dashboard shows a this-week check-in card, project card (editable), team, mentors and timeline.
  - The mentor dashboard shows project cards with attention flags, a project detail with builders and timeline, and **Add note**.

## Security checklist

- A portal session can never authenticate an officer API: separate table, separate cookie, and officer APIs never read the portal cookie.
- Every portal query is scoped server-side by the session's account (cohort, person, or the mentor's assigned projects). IDs in the request body are re-checked against that scope.
- Invite tokens are 32 random bytes, stored only as hashes, single-use (binding needs a pending invite), and expire after 14 days. Rate limiting covers `/auth` and `/api/portal/invite`.
- Mentor portals never see Finance data, officer-only entries, member records, contact notes, or other mentors' emails.
- Every account, cohort, archive and assignment change is audited with `buildspace_launchpad.*` actions.

## Local development

`dashboard seed` adds a **Fall 2026** (active) and an archived **Spring 2026** cohort, mentor roster settings (Maya cap 3, Amara cap 2, Jordan uncapped), an onboarded builder account `lp-account-alex` (Alex Kim, project Moss), an active mentor account `lp-account-maya` (Maya Chen), and two pending builder invites. The invite for Sky Morales uses the demo token `demo-launchpad-invite-sky` (`/launchpad/join?token=demo-launchpad-invite-sky`). *Wayfinder XR* has no mentor, so **Auto-assign** has work. With the dev build, `/auth/dev?as=builder` and `/auth/dev?as=mentor` sign in to the portal as those accounts (add `user=<account id>` to pick another active one).

## Testing

- Go: `launchpad_test.go` (cohorts, invites, OAuth binding, portal scoping, onboarding, auto-assign, archive/delete, calendar and ingest exclusions), `launchpad_migration_test.go`, `devauth_portal_test.go`, `mail_text_test.go`.
- Contract check: every new endpoint is exercised (`npm run contract-check`).
- BDD: `web/tests/features/launchpad.feature` (15 Gherkin scenarios) is implemented step for step by `web/tests/launchpad.bdd.spec.ts`; a drift check fails the suite if the two disagree. Every step saves a screenshot. See `web/tests/features/README.md` to run it.
- Component e2e: `web/tests/launchpad.spec.ts` (phone, tablet, desktop; light and dark).

Screenshots from the BDD run are in [evidence/launchpad-v12](evidence/launchpad-v12/).
