# Documentation

**Deploying?** Start at **[Fresh install](deploy/fresh-install.md)**; for a running host use **[Upgrade/restore](deploy/upgrade-existing.md)**.

| Guide | Contents |
| --- | --- |
| [Development](development.md) | Quick start, live development, seed data, tests, contract checks, Playwright. |
| [Configuration](configuration.md) | Every setting: required or optional, defaults, compose and backup settings, club location/time zone. |
| [Fresh install](deploy/fresh-install.md) | **Start here to deploy.** Docker on a Pi 5 or Linux box, private GHCR access, encrypted D1 and Drive backups, and approved image updates. See [Upgrade/restore](deploy/upgrade-existing.md), [Cloudflare setup](deploy/cloudflare.md), [Google calendar setup](deploy/google-calendar.md) and [Other setups](deploy/other-setups.md). The [original deployment walkthrough](deploy/README.md) retains historical R2/Litestream instructions. |
| [Deployment](deployment.md) | Backup sizing, recovery, integration setup and links to the deployment runbooks. |
| [Architecture](architecture.md) | Go packages, SQLite transactions, authentication, API contracts. |
| [Buildspace + Launchpad](buildspace.md) | Builder register, permissions, draft assumptions and Discord check-in ingestion. |
| [Launchpad cohorts](launchpad-cohorts.md) | Cohorts, email invites, builder/mentor portals, identity and security model, auto-assign. |
| [Home calendar](calendar-widget.md) | Calendar widget views, feed API, adding a source. |
| [Organization calendar](org-calendar.md) | Shared Google calendars, meetings, OAuth, ACLs and two-way sync. |
| [Weekly programs](programs.md) | Tracks, recurring sessions and skip/move exceptions. |
| [Front end](frontend.md) | Routes, preferences, static serving, proxy, UI source layout. |
| [Roles and permissions](roles-and-permissions.md) | Officers management tabs, role matrix, owner provisioning, transfer and recovery. |
| [Purchase deadlines](purchase-deadlines.md) | Optional “needed by” dates on purchase requests, due hints, deadline sort and the Home calendar source. |
| [Accounting](accounting.md) | Ledger, restrictions, purchase lifecycle, Form 990 limits. |
| [Integrations](integrations.md) | Discord, Cloudflare, Luma CSV, Google Drive and Calendar, Notion status. |
| [API](api.md) | HTTP endpoints and payloads, including External Contacts and speaking history. |
| [MCP agents](mcp.md) | Connect your own AI agent to `/mcp`: OAuth sign-in as the officer, tools, audit and revocation. |
| [Notion integration](notion-integration.md) | Placeholder behavior and future implementation boundary. |
| [V0.14.1 performance](v0141-performance.md) | Release-wide web and server performance evidence, reproduction commands, raw route inventories and measurement limits. |
| [V0.6 Home speed](home-speed-v06.md) | Home LCP 2.0s → 1.08s: early API reads, the zod contract off the first paint, module preloads, display-size logos; before/after per page. |
| [V0.5 motion/performance](motion-performance-v05.md) | Before/after client and API measurements, motion behavior, screenshots, and remaining budgets. |
| [Performance](performance.md) | Historical Raspberry Pi measurements and benchmark methodology. |

[Project overview](../README.md) · [Contributing](../CONTRIBUTING.md) · [Security](../SECURITY.md)
