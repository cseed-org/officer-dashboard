# Roles and permissions

## Dashboard ownership and recovery

Set `BOOTSTRAP_OWNER_DISCORD_ID` and `BOOTSTRAP_OWNER_NAME` in the deployment
environment (see `server/deploy/dashboard.env.example`). At startup, if no owner exists,
the server creates or updates that Discord account as active leadership with owner
access and records an audit entry. Provisioning is idempotent. Once an owner exists,
configuration no longer changes ownership or names; a different configured ID logs a
warning. An ownership transfer therefore survives restarts even with the old env values.
The deprecated `BOOTSTRAP_DIRECTOR_DISCORD_ID` remains an alias for the ID.

Owners can add co-owners, remove owner access, or transfer their own ownership from
Officers → Access, using each person’s actions menu. Non-owner leadership cannot manage ownership or demote/deactivate an owner.
Every owner must be active leadership, and the last owner cannot be removed. Transfer
keeps the caller active leadership and keeps any other co-owners.

Existing roles are preserved by migration. To explicitly recover an active account
as an owner, run `dashboard provision-leadership --owner --db /path/dashboard.db
--discord-id 123456789012345678`. Without `--owner`, the command grants leadership
only. Both operations are idempotent, end the target's sessions when changed, and write
an audit entry. Seed data includes a fictional leadership owner; no real deployment
identity is checked in.

The bootstrap environment cannot reclaim ownership after transfer. Explicit operator
recovery with `provision-leadership --owner` remains available even when owners already
exist: shell/database administrators can intentionally add an owner, and this action
is audited. Discord login itself never creates users or grants leadership/ownership.

## Setup before access

Everyone who joins through an invite, and every account added from Officers → Access,
finishes the setup (welcome intro, then a five-stage intake) before the rest of the dashboard opens. Until then the server
refuses every API call except their own account (`/api/me*`: profile, preferences,
calendar, setup), the club time zone and health, with 403 `onboarding_required`, whatever
their role. Leadership or director accounts created from Access go through the same
setup. Accounts with no invite record (seeded, bootstrap owner, created before V0.4) are not
locked out of the API, but see the same flow once (below).

### Onboarding version (V0.9)

`users.onboarding_version` records the newest first-login flow a person finished; the server's
`OnboardingVersion` (9) is the current one. `/api/me` carries `onboarding` whenever the version
is lower (or an invite is unfinished), so **every account, existing officers included, gets the
intro and intake once**, with the profile they already filled in pre-populated, then the Home
tour. `POST /api/me/onboarding/complete` sets the version. A future flow bumps the constant.

The **start term** is set automatically (e.g. `Fall 2026`) when a profile is first saved and
officers cannot change it (the profile endpoint ignores it); directors and leadership edit it
from the officer profile (`PATCH /api/officers/:id`, `startTerm`). The intake also collects an
officer **team/committee** (`profile.team`), chosen from the list in `web/src/lib/officerTeams.ts`.

## Permission changes apply at once

The server reads the signed-in user's role, owner flag, active status, onboarding state and
subteam (Design membership) from the database on every request; nothing caches them. Service
tokens recheck their creator's active director access on every call. So a change made in
Officers → Access applies on the person's very next request.

- **Deactivation** ends all of the person's sessions (and revokes their service tokens): the next
  request answers 401 and the tab goes to sign-in.
- **Role, owner and subteam changes** keep the person signed in (since V0.7; before, a role
  change signed them out). Their open tab follows on its next API request, any 403, or when the
  tab regains focus: every signed-in response carries `X-Perm-Version`, and when it no longer
  matches, the client refetches `/api/me`, updates the navigation and user chip, remounts the
  page for the new permissions (a tab they may no longer open falls back to an allowed one) and
  shows "Your permissions changed". A refused action says so in the same words. With unsaved
  edits, the page re-renders instead of remounting and offers a reload, so no draft is lost.
- Losing director access still revokes the person's service tokens.

## Permission matrix

Roles rank `leadership > director > officer`; unknown roles are denied. Owner is an additional flag on an active leadership account. A higher role inherits lower-role permissions; resource-specific rules still apply.

| Capability | Officer | Director | Leadership | Owner |
| --- | --- | --- | --- | --- |
| Read dashboard, events, contacts, members | Yes | Yes | Yes | Yes |
| Set own automatic/manual/club time zone | Yes | Yes | Yes | Yes |
| Change club location and default time zone | No | No | Yes | Yes |
| Create/edit/delete External Contacts, set speaking history, log outreach | Yes | Yes | Yes | Yes |
| Manage own profile/calendar and submit purchases | Yes | Yes | Yes | Yes |
| Approve/settle purchases, manage budgets/periods | No | Yes | Yes | Yes |
| Officers Access, service tokens, audit history | No | Yes | Yes | Yes |
| Invite officers, fix a not-yet-onboarded invite's name/subteam, manage officer subteam/title | No | Yes, except leadership targets | Yes | Yes |
| Change roles / director active status / edit leadership | No | No | Yes | Yes |
| Manage ownership or demote/deactivate owners | No | No | No | Yes, with last-owner protection |

Approval cannot be self-approval, and the requester never settles or closes their own request (no self-payout). Directors settle up to the approved estimate plus the agent tolerance; larger amounts need leadership. Receipts download (and show a link) for the uploader, the requester and director+. A director's own service token cannot buy or settle that director's requests. Service tokens stop working when their creator is deactivated or loses director access, and those changes revoke them. Member edits/export require director access; ordinary officers can read member records. Design request permissions also depend on authorship, assignment and Design subteam membership. Design members and director+ can assign and move work; only the requester can approve a version in review. Director+ can move unassigned work without becoming its designer. Leadership can explicitly confirm a delivery override, recorded separately in the audit log without recording requester approval. Directors cannot override delivery. Marking an event as not needing design is limited to director+ and the event's host; any officer can say an event needs design. See [API reference](api.md) for endpoint-specific behavior. Source: `server/internal/auth/auth.go`, `server/internal/api/{admin,officers,invites,members,design,requests}.go` and database guards.

Officers is the single people-management page. Its Directory is available to all officers;
Invites, Access (including adding officers), Service tokens and Audit log require director or
leadership access. Directors deactivate/reactivate officers only; leadership also manages
directors and leadership. Role changes require leadership; owner targets require an owner.
The old `/admin` page redirects to Officers Access; `?tab=tokens`, `?tab=audit` and
`#audit` links open the matching tab. API permissions and audit recording are unchanged.
