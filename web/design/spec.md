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
