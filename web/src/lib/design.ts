/**
 * Design requests: labels, due-date wording and the client copy of who may move what.
 * The server is the authority (DesignRequestDetail.permissions); the board menus use
 * designMoveTargets so a card offers the same moves before its detail is loaded.
 */
import type { Hue } from './colors';
import type { DesignRequestItem, DesignStage, DesignType } from './contracts';
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

/** Overdue or due within three days (Vancouver dates), unless already delivered. */
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
