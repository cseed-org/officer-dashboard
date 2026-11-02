import type { EventListItem } from '../../../lib/contracts';
import type { Tone } from '../../../lib/format';
import { Badge } from '../../components/kit';

export type ShownStatus = 'draft' | 'published' | 'completed' | 'cancelled';

/** A published event whose end (or start) has passed reads as completed. */
export function shownStatus(e: Pick<EventListItem, 'status' | 'startsAt' | 'endsAt'>, now = Date.now()): ShownStatus {
  if (e.status === 'published' && new Date(e.endsAt ?? e.startsAt).getTime() < now) return 'completed';
  return e.status;
}

const LABEL: Record<ShownStatus, string> = { draft: 'Draft', published: 'Published', completed: 'Completed', cancelled: 'Cancelled' };
const TONE: Record<ShownStatus, Tone> = { draft: 'attention', published: 'positive', completed: 'positive', cancelled: 'negative' };

export function EventStatusBadge({ event, now }: { event: Pick<EventListItem, 'status' | 'startsAt' | 'endsAt'>; now?: number }) {
  const s = shownStatus(event, now);
  return (
    <Badge tone={TONE[s]}>
      <span data-event-status={s}>{LABEL[s]}</span>
    </Badge>
  );
}

export const statusLabel = (s: ShownStatus) => LABEL[s];

/** Query-string panels on /events. Drafts open in the editor; everything else in its detail panel. */
export const eventPanel = {
  detail: (id: string) => ({ id }),
  edit: (id: string) => ({ edit: id }),
  kit: (id: string) => ({ kit: id }),
  remove: (id: string) => ({ delete: id }),
};
export const openPatch = (e: Pick<EventListItem, 'id' | 'status'>) => (e.status === 'draft' ? eventPanel.edit(e.id) : eventPanel.detail(e.id));

export const hostLabel = (e: Pick<EventListItem, 'host' | 'hostSubteam'>) =>
  [e.hostSubteam?.name, e.host?.name].filter(Boolean).join(' · ') || null;

/** Sort for the list: upcoming soonest first, then past most recent first. */
export function listOrder(items: EventListItem[], now = Date.now()): EventListItem[] {
  const t = (e: EventListItem) => new Date(e.startsAt).getTime();
  const up = items.filter((e) => t(e) >= now).sort((a, b) => t(a) - t(b));
  const past = items.filter((e) => t(e) < now).sort((a, b) => t(b) - t(a));
  return [...up, ...past];
}

/**
 * "Request design": the Design board's new-request form, prefilled from the event by the
 * board itself (design-requests Pane, `GET /api/events/:id/design-request-draft`).
 */
export const designRequestHref = (eventId: string) => `/design?new=1&eventId=${encodeURIComponent(eventId)}`;
