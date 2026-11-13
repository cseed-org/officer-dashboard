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
 * Designers and director+ move work; designers take unassigned work when moving it.
 * Leadership may deliver with explicit confirmation. Other delivery follows requester approval.
 * The design team may reopen a delivered card into any earlier stage (Undo restores the stage it
 * was delivered from: In review, or New/Accepted after a direct leadership delivery);
 * requesters may send work in review back to In progress. The requester who approved may also
 * undo that approval, but list items don't say who approved, so only the detail
 * (permissions.moveTo) offers that move.
 */
export function designMoveTargets(item: DesignRequestItem, viewer: { id: string; isDesigner: boolean; isWorker: boolean; isLeadership?: boolean }): DesignStage[] {
  if (!viewer.isWorker) return item.stage === 'in-review' && item.requester.id === viewer.id ? ['in-progress'] : [];
  return DESIGN_STAGES.filter((s) => s !== item.stage && (s !== 'delivered' || viewer.isLeadership));
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

/** Header figures for the Design page. "Waiting on me" counts distinct requests (assigned and to-approve can overlap). */
export function designStats(items: DesignRequestItem[], meId: string, today = todayPacific()) {
  const open = items.filter((i) => i.stage !== 'delivered');
  const w = myWork(items, meId);
  return {
    open: open.length,
    inProgress: items.filter((i) => i.stage === 'in-progress').length,
    mine: new Set([...w.assigned, ...w.approve].map((i) => i.id)).size,
    due: items.filter((i) => matchesFilter(i, 'due', meId, today)).length,
    overdue: open.filter((i) => dueState(i, today) === 'overdue').length,
  };
}

// ───────────── V0.10 board lanes: To do / Doing / Review / Done over the five stages ─────────────

export const DESIGN_LANES = ['todo', 'doing', 'review', 'done'] as const;
export type DesignLane = (typeof DESIGN_LANES)[number];
export const LANE_LABEL: Record<DesignLane, string> = { todo: 'To do', doing: 'Doing', review: 'Review', done: 'Done' };
/** The stages a lane holds, in the order a drop prefers them (To do takes Accepted before New). */
export const LANE_STAGES: Record<DesignLane, DesignStage[]> = {
  todo: ['accepted', 'new'],
  doing: ['in-progress'],
  review: ['in-review'],
  done: ['delivered'],
};
export const laneOf = (stage: DesignStage): DesignLane =>
  stage === 'new' || stage === 'accepted' ? 'todo' : stage === 'in-progress' ? 'doing' : stage === 'in-review' ? 'review' : 'done';

type Viewer = Parameters<typeof designMoveTargets>[1];
/** The stage a drop on `lane` sets, or null when the viewer may not move the card there (or it is already there). */
export function laneTarget(item: DesignRequestItem, lane: DesignLane, viewer: Viewer): DesignStage | null {
  if (laneOf(item.stage) === lane) return null;
  const allowed = designMoveTargets(item, viewer);
  return LANE_STAGES[lane].find((s) => allowed.includes(s)) ?? null;
}
/** Why a lane refuses a card while dragging (shown on the dimmed lane). */
export function laneRefusal(lane: DesignLane, viewer: Viewer): string {
  if (lane === 'done' && !viewer.isLeadership) return 'The requester approves into Done';
  return 'Not allowed from here';
}

/** "Needs approval": a new request the design team has not accepted yet. */
export const needsApproval = (i: Pick<DesignRequestItem, 'stage'>) => i.stage === 'new';

/** This week: open work that is overdue, due within seven days, or waiting on my approval; most urgent first. */
export function thisWeek(items: DesignRequestItem[], meId: string, today = todayPacific(), limit = 4): DesignRequestItem[] {
  const week = (i: DesignRequestItem) => {
    if (!i.dueDate || i.stage === 'delivered') return false;
    return (Date.parse(`${i.dueDate}T00:00:00Z`) - Date.parse(`${today}T00:00:00Z`)) / dayMs <= 7;
  };
  const approve = (i: DesignRequestItem) => i.stage === 'in-review' && i.requester.id === meId;
  return items.filter((i) => week(i) || approve(i)).sort(byDue).slice(0, limit);
}

/** Group a lane's cards by designer: me first, then by name, unassigned last; nearest due first inside. */
export function byDesigner(items: DesignRequestItem[], meId: string): { key: string; name: string; person: DesignRequestItem['assignee']; items: DesignRequestItem[] }[] {
  const groups = new Map<string, { key: string; name: string; person: DesignRequestItem['assignee']; items: DesignRequestItem[] }>();
  for (const i of items) {
    const key = i.assignee?.id ?? '';
    if (!groups.has(key)) groups.set(key, { key, name: i.assignee ? (i.assignee.id === meId ? 'You' : i.assignee.name) : 'Unassigned', person: i.assignee, items: [] });
    groups.get(key)!.items.push(i);
  }
  const rank = (g: { key: string }) => (g.key === meId ? 0 : g.key === '' ? 2 : 1);
  return [...groups.values()].map((g) => ({ ...g, items: g.items.sort(byDue) })).sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name));
}

/** Design-file links on a request. No data source yet: an agent fills these later. */
export interface DesignLinkSet {
  /** The working file (Figma or Canva, picked from the URL). */
  design?: string | null;
  /** The shared folder with exports and assets. */
  folder?: string | null;
}
export type DesignTool = 'figma' | 'canva' | 'file';
/** Figma or Canva from a link's host; anything else is a generic design file. */
export function designTool(url: string | null | undefined): DesignTool {
  if (!url) return 'file';
  try {
    const host = new URL(url).hostname.toLowerCase();
    if (host === 'figma.com' || host.endsWith('.figma.com')) return 'figma';
    if (host === 'canva.com' || host.endsWith('.canva.com') || host === 'canva.link') return 'canva';
  } catch {
    /* not a URL */
  }
  return 'file';
}
export const DESIGN_TOOL_LABEL: Record<DesignTool, string> = { figma: 'Figma', canva: 'Canva', file: 'Design file' };
