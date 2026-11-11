# Home V0.7 widgets: implementation notes

These notes go with [home-v07.html](home-v07.html), which is design only. Open it in a browser. The top bar switches between both themes, light only and dark only, and has **↻ Replay load motion**, which exists only in the mockup. The page shows each widget at every allowed size, the empty states, forced hover, and edit mode (idle, dragging, and resizing with push-down, which has a ▶ Play button).

The plan is `~/.claude/plans/you-job-in-this-fuzzy-cray.md`. Sizes come from `src/ui/widgets/registry.ts` and are unchanged.

## Tokens

Use only existing tokens: `--c-*`, `--tint`, `--surface`, `--sunken`, `--hover`, `--line`, `--control`, `--muted`, `--text`, `--accent`/`--accent-ink`, `--negative`, `--block-*`, `--overlay-shadow`, `--radius-widget`, `--dur-1/2/3`, `--dur-step`, `--ease-out`, `--ease-soft` and `--motion-distance`.

**Two new tokens** go in `home-grid.css` `:root`:

| Token | Value | Use |
|---|---|---|
| `--home-stagger` | `40ms` | Delay between tiles in the entrance. No existing token is 40ms: `--motion-stagger` is 16ms and `--stagger` is 120ms. |
| `--dur-chart` | `480ms` | Sparkline draw, bar grow, donut sweep and count-up. |

No new colours.

## Icons

New paths for `Icon.tsx` use the same 24px grid, stroke 1.75, and round caps and joins. Copy them from the `ICON` object in the mockup's script:

`clock`, `link`, `flag`, `ticket`, `pie`, `activity`, `globe`, `gauge`, `database`, `users`, `arrow-up-right`, `resize-corner`.

The `activity` path also fixes the missing icon at `CloudflareWidget.tsx:44`.

## Card shell (`shell/WidgetCard.tsx` + `widget.css`)

The mockup classes are a suggestion. Rename them freely, but keep the behaviour.

- **Markup.** `section.wc[data-hue]` holds `header.wc-head`, which contains:
  - `span.wc-chip` (24px, 7px radius, `--hue` icon at 14px on `color-mix(--hue var(--tint), --surface)`)
  - `h2.wc-title` > `a.wc-link`
  - `span.wc-go` (↗, the `arrow-up-right` icon at 14px with stroke 2)
  - an optional `span.wc-aside` (muted 12px, `margin-left:auto`)

  Then come `div.wc-body` and an optional `footer.wc-foot`.
- **Title.** 13px, weight 600, colour `--text`. The old title was lowercase and muted; the new one is sentence case.
- **Whole card clickable.** `a.wc-link::after {position:absolute; inset:0}` stretches the title link over the card. Nested anchors are invalid HTML, so do not wrap the card in an `<a>`. Rows, chips and buttons inside the card get `position:relative; z-index:1`, so they open their own item.
- **Focus.** `.wc:has(.wc-link:focus-visible)` gets a 2px `--c-blue` outline (offset 2px) and shows ↗.
- **Hover** applies only under `(hover:hover)`:
  - The card moves `translateY(-2px)`, gets `box-shadow: var(--overlay-shadow)`, and its border becomes `color-mix(--control 40%, --line)`.
  - Transitions: `--dur-2` with `--ease-out` on transform, shadow and border.
  - ↗ starts at opacity 0, `translate(-3px,3px)`, and ends at 1 and none: opacity over `--dur-1`, transform over `--dur-2`. It sits right after the title, not in the corner.
- **Rows inside.** A hovered row gets the `--hover` background with an 8px radius. The card stays lifted.
- **Corner links are gone** ("all events →", "budgets →" and so on).
- **Links widget exception.** Its card link goes to Settings › Club links for leadership. For other roles the card has no card link and no ↗ (`.wc--nolink`); only the rows link.
- **Reduced motion.** No transitions. Hover still changes the shadow, but the lift jumps with no animation.

## Compact rows (these replace every table)

- **Row:** `a.row` (padding 5px 8px, 8px radius) holds:
  - `row-main`, with `row-title` (13px/18px, weight 500, ellipsis) and `row-meta` (12px/16px, muted, tabular numbers, ellipsis, parts joined by ` · `)
  - `row-side`, right-aligned, with `row-amt` (13px, weight 500, tabular numbers) and a status chip
- **Layout.** No column headers. The gap between rows is 2px, with no dividers.
- **Status chip.** The existing `.badge` look: a 7px dot in the status hue plus the written label. Never use colour alone.
- **Overflow.** When the list is longer than the size allows, end with a muted line: "+3 more · $265".

## Per widget

**Size cells** list what each size shows, joined by " · ". **Empty** is a muted icon (`--control`, 24px at 1×1 and 2×1, 32px otherwise) plus one line.

| Widget | Icon | Accent | Card href | Size → content | Empty |
|---|---|---|---|---|---|
| `calendar` | calendar | `--c-blue` | Events | **2×1:** this week, 7 columns, at most 2 two-line chips a day, otherwise 1 chip and "+n" in the day header · **2×2:** this week, up to 4 chips a day, title wraps to 2 lines · **4×2:** this month, 6 weeks starting Monday, 1 chip a day plus "+n" · aside "Nov 9 – 15" or "November 2026" | The week grid stays, with the line "Nothing scheduled this week" |
| `links` | link | `--c-teal` | Settings › Club links (leadership only) | **2×1:** 3 small tiles (chip + name) · **1×2:** 3 rows (name + domain, or "Add link") · **2×2:** 2×2 tiles with domain, plus an "Add a link" tile | "No club links yet" |
| `kpi-upcoming` | clock | `--c-blue` | Events, upcoming | **1×1:** value + "next: Founders AMA · Thu 7:00 PM" · **2×1:** value + "in the next 30 days", right column lists the next 3 (day, title) | "Nothing on the calendar" |
| `kpi-checkin` | check | `--c-teal` | Events, past | **1×1:** value + 72×28 sparkline, then sub · **2×1:** left value + sub, right a wide sparkline (44px tall) + "best 86% … · low 58%" | "No past events yet" |
| `kpi-reflection` | message | `--c-violet` | Events, past | Same as check-in; 2×1 adds "last event 42% · …" | "No reflections yet" |
| `kpi-budget` | wallet | `--c-green` | Finance › Budgets | **1×1:** value + progress (`usageHue`) + "spent · committed" · **2×1:** value + "left of $9,000 · FY2027", right progress + legend (spent, committed) | "No budget set for FY2027" |
| `kpi-pending` | inbox | `--c-amber` | Requests, pending | **1×1:** value + "$680 awaiting a director" · **2×1:** + the top 3 pending (amount, title) | "Nothing waiting" (keeps the value 0) |
| `my-action` | flag | `--c-amber` | Requests, pending (officer: mine) | **2×1:** 2 rows · **2×2:** 5 rows + "+n more · $" · **4×2:** 2 columns, up to 10 rows; meta adds #number, category dot + name, needed-by · aside "8 waiting" (4×2: "· $960") | Director: "Nothing to approve". Officer: "No open requests" |
| `recent-events` | ticket | `--c-blue` | Events | **2×2:** 5 rows, meta "Oct 28 · 71% checked in", side shows guests · **4×2:** 6 rows, meta adds location, guests and reflected %; side is a 96px check-in progress bar + % (aside "check-in rate") | "No events yet · import a Luma guest list" |
| `budget` | pie | `--c-green` | Finance › Budgets | **1×1:** donut only (56–104px, center "6% used") + a names-only legend of the largest categories that fit, then "+n"; no aside · **2×2:** donut (128px) + legend of the top 5 (dot, name, "$used / $budget") + "+n more" · **4×2:** donut (176px) + 2-column legend of every category with a 4px `usageHue` progress bar, then a totals line · aside "FY2027" | "No budget set for FY2027" |
| `activity` | activity | `--c-coral` | Admin › Audit (officer: Requests) | **2×1:** 2 single-line rows (avatar, **who** what, time on the right) · **1×2 and 2×2:** 6 rows with "2h ago" meta · **4×2:** 2 columns grouped by day (Today, Yesterday, …) | "No activity yet" |
| `web-analytics` | globe | `--c-blue` | Cloudflare dashboard (new tab) | **1×1:** visits + sparkline + "visits · last 7 days" + sync time · **2×2:** Visits and Page views, a wide sparkline, top 3 pages · **4×2:** + 3 columns: Top pages, Referrers, Countries | "Not connected yet" |
| `traffic-security` | shield | `--c-coral` | Cloudflare | **1×1:** requests + "312 security events" · **2×2:** 4 metrics + bars for requests by hour (24) | "Not connected yet" |
| `performance` | gauge | `--c-teal` | Cloudflare | **1×1:** LCP p75 + Good/Needs work chip · **2×2:** rows for LCP, INP, CLS and per-page, each with value, status chip and threshold; "Insufficient samples" row | "Not connected yet" |
| `workers-d1` | database | `--c-violet` | Cloudflare | **1×1:** worker requests · **2×2:** 4 metrics + 7-day request bars | "Not connected yet" |
| `member-signups` | users | `--c-violet` | Members | **1×1:** total + "+18 in 7 days" · **2×2:** 4 metrics + 14-day signup bars | "No members yet" |

### Role variants

The officer versions keep the same shell and row format:

- `my-action` is titled "My open requests" and lists the officer's own open requests.
- `activity` is titled "Latest requests" and uses request rows instead of the audit feed. Its card href is Requests.

### Accent colours

Accents group by domain: events blue, finance green, requests amber, people violet, check-in and performance teal, alerts and activity coral. Some widgets share an accent, so the icon is what tells them apart.

### Cloudflare widgets

- The footer is muted 11px: "Synced daily, last 7:02 AM", or just "7:02 AM" at 1×1.
- The spec's Stale, failed-sync and Connection-details states stay as they are; they render inside `wc-body`.

### Stat tiles

- The value is 2rem with tabular numbers (1.5rem on phone).
- Sub text is muted 12px, clamped to 2 lines.

## Calendar

- **No controls.** The Options button, toolbar, Today/‹/› buttons, source chips and the sources menu are all gone.
- **Clicks.** Clicking the card opens Events. A chip opens its own item.
- **Week view (2×1 and 2×2).**
  - The 7 day columns, Mon–Sun, use a 3px gap.
  - Each day cell has an 8px radius and a `--sunken` background. Weekends use a `color-mix(--sunken 50%, --surface)` background, which is lighter, but they are always shown.
  - The day header reads "Mon **9**" (11px, the number in weight 600).
  - Today gets a column tint of `color-mix(--c-blue calc(var(--tint)*.7), --surface)` and a 1px inset ring of `--c-blue` at 35%. Its number becomes a pill with `--accent` background and `--accent-ink` text.
- **Chips.**
  - Background `color-mix(--hue var(--tint), --surface)`, a 2px `--hue` left border and a 5px radius.
  - The time is muted, 10px with tabular numbers; the title is 11px in `--text`.
  - Use a short time format: "6:30p" or "All day".
  - The chip hue follows the item's source, as `itemHue` does today.
  - On a phone-width Home the chip times hide. Month cells become a date plus dots.
- **Data.** Fetch the month range once. The week view filters it, and a size change never clears the feed.

## Edit mode

- **Whole tile.** It is the drag target: `.is-editing .blk {cursor:grab}` with native cursors only. Dragging starts after the pointer moves 4px. While dragging, `html.is-dragging {cursor:grabbing}`.
- **Tile in edit mode.**
  - The card keeps its header readable.
  - The body and footer dim to 0.55 and become inert.
  - ↗ and the aside hide.
  - The ring is 2px `--block-ring`; on tile hover it goes to 55% blue.
- **Remove button.** 24px circle, top-right, at 12px from the top and right edges, with `--text` background and `--bg` icon.
- **Resize grips.** Right edge (`ew-resize`), bottom edge (`ns-resize`) and corner (`nwse-resize`, the `resize-corner` icon).
  - Each edge has a 12px hit strip centred on the border, with a visible pill (4×28 or 28×4).
  - At rest the pills are `--control` at 45%. They turn `--c-blue` on tile hover or while resizing, and always show on `(pointer:coarse)`.
- **Keyboard.** Keep a focusable move handle for keyboard moves (plan §2). It can be visually hidden until focused.
- **Dragging.**
  - The origin cell becomes `.is-placeholder`: `--block-drop` fill, an inset 2px `--block-ring`, and its content hidden.
  - The pointer carries a **static clone** (`cloneNode`, ids removed, remove button and grips removed), rotated −1.2°, scaled 1.02, with shadow `--block-lift` plus the ring. Reduced motion: no rotation or scale.
- **Resizing.**
  - The tile shows its snapped preview size with a 2px `--c-blue` outline (offset 3px) and a "2×2" pill at the bottom-left.
  - Commit only when the snapped size changes.
  - Neighbours are **pushed down**, never reordered. The mockup's `placePushDown()` places the resized tile first, then each other tile in (y, x) order, moving it down until it fits.
  - Tiles slide with FLIP (translate + scale) over 240ms (`--dur-3`) with `--ease-out`. Cancel running animations before measuring. The ▶ Play resize demo is the reference.
- **Sway.** It stays at ±0.3° over 3.2s with `--ease-soft`. It skips the placeholder and the tile being resized, and is off under reduced motion.

## Load motion (`shell/entrance.ts`)

- **When it plays.** Once per browser session (`sessionStorage` key `home-entered`), and never under `prefers-reduced-motion`. When it plays, add `.is-entering` to the grid.
- **Timing.** With 12 default tiles, everything finishes in about 0.85s. The tile index used for delays is capped at 10.

| Effect | Keyframes | Duration / easing | Delay |
|---|---|---|---|
| Tile fade-up | opacity 0 → 1, `translateY(--motion-distance)` → none | `--dur-step` (420ms), `--ease-soft` | `min(i,10) × --home-stagger` |
| Sparkline draw | `pathLength=1`, `stroke-dasharray:1`, dashoffset 1 → 0; end dot fades in last | `--dur-chart`, `--ease-out` | tile + 40ms |
| Progress bar | `scaleX` 0 → 1, origin left (existing `grow`) | `--dur-chart`, `--ease-out` | tile + 40ms |
| Column bars | `scaleY` 0 → 1, origin bottom | `--dur-chart`, `--ease-out` | tile + 40ms + 8ms × bar index |
| Donut sweep | SVG mask circle, `pathLength=100`, dashoffset 100 → 0 (rotated −90°) | `--dur-chart`, `--ease-out` | tile + 40ms |
| Count-up | rAF from 0 to the value, ease-out cubic; keeps prefix, suffix, decimals and grouping ("$4,420", "71%", "48.2k", "1.1 s") | 480ms | tile + 40ms |
| Calendar chips | opacity 0 → 1 | `--dur-3`, `--ease-out` | tile + 80ms + 12ms × min(chip, 10) |

- **Count-up details.** `CountUp` renders the final text first, for SSR, no-JS and the no-motion path. It counts only when the entrance runs, and keeps the width stable with tabular numbers.
- **Sparklines** keep the current kit.tsx visual: 2px stroke, round joins, and an end dot with a `--surface` stroke.

## Unchanged

- Grid tracks, breakpoints and container queries.
- The add-widget gallery, the `.blocks-bar` edit bar, persistence and the role logic.
- Loading skeletons (`aria-busy`). Loading and error states sit inside `wc-body`.
