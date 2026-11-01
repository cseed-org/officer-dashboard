# CSEED officer dashboard · visual specification

## Principles

A calm working ledger. White is the canvas; structure comes from spacing, typography and one-pixel rules. Square inspires the number density and predictable tables; Notion informs the quiet sidebar, blocks and inline properties. Precision means aligned columns, explicit states, legible metadata and generous separation between tasks—not decoration.

Use a single ink primary action for the current task. Other actions are hairline buttons or underlined text. Status is a five-pixel dot plus a written label; never a coloured capsule. Yellow (#f5d140) appears only in the tiny wordmark underline, active-navigation edge and a secondary focus accent. No gradients, emoji, glow or ornamental illustrations.

Use system sans-serif throughout, including headings. This is an intentional choice over the v1 serif: the same letterforms connect a page title to a financial line item, and load immediately without font downloads. System monospace is reserved for identifiers and literal secrets. Numeric columns use the main font with tabular figures. Retain the CSEED wordmark; use the existing brand artwork when integrating, with accessible text and a dark variant as needed.

Light is the default. Dark is a separately specified neutral palette, not an inversion. Compact density reduces gaps and row padding without shrinking text. Make state explicit in words, including missing metrics, empty lists, closed periods and failed imports. Never imply that zero and unavailable data are equivalent.

## Tokens and contrast

Import `tokens.css`, then `base.css`, then `components.css`. No framework, external styles or remote fonts. `:root[data-theme="dark"]` explicitly selects dark; `data-theme="system"` follows the OS; missing theme means light. `data-density="compact"` selects compact spacing.

All exact light/dark colour values are in `tokens.css`; measured WCAG relative-luminance ratios are in [contrast.md](contrast.md). Text and semantic status colours exceed 4.5:1 on their intended surfaces; control boundaries exceed 3:1. Decorative separators intentionally have lower contrast and must not be the sole signifier of an interactive control. Yellow on white fails text contrast: it is never text or a sole focus indicator. Focus has a two-pixel ink outline, three-pixel offset and inner yellow accent; the ink outline provides accessible contrast in both themes. Disabled controls are exempt and visibly disabled, never the only explanation of a restriction.

Type scale: 12px metadata, 13px controls/navigation, 14px body, 16px section headings with semibold weight, 28px page title, 32px KPI figures. Body line height 1.5; headings 1.25. Phone titles 21.6px and KPI figures 20.8px. Font weights 400, 500, 600; use 700 only for the wordmark/avatar. No uppercase headings except short command-palette group labels.

Spacing: 4, 8, 12, 16, 20, 24, 32, 40px. Comfortable block padding 20, gap 24 and table vertical padding 12px; compact uses 16, 16 and 8px. Optical exceptions are allowed for icon/text separation, small label baselines and one-pixel borders. Main desktop padding 40px top, 48px sides. Radius 4px for panels and controls; 2px for brand mark; circles only for avatars and status dots. One-pixel hairlines. Cards have no elevation. Only overlays have a restrained shadow (6–10% black). Hover transition 140ms, drawer movement 180ms; no skeleton shimmer. Reduced-motion preference removes all transitions and animations.

## Component contract

Use the existing builder markup. These are exact classes, not illustrative names.

| Component | Classes and state contract |
|---|---|
| Shell | `.app`, `.sidebar`, `.main`, `.topbar`, `.nav-scrim`; `.nav-open` on root opens phone sheet. `.skip-link` targets main. |
| Brand / identity | `.wordmark`, `.wordmark-mark`, `.wordmark-text`, `.wordmark-sub`, `.sidebar-foot`, `.user-chip`, `.user-chip-text`, `.user-chip-name`, `.user-chip-role`, `.avatar`, `.avatar--lg`. |
| Navigation | `.nav-list`, `.nav-link`, `.nav-label`, `.icon`; active link uses `aria-current="page"`. Icon-only rail must retain an accessible name and tooltip. |
| Search | `.search-trigger`, `.search-trigger-text`, `.search-trigger-kbd`. Show ⌘K on Mac, Ctrl K elsewhere. Opens native dialog `.palette` with `.palette-inner`, `.palette-search`, `.palette-input`, `.palette-results`, `.palette-group-label`, `.palette-option`, `.palette-option-title`, `.palette-option-sub`, `.palette-status`, `.palette-foot`. Active option uses `aria-selected`. |
| Page header | `.page-header` contains one h1; `.page-sub` and `.eyebrow` provide context. `.toolbar`, `.toolbar-main`, `.toolbar-actions`, `.toolbar-search`, `.filters` handle actions below the title. |
| Tabs | `.tabs`, `.tab`, `.tab-count`, `.tab-panel`; `role=tab`, `aria-selected`, roving tab index and arrow keys. `.segmented`, `.segment[aria-pressed=true]` for view toggles; `.chips`, `.chip` are rectangular filter controls, never status badges. |
| KPIs | `.kpis` containing `.kpi` (or `.kpi.kpi--link`), `.kpi-label`, `.kpi-value`, `.kpi-sub`; `.kpi-value--negative` only for an actual negative outcome. |
| Blocks | `.stack`, `.stack--tight`, `.home-grid`, `.block`, `.block-head`, `.block-body`, `.block--full`, `.block--bare`; `.section`, `.section-head`, `.section-actions` for unboxed sections. |
| Tables | `.table-wrap` scroll container; `.dt`, optional `.dt--dense`, `.dt--wide`, `.dt--edit`. Sticky header in scroll region. `.num` right-aligns tabular figures, `.mono` only for codes. `.cell-title`, `.cell-sub`, `.dt-primary`, `.dt-hide-phone`. Every td gets `data-label` for phone layout. Caption and scoped column headers required. `.dt-caption` shows caption; `.visually-hidden` retains it for assistive technology. |
| Status | `.badge` with `--positive`, `--negative`, `--attention`, `--info` modifiers; always a text label. `.flag` for a secondary warning. Pending = attention, approved/settled = positive, denied/failed = negative, draft = neutral. |
| Buttons | `.btn` secondary, `.btn--primary` ink, `.btn--danger` red text with hairline, `.btn--text` transparent, `.btn--small`, `.btn--block`; `.icon-btn` must have an accessible name; `.linkish`, `.linkish--danger` underlined text. Use native disabled. |
| Forms | `.form`, `.fieldset`, `.field`, `.field-label`, `.field-hint`, `.field-error`, `.field--inline`, `.field--wide`, `.grid-fields`, `.input`, `.select`, `.textarea`, `.money`, `.input--money`. Error sets `aria-invalid` and connects text with `aria-describedby`. `.choice-row`, `.choice`, `.choice-title`, `.choice-sub` contain native radios. |
| File import | `.dropzone`, `.dropzone.is-over`, `.files`, `.file-name`, `.file-meta`; retain a keyboard-accessible file input and written supported format. Preview rows and errors before committing import. |
| Drawer / side panel | Native dialog `.drawer` or `.drawer.drawer--lg`, `.drawer-inner`, `.drawer-head`, `.drawer-body`. Width 560 or 760px; full viewport on phone. `.side-layout` / `.side-panel` for persistent desktop master/detail illustration. |
| Dialog | Native `.dialog`, `.dialog-inner`, `.dialog-head`, `.dialog-body`. Label with heading; focus initial meaningful control, trap focus, Escape closes, restore trigger focus. Prevent background scroll. CSS alone does not implement interaction. |
| Feedback | `.notice`, `.notice--positive/negative/attention/info`, `.notice-stack`; `.toast` for transient status. Use `role=status` / polite live region for completion, alert for blocking errors. Do not auto-dismiss errors. |
| States | `.state`, `.state--empty`, `.state--loading`, `.state--error`, `.state-title`, `.state-body`, `.state-action`; `.skel`, `.skel--row`, `.skel--label`, `.skel--kpi`. Loading has accessible text and aria-busy; empty gives an actionable explanation; error keeps retry available. |
| Properties / activity | `.facts` is a dl, `.report-meta`, `.detail-meta`; `.list`, `.list-row`, `.list-main`, `.list-side`; `.feed`, `.feed-item`, `.feed-text`, `.feed-time`; `.timeline`, `.timeline-title`, `.timeline-meta`, `.timeline-comment`. |
| Charts | `.chart`, `.chart-grid`, `.chart-axis`, `.chart-label`, `.chart-line`, `.chart-area`, `.chart-dot`, `.chart-col`, `.chart-tooltip`; `.legend`, `.legend-item`, `.swatch`; `.meter`, `.meter-fill`; `.funnel-*` and `.budget-*` support existing components. Muted slate series, grey secondary, direct numeric labels. Include a textual/table equivalent. Never rely on colour alone. |
| Focus | Global `:focus-visible`; do not remove it on actionable controls. Programmatically focused headings/main can suppress the decorative outline only when focus placement is intentional. |

## Page composition

Home: title, reporting-period subtitle, five KPIs; recent events full width; requests needing action and budget/actual in adjacent blocks; chronological activity below. Avoid a second primary CTA where no creation task is present.

Events: search and time filter, one Import CSV button, event table. Detail drawer carries date/location properties, attendance metrics, understated trend chart and guests; import dialog uses the file/preview/error pattern.

Requests & Finance: Requests, Budgets, Reports, Periods tabs. Requests default to actionable entries; one New request action opens a drawer. Detail places amount and status above properties, receipts and history; approve/deny/settle controls follow permission and state. Budgets show allocated/spent/committed/available columns. Reports are statement tables with restrained CSV export. Periods expose close state in text and place destructive close/reopen behind a confirmation.

Admin: Users, Tokens, Audit log tabs, searchable tables. Tokens reveal their secret once in `.secret` with copy action. Audit retains actor, timestamp and target. Settings: separate Theme, Density, Profile sections with native radio groups and inline properties; save preferences immediately and announce the change.

## Responsive layouts

- 1440px: 232px sidebar; 48px main gutters; five KPI columns where content allows; paired overview blocks. Drawer is 560px (760px for a complex detail). Tables stay tabular and scroll vertically with sticky headers.
- 820px: 64px icon rail, 24px main gutters. Collapse sidebar labels, preserve accessible names. Overview blocks become one column, KPI grid auto-fits. Wide statements use labelled rows before ordinary tables do.
- 390px: sticky 52px top bar, menu sheet up to 300px with scrim, 16px main gutters. Two KPI columns; stacked blocks; 44px minimum principal controls. Below 640px table rows become labelled records, with the primary cell first. Important financial values remain visible; only explicitly secondary columns may hide. Drawers occupy the viewport. No page-level horizontal scroll; long identifiers wrap. Tabs may scroll locally.

## Alternatives and iteration

Rejected warm beige canvas (insufficiently white), serif display titles (less coherent with utilitarian rows), coloured status fills (too much visual weight), floating rounded cards/shadows (ornamental), external fonts (network cost without a clear benefit), and a saturated yellow primary button (weak contrast and excessive brand area). Open aesthetic question for a later iteration: preserve the compact system typography or adopt a self-hosted sans after viewing the real content. No choice blocks implementation.

Mockups are local HTML documents referencing exactly the three production stylesheets. They show representative content, not live data or a claim of implemented backend behavior. Their small script only supports preview theme/density, tab examples and menu/dialog controls.

### Integration hooks

`.block-action`, `.section-actions`, `.state-action` wrap action groups; `.block-body`, `.budget-label`, `.palette-option-title` permit long content to wrap. `.data-link` wraps external URLs; `.funnel-value` keeps tabular totals together. `.is-future` and `.user-chip--loading` use muted text; `.palette-esc` does not shrink; `.state--error` has an error boundary. `.kpis--4` fixes four equal columns at desktop widths, retaining auto-fit and the two-column phone layout below that.

Intentional structural hooks without separate rules: `.budget-bars` and `.line-chart` inherit `.chart`; `.swatch--spent` inherits `.swatch` (the first series colour); `.toolbar--wrap` inherits the wrapping `.toolbar`; `.state--empty` inherits `.state`. These hooks do not need empty CSS declarations.

## V0.3 additions

Status: proposal from design Pane D0, for the page design Panes D1–D5 and the V0.3 build Panes. Reference page: [mockups/components-v03.html](mockups/components-v03.html), with screenshots `shots/components-v03-{1440,390}-{light,dark}.png`. Every new class lives in [v03.css](v03.css). That file loads after the three production stylesheets, and build Panes move it into `src/styles/` as described in its header. The live system is v2 iteration 2: 12px cards, 8px controls, 14px overlays, the `--c-*` categorical palette, and transform/opacity motion. It supersedes the 4px radius and the "no animation" lines above. Everything below builds on iteration 2.

### Brand typography decision

**cseed.co returns as a second register, used on a few surfaces, not as the app's typeface.** The dashboard keeps system sans for all work. Three brand faces come back, each with one job, and only on brand surfaces:

| Role | Token | Face | Rules |
|---|---|---|---|
| Display | `--font-display` | Playfair Display 400, with italic for one word at most | 56/36/28/22px (`.display--xl/l/m/s`), tracking −0.035 to −0.01em. Lowercase CSEED voice ("hey alex, welcome in."). Never on buttons, labels, numbers or tables. |
| Brand body | `--font-brand` | Poppins 400/500 | Running copy at 16px with line height 1.65 (`.brand-lede`), never below 14px. Onboarding and the gallery intro only. Not the landing page's 275 weight, which is too light for UI. |
| Brand mono | `--font-mono-brand` | Anonymous Pro 400 | 13px lowercase eyebrows, step counters ("02 / 06") and polaroid captions (`.brand-eyebrow`). Identifiers keep the system mono. |
| Work | `--font` | System sans | Unchanged: tables, forms, Kanban, calendar, widgets, nav, dialogs. |

**Where the brand shows:**
- **Brand-forward:** onboarding (paper ground, display headline, highlighter, polaroid collage), the widget gallery sheet (paper, display title, polaroid-framed previews) and first-run empty states (`.state--brand`).
- **One brand line on a work page:** the Home greeting may set `h1.display`.
- **Calm:** the Members table, Officers directory, Kanban, calendar, finance and admin use no brand faces, no paper and no polaroids.

**Loading.** The fonts are self-hosted latin-subset woff2 files (OFL), 93 KB for all five. They are now in `web/design/fonts/`, and the build Pane copies them to `web/public/fonts/` with `font-display: swap`. Browsers fetch a face only when text uses it, so work pages download nothing. Preload Playfair and Poppins on `/onboarding` only. The old rule against remote fonts still holds, because these files are served from our origin.

**Highlighter.** `.mark` draws a solid `--seed` stroke behind one or two display words, rotated −1.2°. Ink on yellow measures 10.64:1. It is static and never animates. Use at most one per screen.

### New tokens

Both themes are in `v03.css` (`:root`, `[data-theme="dark"]`, and system dark), and the measured ratios are in [contrast.md](contrast.md).

| Token | Light | Dark | Use |
|---|---|---|---|
| `--paper` | `#e4e3e1` | `#1a1916` | Brand ground: onboarding, gallery, `.state--brand`. Never behind tables. |
| `--paper-raised` | `#f4f4f4` | `#24231f` | Fields and raised panels on paper |
| `--brand-ink` / `--brand-ink-soft` | `#141414` / `#3d3d3d` | `#f1efe8` / `#c4c1b8` | Text on paper |
| `--brand-control` | `#76756f` | `#87847c` | Field and button borders on paper (≥ 3:1) |
| `--brand-line` | `#c9c8c3` | `#3a3833` | Decorative rules and the progress track |
| `--seed` | `#f5e530` | same | Highlighter stroke only |
| `--polaroid`, `--polaroid-shadow` | `#fff` | `#e8e6df` | Photo frames, which stay light in dark mode because they are objects |
| `--role-leadership-bg` / `-ink` | `#f5d140` / `#202320` | same | Leadership badge |
| `--widget-cols`, `--widget-row`, `--widget-gap`, `--radius-widget` | 4, 168px, 16px, 16px | phone: 2, 156px, 12px | Widget grid |
| `--ease-soft`, `--dur-step` 420ms, `--dur-reveal` 900ms, `--stagger` 120ms, `--rise` 24px, `--wiggle` 0.5deg | | | Brand motion, taken from cseed.co `motion.css`: slow, soft, no bounce |
| `--overlay-shadow` | | | Lifted widget, dragged card, gallery sheet |

Yellow's role widens slightly. It stays decorative everywhere except where ink text sits on it (the leadership badge and the highlighter). It is never a button fill and never text.

### Component contract (V0.3)

| Component | Classes and state contract |
|---|---|
| Role badge | `.role-badge[data-role=officer\|director\|leadership]`, `.role-badge--sm`. Officer has a blue dot and director a violet dot, matching `ROLE_HUE` in `src/lib/colors.ts`. Leadership is an ink label on `--role-leadership-bg` with an ink diamond instead of the dot, so rank never depends on colour alone. The label is always written. `.role-toggle-opt[data-role=leadership]` adds the third option, which is `disabled` with an `aria-describedby` hint for anyone below leadership. Badges go in the people table's role column, the profile drawer and the user chip. |
| Widget grid | `ul.widget-grid` > `li.widget.widget--s\|m\|l[data-hue]`. S is 1×1, M is 2×1 and L is 2×2 on 4 columns (2 on phone, where S stays half width). The flow is `row dense`. Parts: `.widget-head` > `.widget-icon` + `.widget-title` + optional `.widget-link`, then `.widget-body` (`.widget-figure`, `.widget-sub`, `.widget-split` for figure + sparkline, `.widget-list`), then `.widget-foot`. A disconnected or empty widget uses `.widget-state` > `.widget-state-title` + `.widget-state-body` + a link, saying "Not connected" in words, never "0". Each `li` gets `aria-label` set to the widget name. |
| Widget edit mode | `.widget-grid.is-editing`, preceded by `.edit-bar` (`.edit-bar-title`, `.edit-bar-hint`, Add widget plus a primary Done button). Each widget holds `.widget-edit` > `button.widget-handle` (grip; its name says "Move …; arrow keys move it, Enter drops it"), `.widget-label`, `button.widget-remove` ("Remove …") and `.widget-sizes[role=group]` > `button.widget-size[aria-pressed]` (sizes the widget does not support are `disabled`). Content dims to 42% and turns inert. `.widget.is-lifted` marks the one being dragged, `li.widget-slot` (with `--span-c` and `--span-r`) its drop target, and `button.widget-add` the add tile that ends the grid. Announce moves in a polite live region ("Next event, position 3 of 6"). Changes save to `user_prefs` on Done. Removing a widget needs no confirmation and shows "Removed Web Analytics · Undo" in a `.toast`. |
| Widget gallery | Native `dialog.gallery` > `.gallery-inner` > `.gallery-head` (`.brand-eyebrow`, `h2.display.display--l` with an optional `.mark`, close button, `.brand-lede`), `.gallery-cats` (chips), and `.gallery-body` > `ul.gallery-grid` > `li.gallery-item[style=--tilt]`. Each item holds `.gallery-preview` (a polaroid frame around a real `.widget` at M size, `aria-hidden`), `.gallery-name`, `.gallery-sub` and `.gallery-actions` (sizes plus an Add button, or `.gallery-added` "On your home" once `.is-added`). It is a centred 920px sheet on desktop and a bottom sheet at 92dvh on phone. Cloudflare widgets appear even with no token and say so in `.gallery-sub`. |
| Kanban | `.kanban[role=region][tabindex=0]` (it scrolls locally, never the page) > `section.kanban-col[data-stage=new\|accepted\|in-progress\|in-review\|delivered]` > `.kanban-col-head` (`h3.kanban-col-name`, `.kanban-count`) + `ul.kanban-list` > `li.kanban-card`. A card holds `.kanban-card-title` (a link to the detail drawer), `.kanban-card-meta` (`.kanban-tag[data-hue]` for the type, `.kanban-due` with `.is-soon` or `.is-overdue` and the word "Overdue"), an optional `.kanban-link` to the linked event, and `.kanban-card-foot` (`.kanban-people` avatars or `.kanban-unassigned`, plus a ⋯ menu). States: `.kanban-card.is-dragging`, `li.kanban-drop` placeholder, and `.kanban-empty` text in an empty column. Stage hues are grey, blue, amber, violet and green, shown as dots only. Columns are minmax(200px, 1fr), so five fit at 1440. On phone each column is 84% wide with scroll snap. **Keyboard and touch:** the ⋯ menu has "Move to → stage" and that is the accessible path; dragging is an enhancement. |
| Calendar month | `.cal` > `.cal-head` (`.cal-nav` with prev/next `icon-btn`, `h3.cal-title`, Today, and `.segmented` Month/Week/List) + `.cal-month[role=grid]` > `.cal-dow` × 7 (Monday first) + `.cal-day[role=gridcell]` with `.is-outside`, `.is-today` (ink disc on the date) and `.is-selected`. Each day holds `.cal-date` and up to two `a.cal-event[data-hue]` (`.cal-event-time`, `.cal-event-title`, `.is-draft` outlined), then `button.cal-more`. Event hue = team. On phone, day cells show `.cal-dots` and the selected day's events list below in `ul.cal-agenda`. Every day cell's `aria-label` includes the date and the event count. |
| Calendar week | `.cal-week` (52px gutter + 7 columns, `--slot` 22px per 30 minutes, `--rows` set on `.cal-gutter` and `.cal-col`) > `.cal-week-corner`, `.cal-week-day` (`.is-today`), `.cal-gutter` > `.cal-hour`, and `.cal-col` > `.cal-block[data-hue][style=--start;--len]` (`.cal-block-title`, `.cal-block-time`) plus `.cal-now[style=--now]`. The default span is 9 am to 9 pm. On phone the week view is replaced by the agenda. |
| Onboarding shell | `.onb` (fixed, full screen, paper, Poppins) > `.onb-top` (wordmark, `ol.onb-progress[aria-label="Setup progress, step N of 6"]` > `li.onb-seg` with `.is-done` or `[aria-current=step]`, and `.onb-count` in brand mono, `aria-hidden`) + `.onb-main` (two columns with `.onb-art`, or `.onb-main--single`) > `.onb-step` (`.brand-eyebrow`, `h1/h2.display.onb-title[tabindex=-1]`, `.brand-lede.onb-lede`, `.onb-form`, `dl.onb-prefill`, `ul.onb-checks`) + `.onb-foot` (Back as `btn--text`, primary Continue, and `.onb-skip` "Finish later" on desktop). `.onb-art` > `figure.polaroid[style=--tilt]` with `img` and `figcaption`. The art is hidden below 1100px. On phone the foot sticks to the bottom with the safe-area inset and the primary button fills it. `.onb--framed` exists for mockups only. Steps: hello → confirm (pre-filled name, subteam and Discord ID are read-only, with "Tell <leadership name>") → profile (phone and birthday required; phone hint "Visible to directors and leadership only") → calendar (email picker + delivery mode) → widgets (gallery grid inline) → done. |
| Onboarding motion | Add `.onb-step.is-entering` / `.is-leaving`, and `.onb[data-dir=back]` to mirror. Content enters with a 420ms rise and slide, staggered 120ms (headline, lede, form). The old step leaves in 240ms. Progress fills in 320ms. Polaroids settle over 900ms with a 120ms stagger. The highlighter never animates. There is no confetti and no loops. **Reduced motion:** `base.css` removes every animation and transition, and `v03.css` restates the end states (opacity 1, final tilt), so steps swap instantly. In both modes, focus moves to the new step's heading after each step change. |
| Calendar-email picker | `.email-picker` > `.field-label` + `button.email-picker-btn[aria-haspopup=listbox][aria-expanded]` (`.email-picker-value`, `.email-picker-chev`) + `ul.email-picker-list[role=listbox]` (`--float` when overlaying) > `li.email-option[role=option][aria-selected]` (`.email-option-check`, `.email-option-text`, optional `.email-option-tag`) + `li.email-option--add` "Add another…". At 5 saved, the add option is `aria-disabled="true"` and followed by `.email-picker-note` ("5 of 5 saved. Remove one in Settings to add another."). Choosing add swaps it for `form.email-picker-add` (input, Save, Cancel, and a "4 of 5 after saving" note). `.email-picker-count` sits below the button. Keyboard follows the select-only combobox pattern: arrows, Home/End, Enter and Escape. Removal lives in Settings, not in the picker. |
| Invite dialog | A native `.dialog` with heading "Invite an officer". **Ready:** Name, Discord ID (mono, with a hint on how to copy it), Subteam, and `fieldset` > `.invite-delivery` with two checkbox `.choice` cards, Discord DM and Copy link; at least one must stay checked. **Sending:** a `.facts` summary, then `.invite-status[role=status]` with a `.spinner` and "Sending DM to Taylor…", and the primary button disabled as "Sending…". The spinner turns into a static ring under reduced motion, and the text carries the state. **Sent:** `.invite-status--sent` reading "DM sent ✓ <time>4:12 PM</time>", the `.invite-link` row (mono `code` + Copy link) with `.invite-copied` "Link copied ✓", and the buttons Done and Invite another. **Failed:** `.invite-status--failed[role=alert]` gives the reason in plain words and keeps the link as a fallback, with Retry DM. The Officers table shows the same "DM sent ✓ Nov 1, 4:12 PM" in the invitee's status cell. |
| Brand empty state | `.state.state--brand` (paper, Playfair 24px `.state-title`) with optional `.state-photos` > two `figure.polaroid`. Use it only for first-run emptiness (no design requests, no speakers, empty home). Filtered-to-nothing results keep the plain `.state`. |

### Rules for D1–D5

- Reuse v2 classes first. Add a class only when a page truly needs one, and add it to `v03.css` and this table in the same PR.
- Brand register: onboarding (D2) is the most brand-forward surface. The gallery and empty states (D1, D5) come next. Officers, Members, Events, Speakers and the Kanban (D3–D5) stay on the work register.
- Photos come from cseed.co (`web/design/img/` holds three for mockups). The build uses real club photos with `alt=""` when they are decorative. Never use stock imagery.
- Both themes, 1440 and 390, with no page-level horizontal scroll: `node web/design/capture.mjs <page>` captures just your page and fails on overflow.
- Phone principal controls stay at least 44px. The widget handle and remove button grow to 40 and 36px on phone, and the size buttons sit in the same 44px row.
