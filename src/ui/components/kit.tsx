import type { ComponentChildren, TargetedKeyboardEvent } from 'preact';
import { useRef } from 'preact/hooks';
import type { PurchaseStatus } from '../../core/domain';
import { pct, STATUS_LABEL, STATUS_TONE, type Tone } from '../format';

export function Badge({ tone, children }: { tone: Tone; children: ComponentChildren }) {
  return <span class={`badge badge--${tone}`}>{children}</span>;
}

export function StatusBadge({ status }: { status: PurchaseStatus }) {
  return <Badge tone={STATUS_TONE[status]}>{STATUS_LABEL[status]}</Badge>;
}

export function Kpi({ label, value, sub, tone, href }: { label: string; value: ComponentChildren; sub?: ComponentChildren; tone?: Tone; href?: string }) {
  const body = (
    <>
      <span class="kpi-label">{label}</span>
      <span class={`kpi-value${tone ? ` kpi-value--${tone}` : ''}`}>{value}</span>
      {sub && <span class="kpi-sub">{sub}</span>}
    </>
  );
  return href ? (
    <a class="kpi kpi--link" href={href}>
      {body}
    </a>
  ) : (
    <div class="kpi">{body}</div>
  );
}

/** A thin rate meter: 0..1 (values above 1 are drawn full and toned negative). */
export function Meter({ value, label, tone = 'info', max = 1 }: { value: number | null; label: string; tone?: Tone; max?: number }) {
  const v = value === null ? 0 : Math.max(0, Math.min(value / max, 1));
  return (
    <span class="meter" role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value === null ? undefined : Math.round(value * 100)} aria-valuetext={value === null ? 'no data' : pct(value)}>
      <span class={`meter-fill meter-fill--${tone}`} style={{ width: `${v * 100}%` }} />
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
}: {
  rows: T[];
  columns: Column<T>[];
  rowKey: (row: T) => string;
  caption?: string;
  foot?: ComponentChildren;
  dense?: boolean;
  /** Show the caption as a heading (statements with several tables). */
  showCaption?: boolean;
}) {
  return (
    <div class="table-wrap">
      <table class={`dt${dense ? ' dt--dense' : ''}`}>
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
