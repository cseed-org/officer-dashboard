import type { WidgetBlockSize } from '../../lib/contracts';

export type BlockSize = WidgetBlockSize;
export interface WidgetDef {
  /** Stable ID: part of the persisted contract (user_prefs). */
  id: string;
  title: string;
  /** Sizes this widget supports, smallest first. */
  sizes: BlockSize[];
  /** Size used when the widget is added. */
  size: BlockSize;
  /** Widgets offered together as one onboarding choice (the old "Key figures" row). */
  group?: 'kpis';
}

/** Feature Panes can extend this registry. Keep defaults in step with server/internal/api/preferences.go. */
export const widgetRegistry: WidgetDef[] = [
  { id: 'links', title: 'Links', sizes: ['2x1', '1x2', '2x2'], size: '2x1' },
  { id: 'kpi-upcoming', title: 'Upcoming events', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-checkin', title: 'Avg check-in rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-reflection', title: 'Reflection rate', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-budget', title: 'Budget remaining', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'kpi-pending', title: 'Pending requests', sizes: ['1x1', '2x1'], size: '1x1', group: 'kpis' },
  { id: 'my-action', title: 'My actions', sizes: ['2x1', '2x2', '4x2'], size: '2x1' },
  { id: 'recent-events', title: 'Recent events', sizes: ['2x2', '4x2'], size: '2x2' },
  { id: 'budget', title: 'Budget vs actual', sizes: ['2x2', '4x2'], size: '2x2' },
  { id: 'activity', title: 'Activity', sizes: ['2x1', '1x2', '2x2', '4x2'], size: '2x1' },
  { id: 'web-analytics', title: 'Web Analytics', sizes: ['1x1', '2x2', '4x2'], size: '1x1' },
  { id: 'traffic-security', title: 'Traffic & security', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'performance', title: 'Performance', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'workers-d1', title: 'Workers/D1 usage', sizes: ['1x1', '2x2'], size: '1x1' },
  { id: 'member-signups', title: 'Member signups', sizes: ['1x1', '2x2'], size: '1x1' },
];

/** The old single "Key figures" widget, split into one stat tile per figure in V0.4. */
export const KPI_GROUP_ID = 'kpis';
export const kpiIds = widgetRegistry.filter((w) => w.group === 'kpis').map((w) => w.id);

/** Twenty blocks: a complete 4×5 rectangle, or ten full rows on phone. */
export const defaultWidgetLayout: { id: string; size: BlockSize }[] = [
  { id: 'links', size: '2x1' },
  { id: 'kpi-upcoming', size: '1x1' },
  { id: 'kpi-checkin', size: '1x1' },
  { id: 'kpi-reflection', size: '1x1' },
  { id: 'kpi-budget', size: '1x1' },
  { id: 'kpi-pending', size: '1x1' },
  { id: 'web-analytics', size: '1x1' },
  { id: 'recent-events', size: '2x2' },
  { id: 'budget', size: '2x2' },
  { id: 'my-action', size: '2x1' },
  { id: 'activity', size: '2x1' },
];

export const widgetDef = (id: string) => widgetRegistry.find((w) => w.id === id);

/** Onboarding offers the stat tiles as one "Key figures" choice; everything else one by one. */
export const onboardingChoices: { id: string; title: string; ids: string[] }[] = [
  { id: KPI_GROUP_ID, title: 'Key figures', ids: kpiIds },
  ...widgetRegistry.filter((w) => !w.group).map((w) => ({ id: w.id, title: w.title, ids: [w.id] })),
];
/** The onboarding choice a saved widget ID belongs to (V0.3 "kpis" included). */
export const choiceOf = (id: string) => onboardingChoices.find((c) => c.id === id || c.ids.includes(id))?.id;
/** A layout for the picked choices: default order and sizes first, then the rest at their default size. */
export function layoutForChoices(picked: Set<string>): { id: string; size: BlockSize }[] {
  const ids = onboardingChoices.filter((c) => picked.has(c.id)).flatMap((c) => c.ids);
  const first = defaultWidgetLayout.filter((p) => ids.includes(p.id));
  const rest = ids.filter((id) => !first.some((p) => p.id === id)).map((id) => ({ id, size: widgetDef(id)!.size }));
  return [...first, ...rest];
}
