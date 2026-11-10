import type { ComponentChildren } from 'preact';
import type { CurrentUser } from '../../../lib/contracts';
import { Icon, type IconName } from '../../components/Icon';
import { widgetDef, type BlockSize } from '../registry';

/**
 * The V0.7 widget card (design/mockups/home-v07-notes.md § Card shell): a tinted icon chip and
 * title, and the whole card as one link. The title link is stretched over the card with ::after,
 * so rows, chips and buttons inside sit above it and open their own item (no nested anchors).
 */
export default function WidgetCard({ id, me, title, href, aside, foot, busy, class: cls, children }: {
  id: string;
  me: CurrentUser;
  /** Overrides the registry title (role variants). */
  title?: string;
  /** Overrides the registry card link; null removes it. */
  href?: string | null;
  aside?: ComponentChildren;
  foot?: ComponentChildren;
  busy?: boolean;
  class?: string;
  children: ComponentChildren;
}) {
  const def = widgetDef(id);
  const name = title ?? def?.title ?? id;
  const to = href !== undefined ? href : def?.href(me) ?? null;
  const external = !!def?.external && !!to;
  const headingId = `${id}-h`;
  return (
    <section class={`wc${to ? '' : ' wc--nolink'}${cls ? ` ${cls}` : ''}`} data-hue={def?.accent ?? 'blue'} data-block={id} aria-labelledby={headingId} aria-busy={busy ? 'true' : undefined}>
      <header class="wc-head">
        <span class="wc-chip" aria-hidden="true"><Icon name={def?.icon ?? 'blocks'} size={14} /></span>
        <h2 class="wc-title" id={headingId}>
          {to ? <a class="wc-link" href={to} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>{name}{external && <span class="visually-hidden"> (opens in a new tab)</span>}</a> : name}
        </h2>
        {to && <span class="wc-go" aria-hidden="true"><Icon name="arrow-up-right" size={14} stroke={2} /></span>}
        {aside !== undefined && aside !== null && aside !== false && <span class="wc-aside">{aside}</span>}
      </header>
      <div class="wc-body">{children}</div>
      {foot && <footer class="wc-foot">{foot}</footer>}
    </section>
  );
}

/** Empty state: the widget's icon, muted, with one line (24px at 1×1 and 2×1, 32px otherwise). */
export function WidgetEmpty({ icon, size, children }: { icon: IconName; size: BlockSize; children: ComponentChildren }) {
  const small = size === '1x1' || size === '2x1';
  return (
    <div class={`wc-empty${size === '2x1' ? ' wc-empty--row' : ''}`}>
      <Icon name={icon} size={small ? 24 : 32} stroke={1.5} />
      <span>{children}</span>
    </div>
  );
}

/** A widget's loading or error state, inside the card body so the tile keeps its frame. */
export function WidgetState({ state, label }: { state: { status: string; reload?: () => void }; label: string }) {
  if (state.status === 'loading') return <div class="wc-loading" role="status"><span class="visually-hidden">Loading {label}…</span><span class="skel skel--kpi" /><span class="skel skel--kpi" /></div>;
  return <p class="wc-error">Couldn’t load. <button type="button" class="btn btn--small" onClick={state.reload}>Retry</button></p>;
}
