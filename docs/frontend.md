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
| `/officers` | `officers/index.html` | Board Members: `?tab=invites\|access\|tokens\|audit` (directors); directory for everyone (Officers, Club Support Contacts, Ex-Officers) |
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

## Onboarding hello screen (V0.10)

`src/ui/onboarding/` holds `WelcomeIntro({ onDone, media })`, the first step of onboarding. It shows "Hello, welcome to cseed" in 15 languages (`greetings.ts`; `cseed` is always lowercase LTR Latin), set in glass letters: a translucent fill clipped to the text, a specular band, a rim and a cast shadow. Chromium also gets an SVG bevel on the letters and refraction on the glass Skip/hint pills. Other browsers keep the blur-only glass. Behind it, a drifting mesh of colour takes a new random vivid palette every 1.2s, cross-faded over 0.9s between two stacked layers (`palette.ts`; each hue jumps 70–220°, so it never strobes). The background video, when present, plays underneath.

There is no start button. A click, a tap or any key starts it. Tab still moves focus (overlay ↔ Skip), modifier keys do nothing, and Escape skips. Starting brings up the group photo as a glass-framed card from the tap point and starts the arcade snake. The snake moves one grid cell per tick with 90° turns. It eats the greeting, wraps off the right edge to a lane above the word, and eats an apple on every word cell. On each bite the tail stays put (classic growth), so its body ends up exactly on `WORD_PATH`, spelling **cseed** on the grid. The route is the shortest one that writes c-s-e-e-d in one stroke: 58 letter cells plus 15 joins. The joins are dimmed once it is spelled, like the blacked-out tube of a neon sign. It holds about 1s, then the word shrinks into the shell logo and the overlay fades over the intake. A second press fast-forwards. `snakePlan.ts` is pure (grid, route, per-tick body, timing, morph) and unit-tested in `tests/onboarding-snake.spec.ts`. The component renders it with pooled squares moved by `transform` once per tick. Everything else is Web Animations on transform/opacity. With `prefers-reduced-motion` there is no snake and no colour change: the photo fades in, stays 1.5s, then a plain fade to the logo. Skip goes straight to the logo.

The landing spot is measured, never hard-coded. `ShellLogo` (`LogoMark.tsx`) renders an invisible copy of the shell's top bar and sidebar header (`.onb-ghost`, the same classes as `AppLayout.astro`, laid out by the same CSS and `<html data-sidebar>`). It copies whichever logo the ghost shows, the wordmark or the rail's C mark, and that logo's box. The intro lands on `[data-onb-logo]`. While it plays the intro is a modal: the rest of the page is inert and focus returns to the step heading afterwards. Spec: `tests/onboarding-intro.spec.ts`, which checks the landing against the real shell at 820/900/1099/1100/390 (`INTRO_FRAMES=<dir>` records frames).

### Hello screen media (drop-in files)

Put these in `web/public/media/` and rebuild. `src/pages/onboarding.astro` looks for them at build time, so a missing file never costs a 404.

| File | What | If missing |
| --- | --- | --- |
| `hello-loop.webm` and/or `hello-loop.mp4` | Background video: muted, looped, `playsinline`, under the colour (about 70% colour over it). Keep it short (≤ 10s), ≤ 1920px wide and a few MB; H.264 MP4 for Safari, VP9/AV1 WebM for the rest. | Animated gradient |
| `hello-poster.jpg` | First frame shown while the video loads | No poster |
| `hello-group.jpg` (or `.jpeg`, `.webp`, `.png`) | The group photo revealed on the first tap, cropped to fill a landscape card (portrait on phones), so keep faces near the middle; about 2000px wide | `hello-group-placeholder.svg` (illustration) |

The intro also falls back at runtime if a file fails to load. The Go server serves `.mp4`/`.webm` with video types and byte ranges.

## Resizable sidebar (V0.9)

Desktop and tablet users can drag the sidebar's right edge (`SidebarResizer`, a focusable
`role="separator"`; arrows, Home/End, Enter and double-click also work) between 64px (icons
only, labels hidden, tooltips via `title`) and 320px. Dragging below 120px snaps to icons only.
The width is the `sidebarWidth` pref (`PUT /api/me/prefs`, 0 = never resized, else 64 to 320), so
it follows the user across devices. `lib/sidebar.ts` mirrors it into localStorage
(`cseed-sidebar`) and the inline PREPAINT script in `AppLayout` sets `<html data-sidebar>` and
`--sidebar` before first paint; the server value wins once `/api/me/prefs` loads
(`applyTimezone` calls `syncSidebarFromServer`). Phones keep the menu sheet and have no handle.

**Directory (V0.9).** Board Members (`/officers`) has three sub-sections under one search: *Officers* (active accounts), *Club Support Contacts* (external contacts with `clubSupport`, managed from `/contacts`; migration `0021_contact_club_support.sql`) and *Ex-Officers* (deactivated accounts; only directors receive those from `/api/officers`). Members (`/members`) also lists current officers with an Officer badge: `GET /api/members` returns `officers` (active users, with `memberId` when a member row shares one of their emails), and the page shows only the unmatched ones as extra first-page rows and badges the matched member rows. Display only: no member record is written.
