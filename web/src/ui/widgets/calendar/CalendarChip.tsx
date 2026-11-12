import { useEffect, useId, useRef, useState } from 'preact/hooks';
import { createPortal } from 'preact/compat';
import type { CalendarFeedItem, CalendarFeedSource } from '../../../lib/contracts';
import { pacificParts, shortDay, timeRange } from '../../../lib/calendar';
import { Icon } from '../../components/Icon';

const HUES = ['blue', 'amber', 'teal', 'coral', 'violet', 'green'];
export const safeHue = (hue?: string) => (HUES.includes(hue ?? '') ? hue! : 'teal');
const SOURCE_ICONS = ['pen', 'user', 'check', 'wallet'] as const;
export const sourceIcon = (source?: CalendarFeedSource) => SOURCE_ICONS.find((icon) => icon === source?.icon) ?? 'calendar';

/** "6:30p", "12p", or "All day". */
export function shortTime(iso: string | null) {
  if (!iso) return 'All day';
  const minutes = pacificParts(iso).minutes;
  const hour = Math.floor(minutes / 60);
  return `${hour % 12 || 12}${minutes % 60 ? ':' + String(minutes % 60).padStart(2, '0') : ''}${hour < 12 ? 'a' : 'p'}`;
}

/**
 * One calendar item as a chip that opens its own item. Hover or focus shows a details card
 * (portalled, fixed) so the chip never needs a menu or button.
 */
export function CalendarChip({ item, source, index, twoLine }: { item: CalendarFeedItem; source?: CalendarFeedSource; index: number; twoLine?: boolean }) {
  const [position, setPosition] = useState<{ top: number; left: number } | null>(null);
  const wrapper = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLAnchorElement>(null);
  const card = useRef<HTMLDivElement>(null);
  const id = useId();
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const hue = safeHue(source?.hue);
  const cancelClose = () => { if (closeTimer.current) clearTimeout(closeTimer.current); };
  useEffect(() => () => cancelClose(), []);
  const contains = (node: Node | null) => !!node && (!!wrapper.current?.contains(node) || !!card.current?.contains(node));
  const leave = () => { cancelClose(); closeTimer.current = setTimeout(() => { if (!contains(document.activeElement)) setPosition(null); }, 180); };
  const blur = (event: FocusEvent) => { if (!contains(event.relatedTarget as Node | null)) setPosition(null); };
  const escape = (event: KeyboardEvent) => { if (event.key === 'Escape') { event.preventDefault(); cancelClose(); trigger.current?.focus(); setPosition(null); } };
  const open = () => {
    // Edit mode makes the body inert; no details card while arranging.
    if (trigger.current?.closest('.is-editing')) return;
    cancelClose();
    const rect = trigger.current?.getBoundingClientRect();
    if (!rect) return;
    const width = Math.min(280, window.innerWidth - 24);
    const top = rect.bottom + 200 > window.innerHeight ? Math.max(12, rect.top - 190) : rect.bottom + 5;
    setPosition({ top, left: Math.max(12, Math.min(rect.left, window.innerWidth - width - 12)) });
  };
  useEffect(() => {
    if (!position) return;
    const close = () => setPosition(null);
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    return () => { window.removeEventListener('scroll', close, true); window.removeEventListener('resize', close); };
  }, [!!position]);
  return <div ref={wrapper} class="cal-chip-wrap" onMouseEnter={open} onMouseLeave={leave} onFocusIn={open} onFocusOut={blur}
    onKeyDown={(e) => {
      if (e.key === 'Tab' && !e.shiftKey && position && e.target === trigger.current) { e.preventDefault(); card.current?.querySelector<HTMLAnchorElement>('a')?.focus(); }
      else escape(e);
    }}>
    <a ref={trigger} href={item.href} class={`cal-chip${twoLine ? ' cal-chip--2' : ''}`} data-hue={hue} style={{ '--c': index }} aria-describedby={position ? id : undefined}>
      <time dateTime={item.startsAt ?? item.date}>{shortTime(item.startsAt)}</time>
      <span>{item.title}</span>
    </a>
    {position && createPortal(<div ref={card} id={id} class="cw-item-popover" data-hue={hue} onMouseEnter={cancelClose} onMouseLeave={leave} onFocusIn={cancelClose} onFocusOut={blur} onKeyDown={(e) => {
      const links = card.current?.querySelectorAll('a');
      if (e.key === 'Tab' && e.shiftKey && e.target === links?.[0]) { e.preventDefault(); trigger.current?.focus(); }
      else if (e.key === 'Tab' && !e.shiftKey && e.target === links?.[links.length - 1]) {
        const stops = [...document.querySelectorAll<HTMLElement>('a[href],button:not(:disabled),summary,select,input,[tabindex="0"]')].filter((el) => el.getClientRects().length && !card.current?.contains(el));
        const next = stops[stops.indexOf(trigger.current!) + 1];
        if (next) { e.preventDefault(); next.focus(); setPosition(null); }
      } else escape(e);
    }}
    style={{ top: `${position.top}px`, left: `${position.left}px` }} role="group" aria-label={`${item.title} details`}>
      <strong>{item.title}</strong>
      <span>{item.startsAt ? timeRange(item.startsAt, item.endsAt) : 'All day'} · {shortDay(item.date)}</span>
      <span>{item.location || (item.startsAt ? 'Location not set' : 'Deadline')}</span>
      {item.detail && <span class="cw-popover-detail">{item.detail}</span>}
      <span class="cw-popover-source"><Icon name={sourceIcon(source)} size={13} />{source?.label ?? item.source}</span>
      <a href={item.href}>Open details →</a>
      {item.lumaUrl && <a href={item.lumaUrl} target="_blank" rel="noopener noreferrer">Open in Luma ↗</a>}
    </div>, document.body)}
  </div>;
}
