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

**Design board (V0.10).** `/design` shows four lanes over the five stages: To do (`new` + `accepted`;
`new` cards carry a "Needs approval" chip), Doing (`in-progress`), Review (`in-review`), Done
(`delivered`, a gallery). Query: `?view=board|list|mine`, `?filter=mine|unassigned|due`, `?type=`,
`?id=<request>` (detail), `?new=1[&eventId=]`, and `?lane=doing|review|done`, the lane a phone shows
(stage tabs below 640px). A drop on a lane sets its stage only when `laneTarget` in `lib/design.ts`
allows it (it mirrors the server's move rules); refused lanes fade while dragging. Keyboard: Space picks
up, arrows choose a lane, Space drops, Escape cancels, Enter opens. Cards and the detail show the
`DesignLinks` buttons (Figma/Canva + Folder): there is no data yet, so they render "Not linked yet".
Pass `links={{ design, folder }}` once something stores them.

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
| `/officers?person=<userId\|contactId>` · `/members?person=<personId\|userId>` · `/contacts?person=<contactId>` | (same pages) | V0.10 person drawer; the old `?id=` detail links on Members and Contacts are rewritten to `?person=` |
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

## Onboarding hello screen (V0.10, reworked in V0.11)

`src/ui/onboarding/` holds `WelcomeIntro({ onDone, media })`, the first step of onboarding. It shows "Hello, welcome to cseed" in 15 languages (`greetings.ts`; `cseed` is always lowercase LTR Latin) in solid ink type on the app's plain background (`--bg` / `--text`, so it follows light and dark). V0.11 removed the colour wash and the background video: there is no colour change and no `<video>` before or after the click.

There is no start button. A click, a tap or any key starts it. Tab still moves focus (overlay ↔ Skip), modifier keys do nothing, and Escape skips. Starting brings up the group photo as a card from the tap point and starts the arcade snake. The snake moves one grid cell per tick with 90° turns. It eats the greeting, wraps off the right edge to a lane above the word, and eats a seed on every word cell. On each bite the tail stays put (classic growth), so its body ends up exactly on `WORD_PATH`, writing **cseed** on the grid.

The glyphs follow the real wordmark (`public/images/cseed-wordmark.png`): lowercase, 4 cells wide and 5 tall, with the d's stem one row taller, and the cells where a stroke turns get a round outer corner once the word is written. Each letter is one natural pen stroke (c and s from the top right, the e round its eye then along the bottom, the d's bowl then up its stem). Between letters the snake travels through hidden "lift" cells: they are never drawn and pass in 9ms each, like a pen lifted between letters, so nothing but letters is ever visible. The way to the seeds is deliberately slow (about 3.4s for the greeting pass and approach, 44–70ms a cell), the word is written at 34ms a cell. The word sits about 9% of the height above the middle, on the calm top of the photo card (on a phone it stays inside the card's 16px inset). The snake is drawn in the wordmark image's fixed ink (#1d1d1b) in both themes, because the photo behind it is light; a copy of the written word in `--text` fades in over the shrink, so in dark mode it lands as light ink like the white wordmark that takes over. Then a sapling sprouts after the d, the word and sapling hold about 1s, and the whole wordmark shrinks onto the shell's logo: the drawn word is laid out in the image's own proportions (`LOGO`, `SPROUT` in `snakePlan.ts`), so it lands on the image's letters and the real wordmark images fade in on top. On the rail the sapling lands on the sapling mark and the word fades. Click to logo is about 8s. A second press fast-forwards. `snakePlan.ts` is pure (glyphs, strokes, lifts, grid, per-tick body, timing, morph) and unit-tested in `tests/onboarding-snake.spec.ts`. The component renders it with pooled squares moved by `transform` once per tick. Everything else is Web Animations on transform/opacity. With `prefers-reduced-motion` there is no snake: the photo fades in, stays 1.5s, then a plain fade to the logo. Skip goes straight to the logo.

The landing spot is measured, never hard-coded. `ShellLogo` (`LogoMark.tsx`) renders an invisible copy of the shell's top bar and sidebar header (`.onb-ghost`, the same classes as `AppLayout.astro`, laid out by the same CSS and `<html data-sidebar>`). It copies whichever logo the ghost shows, the wordmark or the rail's sapling mark, and that logo's box. The intro lands on `[data-onb-logo]`. While it plays the intro is a modal: the rest of the page is inert and focus returns to the step heading afterwards. Spec: `tests/onboarding-intro.spec.ts`, which checks the landing against the real shell at 820/900/1099/1100/390 (`INTRO_FRAMES=<dir>` records frames).

### Hello screen photo (drop-in file)

Put `hello-group.jpg` (or `.jpeg`, `.webp`, `.png`) in `web/public/media/` and rebuild: the group photo revealed on the first tap, cropped to fill a landscape card (portrait on phones), so keep faces near the middle; about 2000px wide. `src/pages/onboarding.astro` looks for it at build time, so a missing file never costs a 404; without it (or if it fails to load) the intro shows `hello-group-placeholder.svg`.

## Brand marks (V0.11)

The full logo is the `cseed-wordmark` images (ink on light, white on dark). The single-item mark is the sapling from `public/favicon.svg`, as an inline SVG: `Sapling` in `src/ui/brand/Sapling.tsx` (it renders statically from Astro too) and `saplingSvg()` in `src/ui/brand/saplingMarkup.ts` (a different name from `Sapling.tsx`, not just a different case, so case-insensitive file systems never mix them up) for markup built as a string. Its colours are `--sapling-leaf` / `--sapling-stem` in tokens.css, with lighter greens in dark. It is used inside `.wordmark-mark` on the sidebar rail, the sign-in and 404 cards, the onboarding logo ghost, and as the club-website chip in the Links widget. There is no "C" monogram any more.

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

**People pages and PersonDrawer (V0.10).** Board Members, Members and External Contacts each show one rounded card (the Home widget radius, border and shadow; `styles/people.css`) with the toolbar inside it, grouped tables with a sticky column header (`components/PeopleTable.tsx`), and rows that collapse to avatar + name + chip on phones. Clicking a person opens `components/PersonDrawer.tsx` instead of navigating: a modal `Drawer` in `bare` mode with a hero (band in the person's colour, avatar, chips, quick actions, up to three stats) and Overview / Activity / Manage tabs that the page supplies. `usePersonParam()` keeps the open person in `?person=` (Back closes a drawer opened from the list; a deep link closes in place). Officer stats and activity come from `GET /api/officers/{id}/activity` (events hosted, open design requests, purchases in flight, recent items), computed from existing rows only. An officer listed on Members without a membership record opens there as a read-only officer summary (stats, activity, a link to their Board Members profile). Buildspace can reuse the drawer by passing its own tabs.
