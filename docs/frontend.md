# Front-end reference

A static Astro site with Preact islands. There is no server code here: every page is
plain HTML from `npm run build` (`web/dist`), and all data comes from `/api/*` at
runtime, same origin, with the session cookie. The Go backend serves `web/dist`.

- Contract: `src/lib/contracts.ts` is a copy of v1's `src/core/contracts.ts` (plus
  `src/lib/domain.ts`). Every response is validated with its zod schema
  (`src/lib/api/client.ts`); drift shows as a "did not match the API contract" error, not a
  blank page. `SearchResponse` is now also defined in the canonical root contract.
  Since V0.6 the zod contract is not in a page's first JavaScript: `api()` sends requests from
  `src/lib/endpoints.generated.ts` (method and path only, rewritten from `ENDPOINTS` by every
  `npm run build`/`npm run dev`; commit it with contract changes), and the schemas load in the
  background. Reads that arrive before then render at once and are validated when the schemas
  load (a mismatch replaces the page with the same error); writes and later reads are
  validated before they return. See [V0.6 Home speed](home-speed-v06.md).
- Early reads: signed-in pages start `/api/me`, `/api/me/prefs` and `/api/club/settings` (Home
  also `/api/overview` and `/api/cloudflare/widgets`) from an inline head script
  (`src/lib/early.ts`); `api()` uses each response once. The build adds low-priority
  `modulepreload` links for each page's island modules (`integrations/modulepreload.mjs`).
- Auth: each page calls `GET /api/me`; a 401 sends the browser to `/login?next=<here>`.
  Director-or-higher UI (Officers Access, fiscal periods, director reports, approve/settle) is hidden for
  officers; the server still enforces every rule.
- Styles: `src/styles/{tokens,base,components}.css` and `design/` belong to the designer.
  Markup uses the class contract in `design/spec.md`.

Paths in this guide are relative to `web/` unless stated otherwise. The contract copy originated in v1; current schemas include V0.3 endpoints.

## Develop

The default proxy origin is a historical v1 setting; the v1 application has been removed. Use `API_ORIGIN=http://127.0.0.1:8090` for this repository.

```sh
cd web
npm ci
API_ORIGIN=http://127.0.0.1:8090 npm run dev            # http://127.0.0.1:4340
```

The Vite dev server proxies `/api` and `/auth` to `API_ORIGIN`:

| Backend | `API_ORIGIN` |
| --- | --- |
| Historical v1 default (removed) | `http://127.0.0.1:4330` |
| Go server | `http://127.0.0.1:8090` |

```sh
API_ORIGIN=http://127.0.0.1:8090 npm run dev
```

The proxy rewrites `Origin` to the backend (its CSRF check), strips any cookie `Domain`
and keeps redirects on :4340, so dev sign-in works: open `/login` and use the
"dev: director / officer" links (`/auth/dev?as=…`, only shown in dev builds).

## Routes (for the Go server)

The app also serves `/officers`, `/members`, `/contacts`, `/design`, and `/onboarding` from their corresponding `<route>/index.html` files.

Each page is a static file. Details, drawers, tabs and filters live in the query
string, so **no rewrite or SPA fallback is needed**: serve `<path>/index.html` for `<path>`
and `<path>/` (Astro `build.format: 'directory'`), static assets as files, and `404.html`
for anything else (status 404).

| URL | File in `dist/` | Notes |
| --- | --- | --- |
| `/` | `index.html` | Home: KPIs, recent events, my action queue, budget vs actual, activity |
| `/events` | `events/index.html` | `?when=upcoming\|past`, `?q=` filters |
| `/events?id=<eventId>` | `events/index.html` | event detail slide-over |
| `/events?import=1` | `events/index.html` | CSV import dialog |
| `/finance` | `finance/index.html` | requests tab; `?scope=mine\|all`, `?status=` |
| `/finance?id=<requestId>` | `finance/index.html` | request detail slide-over (works with any tab) |
| `/finance?new=1[&eventId=]` | `finance/index.html` | new request drawer |
| `/finance?tab=budgets\|reports\|periods` | `finance/index.html` | `&fy=`, `&fund=`, `&report=<FINANCE_REPORTS key>` |
| `/officers` | `officers/index.html` | `?tab=invites\|access\|tokens\|audit` (directors); directory for everyone |
| `/admin` | `admin/index.html` | Redirects to Officers Access; query/hash tabs and legacy subpaths are mapped |
| `/settings` | `settings/index.html` | theme, density, profile, sign out |
| `/login` | `login/index.html` | `?next=`, `?error=not_allowlisted\|inactive\|state\|discord` |
| anything else | `404.html` | |

`/api/*` and `/auth/*` are the backend's. Sign-out is a form `POST /auth/logout`.

**Search hrefs.** `GET /api/search` results should use the v2 URLs above
(`/finance?id=…`, `/events?id=…`, `/officers?tab=access`, `/finance?tab=budgets`). The palette also maps
v1-style paths (`/requests/:id`, `/events/:id`, `/requests/new`, `/events/import`,
`/admin/users|tokens`) through `normalizeHref` in `src/lib/routes.ts`, so either works.
For compatibility with the historical v1 backend, when `/api/search` answers 404, the palette falls back to
`GET /api/requests?scope=all&q=` and `GET /api/events?q=`.

## Preferences

Theme (`cseed-theme`: `light` default, `dark`, `system`) and density (`cseed-density`:
`comfortable`, `compact`) are in `localStorage` and applied to `<html data-theme
data-density>` by an inline script in `<head>` before first paint. The last known role
(`cseed-role`) is cached the same way so restricted navigation doesn't flash for officers.

## Check and test

```sh
npm run check          # astro check, 0 errors
npx playwright test    # phone 390×844, tablet 820×1180, desktop 1440×900; light + dark
SHOTS=1 npx playwright test shots --project=desktop --project=phone   # tmp/shots/*.png
```

Playwright reuses a dev server already on :4340 (otherwise it starts one). The backend
behind it needs seed data; the lifecycle tests create new requests titled `agent-e2e-…`.

## Layout

```
src/lib/          contract copy, API client + useApi, routes, same-page router, session, prefs
src/layouts/      AppLayout.astro: pre-paint script, sidebar / top bar, palette
src/pages/        page routes + login + 404 (each mounts one island)
src/ui/pages/     page islands (auth gate + view)
src/ui/views/     page bodies, including Home, Events, Finance (+ request detail/form), Settings, Officers, Members, External Contacts, Design, Onboarding
src/ui/components/ kit, states, charts, drawer, icons, CSV import (ported from v1)
src/ui/shell/     command palette, user chip, auth gate
tests/            Playwright
```

External Contacts uses `/contacts`. The legacy `/speakers` route redirects to it (including contact deep links). The directory offers All / Has spoken / Not yet speaking-history filters and an editable “Has spoken for CSEED before” checkbox.
