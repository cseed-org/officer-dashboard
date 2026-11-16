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

## Onboarding hello screen (V0.12)

`src/ui/onboarding/` holds `WelcomeIntro({ onDone, media, leaving })`, the first step of onboarding. The screen is the only thing on screen: a liquid-glass "Hello, welcome to cseed" over a looping background video (or its stand-in), cycling through 15 languages (`greetings.ts`; `cseed` is always lowercase LTR Latin). There are no buttons, hints or photos. A click, a tap, a swipe or any key starts it (printable keys, Enter, Space, the arrows). F-keys, Tab, modifiers, media keys and browser shortcuts are left to the browser. Escape is an invisible skip, and a second press fast-forwards to the final swap. A press within 650 ms of the one that started it (a double-click, a bounce) and key auto-repeat are the same press, so the intro is never skipped by accident.

**What happens (revision 2, D-27/D-28).** The rest of the sentence melts away in a few wide drips (a press during a language change melts both phrases from where they are) while the glass word **cseed** stays. It moves to the centre and holds, then glides and shrinks into the logo's corner, where it becomes the real wordmark (opacity only, same box) and the sapling fades in. The backdrop fades onto `--paper`, the page's own colour, by the end of the glide. The page logo stays hidden (`.onb--intro .onb-shell-logo`) until the overlay unmounts in the same frame, so the two are never drawn at once. On the tablet rail the word fades and the sapling mark fades in. The whole run takes about 1.5 s.

**Who draws what.** The motion is `introPlan.ts` (the timeline: `buildPlan`, `sampleAt`, `retargetPlan`, `skipPlan`; pure, unit-tested in `tests/onboarding-plan.spec.ts`). The screen draws only what `sampleAt(plan, ms)` returns on each animation frame, by `transform` and `opacity`, and skips style writes whose value hasn't changed. Playback, fast-forward, Escape and resize all go through the sampler. The page logo is measured again when the word starts to travel, and a resize mid-run jumps to the end on the freshly measured logo.

**First paint.** The overlay is server-rendered (`OnboardingPage` starts in `pending`), so it is the first thing painted, not the loading state. An inline script (`introEarly.ts`, hashed into the CSP like the other inline scripts) keeps a press made before the page's script has run, and the island replays it. Once the account is loaded, a resumed setup lets the overlay fade out (`leaving`), with the page logo showing under it. The same script hides the overlay before the first paint (`html.wi-off`) on a Google consent return, on the unavailable page, or when this browser has seen the intro (`localStorage['cseed:hello-seen']`, the user ids that finished or resumed it), so resumes and onboarded accounts never get a flash of it. Another invitee on the same browser still gets it, faded in once their account loads. Without the page logo to land on, it fades away without landing.

**Glass.** Liquid glass: each phrase is a translucent slab in the shape of its letters (`.wi-face`). Its fill carries the backdrop's colours bent as through a lens (the stand-in's palette flipped, or a magnified copy of the poster via `--wi-refract`), with a specular line under the top edge and a caustic above the baseline. A light rim on the upper edges and a darker lower edge (its thickness) give it depth; they are two crisp drop-shadows painted once per phrase. (An SVG lighting bevel and a large blurred shadow were tried and dropped: in software rendering they stalled frames at each language change and under the moving light.) A band of light (`.wi-sheen`) glides across the letters once per language: the window moves one way and the copy of the words inside it the other, so only the light travels, by transform. All of it sits over a fixed contrast scrim, and the stack is the same in light and dark. The fill is cloned per line with vertical padding, so tall marks (Vietnamese stacked accents, Devanagari vowel signs, Arabic marks) are never clipped. There are two phrase slots in a fixed-height band, so the layout never jumps between languages. With `prefers-contrast: more`, `prefers-reduced-transparency: reduce` or `forced-colors`, the type is solid. There is no `backdrop-filter`.

**Fonts.** `public/fonts/hello/` holds subsets with exactly the glyphs of the 15 greetings: Poppins for Latin, Vietnamese and Devanagari; Montserrat for Cyrillic; Noto Kufi Arabic; and Noto Sans SC, JP and KR. They are one family (`cseed Hello`, weight 500, no synthesis) split by `unicode-range`, so only the scripts on screen are downloaded. If you change a greeting, regenerate them with Google Fonts' `text=` subsetting (`https://fonts.googleapis.com/css2?family=<Family>:wght@500&text=<characters>`; all OFL). "bem-vindo" has a word joiner after its hyphen so it never breaks.

**Reduced motion.** There is no video, centring or glide. The stand-in (or poster) stands still, the languages cross-fade slowly, and a press fades to the logo in about half a second.

Specs:
- `tests/onboarding-intro.spec.ts`: only intro layers show, first paint, early presses, keys, swipe, all 15 languages in the glass with no ink lost, the video slot, exact landing with no double draw at every width in light and dark, and reduced motion. `INTRO_FRAMES=<dir>` saves frame strips.
- `tests/onboarding-intro-frames.spec.ts`: frame times, also under a 4× CPU throttle. It runs without tracing.
- `tests/csp.spec.ts`: an invitee plays the intro under the page policy.
- `tests/settings-replay-intro.spec.ts`: the Settings replay (below).

**Replay from Settings (D-22).** Settings › Advanced › *Replay welcome intro* (every signed-in role) plays the full intro over Settings (`ReplayIntro.tsx`, loaded only when pressed): the same `WelcomeIntro`, landing on the real shell logo (the top bar's or sidebar's wordmark, or the rail's mark; hidden meanwhile by `html.wi-replay`), then you are back on Settings. It only plays: it reads and saves nothing, so no account or onboarding data changes and the intake steps never run. Escape and reduced motion behave as in onboarding. `helloMedia.ts` finds the video and poster for both pages at build time.

### Hello screen video (drop-in file)

Put `hello-loop.mp4` (one short, silent, landscape loop: H.264, under about 8 s and 3 MB) and its first frame as `hello-poster.webp` (or `.jpg`) in `web/public/media/`. `src/ui/onboarding/helloMedia.ts` looks for them when the site is built (for onboarding and the Settings replay), so they appear **after the next build or deploy**, and a missing file never costs a 404. The video is cropped to fill any screen (`object-fit: cover`, centred), muted, looped, inline and silent. It is only added for full motion (not with reduced motion or Save-Data) and shows once it is actually playing, over the poster or the stand-in, so there is never a black frame. If it fails to load or autoplay is refused, the poster or the stand-in (a calm drifting gradient in the same layer) simply stays. It pauses once the greeting has melted. The Go server serves `.mp4` as `video/mp4` with byte ranges.

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
