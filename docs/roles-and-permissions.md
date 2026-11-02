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
Admin. Non-owner leadership cannot manage ownership or demote/deactivate an owner.
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

## Permission matrix

Roles rank `leadership > director > officer`; unknown roles are denied. Owner is an additional flag on an active leadership account. A higher role inherits lower-role permissions; resource-specific rules still apply.

| Capability | Officer | Director | Leadership | Owner |
| --- | --- | --- | --- | --- |
| Read dashboard, events, contacts, members | Yes | Yes | Yes | Yes |
| Manage own profile/calendar and submit purchases | Yes | Yes | Yes | Yes |
| Approve/settle purchases, manage budgets/periods | No | Yes | Yes | Yes |
| Admin, service tokens, audit history | No | Yes | Yes | Yes |
| Invite officers, manage officer subteam/title | No | Yes, except leadership targets | Yes | Yes |
| Change roles / director active status / edit leadership | No | No | Yes | Yes |
| Manage ownership or demote/deactivate owners | No | No | No | Yes, with last-owner protection |

Approval cannot be self-approval. Member edits/export require director access; ordinary officers can read member records. Design request permissions also depend on authorship, assignment and Design subteam membership. Design members and director+ can assign and move work; only the requester can approve a version in review and deliver it. Director status never overrides design approval. See [API reference](api.md) for endpoint-specific behavior. Source: `server/internal/auth/auth.go`, `server/internal/api/{admin,officers,invites,members,design,requests}.go` and database guards.
