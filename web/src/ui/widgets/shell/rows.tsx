import type { ComponentChildren, VNode } from 'preact';
import { fitRows, useBox } from './fit';

/** Compact row heights (widget.css): a two-line row is 5 + 18 + 16 + 5; a one-line row with an avatar is 5 + 24 + 5. */
export const ROW_2 = 44;
export const ROW_1 = 34;

/** A compact row: title line plus muted meta line, an optional side (amount, status). It links to its own item. */
export function Row({ href, title, meta, side, sideClass, lead, external, attrs }: {
  href?: string | null;
  title: ComponentChildren;
  meta?: ComponentChildren;
  side?: ComponentChildren;
  sideClass?: string;
  lead?: ComponentChildren;
  external?: boolean;
  attrs?: Record<string, string | undefined>;
}) {
  const body = <>
    {lead}
    <span class="row-main"><span class="row-title">{title}</span>{meta && <span class="row-meta">{meta}</span>}</span>
    {side && <span class={`row-side${sideClass ? ` ${sideClass}` : ''}`}>{side}</span>}
  </>;
  return <li>{href
    ? <a class="row" href={href} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})} {...attrs}>{body}</a>
    : <div class="row" {...attrs}>{body}</div>}</li>;
}

/** Meta parts joined with " · " (falsy parts are dropped). */
export function Meta({ parts }: { parts: ComponentChildren[] }) {
  const kept = parts.filter((p) => p !== null && p !== undefined && p !== false && p !== '');
  return <>{kept.map((p, i) => <>{i > 0 && <span class="sep">·</span>}{p}</>)}</>;
}

/**
 * A list that shows as many rows as fit the card, then "+n more". Height is measured once and then
 * by ResizeObserver, so resizing a tile re-fits without measuring on every render.
 */
export function FitList<T>({ items, row, render, fallback, columns = 1, hiddenBeyond = 0, more, gap = 2 }: {
  items: T[];
  /** Height of one row in px. */
  row: number;
  render: (item: T, index: number) => VNode;
  /** Rows to show before the first measurement. */
  fallback: number;
  columns?: number;
  /** Items that exist but were not fetched (counted in "+n more"). */
  hiddenBeyond?: number;
  /** The overflow line, given the hidden items; defaults to "+n more". */
  more?: (hidden: T[], count: number) => ComponentChildren;
  gap?: number;
}) {
  const [ref, box] = useBox();
  const n = fitRows({ height: box.height, total: items.length + (hiddenBeyond ? 1 : 0), row, gap, more: 16, columns, fallback });
  const shown = items.slice(0, Math.min(n, items.length));
  const hidden = items.slice(shown.length);
  const count = hidden.length + hiddenBeyond;
  return (
    <div class="wc-fit" ref={ref}>
      <ul class={`rows${columns > 1 ? ' rows--2' : ''}`}>{shown.map(render)}</ul>
      {count > 0 && <span class="row-more">{more ? more(hidden, count) : `+${count} more`}</span>}
    </div>
  );
}
