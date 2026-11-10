import type { ComponentChildren, TargetedKeyboardEvent } from 'preact';
import { useLayoutEffect, useRef } from 'preact/hooks';
import { categoryHue, STATUS_HUE, type Hue } from '../../lib/colors';
import type { PurchaseStatus } from '../../lib/domain';
import { pct, STATUS_LABEL, STATUS_TONE, type Tone } from '../../lib/format';
import { Icon, type IconName } from './Icon';

/** A status: a coloured dot plus a written label (never colour alone). `hue` overrides the tone's dot. */
export function Badge({ tone, hue, children }: { tone: Tone; hue?: Hue | null; children: ComponentChildren }) {
  return (
    <span class={`badge badge--${tone}`} data-hue={hue ?? undefined}>
      {children}
    </span>
  );
}

export function StatusBadge({ status }: { status: PurchaseStatus }) {
  return (
    <span class={`badge badge--${STATUS_TONE[status]}`} data-hue={STATUS_HUE[status] ?? undefined} data-status={status}>
      {STATUS_LABEL[status]}
    </span>
  );
}

/** Budget category: its fixed hue as a dot beside the name. */
export function CategoryDot({ code }: { code: string }) {
  return <span class="cat-dot" data-hue={categoryHue(code)} aria-hidden="true" />;
}

/** Keep figures stable and readable at first paint; animate the containing surface instead. */
export function CountUp({ value }: { value: string | number }) {
  return <>{value}</>;
}

/** A tiny trend line (decorative: the tile's text carries the numbers). `width`/`height` let a
 *  widget scale it to the space it has; the stroke and end dot stay the same size. */
export function Sparkline({ values, hue, width = 96, height = 28 }: { values: (number | null)[]; hue: Hue; width?: number; height?: number }) {
  const pts = values.filter((v): v is number => v !== null && Number.isFinite(v));
  if (pts.length < 2) return null;
  const W = Math.max(24, Math.round(width));
  const H = Math.max(16, Math.round(height));
  const lo = Math.min(...pts);
  const hi = Math.max(...pts);
  const span = hi - lo || 1;
  const xy = pts.map((v, i) => [(i / (pts.length - 1)) * (W - 6) + 3, H - 4 - ((v - lo) / span) * (H - 8)] as const);
  const d = xy.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('');
  const [lx, ly] = xy[xy.length - 1]!;
  return (
    <svg class="sparkline" data-hue={hue} viewBox={`0 0 ${W} ${H}`} width={W} height={H} aria-hidden="true" focusable="false">
      <path d={d} class="sparkline-line" pathLength={1} />
      <circle cx={lx} cy={ly} r={2.5} class="sparkline-dot" />
    </svg>
  );
}

/** A horizontal progress bar for a share (0..1+), coloured by its hue. */
export function Progress({ value, hue, label }: { value: number | null; hue: Hue; label: string }) {
  const v = value === null ? 0 : Math.max(0, Math.min(value, 1));
  return (
    <span class="progress" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value === null ? undefined : Math.round(value * 100)} aria-valuetext={value === null ? 'no budget' : pct(value)}>
      <span class="progress-fill" data-hue={hue} style={{ width: `${v * 100}%` }} />
    </span>
  );
}

export function Kpi({
  label,
  value,
  sub,
  tone,
  href,
  icon,
  hue,
  trend,
  tile,
}: {
  label: string;
  value: ComponentChildren;
  sub?: ComponentChildren;
  tone?: Tone;
  href?: string;
  /** Tinted icon chip. */
  icon?: IconName;
  hue?: Hue;
  /** Sparkline, progress bar or delta under the figure. */
  trend?: ComponentChildren;
  /** Render as a Home block-grid stat tile (`.tile.tile--stat`). */
  tile?: boolean;
}) {
  const extra = tile ? ' tile tile--stat' : '';
  const body = (
    <>
      <span class="kpi-head">
        {icon && (
          <span class="kpi-chip" data-hue={hue ?? 'blue'} aria-hidden="true">
            <Icon name={icon} size={16} />
          </span>
        )}
        <span class="kpi-label">{label}</span>
      </span>
      <span class={`kpi-value${tone ? ` kpi-value--${tone}` : ''}`}>{typeof value === 'string' || typeof value === 'number' ? <CountUp value={value} /> : value}</span>
      {trend && <span class="kpi-trend">{trend}</span>}
      {sub && <span class="kpi-sub">{sub}</span>}
    </>
  );
  return href ? (
    <a class={`kpi kpi--link${extra}`} href={href}>
      {body}
    </a>
  ) : (
    <div class={`kpi${extra}`}>{body}</div>
  );
}

/** A thin rate meter: 0..1 (values above 1 are drawn full and toned negative). */
export function Meter({ value, label, tone = 'info', max = 1, hue }: { value: number | null; label: string; tone?: Tone; max?: number; hue?: Hue }) {
  const v = value === null ? 0 : Math.max(0, Math.min(value / max, 1));
  return (
    <span class="meter" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value === null ? undefined : Math.round(value * 100)} aria-valuetext={value === null ? 'no data' : pct(value)}>
      <span class={`meter-fill meter-fill--${tone}`} data-hue={hue} style={{ width: `${v * 100}%` }} />
    </span>
  );
}

export interface Column<T> {
  key: string;
  label: ComponentChildren;
  render: (row: T) => ComponentChildren;
  /** Right-aligned mono figures. */
  num?: boolean;
  /** Shown as the card title on phones (first such column). */
  primary?: boolean;
  /** Hide on phones. */
  hideOnPhone?: boolean;
  /** Plain-text label for the phone card (defaults to label when it is a string). */
  cardLabel?: string;
}

/**
 * Hairline table on tablet/desktop; under 640px each row becomes a stacked card where
 * cells show their column label (data-label).
 */
export function DataTable<T>({
  rows,
  columns,
  rowKey,
  caption,
  foot,
  dense,
  showCaption,
  wide,
}: {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  caption?: string;
  foot?: ComponentChildren;
  dense?: boolean;
  /** Show the caption as a heading (statements with several tables). */
  showCaption?: boolean;
  /** Many columns: use stacked cards below desktop width, not only on phones. */
  wide?: boolean;
}) {
  return (
    <div class="table-wrap">
      <table class={`dt${dense ? ' dt--dense' : ''}${wide ? ' dt--wide' : ''}`}>
        {caption && <caption class={showCaption ? 'dt-caption' : 'visually-hidden'}>{caption}</caption>}
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} scope="col" class={c.num ? 'num' : undefined}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={rowKey(row)}>
              {columns.map((c) => (
                <td
                  key={c.key}
                  class={[c.num ? 'num' : '', c.primary ? 'dt-primary' : '', c.hideOnPhone ? 'dt-hide-phone' : ''].filter(Boolean).join(' ') || undefined}
                  data-label={c.cardLabel ?? (typeof c.label === 'string' ? c.label : '')}
                >
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
        {foot && <tfoot>{foot}</tfoot>}
      </table>
    </div>
  );
}

/** Accessible tabs (roving tabindex, arrow keys). Panels are rendered by the caller. */
export function Tabs<K extends string>({
  tabs,
  active,
  onChange,
  label,
  idPrefix,
}: {
  tabs: { key: K; label: ComponentChildren; count?: number }[];
  active: K;
  onChange: (k: K) => void;
  label: string;
  idPrefix: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const bar = useRef<HTMLSpanElement>(null);
  // The underline is one element that slides (transform only) to the selected tab.
  useLayoutEffect(() => {
    const place = () => {
      const t = ref.current?.querySelector<HTMLElement>(`[data-key="${active}"]`);
      if (!t || !bar.current) return;
      bar.current.style.transform = `translateX(${t.offsetLeft}px) scaleX(${t.offsetWidth})`;
      bar.current.dataset.ready = '';
    };
    place();
    addEventListener('resize', place);
    return () => removeEventListener('resize', place);
  }, [active, tabs.length]);
  const onKey = (e: TargetedKeyboardEvent<HTMLButtonElement>) => {
    const i = tabs.findIndex((t) => t.key === active);
    let next = -1;
    if (e.key === 'ArrowRight') next = (i + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    if (next < 0) return;
    e.preventDefault();
    onChange(tabs[next]!.key);
    requestAnimationFrame(() => ref.current?.querySelector<HTMLButtonElement>(`[data-key="${tabs[next]!.key}"]`)?.focus());
  };
  return (
    <div class="tabs" role="tablist" aria-label={label} ref={ref}>
      {tabs.map((t) => (
        <button
          type="button"
          role="tab"
          key={t.key}
          data-key={t.key}
          id={`${idPrefix}-tab-${t.key}`}
          aria-controls={`${idPrefix}-panel`}
          aria-selected={t.key === active}
          tabIndex={t.key === active ? 0 : -1}
          class="tab"
          onClick={() => onChange(t.key)}
          onKeyDown={onKey}
        >
          {t.label}
          {t.count !== undefined && <span class="tab-count">{t.count}</span>}
        </button>
      ))}
      <span class="tab-indicator" ref={bar} aria-hidden="true" />
    </div>
  );
}

export function Field({
  id,
  label,
  hint,
  error,
  children,
  optional,
  wide,
}: {
  id: string;
  label: ComponentChildren;
  hint?: ComponentChildren;
  error?: string;
  children: ComponentChildren;
  optional?: boolean;
  wide?: boolean;
}) {
  return (
    <div class={`field${error ? ' field--error' : ''}${wide ? ' field--wide' : ''}`}>
      <label class="field-label" for={id}>
        {label}
        {optional && <span class="field-optional"> (optional)</span>}
      </label>
      {children}
      {hint && !error && (
        <p class="field-hint" id={`${id}-hint`}>
          {hint}
        </p>
      )}
      {error && (
        <p class="field-error" id={`${id}-error`}>
          {error}
        </p>
      )}
    </div>
  );
}

/** aria-describedby / aria-invalid for an input inside <Field>. */
export const describe = (id: string, error?: string, hint?: boolean) => ({
  id,
  'aria-invalid': error ? true : undefined,
  'aria-describedby': error ? `${id}-error` : hint ? `${id}-hint` : undefined,
});

export function PageHead({ children, actions }: { children?: ComponentChildren; actions?: ComponentChildren }) {
  return (
    <div class="page-tools">
      <div class="page-tools-main">{children}</div>
      {actions && <div class="page-tools-actions">{actions}</div>}
    </div>
  );
}

export function Section({ title, children, actions, id }: { title: ComponentChildren; children: ComponentChildren; actions?: ComponentChildren; id?: string }) {
  return (
    <section class="section" aria-labelledby={id}>
      <div class="section-head">
        <h2 id={id}>{title}</h2>
        {actions && <div class="section-actions">{actions}</div>}
      </div>
      {children}
    </section>
  );
}
