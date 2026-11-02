/**
 * Design requests: labels, due-date wording and the client copy of who may move what.
 * The server is the authority (DesignRequestDetail.permissions); the board menus use
 * designMoveTargets so a card offers the same moves before its detail is loaded.
 */
import type { Hue } from './colors';
import type { DesignIndicator, DesignRequestItem, DesignStage, DesignType, DesignUpcomingEvent } from './contracts';
import { DESIGN_STAGES } from './contracts';
import { todayPacific } from './format';

export const STAGE_LABEL: Record<DesignStage, string> = {
  new: 'New',
  accepted: 'Accepted',
  'in-progress': 'In progress',
  'in-review': 'In review',
  delivered: 'Delivered',
};

/** Stage hues are dots only (grey, blue, amber, violet, green); null is the neutral grey. */
export const STAGE_HUE: Record<DesignStage, Hue | null> = {
  new: null,
  accepted: 'blue',
  'in-progress': 'amber',
  'in-review': 'violet',
  delivered: 'green',
};

export const TYPE_LABEL: Record<DesignType, string> = {
  poster: 'Poster',
  social: 'Social',
  slides: 'Slides',
  merch: 'Merch',
  video: 'Video',
  other: 'Other',
};

export const TYPE_HUE: Record<DesignType, Hue> = {
  poster: 'blue',
  social: 'violet',
  slides: 'amber',
  merch: 'teal',
  video: 'coral',
  other: 'green',
};

export const URGENCY_LABEL = { normal: 'Normal', high: 'High · date cannot move' } as const;

const dayMs = 86_400_000;
const shortDate = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
const longDate = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' });

export const fmtDue = (d: string) => shortDate.format(new Date(`${d}T00:00:00Z`));
export const fmtDueLong = (d: string) => longDate.format(new Date(`${d}T00:00:00Z`));

export type DueState = 'overdue' | 'soon' | 'ok' | 'none';

/** Overdue or due within three days (selected-zone dates), unless already delivered. */
export function dueState(item: Pick<DesignRequestItem, 'dueDate' | 'stage'>, today = todayPacific()): DueState {
  if (!item.dueDate) return 'none';
  if (item.stage === 'delivered') return 'ok';
  const days = (Date.parse(`${item.dueDate}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / dayMs;
  if (days < 0) return 'overdue';
  if (days <= 3) return 'soon';
  return 'ok';
}

/** "Nov 6 · High urgency", "Nov 4 · Due soon", "Nov 1 · Overdue", "No due date". */
export function dueText(item: Pick<DesignRequestItem, 'dueDate' | 'stage' | 'urgency'>, today = todayPacific()): string {
  if (!item.dueDate) return item.urgency === 'high' ? 'No due date · High urgency' : 'No due date';
  const s = dueState(item, today);
  const tail = item.stage === 'delivered' ? '' : s === 'overdue' ? ' · Overdue' : s === 'soon' ? ' · Due soon' : item.urgency === 'high' ? ' · High urgency' : '';
  return fmtDue(item.dueDate) + tail;
}

/**
 * Where the viewer may move a card: mirrors designMoveTargets + permissions in the Go API.
 * Designers and director+ ("workers") move between the four working stages; an unassigned card
 * leaves New only with a designer (who then takes it). Delivered only follows approval; a
 * delivered card can be reopened. The requester may send a card in review back to In progress.
 */
export function designMoveTargets(item: DesignRequestItem, viewer: { id: string; isDesigner: boolean; isWorker: boolean }): DesignStage[] {
  if (!viewer.isWorker) return item.stage === 'in-review' && item.requester.id === viewer.id ? ['in-progress'] : [];
  if (item.stage === 'delivered') return ['in-progress'];
  return DESIGN_STAGES.filter((s) => s !== item.stage && s !== 'delivered' && (s === 'new' || item.assignee !== null || viewer.isDesigner));
}

export const firstName = (name: string) => name.split(/\s+/)[0] ?? name;

/** The rail's design indicator, written out (the dot is never the only signal). */
export const INDICATOR_LABEL: Record<DesignIndicator, string> = {
  'needs-design': 'Needs design',
  requested: 'Requested',
  'not-needed': 'Not needed',
  done: 'Done',
};

/** "In progress · Jordan", "New · unassigned", "2 requests"; empty when there is nothing to add. */
export function indicatorDetail(e: Pick<DesignUpcomingEvent, 'indicator' | 'requests'>): string {
  if (e.indicator !== 'requested') return '';
  const open = e.requests.filter((r) => r.stage !== 'delivered');
  if (open.length !== 1) return `${open.length} requests open`;
  const r = open[0]!;
  return `${STAGE_LABEL[r.stage]} · ${r.assignee ? firstName(r.assignee.name) : 'unassigned'}`;
}

/** "3 need design · 2 requested · 1 done · 1 not needed" (zero counts left out). */
export function railSummary(events: Pick<DesignUpcomingEvent, 'indicator'>[]): {
  needs: number;
  rest: string;
} {
  const n = (k: DesignIndicator) => events.filter((e) => e.indicator === k).length;
  const rest = [
    [n('requested'), 'requested'],
    [n('done'), 'done'],
    [n('not-needed'), 'not needed'],
  ]
    .filter(([c]) => (c as number) > 0)
    .map(([c, w]) => `${c} ${w}`)
    .join(' · ');
  return { needs: n('needs-design'), rest };
}

export const DESIGN_FILTERS = ['all', 'mine', 'unassigned', 'due'] as const;
export type DesignFilter = (typeof DESIGN_FILTERS)[number];
export const FILTER_LABEL: Record<DesignFilter, string> = {
  all: 'All',
  mine: 'Mine',
  unassigned: 'Unassigned',
  due: 'Due soon',
};

/** Mine: assigned to me or requested by me. Unassigned and Due soon leave delivered work out. */
export function matchesFilter(item: DesignRequestItem, filter: DesignFilter, meId: string, today = todayPacific()): boolean {
  switch (filter) {
    case 'mine':
      return item.assignee?.id === meId || item.requester.id === meId;
    case 'unassigned':
      return item.assignee === null && item.stage !== 'delivered';
    case 'due': {
      const s = dueState(item, today);
      return s === 'soon' || s === 'overdue';
    }
    default:
      return true;
  }
}

/** Nearest due first; undated work last, then most recently updated. */
export const byDue = (a: DesignRequestItem, b: DesignRequestItem) => (a.dueDate ?? '9999-12-31').localeCompare(b.dueDate ?? '9999-12-31') || b.updatedAt.localeCompare(a.updatedAt);

/** My work: what is assigned to me (open), what waits on my approval, and what nobody has taken. */
export function myWork(items: DesignRequestItem[], meId: string) {
  return {
    assigned: items.filter((i) => i.assignee?.id === meId && i.stage !== 'delivered').sort(byDue),
    approve: items.filter((i) => i.requester.id === meId && i.stage === 'in-review').sort(byDue),
    requested: items.filter((i) => i.requester.id === meId && i.stage !== 'in-review' && i.stage !== 'delivered').sort(byDue),
    unassigned: items.filter((i) => i.assignee === null && i.stage !== 'delivered').sort(byDue),
  };
}
