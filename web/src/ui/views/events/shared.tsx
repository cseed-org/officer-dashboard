import type { EventListItem } from '../../../lib/contracts';
import { whenShort } from '../../../lib/calendar';
import { fmtShort, todayPacific, type Tone } from '../../../lib/format';
import { Badge } from '../../components/kit';

export type ShownStatus = 'draft' | 'published' | 'completed' | 'cancelled';

/** An event planned as a date range instead of a fixed start ("No set date yet"). */
export const isTentative = (e: Pick<EventListItem, 'tentativeFrom'>) => !!e.tentativeFrom;

/** The last day of a tentative range (a range with no end is one day). */
export const tentativeEnd = (e: Pick<EventListItem, 'tentativeFrom' | 'tentativeTo'>) => (e.tentativeTo ?? e.tentativeFrom)!;

/** Still ahead: a fixed event that hasn't started, or a tentative one whose range hasn't ended. */
export function isUpcoming(e: Pick<EventListItem, 'startsAt' | 'tentativeFrom' | 'tentativeTo'>, now = Date.now()): boolean {
  return isTentative(e) ? tentativeEnd(e) >= todayPacific(new Date(now)) : new Date(e.startsAt).getTime() >= now;
}

/** Tentative events whose range overlaps the inclusive days from–to (cancelled ones are left out). */
export const tentativeIn = (items: EventListItem[], from: string, to: string) =>
  items.filter((e) => isTentative(e) && e.status !== 'cancelled' && e.tentativeFrom! <= to && tentativeEnd(e) >= from);

/** A published event whose end (or start, or tentative range) has passed reads as completed. */
export function shownStatus(e: Pick<EventListItem, 'status' | 'startsAt' | 'endsAt' | 'tentativeFrom' | 'tentativeTo'>, now = Date.now()): ShownStatus {
  if (e.status === 'published') {
    if (isTentative(e)) {
      if (tentativeEnd(e) < todayPacific(new Date(now))) return 'completed';
    } else if (new Date(e.endsAt ?? e.startsAt).getTime() < now) return 'completed';
  }
  return e.status;
}

const LABEL: Record<ShownStatus, string> = { draft: 'Draft', published: 'Published', completed: 'Completed', cancelled: 'Cancelled' };
const TONE: Record<ShownStatus, Tone> = { draft: 'attention', published: 'positive', completed: 'positive', cancelled: 'negative' };

export function EventStatusBadge({ event, now }: { event: Pick<EventListItem, 'status' | 'startsAt' | 'endsAt' | 'tentativeFrom' | 'tentativeTo'>; now?: number }) {
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

/** The list's When cell: a fixed start/end, or the tentative range with its badge. */
export function EventWhen({ e }: { e: Pick<EventListItem, 'startsAt' | 'endsAt' | 'tentativeFrom' | 'tentativeTo'> }) {
  if (isTentative(e)) {
    return (
      <span class="bs-nowrap" data-tentative-range>
        {e.tentativeFrom === tentativeEnd(e) ? fmtShort(e.tentativeFrom!) : `${fmtShort(e.tentativeFrom!)} – ${fmtShort(tentativeEnd(e))}`} <Badge tone="attention">Tentative</Badge>
      </span>
    );
  }
  return <span class="bs-nowrap">{whenShort(e.startsAt, e.endsAt)}</span>;
}

/** Sort for the list: upcoming soonest first, then past most recent first. A tentative event is upcoming until its range ends. */
export function listOrder(items: EventListItem[], now = Date.now()): EventListItem[] {
  const today = todayPacific(new Date(now));
  const t = (e: EventListItem) => (isTentative(e) ? Date.parse(e.tentativeFrom + 'T12:00:00Z') : new Date(e.startsAt).getTime());
  const upcoming = (e: EventListItem) => (isTentative(e) ? tentativeEnd(e) >= today : t(e) >= now);
  const up = items.filter(upcoming).sort((a, b) => t(a) - t(b));
  const past = items.filter((e) => !upcoming(e)).sort((a, b) => t(b) - t(a));
  return [...up, ...past];
}

/**
 * "Request design": the Design board's new-request form, prefilled from the event by the
 * board itself (design-requests Pane, `GET /api/events/:id/design-request-draft`).
 */
export const designRequestHref = (eventId: string) => `/design?new=1&eventId=${encodeURIComponent(eventId)}`;
